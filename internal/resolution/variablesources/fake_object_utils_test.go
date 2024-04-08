package variablesources_test

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/utils/ptr"

	rukpakv1alpha2 "github.com/operator-framework/rukpak/api/v1alpha2"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

func fakeClusterExtension(name, packageName string, upgradeConstraintPolicy ocv1alpha1.UpgradeConstraintPolicy) ocv1alpha1.ClusterExtension {
	return ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			// We manually set a fake UID here because the code we test
			// uses UID to determine ClusterExtension CR which
			// owns `BundleDeployment`
			UID: uuid.NewUUID(),
		},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			PackageName:             packageName,
			UpgradeConstraintPolicy: upgradeConstraintPolicy,
		},
	}
}

func fakeBundleDeployment(name, bundleImage string, owner *ocv1alpha1.ClusterExtension) rukpakv1alpha2.BundleDeployment {
	bd := rukpakv1alpha2.BundleDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: rukpakv1alpha2.BundleDeploymentSpec{
			ProvisionerClassName: "core-rukpak-io-plain",
			Source: rukpakv1alpha2.BundleSource{
				Image: &rukpakv1alpha2.ImageSource{
					Ref: bundleImage,
				},
			},
		},
	}

	if owner != nil {
		bd.SetOwnerReferences([]metav1.OwnerReference{
			{
				APIVersion:         ocv1alpha1.GroupVersion.String(),
				Kind:               "ClusterExtension",
				Name:               owner.Name,
				UID:                owner.UID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			},
		})
	}

	return bd
}
