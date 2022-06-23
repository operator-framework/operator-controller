/*
Copyright 2021.

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

package controllers

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"

	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"sigs.k8s.io/yaml"

	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	helmpredicate "github.com/operator-framework/rukpak/internal/helm-operator-plugins/predicate"
	plain "github.com/operator-framework/rukpak/internal/provisioner/plain/types"
	"github.com/operator-framework/rukpak/internal/storage"
	updater "github.com/operator-framework/rukpak/internal/updater/bundle-instance"
	"github.com/operator-framework/rukpak/internal/util"
)

const (
	maxGeneratedBundleLimit = 4
)

var (
	// ErrMaxGeneratedLimit is the error returned by the BundleInstance controller
	// when the configured maximum number of Bundles that match a label selector
	// has been reached.
	ErrMaxGeneratedLimit = errors.New("reached the maximum generated Bundle limit")
)

// BundleInstanceReconciler reconciles a BundleInstance object
type BundleInstanceReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Controller controller.Controller

	ActionClientGetter helmclient.ActionClientGetter
	BundleStorage      storage.Storage
	ReleaseNamespace   string

	dynamicWatchMutex sync.RWMutex
	dynamicWatchGVKs  map[schema.GroupVersionKind]struct{}
}

//+kubebuilder:rbac:groups=core.rukpak.io,resources=bundleinstances,verbs=list;watch
//+kubebuilder:rbac:groups=core.rukpak.io,resources=bundleinstances/status,verbs=update;patch
//+kubebuilder:rbac:groups=core.rukpak.io,resources=bundleinstances/finalizers,verbs=update
//+kubebuilder:rbac:groups=*,resources=*,verbs=*

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.9.2/pkg/reconcile
func (r *BundleInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	l.V(1).Info("starting reconciliation")
	defer l.V(1).Info("ending reconciliation")

	bi := &rukpakv1alpha1.BundleInstance{}
	if err := r.Get(ctx, req.NamespacedName, bi); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	defer func() {
		bi := bi.DeepCopy()
		bi.ObjectMeta.ManagedFields = nil
		bi.Spec.Template = nil
		if err := r.Status().Patch(ctx, bi, client.Apply, client.FieldOwner(plain.ProvisionerID)); err != nil {
			l.Error(err, "failed to patch status")
		}
	}()

	u := updater.NewBundleInstanceUpdater(r.Client)
	defer func() {
		if err := u.Apply(ctx, bi); err != nil {
			l.Error(err, "failed to update status")
		}
	}()

	bundle, allBundles, err := reconcileDesiredBundle(ctx, r.Client, bi)
	if err != nil {
		u.UpdateStatus(
			updater.EnsureCondition(metav1.Condition{
				Type:    rukpakv1alpha1.TypeHasValidBundle,
				Status:  metav1.ConditionUnknown,
				Reason:  rukpakv1alpha1.ReasonReconcileFailed,
				Message: err.Error(),
			}),
		)
		return ctrl.Result{}, err
	}
	if bundle.Status.Phase != rukpakv1alpha1.PhaseUnpacked {
		reason := rukpakv1alpha1.ReasonUnpackPending
		status := metav1.ConditionTrue
		message := fmt.Sprintf("Waiting for the %s Bundle to be unpacked", bundle.GetName())
		if bundle.Status.Phase == rukpakv1alpha1.PhaseFailing {
			reason = rukpakv1alpha1.ReasonUnpackFailed
			status = metav1.ConditionFalse
			message = fmt.Sprintf("Failed to unpack the %s Bundle", bundle.GetName())
		}
		u.UpdateStatus(
			updater.EnsureCondition(metav1.Condition{
				Type:    rukpakv1alpha1.TypeHasValidBundle,
				Status:  status,
				Reason:  reason,
				Message: message,
			}),
		)
		return ctrl.Result{}, nil
	}
	u.UpdateStatus(
		updater.EnsureCondition(metav1.Condition{
			Type:    rukpakv1alpha1.TypeHasValidBundle,
			Status:  metav1.ConditionTrue,
			Reason:  rukpakv1alpha1.ReasonUnpackSuccessful,
			Message: fmt.Sprintf("Successfully unpacked the %s Bundle", bundle.GetName()),
		}))

	desiredObjects, err := r.loadBundle(ctx, bundle, bi.GetName())
	if err != nil {
		u.UpdateStatus(
			updater.EnsureCondition(metav1.Condition{
				Type:    rukpakv1alpha1.TypeHasValidBundle,
				Status:  metav1.ConditionFalse,
				Reason:  rukpakv1alpha1.ReasonBundleLoadFailed,
				Message: err.Error(),
			}))
		return ctrl.Result{}, err
	}

	chrt := &chart.Chart{
		Metadata: &chart.Metadata{},
	}
	for _, obj := range desiredObjects {
		jsonData, err := yaml.Marshal(obj)
		if err != nil {
			u.UpdateStatus(
				updater.EnsureCondition(metav1.Condition{
					Type:    rukpakv1alpha1.TypeInvalidBundleContent,
					Status:  metav1.ConditionTrue,
					Reason:  rukpakv1alpha1.ReasonReadingContentFailed,
					Message: err.Error(),
				}))
			return ctrl.Result{}, err
		}
		hash := sha256.Sum256(jsonData)
		chrt.Templates = append(chrt.Templates, &chart.File{
			Name: fmt.Sprintf("object-%x.yaml", hash[0:8]),
			Data: jsonData,
		})
	}

	bi.SetNamespace(r.ReleaseNamespace)
	cl, err := r.ActionClientGetter.ActionClientFor(bi)
	bi.SetNamespace("")
	if err != nil {
		u.UpdateStatus(
			updater.EnsureCondition(metav1.Condition{
				Type:    rukpakv1alpha1.TypeInstalled,
				Status:  metav1.ConditionFalse,
				Reason:  rukpakv1alpha1.ReasonErrorGettingClient,
				Message: err.Error(),
			}))
		return ctrl.Result{}, err
	}

	rel, state, err := r.getReleaseState(cl, bi, chrt)
	if err != nil {
		u.UpdateStatus(
			updater.EnsureCondition(metav1.Condition{
				Type:    rukpakv1alpha1.TypeInstalled,
				Status:  metav1.ConditionFalse,
				Reason:  rukpakv1alpha1.ReasonErrorGettingReleaseState,
				Message: err.Error(),
			}))
		return ctrl.Result{}, err
	}

	switch state {
	case stateNeedsInstall:
		_, err = cl.Install(bi.Name, r.ReleaseNamespace, chrt, nil, func(install *action.Install) error {
			install.CreateNamespace = false
			return nil
		})
		if err != nil {
			if isResourceNotFoundErr(err) {
				err = errRequiredResourceNotFound{err}
			}
			u.UpdateStatus(
				updater.EnsureCondition(metav1.Condition{
					Type:    rukpakv1alpha1.TypeInstalled,
					Status:  metav1.ConditionFalse,
					Reason:  rukpakv1alpha1.ReasonInstallFailed,
					Message: err.Error(),
				}))
			return ctrl.Result{}, err
		}
	case stateNeedsUpgrade:
		_, err = cl.Upgrade(bi.Name, r.ReleaseNamespace, chrt, nil)
		if err != nil {
			if isResourceNotFoundErr(err) {
				err = errRequiredResourceNotFound{err}
			}
			u.UpdateStatus(
				updater.EnsureCondition(metav1.Condition{
					Type:    rukpakv1alpha1.TypeInstalled,
					Status:  metav1.ConditionFalse,
					Reason:  rukpakv1alpha1.ReasonUpgradeFailed,
					Message: err.Error(),
				}))
			return ctrl.Result{}, err
		}
	case stateUnchanged:
		if err := cl.Reconcile(rel); err != nil {
			if isResourceNotFoundErr(err) {
				err = errRequiredResourceNotFound{err}
			}
			u.UpdateStatus(
				updater.EnsureCondition(metav1.Condition{
					Type:    rukpakv1alpha1.TypeInstalled,
					Status:  metav1.ConditionFalse,
					Reason:  rukpakv1alpha1.ReasonReconcileFailed,
					Message: err.Error(),
				}))
			return ctrl.Result{}, err
		}
	default:
		return ctrl.Result{}, fmt.Errorf("unexpected release state %q", state)
	}

	for _, obj := range desiredObjects {
		uMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			u.UpdateStatus(
				updater.EnsureCondition(metav1.Condition{
					Type:    rukpakv1alpha1.TypeInstalled,
					Status:  metav1.ConditionFalse,
					Reason:  rukpakv1alpha1.ReasonCreateDynamicWatchFailed,
					Message: err.Error(),
				}))
			return ctrl.Result{}, err
		}

		unstructuredObj := &unstructured.Unstructured{Object: uMap}
		if err := func() error {
			r.dynamicWatchMutex.Lock()
			defer r.dynamicWatchMutex.Unlock()

			_, isWatched := r.dynamicWatchGVKs[unstructuredObj.GroupVersionKind()]
			if !isWatched {
				if err := r.Controller.Watch(
					&source.Kind{Type: unstructuredObj},
					&handler.EnqueueRequestForOwner{OwnerType: bi, IsController: true},
					helmpredicate.DependentPredicateFuncs()); err != nil {
					return err
				}
				r.dynamicWatchGVKs[unstructuredObj.GroupVersionKind()] = struct{}{}
			}
			return nil
		}(); err != nil {
			u.UpdateStatus(
				updater.EnsureCondition(metav1.Condition{
					Type:    rukpakv1alpha1.TypeInstalled,
					Status:  metav1.ConditionFalse,
					Reason:  rukpakv1alpha1.ReasonCreateDynamicWatchFailed,
					Message: err.Error(),
				}))
			return ctrl.Result{}, err
		}
	}
	u.UpdateStatus(
		updater.EnsureCondition(metav1.Condition{
			Type:    rukpakv1alpha1.TypeInstalled,
			Status:  metav1.ConditionTrue,
			Reason:  rukpakv1alpha1.ReasonInstallationSucceeded,
			Message: fmt.Sprintf("instantiated bundle %s successfully", bundle.GetName()),
		}),
		updater.EnsureInstalledName(bundle.GetName()),
	)

	if err := r.reconcileOldBundles(ctx, bundle, allBundles); err != nil {
		l.Error(err, "failed to delete old bundles")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// reconcileDesiredBundle is responsible for checking whether the desired
// Bundle resource that's specified in the BundleInstance parameter's
// spec.Template configuration is present on cluster, and if not, creates
// a new Bundle resource matching that desired specification.
func reconcileDesiredBundle(ctx context.Context, c client.Client, bi *rukpakv1alpha1.BundleInstance) (*rukpakv1alpha1.Bundle, *rukpakv1alpha1.BundleList, error) {
	// get the set of Bundle resources that already exist on cluster, and sort
	// by metadata.CreationTimestamp in the case there's multiple Bundles
	// that match the label selector.
	existingBundles, err := util.GetBundlesForBundleInstanceSelector(ctx, c, bi)
	if err != nil {
		return nil, nil, err
	}
	util.SortBundlesByCreation(existingBundles)

	// check whether the BI controller has already reached the maximum
	// generated Bundle limit to avoid hotlooping scenarios.
	if len(existingBundles.Items) > maxGeneratedBundleLimit {
		return nil, nil, ErrMaxGeneratedLimit
	}

	// check whether there's an existing Bundle that matches the desired Bundle template
	// specified in the BI resource, and if not, generate a new Bundle that matches the template.
	b := util.CheckExistingBundlesMatchesTemplate(existingBundles, bi.Spec.Template)
	if b == nil {
		controllerRef := metav1.NewControllerRef(bi, bi.GroupVersionKind())
		hash := util.GenerateTemplateHash(bi.Spec.Template)

		labels := bi.Spec.Template.Labels
		if len(labels) == 0 {
			labels = make(map[string]string)
		}
		labels[util.CoreOwnerKindKey] = rukpakv1alpha1.BundleInstanceKind
		labels[util.CoreOwnerNameKey] = bi.GetName()
		labels[util.CoreBundleTemplateHashKey] = hash

		b = &rukpakv1alpha1.Bundle{
			ObjectMeta: metav1.ObjectMeta{
				Name:            util.GenerateBundleName(bi.GetName(), hash),
				OwnerReferences: []metav1.OwnerReference{*controllerRef},
				Labels:          labels,
				Annotations:     bi.Spec.Template.Annotations,
			},
			Spec: bi.Spec.Template.Spec,
		}
		if err := c.Create(ctx, b); err != nil {
			return nil, nil, err
		}
	}
	return b, existingBundles, err
}

// reconcileOldBundles is responsible for garbage collecting any Bundles
// that no longer match the desired Bundle template.
func (r *BundleInstanceReconciler) reconcileOldBundles(ctx context.Context, currBundle *rukpakv1alpha1.Bundle, allBundles *rukpakv1alpha1.BundleList) error {
	var (
		errors []error
	)
	for _, b := range allBundles.Items {
		if b.GetName() == currBundle.GetName() {
			continue
		}
		if err := r.Delete(ctx, &b); err != nil {
			errors = append(errors, err)
			continue
		}
	}
	return utilerrors.NewAggregate(errors)
}

type releaseState string

const (
	stateNeedsInstall releaseState = "NeedsInstall"
	stateNeedsUpgrade releaseState = "NeedsUpgrade"
	stateUnchanged    releaseState = "Unchanged"
	stateError        releaseState = "Error"
)

func (r *BundleInstanceReconciler) getReleaseState(cl helmclient.ActionInterface, obj metav1.Object, chrt *chart.Chart) (*release.Release, releaseState, error) {
	currentRelease, err := cl.Get(obj.GetName())
	if err != nil && !errors.Is(err, driver.ErrReleaseNotFound) {
		return nil, stateError, err
	}
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return nil, stateNeedsInstall, nil
	}
	desiredRelease, err := cl.Upgrade(obj.GetName(), r.ReleaseNamespace, chrt, nil, func(upgrade *action.Upgrade) error {
		upgrade.DryRun = true
		return nil
	})
	if err != nil {
		return currentRelease, stateError, err
	}
	if desiredRelease.Manifest != currentRelease.Manifest ||
		currentRelease.Info.Status == release.StatusFailed ||
		currentRelease.Info.Status == release.StatusSuperseded {
		return currentRelease, stateNeedsUpgrade, nil
	}
	return currentRelease, stateUnchanged, nil
}

func (r *BundleInstanceReconciler) loadBundle(ctx context.Context, bundle *rukpakv1alpha1.Bundle, biName string) ([]client.Object, error) {
	bundleFS, err := r.BundleStorage.Load(ctx, bundle)
	if err != nil {
		return nil, fmt.Errorf("load bundle: %v", err)
	}

	objects, err := getObjects(bundleFS)
	if err != nil {
		return nil, fmt.Errorf("read bundle objects from bundle: %v", err)
	}

	objs := make([]client.Object, 0, len(objects))
	for _, obj := range objects {
		obj := obj
		obj.SetLabels(util.MergeMaps(obj.GetLabels(), map[string]string{
			util.CoreOwnerKindKey: rukpakv1alpha1.BundleInstanceKind,
			util.CoreOwnerNameKey: biName,
		}))
		objs = append(objs, obj)
	}
	return objs, nil
}

type errRequiredResourceNotFound struct {
	error
}

func (err errRequiredResourceNotFound) Error() string {
	return fmt.Sprintf("required resource not found: %v", err.error)
}

func isResourceNotFoundErr(err error) bool {
	var agg utilerrors.Aggregate
	if errors.As(err, &agg) {
		for _, err := range agg.Errors() {
			return isResourceNotFoundErr(err)
		}
	}

	nkme := &meta.NoKindMatchError{}
	if errors.As(err, &nkme) {
		return true
	}
	if apierrors.IsNotFound(err) {
		return true
	}

	// TODO: improve NoKindMatchError matching
	//   An error that is bubbled up from the k8s.io/cli-runtime library
	//   does not wrap meta.NoKindMatchError, so we need to fallback to
	//   the use of string comparisons for now.
	return strings.Contains(err.Error(), "no matches for kind")
}

// SetupWithManager sets up the controller with the Manager.
func (r *BundleInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	controller, err := ctrl.NewControllerManagedBy(mgr).
		For(&rukpakv1alpha1.BundleInstance{}, builder.WithPredicates(util.BundleInstanceProvisionerFilter(plain.ProvisionerID))).
		Watches(&source.Kind{Type: &rukpakv1alpha1.Bundle{}}, handler.EnqueueRequestsFromMapFunc(util.MapBundleToBundleInstanceHandler(mgr.GetClient(), mgr.GetLogger()))).
		Build(r)
	if err != nil {
		return err
	}
	r.Controller = controller
	r.dynamicWatchGVKs = map[schema.GroupVersionKind]struct{}{}
	return nil
}
