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
	"time"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1beta1 "github.com/operator-framework/catalogd/pkg/apis/core/v1beta1"
)

// CatalogSourceReconciler reconciles a CatalogSource object
type CatalogSourceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Cfg      *rest.Config
	OpmImage string
}

//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=catalogsources,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=catalogsources/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=catalogsources/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=pods/log,verbs=get;list;watch
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=create;get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the CatalogSource object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.0/pkg/reconcile
func (r *CatalogSourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	catalogSource := corev1beta1.CatalogSource{}
	if err := r.Client.Get(ctx, req.NamespacedName, &catalogSource); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	job := r.unpackJob(catalogSource)
	err := r.Client.Get(ctx, client.ObjectKeyFromObject(job), job)
	if err != nil {
		if errors.IsNotFound(err) {
			if err = r.createUnpackJob(ctx, catalogSource); err != nil {
				return ctrl.Result{}, err
			}
			// after creating the job requeue
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	declCfg, err := r.parseUnpackLogs(ctx, job)
	if err != nil {
		// check if this is a pod phase error and requeue if it is
		if corev1beta1.IsUnpackPhaseError(err) {
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	// TODO: Can we create these resources in parallel using goroutines?
	if err := r.buildPackages(ctx, declCfg, catalogSource); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.buildBundleMetadata(ctx, declCfg, catalogSource); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CatalogSourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1beta1.CatalogSource{}).
		Complete(r)
}

func (r *CatalogSourceReconciler) buildBundleMetadata(ctx context.Context, declCfg *declcfg.DeclarativeConfig, catalogSource corev1beta1.CatalogSource) error {
	for _, bundle := range declCfg.Bundles {
		bundleMeta := corev1beta1.BundleMetadata{
			ObjectMeta: metav1.ObjectMeta{
				Name: bundle.Name,
			},
			Spec: corev1beta1.BundleMetadataSpec{
				CatalogSource: catalogSource.Name,
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
				Value: prop.Value,
			})
		}

		ctrlutil.SetOwnerReference(&catalogSource, &bundleMeta, r.Scheme)

		if err := r.Client.Create(ctx, &bundleMeta); err != nil {
			return fmt.Errorf("creating bundlemetadata %q: %w", bundleMeta.Name, err)
		}
	}

	return nil
}

func (r *CatalogSourceReconciler) buildPackages(ctx context.Context, declCfg *declcfg.DeclarativeConfig, catalogSource corev1beta1.CatalogSource) error {
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
				CatalogSource:  catalogSource.Name,
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

		ctrlutil.SetOwnerReference(&catalogSource, &pack, r.Scheme)

		if err := r.Client.Create(ctx, &pack); err != nil {
			return fmt.Errorf("creating package %q: %w", pack.Name, err)
		}
	}
	return nil
}

func (r *CatalogSourceReconciler) createUnpackJob(ctx context.Context, cs corev1beta1.CatalogSource) error {
	job := r.unpackJob(cs)

	ctrlutil.SetOwnerReference(&cs, job, r.Scheme)

	if err := r.Client.Create(ctx, job); err != nil {
		return fmt.Errorf("creating unpackJob: %w", err)
	}

	return nil
}

func (r *CatalogSourceReconciler) parseUnpackLogs(ctx context.Context, job *batchv1.Job) (*declcfg.DeclarativeConfig, error) {
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

	if pod.Status.Phase != v1.PodSucceeded {
		return nil, corev1beta1.NewUnpackPhaseError(fmt.Sprintf("job pod in phase %q, expected %q", pod.Status.Phase, v1.PodSucceeded))
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

func (r *CatalogSourceReconciler) unpackJob(cs corev1beta1.CatalogSource) *batchv1.Job {
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
					Containers: []v1.Container{
						{
							Image: r.OpmImage,
							Name:  "unpacker",
							Command: []string{
								"opm",
								"render",
								cs.Spec.Image,
							},
						},
					},
				},
			},
		},
	}
}
