package variablesources_test

import (
	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/utils/pointer"

	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
)

func fakeOperator(name, packageName string, upgradeConstraintPolicy operatorsv1alpha1.UpgradeConstraintPolicy) operatorsv1alpha1.Operator {
	return operatorsv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			// We manually set a fake UID here because the code we test
			// uses UID to determine Operator CR which
			// owns `BundleDeployment`
			UID: uuid.NewUUID(),
		},
		Spec: operatorsv1alpha1.OperatorSpec{
			PackageName:             packageName,
			UpgradeConstraintPolicy: upgradeConstraintPolicy,
		},
	}
}

func fakeBundleDeployment(name, bundleImage string, owner *operatorsv1alpha1.Operator) rukpakv1alpha1.BundleDeployment {
	bd := rukpakv1alpha1.BundleDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: rukpakv1alpha1.BundleDeploymentSpec{
			ProvisionerClassName: "core-rukpak-io-plain",
			Template: &rukpakv1alpha1.BundleTemplate{
				Spec: rukpakv1alpha1.BundleSpec{
					ProvisionerClassName: "core-rukpak-io-plain",
					Source: rukpakv1alpha1.BundleSource{
						Image: &rukpakv1alpha1.ImageSource{
							Ref: bundleImage,
						},
					},
				},
			},
		},
	}

	if owner != nil {
		bd.SetOwnerReferences([]metav1.OwnerReference{
			{
				APIVersion:         operatorsv1alpha1.GroupVersion.String(),
				Kind:               "Operator",
				Name:               owner.Name,
				UID:                owner.UID,
				Controller:         pointer.Bool(true),
				BlockOwnerDeletion: pointer.Bool(true),
			},
		})
	}

	return bd
}
