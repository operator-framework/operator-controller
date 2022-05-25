package applier

import (
	"context"

	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"github.com/timflannagan/platform-operators/api/v1alpha1"
	"github.com/timflannagan/platform-operators/internal/sourcer"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	plainProvisionerID    = "core.rukpak.io/plain"
	registryProvisionerID = "core.rukpak.io/registry"
)

type Applier interface {
	Apply(context.Context, *v1alpha1.PlatformOperator, *sourcer.Bundle) error
}

type biApplier struct {
	c client.Client
}

func NewBundleInstanceHandler(c client.Client) Applier {
	return &biApplier{
		c: c,
	}
}

func (a *biApplier) Apply(ctx context.Context, po *v1alpha1.PlatformOperator, b *sourcer.Bundle) error {
	bi := &rukpakv1alpha1.BundleInstance{}
	bi.SetName(po.GetName())
	controllerRef := metav1.NewControllerRef(po, po.GroupVersionKind())

	_, err := controllerutil.CreateOrUpdate(ctx, a.c, bi, func() error {
		bi.SetOwnerReferences([]metav1.OwnerReference{*controllerRef})
		bi.Spec = *buildBundleInstance(bi.GetName(), b.Image)
		return nil
	})
	return err
}

// createBundleInstance is responsible for taking a name and image to create an embedded BundleInstance
func buildBundleInstance(name, image string) *rukpakv1alpha1.BundleInstanceSpec {
	// TODO(tflannag): Generate a BI that specifies the registry+v1 provisioner ID
	// once https://github.com/operator-framework/rukpak/pull/387 lands.
	return &rukpakv1alpha1.BundleInstanceSpec{
		// TODO(tflannag): The metadata field is empty for whatever reason
		ProvisionerClassName: plainProvisionerID,
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
