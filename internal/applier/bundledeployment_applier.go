package applier

import (
	"context"

	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	operatorv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/sourcer"
)

const (
	plainProvisionerID    = "core-rukpak-io-plain"
	registryProvisionerID = "core-rukpak-io-registry"
)

func Apply(ctx context.Context, o *operatorv1alpha1.Operator, c client.Client, source sourcer.Bundle) (*rukpakv1alpha1.BundleDeployment, error) {
	bd := &rukpakv1alpha1.BundleDeployment{}
	bd.SetName(o.GetName())
	controllerRef := metav1.NewControllerRef(o, o.GroupVersionKind())

	labels := map[string]string{
		"core.olm.io/package":          o.Spec.Package.Name,
		"core.olm.io/version":          source.Version,
		"core.olm.io/source-name":      source.SourceInfo.Name,
		"core.olm.io/source-namespace": source.SourceInfo.Namespace,
	}
	_, err := controllerutil.CreateOrUpdate(ctx, c, bd, func() error {
		bd.SetLabels(labels)
		bd.SetOwnerReferences([]metav1.OwnerReference{*controllerRef})
		bd.Spec = buildBundleDeployment(source.Image, labels)
		return nil
	})
	return bd, err
}

// buildBundleDeployment is responsible for taking a name and image to create an embedded BundleDeployment
func buildBundleDeployment(image string, labels map[string]string) rukpakv1alpha1.BundleDeploymentSpec {
	return rukpakv1alpha1.BundleDeploymentSpec{
		ProvisionerClassName: plainProvisionerID,
		Template: &rukpakv1alpha1.BundleTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Labels: labels,
			},
			Spec: rukpakv1alpha1.BundleSpec{
				ProvisionerClassName: registryProvisionerID,
				Source: rukpakv1alpha1.BundleSource{
					Type: rukpakv1alpha1.SourceTypeImage,
					Image: &rukpakv1alpha1.ImageSource{
						Ref: image,
					},
				},
			},
		},
	}
}
