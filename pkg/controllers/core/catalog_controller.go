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
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	apimacherrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	corev1beta1 "github.com/operator-framework/catalogd/pkg/apis/core/v1beta1"
)

// CatalogReconciler reconciles a Catalog object
type CatalogReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Cfg      *rest.Config
	OpmImage string
}

//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=catalogs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=catalogs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=catalogs/finalizers,verbs=update
//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=bundlemetadata,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=bundlemetadata/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=bundlemetadata/finalizers,verbs=update
//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=packages,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=packages/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=packages/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=pods/log,verbs=get;list;watch
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=create;get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.0/pkg/reconcile
func (r *CatalogReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// TODO: Where and when should we be logging errors and at which level?
	_ = log.FromContext(ctx).WithName("catalogd-controller")

	existingCatsrc := corev1beta1.Catalog{}
	if err := r.Client.Get(ctx, req.NamespacedName, &existingCatsrc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	reconciledCatsrc := existingCatsrc.DeepCopy()
	res, reconcileErr := r.reconcile(ctx, reconciledCatsrc)

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
	existingCatsrc.Status, reconciledCatsrc.Status = corev1beta1.CatalogStatus{}, corev1beta1.CatalogStatus{}
	if !equality.Semantic.DeepEqual(existingCatsrc, reconciledCatsrc) {
		if updateErr := r.Client.Update(ctx, reconciledCatsrc); updateErr != nil {
			return res, apimacherrors.NewAggregate([]error{reconcileErr, updateErr})
		}
	}
	return res, reconcileErr
}

// SetupWithManager sets up the controller with the Manager.
func (r *CatalogReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// TODO: Due to us not having proper error handling,
		// not having this results in the controller getting into
		// an error state because once we update the status it requeues
		// and then errors out when trying to create all the Packages again
		// even though they already exist. This should be resolved by the fix
		// for https://github.com/operator-framework/catalogd/issues/6. The fix for
		// #6 should also remove the usage of `builder.WithPredicates(predicate.GenerationChangedPredicate{})`
		For(&corev1beta1.Catalog{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}

func (r *CatalogReconciler) reconcile(ctx context.Context, catalog *corev1beta1.Catalog) (ctrl.Result, error) {
	job, err := r.ensureUnpackJob(ctx, catalog)
	if err != nil {
		updateStatusError(catalog, err)
		return ctrl.Result{}, fmt.Errorf("ensuring unpack job: %v", err)
	}

	complete, err := r.checkUnpackJobComplete(ctx, job)
	if err != nil {
		updateStatusError(catalog, err)
		return ctrl.Result{}, fmt.Errorf("ensuring unpack job completed: %v", err)
	}
	if !complete {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	declCfg, err := r.parseUnpackLogs(ctx, job)
	if err != nil {
		updateStatusError(catalog, err)
		return ctrl.Result{}, err
	}

	if err := r.createPackages(ctx, declCfg, catalog); err != nil {
		updateStatusError(catalog, err)
		return ctrl.Result{}, err
	}

	if err := r.createBundleMetadata(ctx, declCfg, catalog); err != nil {
		updateStatusError(catalog, err)
		return ctrl.Result{}, err
	}

	// update Catalog status as "Ready" since at this point
	// all catalog content should be available on cluster
	updateStatusReady(catalog)
	return ctrl.Result{}, nil
}

// ensureUnpackJob will ensure that an unpack job has been created for the given
// Catalog. It will return the unpack job if successful (either the Job already
// exists or one was successfully created) or an error if it is unsuccessful
func (r *CatalogReconciler) ensureUnpackJob(ctx context.Context, catalog *corev1beta1.Catalog) (*batchv1.Job, error) {
	// Create the unpack Job manifest for the given Catalog
	job := r.unpackJob(catalog)

	// If the Job already exists just return it. If it doesn't then attempt to create it
	err := r.Client.Get(ctx, client.ObjectKeyFromObject(job), job)
	if err != nil {
		if errors.IsNotFound(err) {
			if err = r.createUnpackJob(ctx, catalog); err != nil {
				return nil, err
			}
			return job, nil
		}
		return nil, err
	}

	return job, nil
}

// checkUnpackJobComplete will check whether or not an unpack Job has completed.
// It will return a boolean that is true if the Job has successfully completed,
// false if the Job has not completed, or an error if the Job is completed but in a
// "Failed", "FailureTarget", or "Suspended" state or an error is encountered
// when attempting to check the status of the Job
func (r *CatalogReconciler) checkUnpackJobComplete(ctx context.Context, job *batchv1.Job) (bool, error) {
	// If the completion time is non-nil that means the Job has completed
	if job.Status.CompletionTime != nil {
		// Loop through the conditions and check for any fail conditions
		for _, cond := range job.Status.Conditions {
			if cond.Status == v1.ConditionTrue && cond.Type != batchv1.JobComplete {
				return false, fmt.Errorf("unpack job has condition %q with a status of %q", cond.Type, cond.Status)
			}
		}
		// No failures and job has a completion time so job successfully completed
		return true, nil
	}
	return false, nil
}

// updateStatusReady will update the Catalog.Status.Conditions
// to have the "Ready" condition with a status of "True" and a Reason
// of "ContentsAvailable". This function is used to signal that a Catalog
// has been successfully unpacked and all catalog contents are available on cluster
func updateStatusReady(catalog *corev1beta1.Catalog) {
	meta.SetStatusCondition(&catalog.Status.Conditions, metav1.Condition{
		Type:    corev1beta1.TypeReady,
		Reason:  corev1beta1.ReasonContentsAvailable,
		Status:  metav1.ConditionTrue,
		Message: "catalog contents have been unpacked and are available on cluster",
	})
}

// updateStatusError will update the Catalog.Status.Conditions
// to have the condition Type "Ready" with a Status of "False" and a Reason
// of "UnpackError". This function is used to signal that a Catalog
// is in an error state and that catalog contents are not available on cluster
func updateStatusError(catalog *corev1beta1.Catalog, err error) {
	meta.SetStatusCondition(&catalog.Status.Conditions, metav1.Condition{
		Type:    corev1beta1.TypeReady,
		Status:  metav1.ConditionFalse,
		Reason:  corev1beta1.ReasonUnpackError,
		Message: err.Error(),
	})
}

// createBundleMetadata will create a `BundleMetadata` resource for each
// "olm.bundle" object that exists for the given catalog contents. Returns an
// error if any are encountered.
func (r *CatalogReconciler) createBundleMetadata(ctx context.Context, declCfg *declcfg.DeclarativeConfig, catalog *corev1beta1.Catalog) error {
	for _, bundle := range declCfg.Bundles {
		bundleMeta := corev1beta1.BundleMetadata{
			ObjectMeta: metav1.ObjectMeta{
				Name: bundle.Name,
			},
			Spec: corev1beta1.BundleMetadataSpec{
				CatalogSource: catalog.Name,
				Package:       bundle.Package,
				Image:         bundle.Image,
				Properties:    []corev1beta1.Property{},
				RelatedImages: []corev1beta1.RelatedImage{},
			},
		}

		for _, relatedImage := range bundle.RelatedImages {
			bundleMeta.Spec.RelatedImages = append(bundleMeta.Spec.RelatedImages, corev1beta1.RelatedImage{
				Name:  relatedImage.Name,
				Image: relatedImage.Image,
			})
		}

		for _, prop := range bundle.Properties {
			// skip any properties that are of type `olm.bundle.object`
			if prop.Type == "olm.bundle.object" {
				continue
			}

			bundleMeta.Spec.Properties = append(bundleMeta.Spec.Properties, corev1beta1.Property{
				Type:  prop.Type,
				Value: runtime.RawExtension{Raw: prop.Value},
			})
		}

		ctrlutil.SetOwnerReference(catalog, &bundleMeta, r.Scheme)

		if err := r.Client.Create(ctx, &bundleMeta); err != nil {
			return fmt.Errorf("creating bundlemetadata %q: %w", bundleMeta.Name, err)
		}
	}

	return nil
}

// createPackages will create a `Package` resource for each
// "olm.package" object that exists for the given catalog contents.
// `Package.Spec.Channels` is populated by filtering all "olm.channel" objects
// where the "packageName" == `Package.Name`. Returns an error if any are encountered.
func (r *CatalogReconciler) createPackages(ctx context.Context, declCfg *declcfg.DeclarativeConfig, catalog *corev1beta1.Catalog) error {
	for _, pkg := range declCfg.Packages {
		pack := corev1beta1.Package{
			ObjectMeta: metav1.ObjectMeta{
				// TODO: If we just provide the name of the package, then
				// we are inherently saying no other catalog sources can provide a package
				// of the same name due to this being a cluster scoped resource. We should
				// look into options for configuring admission criteria for the Package
				// resource to resolve this potential clash.
				Name: pkg.Name,
			},
			Spec: corev1beta1.PackageSpec{
				CatalogSource:  catalog.Name,
				DefaultChannel: pkg.DefaultChannel,
				Channels:       []corev1beta1.PackageChannel{},
				Description:    pkg.Description,
			},
		}
		for _, ch := range declCfg.Channels {
			if ch.Package == pkg.Name {
				packChannel := corev1beta1.PackageChannel{
					Name:    ch.Name,
					Entries: []corev1beta1.ChannelEntry{},
				}
				for _, entry := range ch.Entries {
					packChannel.Entries = append(packChannel.Entries, corev1beta1.ChannelEntry{
						Name:      entry.Name,
						Replaces:  entry.Replaces,
						Skips:     entry.Skips,
						SkipRange: entry.SkipRange,
					})
				}

				pack.Spec.Channels = append(pack.Spec.Channels, packChannel)
			}
		}

		ctrlutil.SetOwnerReference(catalog, &pack, r.Scheme)

		if err := r.Client.Create(ctx, &pack); err != nil {
			return fmt.Errorf("creating package %q: %w", pack.Name, err)
		}
	}
	return nil
}

// createUnpackJob creates an unpack Job for the given Catalog
func (r *CatalogReconciler) createUnpackJob(ctx context.Context, cs *corev1beta1.Catalog) error {
	job := r.unpackJob(cs)

	ctrlutil.SetOwnerReference(cs, job, r.Scheme)

	if err := r.Client.Create(ctx, job); err != nil {
		return fmt.Errorf("creating unpackJob: %w", err)
	}

	return nil
}

// parseUnpackLogs parses the Pod logs from the Pod created by the
// provided unpack Job into a `declcfg.DeclarativeConfig` object
func (r *CatalogReconciler) parseUnpackLogs(ctx context.Context, job *batchv1.Job) (*declcfg.DeclarativeConfig, error) {
	clientset, err := kubernetes.NewForConfig(r.Cfg)
	if err != nil {
		return nil, fmt.Errorf("creating clientset: %w", err)
	}

	podsForJob := &v1.PodList{}
	err = r.Client.List(ctx, podsForJob, client.MatchingLabels{"job-name": job.GetName()})
	if err != nil {
		return nil, fmt.Errorf("listing pods: %w", err)
	}

	if len(podsForJob.Items) <= 0 {
		return nil, fmt.Errorf("no pods for job")
	}
	pod := podsForJob.Items[0]

	// TODO: Should we remove this check since we verify the Job has completed before calling this making this redundant?
	if pod.Status.Phase != v1.PodSucceeded {
		return nil, fmt.Errorf("job pod in phase %q, expected %q", pod.Status.Phase, v1.PodSucceeded)
	}

	req := clientset.CoreV1().Pods(job.Namespace).GetLogs(pod.GetName(), &v1.PodLogOptions{})
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("streaming pod logs: %w", err)
	}
	defer podLogs.Close()

	logs, err := io.ReadAll(podLogs)
	if err != nil {
		return nil, fmt.Errorf("reading pod logs: %w", err)
	}

	return declcfg.LoadReader(bytes.NewReader(logs))
}

// unpackJob creates the manifest for an unpack Job given a Catalog
func (r *CatalogReconciler) unpackJob(cs *corev1beta1.Catalog) *batchv1.Job {
	opmVol := "opm"
	mountPath := "opmvol/"
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "catalogd-system",
			Name:      fmt.Sprintf("%s-image-unpack", cs.Name),
		},
		Spec: batchv1.JobSpec{
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "catalogd-system",
					Name:      fmt.Sprintf("%s-image-unpack-pod", cs.Name),
				},
				Spec: v1.PodSpec{
					RestartPolicy: v1.RestartPolicyOnFailure,
					InitContainers: []v1.Container{
						{
							Image: r.OpmImage,
							Name:  "initializer",
							Command: []string{
								"cp",
								"/bin/opm",
								filepath.Join(mountPath, "opm"),
							},
							VolumeMounts: []v1.VolumeMount{
								{
									Name:      opmVol,
									MountPath: mountPath,
								},
							},
						},
					},
					Containers: []v1.Container{
						{
							Image: cs.Spec.Image,
							Name:  "unpacker",
							Command: []string{
								filepath.Join(mountPath, "opm"),
								"render",
								"/configs/",
							},
							VolumeMounts: []v1.VolumeMount{
								{
									Name:      opmVol,
									MountPath: mountPath,
								},
							},
						},
					},
					Volumes: []v1.Volume{
						{
							Name: opmVol,
							VolumeSource: v1.VolumeSource{
								EmptyDir: &v1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}
}
