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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimacherrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/operator-framework/catalogd/api/core/v1alpha1"
	catalogderrors "github.com/operator-framework/catalogd/internal/errors"
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
	Unpacker source.Unpacker
	Storage  storage.Instance
}

//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clustercatalogs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clustercatalogs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clustercatalogs/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=pods,verbs=create;update;patch;delete;get;list;watch
//+kubebuilder:rbac:groups=core,resources=pods/log,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,namespace=system,resources=secrets,verbs=get;

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.0/pkg/reconcile
func (r *ClusterCatalogReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// TODO: Where and when should we be logging errors and at which level?
	_ = log.FromContext(ctx).WithName("catalogd-controller")

	existingCatsrc := v1alpha1.ClusterCatalog{}
	if err := r.Client.Get(ctx, req.NamespacedName, &existingCatsrc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	reconciledCatsrc := existingCatsrc.DeepCopy()
	res, reconcileErr := r.reconcile(ctx, reconciledCatsrc)

	var unrecov *catalogderrors.Unrecoverable
	if errors.As(reconcileErr, &unrecov) {
		reconcileErr = nil
	}

	// Update the status subresource before updating the main object. This is
	// necessary because, in many cases, the main object update will remove the
	// finalizer, which will cause the core Kubernetes deletion logic to
	// complete. Therefore, we need to make the status update prior to the main
	// object update to ensure that the status update can be processed before
	// a potential deletion.
	if !equality.Semantic.DeepEqual(existingCatsrc.Status, reconciledCatsrc.Status) {
		if updateErr := r.Client.Status().Update(ctx, reconciledCatsrc); updateErr != nil {
			return res, apimacherrors.NewAggregate([]error{reconcileErr, updateErr})
		}
	}
	existingCatsrc.Status, reconciledCatsrc.Status = v1alpha1.ClusterCatalogStatus{}, v1alpha1.ClusterCatalogStatus{}
	if !equality.Semantic.DeepEqual(existingCatsrc, reconciledCatsrc) {
		if updateErr := r.Client.Update(ctx, reconciledCatsrc); updateErr != nil {
			return res, apimacherrors.NewAggregate([]error{reconcileErr, updateErr})
		}
	}
	return res, reconcileErr
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterCatalogReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ClusterCatalog{}).
		Owns(&corev1.Pod{}).
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
	if catalog.DeletionTimestamp.IsZero() && !controllerutil.ContainsFinalizer(catalog, fbcDeletionFinalizer) {
		controllerutil.AddFinalizer(catalog, fbcDeletionFinalizer)
		return ctrl.Result{}, nil
	}
	if !catalog.DeletionTimestamp.IsZero() {
		if err := r.Storage.Delete(catalog.Name); err != nil {
			return ctrl.Result{}, updateStatusStorageDeleteError(&catalog.Status, err)
		}
		if err := r.Unpacker.Cleanup(ctx, catalog); err != nil {
			return ctrl.Result{}, updateStatusStorageDeleteError(&catalog.Status, err)
		}
		controllerutil.RemoveFinalizer(catalog, fbcDeletionFinalizer)
		return ctrl.Result{}, nil
	}

	if !r.needsUnpacking(catalog) {
		return ctrl.Result{}, nil
	}

	unpackResult, err := r.Unpacker.Unpack(ctx, catalog)
	if err != nil {
		return ctrl.Result{}, updateStatusUnpackFailing(&catalog.Status, fmt.Errorf("source bundle content: %v", err))
	}

	switch unpackResult.State {
	case source.StatePending:
		updateStatusUnpackPending(&catalog.Status, unpackResult)
		return ctrl.Result{}, nil
	case source.StateUnpacking:
		updateStatusUnpacking(&catalog.Status, unpackResult)
		return ctrl.Result{}, nil
	case source.StateUnpacked:
		contentURL := ""
		// TODO: We should check to see if the unpacked result has the same content
		//   as the already unpacked content. If it does, we should skip this rest
		//   of the unpacking steps.
		err := r.Storage.Store(ctx, catalog.Name, unpackResult.FS)
		if err != nil {
			return ctrl.Result{}, updateStatusStorageError(&catalog.Status, fmt.Errorf("error storing fbc: %v", err))
		}
		contentURL = r.Storage.ContentURL(catalog.Name)

		var lastUnpacked metav1.Time

		if unpackResult != nil && unpackResult.ResolvedSource != nil && unpackResult.ResolvedSource.Image != nil {
			lastUnpacked = unpackResult.ResolvedSource.Image.LastUnpacked
		} else {
			lastUnpacked = metav1.Time{}
		}

		updateStatusUnpacked(&catalog.Status, unpackResult, contentURL, catalog.Generation, lastUnpacked)

		var requeueAfter time.Duration
		switch catalog.Spec.Source.Type {
		case v1alpha1.SourceTypeImage:
			if catalog.Spec.Source.Image != nil && catalog.Spec.Source.Image.PollInterval != nil {
				requeueAfter = wait.Jitter(catalog.Spec.Source.Image.PollInterval.Duration, requeueJitterMaxFactor)
			}
		}

		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	default:
		return ctrl.Result{}, updateStatusUnpackFailing(&catalog.Status, fmt.Errorf("unknown unpack state %q: %v", unpackResult.State, err))
	}
}

func updateStatusUnpackPending(status *v1alpha1.ClusterCatalogStatus, result *source.Result) {
	status.ResolvedSource = nil
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:    v1alpha1.TypeUnpacked,
		Status:  metav1.ConditionFalse,
		Reason:  v1alpha1.ReasonUnpackPending,
		Message: result.Message,
	})
}

func updateStatusUnpacking(status *v1alpha1.ClusterCatalogStatus, result *source.Result) {
	status.ResolvedSource = nil
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:    v1alpha1.TypeUnpacked,
		Status:  metav1.ConditionFalse,
		Reason:  v1alpha1.ReasonUnpacking,
		Message: result.Message,
	})
}

func updateStatusUnpacked(status *v1alpha1.ClusterCatalogStatus, result *source.Result, contentURL string, generation int64, lastUnpacked metav1.Time) {
	status.ResolvedSource = result.ResolvedSource
	status.ContentURL = contentURL
	status.ObservedGeneration = generation
	status.LastUnpacked = lastUnpacked
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:    v1alpha1.TypeUnpacked,
		Status:  metav1.ConditionTrue,
		Reason:  v1alpha1.ReasonUnpackSuccessful,
		Message: result.Message,
	})
}

func updateStatusUnpackFailing(status *v1alpha1.ClusterCatalogStatus, err error) error {
	status.ResolvedSource = nil
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:    v1alpha1.TypeUnpacked,
		Status:  metav1.ConditionFalse,
		Reason:  v1alpha1.ReasonUnpackFailed,
		Message: err.Error(),
	})
	return err
}

func updateStatusStorageError(status *v1alpha1.ClusterCatalogStatus, err error) error {
	status.ResolvedSource = nil
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:    v1alpha1.TypeUnpacked,
		Status:  metav1.ConditionFalse,
		Reason:  v1alpha1.ReasonStorageFailed,
		Message: fmt.Sprintf("failed to store bundle: %s", err.Error()),
	})
	return err
}

func updateStatusStorageDeleteError(status *v1alpha1.ClusterCatalogStatus, err error) error {
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:    v1alpha1.TypeDelete,
		Status:  metav1.ConditionFalse,
		Reason:  v1alpha1.ReasonStorageDeleteFailed,
		Message: fmt.Sprintf("failed to delete storage: %s", err.Error()),
	})
	return err
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
