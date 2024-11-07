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
	"slices"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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
	Unpacker source.Unpacker
	Storage  storage.Instance

	finalizers crfinalizer.Finalizers

	// TODO: The below storedCatalogs fields are used for a quick a hack that helps
	//    us correctly populate a ClusterCatalog's status. The fact that we need
	//    these is indicative of a larger problem with the design of one or both
	//    of the Unpacker and Storage interfaces. We should fix this.
	storedCatalogsMu sync.RWMutex
	storedCatalogs   map[string]storedCatalogData
}

type storedCatalogData struct {
	observedGeneration int64
	unpackResult       source.Result
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

	l.Info("reconcile starting")
	defer l.Info("reconcile ending")

	existingCatsrc := v1alpha1.ClusterCatalog{}
	if err := r.Client.Get(ctx, req.NamespacedName, &existingCatsrc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	reconciledCatsrc := existingCatsrc.DeepCopy()
	res, reconcileErr := r.reconcile(ctx, reconciledCatsrc)

	// If we encounter an error, we should delete the stored catalog metadata
	// which represents the state of a successfully unpacked catalog. Deleting
	// this state ensures that we will continue retrying the unpacking process
	// until it succeeds.
	if reconcileErr != nil {
		r.deleteStoredCatalog(reconciledCatsrc.Name)
	}

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
	r.storedCatalogsMu.Lock()
	defer r.storedCatalogsMu.Unlock()
	r.storedCatalogs = make(map[string]storedCatalogData)

	if err := r.setupFinalizers(); err != nil {
		return fmt.Errorf("failed to setup finalizers: %v", err)
	}

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
	l := log.FromContext(ctx)
	// Check if the catalog availability is set to disabled, if true then
	// unset base URL, delete it from the cache and set appropriate status
	if catalog.Spec.AvailabilityMode == v1alpha1.AvailabilityModeUnavailable {
		// Delete the catalog from local cache
		err := r.deleteCatalogCache(ctx, catalog)
		if err != nil {
			return ctrl.Result{}, err
		}

		// Set status.conditions[type=Progressing] to False as we are done with
		// all that needs to be done with the catalog
		updateStatusProgressingUserSpecifiedUnavailable(&catalog.Status, catalog.GetGeneration())

		// Remove the fbcDeletionFinalizer as we do not want a finalizer attached to the catalog
		// when it is disabled. Because the finalizer serves no purpose now.
		controllerutil.RemoveFinalizer(catalog, fbcDeletionFinalizer)

		return ctrl.Result{}, nil
	}

	finalizeResult, err := r.finalizers.Finalize(ctx, catalog)
	if err != nil {
		return ctrl.Result{}, err
	}
	if finalizeResult.Updated || finalizeResult.StatusUpdated {
		// On create: make sure the finalizer is applied before we do anything
		// On delete: make sure we do nothing after the finalizer is removed
		return ctrl.Result{}, nil
	}

	// TODO: The below algorithm to get the current state based on an in-memory
	//    storedCatalogs map is a hack that helps us keep the ClusterCatalog's
	//    status up-to-date. The fact that we need this setup is indicative of
	//    a larger problem with the design of one or both of the Unpacker and
	//    Storage interfaces and/or their interactions. We should fix this.
	expectedStatus, storedCatalog, hasStoredCatalog := r.getCurrentState(catalog)

	// If any of the following are true, we need to unpack the catalog:
	//   - we don't have a stored catalog in the map
	//   - we have a stored catalog, but the content doesn't exist on disk
	//   - we have a stored catalog, the content exists, but the expected status differs from the actual status
	//   - we have a stored catalog, the content exists, the status looks correct, but the catalog generation is different from the observed generation in the stored catalog
	//   - we have a stored catalog, the content exists, the status looks correct and reflects the catalog generation, but it is time to poll again
	needsUnpack := false
	switch {
	case !hasStoredCatalog:
		l.Info("unpack required: no cached catalog metadata found for this catalog")
		needsUnpack = true
	case !r.Storage.ContentExists(catalog.Name):
		l.Info("unpack required: no stored content found for this catalog")
		needsUnpack = true
	case !equality.Semantic.DeepEqual(catalog.Status, *expectedStatus):
		l.Info("unpack required: current ClusterCatalog status differs from expected status")
		needsUnpack = true
	case catalog.Generation != storedCatalog.observedGeneration:
		l.Info("unpack required: catalog generation differs from observed generation")
		needsUnpack = true
	case r.needsPoll(storedCatalog.unpackResult.LastSuccessfulPollAttempt.Time, catalog):
		l.Info("unpack required: poll duration has elapsed")
		needsUnpack = true
	}

	if !needsUnpack {
		// No need to update the status because we've already checked
		// that it is set correctly. Otherwise, we'd be unpacking again.
		return nextPollResult(storedCatalog.unpackResult.LastSuccessfulPollAttempt.Time, catalog), nil
	}

	unpackResult, err := r.Unpacker.Unpack(ctx, catalog)
	if err != nil {
		unpackErr := fmt.Errorf("source catalog content: %w", err)
		updateStatusProgressing(&catalog.Status, catalog.GetGeneration(), unpackErr)
		return ctrl.Result{}, unpackErr
	}

	switch unpackResult.State {
	case source.StateUnpacked:
		// TODO: We should check to see if the unpacked result has the same content
		//   as the already unpacked content. If it does, we should skip this rest
		//   of the unpacking steps.
		err := r.Storage.Store(ctx, catalog.Name, unpackResult.FS)
		if err != nil {
			storageErr := fmt.Errorf("error storing fbc: %v", err)
			updateStatusProgressing(&catalog.Status, catalog.GetGeneration(), storageErr)
			return ctrl.Result{}, storageErr
		}
		baseURL := r.Storage.BaseURL(catalog.Name)

		updateStatusProgressing(&catalog.Status, catalog.GetGeneration(), nil)
		updateStatusServing(&catalog.Status, *unpackResult, baseURL, catalog.GetGeneration())
	default:
		panic(fmt.Sprintf("unknown unpack state %q", unpackResult.State))
	}

	r.storedCatalogsMu.Lock()
	r.storedCatalogs[catalog.Name] = storedCatalogData{
		unpackResult:       *unpackResult,
		observedGeneration: catalog.GetGeneration(),
	}
	r.storedCatalogsMu.Unlock()
	return nextPollResult(unpackResult.LastSuccessfulPollAttempt.Time, catalog), nil
}

func (r *ClusterCatalogReconciler) getCurrentState(catalog *v1alpha1.ClusterCatalog) (*v1alpha1.ClusterCatalogStatus, storedCatalogData, bool) {
	r.storedCatalogsMu.RLock()
	storedCatalog, hasStoredCatalog := r.storedCatalogs[catalog.Name]
	r.storedCatalogsMu.RUnlock()

	expectedStatus := catalog.Status.DeepCopy()

	// Set expected status based on what we see in the stored catalog
	clearUnknownConditions(expectedStatus)
	if hasStoredCatalog && r.Storage.ContentExists(catalog.Name) {
		updateStatusServing(expectedStatus, storedCatalog.unpackResult, r.Storage.BaseURL(catalog.Name), storedCatalog.observedGeneration)
		updateStatusProgressing(expectedStatus, storedCatalog.observedGeneration, nil)
	}

	return expectedStatus, storedCatalog, hasStoredCatalog
}

func nextPollResult(lastSuccessfulPoll time.Time, catalog *v1alpha1.ClusterCatalog) ctrl.Result {
	var requeueAfter time.Duration
	switch catalog.Spec.Source.Type {
	case v1alpha1.SourceTypeImage:
		if catalog.Spec.Source.Image != nil && catalog.Spec.Source.Image.PollIntervalMinutes != nil {
			pollDuration := time.Duration(*catalog.Spec.Source.Image.PollIntervalMinutes) * time.Minute
			jitteredDuration := wait.Jitter(pollDuration, requeueJitterMaxFactor)
			requeueAfter = time.Until(lastSuccessfulPoll.Add(jitteredDuration))
		}
	}
	return ctrl.Result{RequeueAfter: requeueAfter}
}

func clearUnknownConditions(status *v1alpha1.ClusterCatalogStatus) {
	knownTypes := sets.New[string](
		v1alpha1.TypeServing,
		v1alpha1.TypeProgressing,
	)
	status.Conditions = slices.DeleteFunc(status.Conditions, func(cond metav1.Condition) bool {
		return !knownTypes.Has(cond.Type)
	})
}

func updateStatusProgressing(status *v1alpha1.ClusterCatalogStatus, generation int64, err error) {
	progressingCond := metav1.Condition{
		Type:               v1alpha1.TypeProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             v1alpha1.ReasonSucceeded,
		Message:            "Successfully unpacked and stored content from resolved source",
		ObservedGeneration: generation,
	}

	if err != nil {
		progressingCond.Status = metav1.ConditionTrue
		progressingCond.Reason = v1alpha1.ReasonRetrying
		progressingCond.Message = err.Error()
	}

	if errors.Is(err, reconcile.TerminalError(nil)) {
		progressingCond.Status = metav1.ConditionFalse
		progressingCond.Reason = v1alpha1.ReasonBlocked
	}

	meta.SetStatusCondition(&status.Conditions, progressingCond)
}

func updateStatusServing(status *v1alpha1.ClusterCatalogStatus, result source.Result, baseURL string, generation int64) {
	status.ResolvedSource = result.ResolvedSource
	if status.URLs == nil {
		status.URLs = &v1alpha1.ClusterCatalogURLs{}
	}
	status.URLs.Base = baseURL
	status.LastUnpacked = ptr.To(metav1.NewTime(result.UnpackTime))
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               v1alpha1.TypeServing,
		Status:             metav1.ConditionTrue,
		Reason:             v1alpha1.ReasonAvailable,
		Message:            "Serving desired content from resolved source",
		ObservedGeneration: generation,
	})
}

func updateStatusProgressingUserSpecifiedUnavailable(status *v1alpha1.ClusterCatalogStatus, generation int64) {
	// Set Progressing condition to True with reason Succeeded
	// since we have successfully progressed to the unavailable
	// availability mode and are ready to progress to any future
	// desired state.
	progressingCond := metav1.Condition{
		Type:               v1alpha1.TypeProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             v1alpha1.ReasonSucceeded,
		Message:            "Catalog availability mode is set to Unavailable",
		ObservedGeneration: generation,
	}

	// Set Serving condition to False with reason UserSpecifiedUnavailable
	// so that users of this condition are aware that this catalog is
	// intentionally not being served
	servingCond := metav1.Condition{
		Type:               v1alpha1.TypeServing,
		Status:             metav1.ConditionFalse,
		Reason:             v1alpha1.ReasonUserSpecifiedUnavailable,
		Message:            "Catalog availability mode is set to Unavailable",
		ObservedGeneration: generation,
	}

	meta.SetStatusCondition(&status.Conditions, progressingCond)
	meta.SetStatusCondition(&status.Conditions, servingCond)
}

func updateStatusNotServing(status *v1alpha1.ClusterCatalogStatus, generation int64) {
	status.ResolvedSource = nil
	status.URLs = nil
	status.LastUnpacked = nil
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               v1alpha1.TypeServing,
		Status:             metav1.ConditionFalse,
		Reason:             v1alpha1.ReasonUnavailable,
		ObservedGeneration: generation,
	})
}

func (r *ClusterCatalogReconciler) needsPoll(lastSuccessfulPoll time.Time, catalog *v1alpha1.ClusterCatalog) bool {
	// If polling is disabled, we don't need to poll.
	if catalog.Spec.Source.Image.PollIntervalMinutes == nil {
		return false
	}

	// Only poll if the next poll time is in the past.
	nextPoll := lastSuccessfulPoll.Add(time.Duration(*catalog.Spec.Source.Image.PollIntervalMinutes) * time.Minute)
	return nextPoll.Before(time.Now())
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

func (r *ClusterCatalogReconciler) setupFinalizers() error {
	f := crfinalizer.NewFinalizers()
	err := f.Register(fbcDeletionFinalizer, finalizerFunc(func(ctx context.Context, obj client.Object) (crfinalizer.Result, error) {
		catalog, ok := obj.(*v1alpha1.ClusterCatalog)
		if !ok {
			panic("could not convert object to clusterCatalog")
		}
		err := r.deleteCatalogCache(ctx, catalog)
		return crfinalizer.Result{StatusUpdated: true}, err
	}))
	if err != nil {
		return err
	}
	r.finalizers = f
	return nil
}

func (r *ClusterCatalogReconciler) deleteStoredCatalog(catalogName string) {
	r.storedCatalogsMu.Lock()
	defer r.storedCatalogsMu.Unlock()
	delete(r.storedCatalogs, catalogName)
}

func (r *ClusterCatalogReconciler) deleteCatalogCache(ctx context.Context, catalog *v1alpha1.ClusterCatalog) error {
	if err := r.Storage.Delete(catalog.Name); err != nil {
		updateStatusProgressing(&catalog.Status, catalog.GetGeneration(), err)
		return err
	}
	updateStatusNotServing(&catalog.Status, catalog.GetGeneration())
	if err := r.Unpacker.Cleanup(ctx, catalog); err != nil {
		updateStatusProgressing(&catalog.Status, catalog.GetGeneration(), err)
		return err
	}
	r.deleteStoredCatalog(catalog.Name)
	return nil
}
