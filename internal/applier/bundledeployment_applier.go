package applier

import (
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformtypes "github.com/timflannagan/platform-operators/api/v1alpha1"
)

const (
	plainProvisionerID    = "core-rukpak-io-plain"
	registryProvisionerID = "core-rukpak-io-registry"
)

func NewBundleDeployment(o *platformtypes.Operator, image string) *rukpakv1alpha1.BundleDeployment {
	bd := &rukpakv1alpha1.BundleDeployment{}
	bd.SetName(o.GetName())

	controllerRef := metav1.NewControllerRef(o, o.GroupVersionKind())
	bd.SetOwnerReferences([]metav1.OwnerReference{*controllerRef})

	bd.Spec = buildBundleDeployment(image)
	return bd
}

// buildBundleDeployment is responsible for taking a name and image to create an embedded BundleDeployment
func buildBundleDeployment(image string) rukpakv1alpha1.BundleDeploymentSpec {
	return rukpakv1alpha1.BundleDeploymentSpec{
		ProvisionerClassName: plainProvisionerID,
		// TODO(tflannag): Investigate why the metadata key is empty when this
		// resource has been created on cluster despite the field being omitempty.
		Template: &rukpakv1alpha1.BundleTemplate{
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
