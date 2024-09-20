/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package core

import (
	"context" // #nosec
	"errors"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crfinalizer "sigs.k8s.io/controller-runtime/pkg/finalizer"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/operator-framework/catalogd/internal/source"
	"github.com/operator-framework/catalogd/internal/storage"
)

const (
	fbcDeletionFinalizer = "olm.operatorframework.io/delete-server-cache"
	// CatalogSources are polled if PollInterval is mentioned, in intervals of wait.Jitter(pollDuration, maxFactor)
	// wait.Jitter returns a time.Duration between pollDuration and pollDuration + maxFactor * pollDuration.
	requeueJitterMaxFactor = 0.01
)

// ClusterCatalogReconciler reconciles a Catalog object
type ClusterCatalogReconciler struct {
	client.Client
	Unpacker   source.Unpacker
	Storage    storage.Instance
	Finalizers crfinalizer.Finalizers
}

//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clustercatalogs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clustercatalogs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clustercatalogs/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.0/pkg/reconcile
func (r *ClusterCatalogReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("catalogd-controller")
	ctx = log.IntoContext(ctx, l)

	l.V(1).Info("reconcile starting")
	defer l.V(1).Info("reconcile ending")

	existingCatsrc := v1alpha1.ClusterCatalog{}
	if err := r.Client.Get(ctx, req.NamespacedName, &existingCatsrc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	reconciledCatsrc := existingCatsrc.DeepCopy()
	res, reconcileErr := r.reconcile(ctx, reconciledCatsrc)

	// Do checks before any Update()s, as Update() may modify the resource structure!
	updateStatus := !equality.Semantic.DeepEqual(existingCatsrc.Status, reconciledCatsrc.Status)
	updateFinalizers := !equality.Semantic.DeepEqual(existingCatsrc.Finalizers, reconciledCatsrc.Finalizers)
	unexpectedFieldsChanged := checkForUnexpectedFieldChange(existingCatsrc, *reconciledCatsrc)

	if unexpectedFieldsChanged {
		panic("spec or metadata changed by reconciler")
	}

	// Save the finalizers off to the side. If we update the status, the reconciledCatsrc will be updated
	// to contain the new state of the ClusterCatalog, which contains the status update, but (critically)
	// does not contain the finalizers. After the status update, we need to re-add the finalizers to the
	// reconciledCatsrc before updating the object.
	finalizers := reconciledCatsrc.Finalizers

	if updateStatus {
		if err := r.Client.Status().Update(ctx, reconciledCatsrc); err != nil {
			reconcileErr = errors.Join(reconcileErr, fmt.Errorf("error updating status: %v", err))
		}
	}

	reconciledCatsrc.Finalizers = finalizers

	if updateFinalizers {
		if err := r.Client.Update(ctx, reconciledCatsrc); err != nil {
			reconcileErr = errors.Join(reconcileErr, fmt.Errorf("error updating finalizers: %v", err))
		}
	}

	return res, reconcileErr
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterCatalogReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ClusterCatalog{}).
		Complete(r)
}

// Note: This function always returns ctrl.Result{}. The linter
// fusses about this as we could instead just return error. This was
// discussed in https://github.com/operator-framework/rukpak/pull/635#discussion_r1229859464
// and the consensus was that it is better to keep the ctrl.Result return
// type so that if we do end up needing to return something else we don't forget
// to add the ctrl.Result type back as a return value. Adding a comment to ignore
// linting from the linter that was fussing about this.
// nolint:unparam
func (r *ClusterCatalogReconciler) reconcile(ctx context.Context, catalog *v1alpha1.ClusterCatalog) (ctrl.Result, error) {
	finalizeResult, err := r.Finalizers.Finalize(ctx, catalog)
	if err != nil {
		return ctrl.Result{}, err
	}
	if finalizeResult.Updated || finalizeResult.StatusUpdated {
		// On create: make sure the finalizer is applied before we do anything
		// On delete: make sure we do nothing after the finalizer is removed
		return ctrl.Result{}, nil
	}

	if !r.needsUnpacking(catalog) {
		return ctrl.Result{}, nil
	}

	unpackResult, err := r.Unpacker.Unpack(ctx, catalog)
	if err != nil {
		unpackErr := fmt.Errorf("source bundle content: %w", err)
		updateStatusProgressing(catalog, unpackErr)
		return ctrl.Result{}, unpackErr
	}

	switch unpackResult.State {
	case source.StateUnpacked:
		contentURL := ""
		// TODO: We should check to see if the unpacked result has the same content
		//   as the already unpacked content. If it does, we should skip this rest
		//   of the unpacking steps.
		err := r.Storage.Store(ctx, catalog.Name, unpackResult.FS)
		if err != nil {
			storageErr := fmt.Errorf("error storing fbc: %v", err)
			updateStatusProgressing(catalog, storageErr)
			return ctrl.Result{}, storageErr
		}
		contentURL = r.Storage.ContentURL(catalog.Name)

		var lastUnpacked metav1.Time

		if unpackResult != nil && unpackResult.ResolvedSource != nil && unpackResult.ResolvedSource.Image != nil {
			lastUnpacked = unpackResult.ResolvedSource.Image.LastUnpacked
		}

		updateStatusProgressing(catalog, nil)
		updateStatusServing(&catalog.Status, unpackResult, contentURL, catalog.Generation, lastUnpacked)

		var requeueAfter time.Duration
		switch catalog.Spec.Source.Type {
		case v1alpha1.SourceTypeImage:
			if catalog.Spec.Source.Image != nil && catalog.Spec.Source.Image.PollInterval != nil {
				requeueAfter = wait.Jitter(catalog.Spec.Source.Image.PollInterval.Duration, requeueJitterMaxFactor)
			}
		}

		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	default:
		panic(fmt.Sprintf("unknown unpack state %q", unpackResult.State))
	}
}

func updateStatusProgressing(catalog *v1alpha1.ClusterCatalog, err error) {
	progressingCond := metav1.Condition{
		Type:    v1alpha1.TypeProgressing,
		Status:  metav1.ConditionFalse,
		Reason:  v1alpha1.ReasonSucceeded,
		Message: "Successfully unpacked and stored content from resolved source",
	}

	if err != nil {
		progressingCond.Status = metav1.ConditionTrue
		progressingCond.Reason = v1alpha1.ReasonRetrying
		progressingCond.Message = err.Error()
	}

	if errors.Is(err, reconcile.TerminalError(nil)) {
		progressingCond.Status = metav1.ConditionFalse
		progressingCond.Reason = v1alpha1.ReasonTerminal
	}

	meta.SetStatusCondition(&catalog.Status.Conditions, progressingCond)
}
func updateStatusServing(status *v1alpha1.ClusterCatalogStatus, result *source.Result, contentURL string, generation int64, unpackedAt metav1.Time) {
	status.ResolvedSource = result.ResolvedSource
	status.ContentURL = contentURL
	status.ObservedGeneration = generation
	status.LastUnpacked = unpackedAt
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:    v1alpha1.TypeServing,
		Status:  metav1.ConditionTrue,
		Reason:  v1alpha1.ReasonAvailable,
		Message: "Serving desired content from resolved source",
	})
}

func updateStatusNotServing(status *v1alpha1.ClusterCatalogStatus) {
	status.ResolvedSource = nil
	status.ContentURL = ""
	status.ObservedGeneration = 0
	status.LastUnpacked = metav1.Time{}
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:   v1alpha1.TypeServing,
		Status: metav1.ConditionFalse,
		Reason: v1alpha1.ReasonUnavailable,
	})
}

func (r *ClusterCatalogReconciler) needsUnpacking(catalog *v1alpha1.ClusterCatalog) bool {
	// if ResolvedSource is nil, it indicates that this is the first time we're
	// unpacking this catalog.
	if catalog.Status.ResolvedSource == nil {
		return true
	}
	if !r.Storage.ContentExists(catalog.Name) {
		return true
	}
	// if there is no spec.Source.Image, don't unpack again
	if catalog.Spec.Source.Image == nil {
		return false
	}
	// if the spec.Source.Image.Ref was changed, unpack the new ref
	// NOTE: we must compare image reference WITHOUT sha hash here
	// otherwise we will always be unpacking image even when poll interval not lapsed
	if catalog.Spec.Source.Image.Ref != catalog.Status.ResolvedSource.Image.Ref {
		return true
	}
	// if pollInterval is nil, don't unpack again
	if catalog.Spec.Source.Image.PollInterval == nil {
		return false
	}
	// if it's not time to poll yet, and the CR wasn't changed don't unpack again
	nextPoll := catalog.Status.ResolvedSource.Image.LastPollAttempt.Add(catalog.Spec.Source.Image.PollInterval.Duration)
	if nextPoll.After(time.Now()) && catalog.Generation == catalog.Status.ObservedGeneration {
		return false
	}
	// time to unpack
	return true
}

// Compare resources - ignoring status & metadata.finalizers
func checkForUnexpectedFieldChange(a, b v1alpha1.ClusterCatalog) bool {
	a.Status, b.Status = v1alpha1.ClusterCatalogStatus{}, v1alpha1.ClusterCatalogStatus{}
	a.Finalizers, b.Finalizers = []string{}, []string{}
	return !equality.Semantic.DeepEqual(a, b)
}

type finalizerFunc func(ctx context.Context, obj client.Object) (crfinalizer.Result, error)

func (f finalizerFunc) Finalize(ctx context.Context, obj client.Object) (crfinalizer.Result, error) {
	return f(ctx, obj)
}

func NewFinalizers(localStorage storage.Instance, unpacker source.Unpacker) (crfinalizer.Finalizers, error) {
	f := crfinalizer.NewFinalizers()
	err := f.Register(fbcDeletionFinalizer, finalizerFunc(func(ctx context.Context, obj client.Object) (crfinalizer.Result, error) {
		catalog, ok := obj.(*v1alpha1.ClusterCatalog)
		if !ok {
			panic("could not convert object to clusterCatalog")
		}
		if err := localStorage.Delete(catalog.Name); err != nil {
			updateStatusProgressing(catalog, err)
			return crfinalizer.Result{StatusUpdated: true}, err
		}
		updateStatusNotServing(&catalog.Status)
		if err := unpacker.Cleanup(ctx, catalog); err != nil {
			updateStatusProgressing(catalog, err)
			return crfinalizer.Result{StatusUpdated: true}, err
		}
		return crfinalizer.Result{StatusUpdated: true}, nil
	}))
	if err != nil {
		return f, err
	}
	return f, nil
}
