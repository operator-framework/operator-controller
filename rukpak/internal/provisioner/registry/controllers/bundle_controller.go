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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	apimacherrors "k8s.io/apimachinery/pkg/util/errors"
	apimachyaml "k8s.io/apimachinery/pkg/util/yaml"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/finalizer"
	"sigs.k8s.io/controller-runtime/pkg/log"
	crsource "sigs.k8s.io/controller-runtime/pkg/source"

	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"github.com/operator-framework/rukpak/internal/convert"
	registry "github.com/operator-framework/rukpak/internal/provisioner/registry/types"
	"github.com/operator-framework/rukpak/internal/source"
	"github.com/operator-framework/rukpak/internal/storage"
	updater "github.com/operator-framework/rukpak/internal/updater/bundle"
	"github.com/operator-framework/rukpak/internal/util"
)

// BundleReconciler reconciles a Bundle object
type BundleReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Storage    storage.Storage
	Finalizers finalizer.Finalizers
	Unpacker   source.Unpacker
}

//+kubebuilder:rbac:groups=core.rukpak.io,resources=bundles,verbs=list;watch;update;patch
//+kubebuilder:rbac:groups=core.rukpak.io,resources=bundles/status,verbs=update;patch
//+kubebuilder:rbac:groups=core.rukpak.io,resources=bundles/finalizers,verbs=update
//+kubebuilder:rbac:verbs=get,urls=/bundles/*
//+kubebuilder:rbac:groups=core,resources=pods,verbs=list;watch;create;delete
//+kubebuilder:rbac:groups=core,resources=pods/log,verbs=get
//+kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
//+kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.9.2/pkg/reconcile
func (r *BundleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	l.V(1).Info("starting reconciliation")
	defer l.V(1).Info("ending reconciliation")
	bundle := &rukpakv1alpha1.Bundle{}
	if err := r.Get(ctx, req.NamespacedName, bundle); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	u := updater.NewBundleUpdater(r.Client)
	defer func() {
		if err := u.Apply(ctx, bundle); err != nil {
			l.Error(err, "failed to update status")
		}
	}()
	u.UpdateStatus(updater.EnsureObservedGeneration(bundle.Generation))

	finalizerResult, err := r.Finalizers.Finalize(ctx, bundle)
	if err != nil {
		u.UpdateStatus(
			updater.EnsureResolvedSource(nil),
			updater.EnsureContentURL(""),
			updater.SetPhase(rukpakv1alpha1.PhaseFailing),
			updater.EnsureCondition(metav1.Condition{
				Type:    rukpakv1alpha1.TypeUnpacked,
				Status:  metav1.ConditionUnknown,
				Reason:  rukpakv1alpha1.ReasonProcessingFinalizerFailed,
				Message: err.Error(),
			}),
		)
		return ctrl.Result{}, err
	}
	var (
		finalizerUpdateErrs []error
	)
	// Update the status subresource before updating the main object. This is
	// necessary because, in many cases, the main object update will remove the
	// finalizer, which will cause the core Kubernetes deletion logic to
	// complete. Therefore, we need to make the status update prior to the main
	// object update to ensure that the status update can be processed before
	// a potential deletion.
	if finalizerResult.StatusUpdated {
		finalizerUpdateErrs = append(finalizerUpdateErrs, r.Status().Update(ctx, bundle))
	}
	if finalizerResult.Updated {
		finalizerUpdateErrs = append(finalizerUpdateErrs, r.Update(ctx, bundle))
	}
	if finalizerResult.Updated || finalizerResult.StatusUpdated || !bundle.GetDeletionTimestamp().IsZero() {
		err := apimacherrors.NewAggregate(finalizerUpdateErrs)
		if err != nil {
			u.UpdateStatus(
				updater.EnsureResolvedSource(nil),
				updater.EnsureContentURL(""),
				updater.SetPhase(rukpakv1alpha1.PhaseFailing),
				updater.EnsureCondition(metav1.Condition{
					Type:    rukpakv1alpha1.TypeUnpacked,
					Status:  metav1.ConditionUnknown,
					Reason:  rukpakv1alpha1.ReasonProcessingFinalizerFailed,
					Message: err.Error(),
				}),
			)
		}
		return ctrl.Result{}, err
	}

	unpackResult, err := r.Unpacker.Unpack(ctx, bundle)
	if err != nil {
		return ctrl.Result{}, updateStatusUnpackFailing(&u, fmt.Errorf("source bundle content: %v", err))
	}
	switch unpackResult.State {
	case source.StatePending:
		updateStatusUnpackPending(&u, unpackResult)
		return ctrl.Result{}, nil
	case source.StateUnpacking:
		updateStatusUnpacking(&u, unpackResult)
		return ctrl.Result{}, nil
	case source.StateUnpacked:
		plainFS, err := convert.RegistryV1ToPlain(unpackResult.Bundle)
		if err != nil {
			return ctrl.Result{}, updateStatusUnpackFailing(&u, fmt.Errorf("convert registry+v1 bundle to plain+v0 bundle: %v", err))
		}

		objects, err := getObjects(plainFS)
		if err != nil {
			return ctrl.Result{}, updateStatusUnpackFailing(&u, fmt.Errorf("get objects from bundle manifests: %v", err))
		}
		if len(objects) == 0 {
			return ctrl.Result{}, updateStatusUnpackFailing(&u, errors.New("invalid bundle: found zero objects: "+
				"plain+v0 bundles are required to contain at least one object"))
		}

		if err := r.Storage.Store(ctx, bundle, plainFS); err != nil {
			return ctrl.Result{}, updateStatusUnpackFailing(&u, fmt.Errorf("persist bundle objects: %v", err))
		}

		contentURL, err := r.Storage.URLFor(ctx, bundle)
		if err != nil {
			return ctrl.Result{}, updateStatusUnpackFailing(&u, fmt.Errorf("get content URL: %v", err))
		}

		updateStatusUnpacked(&u, unpackResult, contentURL)
		return ctrl.Result{}, nil
	default:
		return ctrl.Result{}, updateStatusUnpackFailing(&u, fmt.Errorf("unknown unpack state %q: %v", unpackResult.State, err))
	}
}

func updateStatusUnpackPending(u *updater.Updater, result *source.Result) {
	u.UpdateStatus(
		updater.EnsureResolvedSource(nil),
		updater.EnsureContentURL(""),
		updater.SetPhase(rukpakv1alpha1.PhasePending),
		updater.EnsureCondition(metav1.Condition{
			Type:    rukpakv1alpha1.TypeUnpacked,
			Status:  metav1.ConditionFalse,
			Reason:  rukpakv1alpha1.ReasonUnpackPending,
			Message: result.Message,
		}),
	)
}

func updateStatusUnpacking(u *updater.Updater, result *source.Result) {
	u.UpdateStatus(
		updater.EnsureResolvedSource(nil),
		updater.EnsureContentURL(""),
		updater.SetPhase(rukpakv1alpha1.PhaseUnpacking),
		updater.EnsureCondition(metav1.Condition{
			Type:    rukpakv1alpha1.TypeUnpacked,
			Status:  metav1.ConditionFalse,
			Reason:  rukpakv1alpha1.ReasonUnpacking,
			Message: result.Message,
		}),
	)
}

func updateStatusUnpacked(u *updater.Updater, result *source.Result, contentURL string) {
	u.UpdateStatus(
		updater.EnsureResolvedSource(result.ResolvedSource),
		updater.EnsureContentURL(contentURL),
		updater.SetPhase(rukpakv1alpha1.PhaseUnpacked),
		updater.EnsureCondition(metav1.Condition{
			Type:    rukpakv1alpha1.TypeUnpacked,
			Status:  metav1.ConditionTrue,
			Reason:  rukpakv1alpha1.ReasonUnpackSuccessful,
			Message: result.Message,
		}),
	)
}

func updateStatusUnpackFailing(u *updater.Updater, err error) error {
	u.UpdateStatus(
		updater.EnsureResolvedSource(nil),
		updater.EnsureContentURL(""),
		updater.SetPhase(rukpakv1alpha1.PhaseFailing),
		updater.EnsureCondition(metav1.Condition{
			Type:    rukpakv1alpha1.TypeUnpacked,
			Status:  metav1.ConditionFalse,
			Reason:  rukpakv1alpha1.ReasonUnpackFailed,
			Message: err.Error(),
		}),
	)
	return err
}

func getObjects(bundleFS fs.FS) ([]client.Object, error) {
	var objects []client.Object
	const manifestsDir = "manifests"

	entries, err := fs.ReadDir(bundleFS, manifestsDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			return nil, fmt.Errorf("subdirectories are not allowed within the %q directory of the bundle image filesystem: found %q", manifestsDir, filepath.Join(manifestsDir, e.Name()))
		}
		fileData, err := fs.ReadFile(bundleFS, filepath.Join(manifestsDir, e.Name()))
		if err != nil {
			return nil, err
		}

		dec := apimachyaml.NewYAMLOrJSONDecoder(bytes.NewReader(fileData), 1024)
		for {
			obj := unstructured.Unstructured{}
			err := dec.Decode(&obj)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("read %q: %v", e.Name(), err)
			}
			objects = append(objects, &obj)
		}
	}
	return objects, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *BundleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	l := mgr.GetLogger().WithName("controller.bundle")
	return ctrl.NewControllerManagedBy(mgr).
		For(&rukpakv1alpha1.Bundle{}, builder.WithPredicates(
			util.BundleProvisionerFilter(registry.ProvisionerID),
		)).
		// The default unpacker creates Pod's ownerref'd to its bundle, so
		// we need to watch pods to ensure we reconcile events coming from these
		// pods.
		Watches(&crsource.Kind{Type: &corev1.Pod{}}, util.MapOwneeToOwnerProvisionerHandler(context.TODO(), mgr.GetClient(), l, registry.ProvisionerID, &rukpakv1alpha1.Bundle{})).
		Complete(r)
}
