package controllers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	"github.com/operator-framework/operator-controller/internal/conditionsets"
	"github.com/operator-framework/operator-controller/pkg/features"
)

// Describe: Operator Controller Test
func TestOperatorDoesNotExist(t *testing.T) {
	_, reconciler := newClientAndReconciler(t)

	t.Log("When the operator does not exist")
	t.Log("It returns no error")
	res, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "non-existent"}})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)
}

func TestOperatorNonExistantPackage(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	opKey := types.NamespacedName{Name: fmt.Sprintf("operator-test-%s", rand.String(8))}

	t.Log("When the operator specifies a non-existent package")
	t.Log("By initializing cluster state")
	pkgName := fmt.Sprintf("non-existent-%s", rand.String(6))
	operator := &operatorsv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
		Spec:       operatorsv1alpha1.OperatorSpec{PackageName: pkgName},
	}
	require.NoError(t, cl.Create(ctx, operator))

	t.Log("It sets resolution failure status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
	require.Equal(t, ctrl.Result{}, res)
	require.EqualError(t, err, fmt.Sprintf("no package %q found", pkgName))

	t.Log("By fetching updated operator after reconcile")
	require.NoError(t, cl.Get(ctx, opKey, operator))

	t.Log("By checking the status fields")
	require.Empty(t, operator.Status.ResolvedBundleResource)
	require.Empty(t, operator.Status.InstalledBundleResource)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonResolutionFailed, cond.Reason)
	require.Equal(t, fmt.Sprintf("no package %q found", pkgName), cond.Message)

	verifyInvariants(ctx, t, reconciler.Client, operator)
	require.NoError(t, cl.DeleteAllOf(ctx, &operatorsv1alpha1.Operator{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha1.BundleDeployment{}))
}

func TestOperatorNonExistantVersion(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	opKey := types.NamespacedName{Name: fmt.Sprintf("operator-test-%s", rand.String(8))}

	t.Log("When the operator specifies a version that does not exist")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	operator := &operatorsv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
		Spec: operatorsv1alpha1.OperatorSpec{
			PackageName: pkgName,
			Version:     "0.50.0", // this version of the package does not exist
		},
	}
	require.NoError(t, cl.Create(ctx, operator))

	t.Log("It sets resolution failure status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
	require.Equal(t, ctrl.Result{}, res)
	require.EqualError(t, err, fmt.Sprintf(`no package %q matching version "0.50.0" found`, pkgName))

	t.Log("By fetching updated operator after reconcile")
	require.NoError(t, cl.Get(ctx, opKey, operator))

	t.Log("By checking the status fields")
	require.Empty(t, operator.Status.ResolvedBundleResource)
	require.Empty(t, operator.Status.InstalledBundleResource)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonResolutionFailed, cond.Reason)
	require.Equal(t, fmt.Sprintf(`no package %q matching version "0.50.0" found`, pkgName), cond.Message)
	cond = apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "installation has not been attempted as resolution failed", cond.Message)

	verifyInvariants(ctx, t, reconciler.Client, operator)
	require.NoError(t, cl.DeleteAllOf(ctx, &operatorsv1alpha1.Operator{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha1.BundleDeployment{}))
}

func TestOperatorBundleDeploymentDoesNotExist(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	opKey := types.NamespacedName{Name: fmt.Sprintf("operator-test-%s", rand.String(8))}
	const pkgName = "prometheus"

	t.Log("When the operator specifies a valid available package")
	t.Log("By initializing cluster state")
	operator := &operatorsv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
		Spec:       operatorsv1alpha1.OperatorSpec{PackageName: pkgName},
	}
	require.NoError(t, cl.Create(ctx, operator))

	t.Log("When the BundleDeployment does not exist")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching updated operator after reconcile")
	require.NoError(t, cl.Get(ctx, opKey, operator))

	t.Log("It results in the expected BundleDeployment")
	bd := &rukpakv1alpha1.BundleDeployment{}
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: opKey.Name}, bd))
	require.Equal(t, "core-rukpak-io-plain", bd.Spec.ProvisionerClassName)
	require.Equal(t, "core-rukpak-io-registry", bd.Spec.Template.Spec.ProvisionerClassName)
	require.Equal(t, rukpakv1alpha1.SourceTypeImage, bd.Spec.Template.Spec.Source.Type)
	require.NotNil(t, bd.Spec.Template.Spec.Source.Image)
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", bd.Spec.Template.Spec.Source.Image.Ref)

	t.Log("It sets the resolvedBundleResource status field")
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", operator.Status.ResolvedBundleResource)

	t.Log("It sets the InstalledBundleResource status field")
	require.Empty(t, operator.Status.InstalledBundleResource)

	t.Log("It sets the status on operator")
	cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)

	cond = apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "bundledeployment status is unknown", cond.Message)

	verifyInvariants(ctx, t, reconciler.Client, operator)
	require.NoError(t, cl.DeleteAllOf(ctx, &operatorsv1alpha1.Operator{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha1.BundleDeployment{}))
}

func TestOperatorBundleDeploymentOutOfDate(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	opKey := types.NamespacedName{Name: fmt.Sprintf("operator-test-%s", rand.String(8))}
	const pkgName = "prometheus"

	t.Log("When the operator specifies a valid available package")
	t.Log("By initializing cluster state")
	operator := &operatorsv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
		Spec:       operatorsv1alpha1.OperatorSpec{PackageName: pkgName},
	}
	require.NoError(t, cl.Create(ctx, operator))

	t.Log("When the expected BundleDeployment already exists")
	t.Log("When the BundleDeployment spec is out of date")
	t.Log("By patching the existing BD")
	bd := &rukpakv1alpha1.BundleDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: opKey.Name,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         operatorsv1alpha1.GroupVersion.String(),
					Kind:               "Operator",
					Name:               operator.Name,
					UID:                operator.UID,
					Controller:         pointer.Bool(true),
					BlockOwnerDeletion: pointer.Bool(true),
				},
			},
		},
		Spec: rukpakv1alpha1.BundleDeploymentSpec{
			ProvisionerClassName: "core-rukpak-io-plain",
			Template: rukpakv1alpha1.BundleTemplate{
				Spec: rukpakv1alpha1.BundleSpec{
					ProvisionerClassName: "core-rukpak-io-registry",
					Source: rukpakv1alpha1.BundleSource{
						Type: rukpakv1alpha1.SourceTypeImage,
						Image: &rukpakv1alpha1.ImageSource{
							Ref: "quay.io/operatorhubio/prometheus@fake2.0.0",
						},
					},
				},
			},
		},
	}

	t.Log("By modifying the BD spec and creating the object")
	bd.Spec.ProvisionerClassName = "core-rukpak-io-helm"
	require.NoError(t, cl.Create(ctx, bd))

	t.Log("It results in the expected BundleDeployment")

	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching updated operator after reconcile")
	require.NoError(t, cl.Get(ctx, opKey, operator))

	t.Log("By checking the expected BD spec")
	bd = &rukpakv1alpha1.BundleDeployment{}
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: opKey.Name}, bd))
	require.Equal(t, "core-rukpak-io-plain", bd.Spec.ProvisionerClassName)
	require.Equal(t, "core-rukpak-io-registry", bd.Spec.Template.Spec.ProvisionerClassName)
	require.Equal(t, rukpakv1alpha1.SourceTypeImage, bd.Spec.Template.Spec.Source.Type)
	require.NotNil(t, bd.Spec.Template.Spec.Source.Image)
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", bd.Spec.Template.Spec.Source.Image.Ref)

	t.Log("By checking the status fields")
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", operator.Status.ResolvedBundleResource)
	require.Empty(t, operator.Status.InstalledBundleResource)

	t.Log("By checking the expected status conditions")
	cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)
	cond = apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "bundledeployment status is unknown", cond.Message)

	verifyInvariants(ctx, t, reconciler.Client, operator)
	require.NoError(t, cl.DeleteAllOf(ctx, &operatorsv1alpha1.Operator{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha1.BundleDeployment{}))
}

func TestOperatorBundleDeploymentUpToDate(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	opKey := types.NamespacedName{Name: fmt.Sprintf("operator-test-%s", rand.String(8))}
	const pkgName = "prometheus"

	t.Log("When the operator specifies a valid available package")
	t.Log("By initializing cluster state")
	operator := &operatorsv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
		Spec:       operatorsv1alpha1.OperatorSpec{PackageName: pkgName},
	}
	require.NoError(t, cl.Create(ctx, operator))

	t.Log("When the expected BundleDeployment already exists")
	t.Log("When the BundleDeployment spec is up-to-date")
	t.Log("By patching the existing BD")
	bd := &rukpakv1alpha1.BundleDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: opKey.Name,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         operatorsv1alpha1.GroupVersion.String(),
					Kind:               "Operator",
					Name:               operator.Name,
					UID:                operator.UID,
					Controller:         pointer.Bool(true),
					BlockOwnerDeletion: pointer.Bool(true),
				},
			},
		},
		Spec: rukpakv1alpha1.BundleDeploymentSpec{
			ProvisionerClassName: "core-rukpak-io-plain",
			Template: rukpakv1alpha1.BundleTemplate{
				Spec: rukpakv1alpha1.BundleSpec{
					ProvisionerClassName: "core-rukpak-io-registry",
					Source: rukpakv1alpha1.BundleSource{
						Type: rukpakv1alpha1.SourceTypeImage,
						Image: &rukpakv1alpha1.ImageSource{
							Ref: "quay.io/operatorhubio/prometheus@fake2.0.0",
						},
					},
				},
			},
		},
	}

	require.NoError(t, cl.Create(ctx, bd))
	bd.Status.ObservedGeneration = bd.GetGeneration()

	t.Log("When the BundleDeployment status is mapped to the expected Operator status")
	t.Log("It verifies operator status when bundle deployment is waiting to be created")
	t.Log("By updating the status of bundleDeployment")
	require.NoError(t, cl.Status().Update(ctx, bd))

	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching the updated operator after reconcile")
	op := &operatorsv1alpha1.Operator{}
	require.NoError(t, cl.Get(ctx, opKey, op))

	t.Log("By checking the status fields")
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", op.Status.ResolvedBundleResource)
	require.Empty(t, op.Status.InstalledBundleResource)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(op.Status.Conditions, operatorsv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)
	cond = apimeta.FindStatusCondition(op.Status.Conditions, operatorsv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "bundledeployment status is unknown", cond.Message)

	t.Log("It verifies operator status when `HasValidBundle` condition of rukpak is false")
	apimeta.SetStatusCondition(&bd.Status.Conditions, metav1.Condition{
		Type:    rukpakv1alpha1.TypeHasValidBundle,
		Status:  metav1.ConditionFalse,
		Message: "failed to unpack",
		Reason:  rukpakv1alpha1.ReasonUnpackFailed,
	})

	t.Log("By updating the status of bundleDeployment")
	require.NoError(t, cl.Status().Update(ctx, bd))

	t.Log("By running reconcile")
	res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching the updated operator after reconcile")
	op = &operatorsv1alpha1.Operator{}
	require.NoError(t, cl.Get(ctx, opKey, op))

	t.Log("By checking the status fields")
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", op.Status.ResolvedBundleResource)
	require.Equal(t, "", op.Status.InstalledBundleResource)

	t.Log("By checking the expected conditions")
	cond = apimeta.FindStatusCondition(op.Status.Conditions, operatorsv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)
	cond = apimeta.FindStatusCondition(op.Status.Conditions, operatorsv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "bundledeployment status is unknown", cond.Message)

	t.Log("It verifies operator status when `InstallReady` condition of rukpak is false")
	apimeta.SetStatusCondition(&bd.Status.Conditions, metav1.Condition{
		Type:    rukpakv1alpha1.TypeInstalled,
		Status:  metav1.ConditionFalse,
		Message: "failed to install",
		Reason:  rukpakv1alpha1.ReasonInstallFailed,
	})

	t.Log("By updating the status of bundleDeployment")
	require.NoError(t, cl.Status().Update(ctx, bd))

	t.Log("By running reconcile")
	res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching the updated operator after reconcile")
	op = &operatorsv1alpha1.Operator{}
	err = cl.Get(ctx, opKey, op)
	require.NoError(t, err)

	t.Log("By checking the status fields")
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", op.Status.ResolvedBundleResource)
	require.Empty(t, op.Status.InstalledBundleResource)

	t.Log("By checking the expected conditions")
	cond = apimeta.FindStatusCondition(op.Status.Conditions, operatorsv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)

	cond = apimeta.FindStatusCondition(op.Status.Conditions, operatorsv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonInstallationFailed, cond.Reason)
	require.Contains(t, cond.Message, `failed to install`)

	t.Log("It verifies operator status when `InstallReady` condition of rukpak is true")
	apimeta.SetStatusCondition(&bd.Status.Conditions, metav1.Condition{
		Type:    rukpakv1alpha1.TypeInstalled,
		Status:  metav1.ConditionTrue,
		Message: "operator installed successfully",
		Reason:  rukpakv1alpha1.ReasonInstallationSucceeded,
	})

	t.Log("By updating the status of bundleDeployment")
	require.NoError(t, cl.Status().Update(ctx, bd))

	t.Log("By running reconcile")
	res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching the updated operator after reconcile")
	op = &operatorsv1alpha1.Operator{}
	require.NoError(t, cl.Get(ctx, opKey, op))

	t.Log("By checking the status fields")
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", op.Status.ResolvedBundleResource)
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", op.Status.InstalledBundleResource)

	t.Log("By checking the expected conditions")
	cond = apimeta.FindStatusCondition(op.Status.Conditions, operatorsv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)
	cond = apimeta.FindStatusCondition(op.Status.Conditions, operatorsv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "installed from \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)

	t.Log("It verifies any other unknown status of bundledeployment")
	apimeta.SetStatusCondition(&bd.Status.Conditions, metav1.Condition{
		Type:    rukpakv1alpha1.TypeHasValidBundle,
		Status:  metav1.ConditionUnknown,
		Message: "unpacking",
		Reason:  rukpakv1alpha1.ReasonUnpackSuccessful,
	})

	apimeta.SetStatusCondition(&bd.Status.Conditions, metav1.Condition{
		Type:    rukpakv1alpha1.TypeInstalled,
		Status:  metav1.ConditionUnknown,
		Message: "installing",
		Reason:  rukpakv1alpha1.ReasonInstallationSucceeded,
	})

	t.Log("By updating the status of bundleDeployment")
	require.NoError(t, cl.Status().Update(ctx, bd))

	t.Log("By running reconcile")
	res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching the updated operator after reconcile")
	op = &operatorsv1alpha1.Operator{}
	require.NoError(t, cl.Get(ctx, opKey, op))

	t.Log("By checking the status fields")
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", op.Status.ResolvedBundleResource)
	require.Empty(t, op.Status.InstalledBundleResource)

	t.Log("By checking the expected conditions")
	cond = apimeta.FindStatusCondition(op.Status.Conditions, operatorsv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)

	cond = apimeta.FindStatusCondition(op.Status.Conditions, operatorsv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonInstallationFailed, cond.Reason)
	require.Equal(t, "bundledeployment not ready: installing", cond.Message)

	t.Log("It verifies operator status when bundleDeployment installation status is unknown")
	apimeta.SetStatusCondition(&bd.Status.Conditions, metav1.Condition{
		Type:    rukpakv1alpha1.TypeInstalled,
		Status:  metav1.ConditionUnknown,
		Message: "installing",
		Reason:  rukpakv1alpha1.ReasonInstallationSucceeded,
	})

	t.Log("By updating the status of bundleDeployment")
	require.NoError(t, cl.Status().Update(ctx, bd))

	t.Log("running reconcile")
	res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching the updated operator after reconcile")
	op = &operatorsv1alpha1.Operator{}
	require.NoError(t, cl.Get(ctx, opKey, op))

	t.Log("By checking the status fields")
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", op.Status.ResolvedBundleResource)
	require.Empty(t, op.Status.InstalledBundleResource)

	t.Log("By cchecking the expected conditions")
	cond = apimeta.FindStatusCondition(op.Status.Conditions, operatorsv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)
	cond = apimeta.FindStatusCondition(op.Status.Conditions, operatorsv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonInstallationFailed, cond.Reason)
	require.Equal(t, "bundledeployment not ready: installing", cond.Message)

	verifyInvariants(ctx, t, reconciler.Client, operator)
	require.NoError(t, cl.DeleteAllOf(ctx, &operatorsv1alpha1.Operator{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha1.BundleDeployment{}))
}

func TestOperatorExpectedBundleDeployment(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	opKey := types.NamespacedName{Name: fmt.Sprintf("operator-test-%s", rand.String(8))}
	const pkgName = "prometheus"

	t.Log("When the operator specifies a valid available package")
	t.Log("By initializing cluster state")
	operator := &operatorsv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
		Spec:       operatorsv1alpha1.OperatorSpec{PackageName: pkgName},
	}
	require.NoError(t, cl.Create(ctx, operator))

	t.Log("When an out-of-date BundleDeployment exists")
	t.Log("By creating the expected BD")
	bd := &rukpakv1alpha1.BundleDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
		Spec: rukpakv1alpha1.BundleDeploymentSpec{
			ProvisionerClassName: "foo",
			Template: rukpakv1alpha1.BundleTemplate{
				Spec: rukpakv1alpha1.BundleSpec{
					ProvisionerClassName: "bar",
					Source: rukpakv1alpha1.BundleSource{
						Type: rukpakv1alpha1.SourceTypeHTTP,
						HTTP: &rukpakv1alpha1.HTTPSource{
							URL: "http://localhost:8080/",
						},
					},
				},
			},
		},
	}
	require.NoError(t, cl.Create(ctx, bd))

	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching updated operator after reconcile")
	require.NoError(t, cl.Get(ctx, opKey, operator))

	t.Log("It results in the expected BundleDeployment")
	bd = &rukpakv1alpha1.BundleDeployment{}
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: opKey.Name}, bd))
	require.Equal(t, "core-rukpak-io-plain", bd.Spec.ProvisionerClassName)
	require.Equal(t, "core-rukpak-io-registry", bd.Spec.Template.Spec.ProvisionerClassName)
	require.Equal(t, rukpakv1alpha1.SourceTypeImage, bd.Spec.Template.Spec.Source.Type)
	require.NotNil(t, bd.Spec.Template.Spec.Source.Image)
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", bd.Spec.Template.Spec.Source.Image.Ref)

	t.Log("It sets the resolvedBundleResource status field")
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", operator.Status.ResolvedBundleResource)

	t.Log("It sets the InstalledBundleResource status field")
	require.Empty(t, operator.Status.InstalledBundleResource)

	t.Log("It sets resolution to unknown status")
	cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)
	cond = apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "bundledeployment status is unknown", cond.Message)

	verifyInvariants(ctx, t, reconciler.Client, operator)
	require.NoError(t, cl.DeleteAllOf(ctx, &operatorsv1alpha1.Operator{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha1.BundleDeployment{}))
}

func TestOperatorDuplicatePackage(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	opKey := types.NamespacedName{Name: fmt.Sprintf("operator-test-%s", rand.String(8))}
	const pkgName = "prometheus"

	t.Log("When the operator specifies a duplicate package")
	t.Log("By initializing cluster state")
	dupOperator := &operatorsv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("orig-%s", opKey.Name)},
		Spec:       operatorsv1alpha1.OperatorSpec{PackageName: pkgName},
	}
	require.NoError(t, cl.Create(ctx, dupOperator))

	operator := &operatorsv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
		Spec:       operatorsv1alpha1.OperatorSpec{PackageName: pkgName},
	}
	require.NoError(t, cl.Create(ctx, operator))

	t.Log("It sets resolution failure status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
	require.Equal(t, ctrl.Result{}, res)
	require.EqualError(t, err, `duplicate identifier "required package prometheus" in input`)

	t.Log("By fetching updated operator after reconcile")
	require.NoError(t, cl.Get(ctx, opKey, operator))

	t.Log("By checking the status fields")
	require.Empty(t, operator.Status.ResolvedBundleResource)
	require.Empty(t, operator.Status.InstalledBundleResource)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonResolutionFailed, cond.Reason)
	require.Equal(t, `duplicate identifier "required package prometheus" in input`, cond.Message)

	cond = apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "installation has not been attempted as resolution failed", cond.Message)

	verifyInvariants(ctx, t, reconciler.Client, operator)
	require.NoError(t, cl.DeleteAllOf(ctx, &operatorsv1alpha1.Operator{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha1.BundleDeployment{}))
}

func TestOperatorChannelVersionExists(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	opKey := types.NamespacedName{Name: fmt.Sprintf("operator-test-%s", rand.String(8))}

	t.Log("When the operator specifies a channel with version that exist")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgVer := "1.0.0"
	pkgChan := "beta"
	operator := &operatorsv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
		Spec: operatorsv1alpha1.OperatorSpec{
			PackageName: pkgName,
			Version:     pkgVer,
			Channel:     pkgChan,
		},
	}
	err := cl.Create(ctx, operator)
	require.NoError(t, err)

	t.Log("It sets resolution success status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching updated operator after reconcile")
	require.NoError(t, cl.Get(ctx, opKey, operator))

	t.Log("By checking the status fields")
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake1.0.0", operator.Status.ResolvedBundleResource)
	require.Empty(t, operator.Status.InstalledBundleResource)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake1.0.0\"", cond.Message)
	cond = apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "bundledeployment status is unknown", cond.Message)

	t.Log("By fetching the bundled deployment")
	bd := &rukpakv1alpha1.BundleDeployment{}
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: opKey.Name}, bd))
	require.Equal(t, "core-rukpak-io-plain", bd.Spec.ProvisionerClassName)
	require.Equal(t, "core-rukpak-io-registry", bd.Spec.Template.Spec.ProvisionerClassName)
	require.Equal(t, rukpakv1alpha1.SourceTypeImage, bd.Spec.Template.Spec.Source.Type)
	require.NotNil(t, bd.Spec.Template.Spec.Source.Image)
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake1.0.0", bd.Spec.Template.Spec.Source.Image.Ref)

	verifyInvariants(ctx, t, reconciler.Client, operator)
	require.NoError(t, cl.DeleteAllOf(ctx, &operatorsv1alpha1.Operator{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha1.BundleDeployment{}))
}

func TestOperatorChannelExistsNoVersion(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	opKey := types.NamespacedName{Name: fmt.Sprintf("operator-test-%s", rand.String(8))}

	t.Log("When the operator specifies a package that exists within a channel but no version specified")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgVer := ""
	pkgChan := "beta"
	operator := &operatorsv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
		Spec: operatorsv1alpha1.OperatorSpec{
			PackageName: pkgName,
			Version:     pkgVer,
			Channel:     pkgChan,
		},
	}
	require.NoError(t, cl.Create(ctx, operator))

	t.Log("It sets resolution success status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)
	t.Log("By fetching updated operator after reconcile")
	require.NoError(t, cl.Get(ctx, opKey, operator))

	t.Log("By checking the status fields")
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", operator.Status.ResolvedBundleResource)
	require.Empty(t, operator.Status.InstalledBundleResource)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)
	cond = apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "bundledeployment status is unknown", cond.Message)

	t.Log("By fetching the bundledeployment")
	bd := &rukpakv1alpha1.BundleDeployment{}
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: opKey.Name}, bd))
	require.Equal(t, "core-rukpak-io-plain", bd.Spec.ProvisionerClassName)
	require.Equal(t, "core-rukpak-io-registry", bd.Spec.Template.Spec.ProvisionerClassName)
	require.Equal(t, rukpakv1alpha1.SourceTypeImage, bd.Spec.Template.Spec.Source.Type)
	require.NotNil(t, bd.Spec.Template.Spec.Source.Image)
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", bd.Spec.Template.Spec.Source.Image.Ref)

	verifyInvariants(ctx, t, reconciler.Client, operator)
	require.NoError(t, cl.DeleteAllOf(ctx, &operatorsv1alpha1.Operator{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha1.BundleDeployment{}))
}

func TestOperatorVersionNoChannel(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	opKey := types.NamespacedName{Name: fmt.Sprintf("operator-test-%s", rand.String(8))}

	t.Log("When the operator specifies a package version in a channel that does not exist")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgVer := "0.47.0"
	pkgChan := "alpha"
	operator := &operatorsv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
		Spec: operatorsv1alpha1.OperatorSpec{
			PackageName: pkgName,
			Version:     pkgVer,
			Channel:     pkgChan,
		},
	}
	require.NoError(t, cl.Create(ctx, operator))

	t.Log("It sets resolution failure status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
	require.Equal(t, ctrl.Result{}, res)
	require.EqualError(t, err, fmt.Sprintf("no package %q matching version %q found in channel %q", pkgName, pkgVer, pkgChan))

	t.Log("By fetching updated operator after reconcile")
	require.NoError(t, cl.Get(ctx, opKey, operator))

	t.Log("By checking the status fields")
	require.Empty(t, operator.Status.ResolvedBundleResource)
	require.Empty(t, operator.Status.InstalledBundleResource)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonResolutionFailed, cond.Reason)
	require.Equal(t, fmt.Sprintf("no package %q matching version %q found in channel %q", pkgName, pkgVer, pkgChan), cond.Message)
	cond = apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeInstalled)

	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "installation has not been attempted as resolution failed", cond.Message)

	verifyInvariants(ctx, t, reconciler.Client, operator)
	require.NoError(t, cl.DeleteAllOf(ctx, &operatorsv1alpha1.Operator{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha1.BundleDeployment{}))
}

func TestOperatorNoChannel(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	opKey := types.NamespacedName{Name: fmt.Sprintf("operator-test-%s", rand.String(8))}

	t.Log("When the operator specifies a package in a channel that does not exist")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgChan := "non-existent"
	operator := &operatorsv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
		Spec: operatorsv1alpha1.OperatorSpec{
			PackageName: pkgName,
			Channel:     pkgChan,
		},
	}
	require.NoError(t, cl.Create(ctx, operator))

	t.Log("It sets resolution failure status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
	require.Equal(t, ctrl.Result{}, res)
	require.EqualError(t, err, fmt.Sprintf("no package %q found in channel %q", pkgName, pkgChan))

	t.Log("By fetching updated operator after reconcile")
	require.NoError(t, cl.Get(ctx, opKey, operator))

	t.Log("By checking the status fields")
	require.Empty(t, operator.Status.ResolvedBundleResource)
	require.Empty(t, operator.Status.InstalledBundleResource)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonResolutionFailed, cond.Reason)
	require.Equal(t, fmt.Sprintf("no package %q found in channel %q", pkgName, pkgChan), cond.Message)
	cond = apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "installation has not been attempted as resolution failed", cond.Message)

	verifyInvariants(ctx, t, reconciler.Client, operator)
	require.NoError(t, cl.DeleteAllOf(ctx, &operatorsv1alpha1.Operator{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha1.BundleDeployment{}))
}

func TestOperatorNoVersion(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	opKey := types.NamespacedName{Name: fmt.Sprintf("operator-test-%s", rand.String(8))}

	t.Log("When the operator specifies a package version that does not exist in the channel")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgVer := "0.57.0"
	pkgChan := "non-existent"
	operator := &operatorsv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
		Spec: operatorsv1alpha1.OperatorSpec{
			PackageName: pkgName,
			Version:     pkgVer,
			Channel:     pkgChan,
		},
	}
	require.NoError(t, cl.Create(ctx, operator))

	t.Log("It sets resolution failure status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
	require.Equal(t, ctrl.Result{}, res)
	require.EqualError(t, err, fmt.Sprintf("no package %q matching version %q found in channel %q", pkgName, pkgVer, pkgChan))

	t.Log("By fetching updated operator after reconcile")
	require.NoError(t, cl.Get(ctx, opKey, operator))

	t.Log("By checking the status fields")
	require.Empty(t, operator.Status.ResolvedBundleResource)
	require.Empty(t, operator.Status.InstalledBundleResource)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonResolutionFailed, cond.Reason)
	require.Equal(t, fmt.Sprintf("no package %q matching version %q found in channel %q", pkgName, pkgVer, pkgChan), cond.Message)
	cond = apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "installation has not been attempted as resolution failed", cond.Message)

	verifyInvariants(ctx, t, reconciler.Client, operator)
	require.NoError(t, cl.DeleteAllOf(ctx, &operatorsv1alpha1.Operator{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha1.BundleDeployment{}))
}

func TestOperatorPlainV0Bundle(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	opKey := types.NamespacedName{Name: fmt.Sprintf("operator-test-%s", rand.String(8))}

	t.Log("When the operator specifies a package with a plain+v0 bundle")
	t.Log("By initializing cluster state")
	pkgName := "plain"
	pkgVer := "0.1.0"
	pkgChan := "beta"
	operator := &operatorsv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
		Spec: operatorsv1alpha1.OperatorSpec{
			PackageName: pkgName,
			Version:     pkgVer,
			Channel:     pkgChan,
		},
	}
	require.NoError(t, cl.Create(ctx, operator))

	t.Log("It sets resolution success status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching updated operator after reconcile")
	require.NoError(t, cl.Get(ctx, opKey, operator))

	t.Log("By checking the status fields")
	require.Equal(t, "quay.io/operatorhub/plain@sha256:plain", operator.Status.ResolvedBundleResource)
	require.Empty(t, operator.Status.InstalledBundleResource)
	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhub/plain@sha256:plain\"", cond.Message)
	cond = apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "bundledeployment status is unknown", cond.Message)

	t.Log("By fetching the bundled deployment")
	bd := &rukpakv1alpha1.BundleDeployment{}
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: opKey.Name}, bd))
	require.Equal(t, "core-rukpak-io-plain", bd.Spec.ProvisionerClassName)
	require.Equal(t, "core-rukpak-io-plain", bd.Spec.Template.Spec.ProvisionerClassName)
	require.Equal(t, rukpakv1alpha1.SourceTypeImage, bd.Spec.Template.Spec.Source.Type)
	require.NotNil(t, bd.Spec.Template.Spec.Source.Image)
	require.Equal(t, "quay.io/operatorhub/plain@sha256:plain", bd.Spec.Template.Spec.Source.Image.Ref)

	verifyInvariants(ctx, t, reconciler.Client, operator)
	require.NoError(t, cl.DeleteAllOf(ctx, &operatorsv1alpha1.Operator{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha1.BundleDeployment{}))
}

func TestOperatorBadBundleMediaType(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	opKey := types.NamespacedName{Name: fmt.Sprintf("operator-test-%s", rand.String(8))}

	t.Log("When the operator specifies a package with a bad bundle mediatype")
	t.Log("By initializing cluster state")
	pkgName := "badmedia"
	pkgVer := "0.1.0"
	pkgChan := "beta"
	operator := &operatorsv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
		Spec: operatorsv1alpha1.OperatorSpec{
			PackageName: pkgName,
			Version:     pkgVer,
			Channel:     pkgChan,
		},
	}
	require.NoError(t, cl.Create(ctx, operator))

	t.Log("It sets resolution success status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
	require.Equal(t, ctrl.Result{}, res)
	require.Error(t, err)
	require.ErrorContains(t, err, "unknown bundle mediatype: badmedia+v1")

	t.Log("By fetching updated operator after reconcile")
	require.NoError(t, cl.Get(ctx, opKey, operator))

	t.Log("By checking the status fields")
	require.Equal(t, "quay.io/operatorhub/badmedia@sha256:badmedia", operator.Status.ResolvedBundleResource)
	require.Empty(t, operator.Status.InstalledBundleResource)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhub/badmedia@sha256:badmedia\"", cond.Message)
	cond = apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonInstallationFailed, cond.Reason)
	require.Equal(t, "unknown bundle mediatype: badmedia+v1", cond.Message)

	verifyInvariants(ctx, t, reconciler.Client, operator)
	require.NoError(t, cl.DeleteAllOf(ctx, &operatorsv1alpha1.Operator{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha1.BundleDeployment{}))
}

func TestOperatorInvalidSemverPastRegex(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()
	t.Log("When an invalid semver is provided that bypasses the regex validation")
	opKey := types.NamespacedName{Name: fmt.Sprintf("operator-validation-test-%s", rand.String(8))}

	t.Log("By injecting creating a client with the bad operator CR")
	pkgName := fmt.Sprintf("exists-%s", rand.String(6))
	operator := &operatorsv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
		Spec: operatorsv1alpha1.OperatorSpec{
			PackageName: pkgName,
			Version:     "1.2.3-123abc_def", // bad semver that matches the regex on the CR validation
		},
	}

	// this bypasses client/server-side CR validation and allows us to test the reconciler's validation
	fakeClient := fake.NewClientBuilder().WithScheme(sch).WithObjects(operator).WithStatusSubresource(operator).Build()

	t.Log("By changing the reconciler client to the fake client")
	reconciler.Client = fakeClient

	t.Log("It should add an invalid spec condition and *not* re-enqueue for reconciliation")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching updated operator after reconcile")
	require.NoError(t, fakeClient.Get(ctx, opKey, operator))

	t.Log("By checking the status fields")
	require.Empty(t, operator.Status.ResolvedBundleResource)
	require.Empty(t, operator.Status.InstalledBundleResource)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonResolutionUnknown, cond.Reason)
	require.Equal(t, "validation has not been attempted as spec is invalid", cond.Message)
	cond = apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, operatorsv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "installation has not been attempted as spec is invalid", cond.Message)

	verifyInvariants(ctx, t, reconciler.Client, operator)
	require.NoError(t, cl.DeleteAllOf(ctx, &operatorsv1alpha1.Operator{}))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha1.BundleDeployment{}))
}

func verifyInvariants(ctx context.Context, t *testing.T, c client.Client, op *operatorsv1alpha1.Operator) {
	key := client.ObjectKeyFromObject(op)
	require.NoError(t, c.Get(ctx, key, op))

	verifyConditionsInvariants(t, op)
}

func verifyConditionsInvariants(t *testing.T, op *operatorsv1alpha1.Operator) {
	// Expect that the operator's set of conditions contains all defined
	// condition types for the Operator API. Every reconcile should always
	// ensure every condition type's status/reason/message reflects the state
	// read during _this_ reconcile call.
	require.Len(t, op.Status.Conditions, len(conditionsets.ConditionTypes))
	for _, tt := range conditionsets.ConditionTypes {
		cond := apimeta.FindStatusCondition(op.Status.Conditions, tt)
		require.NotNil(t, cond)
		require.NotEmpty(t, cond.Status)
		require.Contains(t, conditionsets.ConditionReasons, cond.Reason)
		require.Equal(t, op.GetGeneration(), cond.ObservedGeneration)
	}
}

func TestOperatorUpgrade(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()

	t.Run("semver upgrade constraints enforcement of upgrades within major version", func(t *testing.T) {
		defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.ForceSemverUpgradeConstraints, true)()
		defer func() {
			require.NoError(t, cl.DeleteAllOf(ctx, &operatorsv1alpha1.Operator{}))
			require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha1.BundleDeployment{}))
		}()

		pkgName := "prometheus"
		pkgVer := "1.0.0"
		pkgChan := "beta"
		opKey := types.NamespacedName{Name: fmt.Sprintf("operator-test-%s", rand.String(8))}
		operator := &operatorsv1alpha1.Operator{
			ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
			Spec: operatorsv1alpha1.OperatorSpec{
				PackageName: pkgName,
				Version:     pkgVer,
				Channel:     pkgChan,
			},
		}
		// Create an operator
		err := cl.Create(ctx, operator)
		require.NoError(t, err)

		// Run reconcile
		res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
		require.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, res)

		// Refresh the operator after reconcile
		err = cl.Get(ctx, opKey, operator)
		require.NoError(t, err)

		// Checking the status fields
		assert.Equal(t, "quay.io/operatorhubio/prometheus@fake1.0.0", operator.Status.ResolvedBundleResource)

		// checking the expected conditions
		cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeResolved)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
		assert.Equal(t, operatorsv1alpha1.ReasonSuccess, cond.Reason)
		assert.Equal(t, `resolved to "quay.io/operatorhubio/prometheus@fake1.0.0"`, cond.Message)

		// Invalid update: can not go to the next major version
		operator.Spec.Version = "2.0.0"
		err = cl.Update(ctx, operator)
		require.NoError(t, err)

		// Run reconcile again
		res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
		require.Error(t, err)
		assert.Equal(t, ctrl.Result{}, res)

		// Refresh the operator after reconcile
		err = cl.Get(ctx, opKey, operator)
		require.NoError(t, err)

		// Checking the status fields
		// TODO: https://github.com/operator-framework/operator-controller/issues/320
		assert.Equal(t, "", operator.Status.ResolvedBundleResource)

		// checking the expected conditions
		cond = apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeResolved)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, operatorsv1alpha1.ReasonResolutionFailed, cond.Reason)
		assert.Contains(t, cond.Message, "constraints not satisfiable")
		assert.Regexp(t, "installed package prometheus requires at least one of fake-catalog-prometheus-operatorhub/prometheus/beta/1.2.0, fake-catalog-prometheus-operatorhub/prometheus/beta/1.0.1, fake-catalog-prometheus-operatorhub/prometheus/beta/1.0.0$", cond.Message)

		// Valid update skipping one version
		operator.Spec.Version = "1.2.0"
		err = cl.Update(ctx, operator)
		require.NoError(t, err)

		// Run reconcile again
		res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
		require.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, res)

		// Refresh the operator after reconcile
		err = cl.Get(ctx, opKey, operator)
		require.NoError(t, err)

		// Checking the status fields
		assert.Equal(t, "quay.io/operatorhubio/prometheus@fake1.2.0", operator.Status.ResolvedBundleResource)

		// checking the expected conditions
		cond = apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeResolved)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
		assert.Equal(t, operatorsv1alpha1.ReasonSuccess, cond.Reason)
		assert.Equal(t, `resolved to "quay.io/operatorhubio/prometheus@fake1.2.0"`, cond.Message)
	})

	t.Run("legacy semantics upgrade constraints enforcement", func(t *testing.T) {
		defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.ForceSemverUpgradeConstraints, false)()
		defer func() {
			require.NoError(t, cl.DeleteAllOf(ctx, &operatorsv1alpha1.Operator{}))
			require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha1.BundleDeployment{}))
		}()

		pkgName := "prometheus"
		pkgVer := "1.0.0"
		pkgChan := "beta"
		opKey := types.NamespacedName{Name: fmt.Sprintf("operator-test-%s", rand.String(8))}
		operator := &operatorsv1alpha1.Operator{
			ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
			Spec: operatorsv1alpha1.OperatorSpec{
				PackageName: pkgName,
				Version:     pkgVer,
				Channel:     pkgChan,
			},
		}
		// Create an operator
		err := cl.Create(ctx, operator)
		require.NoError(t, err)

		// Run reconcile
		res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
		require.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, res)

		// Refresh the operator after reconcile
		err = cl.Get(ctx, opKey, operator)
		require.NoError(t, err)

		// Checking the status fields
		assert.Equal(t, "quay.io/operatorhubio/prometheus@fake1.0.0", operator.Status.ResolvedBundleResource)

		// checking the expected conditions
		cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeResolved)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
		assert.Equal(t, operatorsv1alpha1.ReasonSuccess, cond.Reason)
		assert.Equal(t, `resolved to "quay.io/operatorhubio/prometheus@fake1.0.0"`, cond.Message)

		// Invalid update: can not upgrade by skipping a version in the replaces chain
		operator.Spec.Version = "1.2.0"
		err = cl.Update(ctx, operator)
		require.NoError(t, err)

		// Run reconcile again
		res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
		require.Error(t, err)
		assert.Equal(t, ctrl.Result{}, res)

		// Refresh the operator after reconcile
		err = cl.Get(ctx, opKey, operator)
		require.NoError(t, err)

		// Checking the status fields
		// TODO: https://github.com/operator-framework/operator-controller/issues/320
		assert.Equal(t, "", operator.Status.ResolvedBundleResource)

		// checking the expected conditions
		cond = apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeResolved)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, operatorsv1alpha1.ReasonResolutionFailed, cond.Reason)
		assert.Contains(t, cond.Message, "constraints not satisfiable")
		assert.Contains(t, cond.Message, "installed package prometheus requires at least one of fake-catalog-prometheus-operatorhub/prometheus/beta/1.0.1, fake-catalog-prometheus-operatorhub/prometheus/beta/1.0.0\n")

		// Valid update skipping one version
		operator.Spec.Version = "1.0.1"
		err = cl.Update(ctx, operator)
		require.NoError(t, err)

		// Run reconcile again
		res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
		require.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, res)

		// Refresh the operator after reconcile
		err = cl.Get(ctx, opKey, operator)
		require.NoError(t, err)

		// Checking the status fields
		assert.Equal(t, "quay.io/operatorhubio/prometheus@fake1.0.1", operator.Status.ResolvedBundleResource)

		// checking the expected conditions
		cond = apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeResolved)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
		assert.Equal(t, operatorsv1alpha1.ReasonSuccess, cond.Reason)
		assert.Equal(t, `resolved to "quay.io/operatorhubio/prometheus@fake1.0.1"`, cond.Message)
	})

	t.Run("ignore upgrade constraints", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			flagState bool
		}{
			{
				name:      "ForceSemverUpgradeConstraints feature gate enabled",
				flagState: true,
			},
			{
				name:      "ForceSemverUpgradeConstraints feature gate disabled",
				flagState: false,
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.ForceSemverUpgradeConstraints, tt.flagState)()
				defer func() {
					require.NoError(t, cl.DeleteAllOf(ctx, &operatorsv1alpha1.Operator{}))
					require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha1.BundleDeployment{}))
				}()

				opKey := types.NamespacedName{Name: fmt.Sprintf("operator-test-%s", rand.String(8))}
				operator := &operatorsv1alpha1.Operator{
					ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
					Spec: operatorsv1alpha1.OperatorSpec{
						PackageName:             "prometheus",
						Version:                 "1.0.0",
						Channel:                 "beta",
						UpgradeConstraintPolicy: operatorsv1alpha1.UpgradeConstraintPolicyIgnore,
					},
				}
				// Create an operator
				err := cl.Create(ctx, operator)
				require.NoError(t, err)

				// Run reconcile
				res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
				require.NoError(t, err)
				assert.Equal(t, ctrl.Result{}, res)

				// Refresh the operator after reconcile
				err = cl.Get(ctx, opKey, operator)
				require.NoError(t, err)

				// Checking the status fields
				assert.Equal(t, "quay.io/operatorhubio/prometheus@fake1.0.0", operator.Status.ResolvedBundleResource)

				// checking the expected conditions
				cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeResolved)
				require.NotNil(t, cond)
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
				assert.Equal(t, operatorsv1alpha1.ReasonSuccess, cond.Reason)
				assert.Equal(t, `resolved to "quay.io/operatorhubio/prometheus@fake1.0.0"`, cond.Message)

				// We can go to the next major version when using semver
				// as well as to the version which is not next in the channel
				// when using legacy constraints
				operator.Spec.Version = "2.0.0"
				err = cl.Update(ctx, operator)
				require.NoError(t, err)

				// Run reconcile again
				res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
				require.NoError(t, err)
				assert.Equal(t, ctrl.Result{}, res)

				// Refresh the operator after reconcile
				err = cl.Get(ctx, opKey, operator)
				require.NoError(t, err)

				// Checking the status fields
				assert.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", operator.Status.ResolvedBundleResource)

				// checking the expected conditions
				cond = apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeResolved)
				require.NotNil(t, cond)
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
				assert.Equal(t, operatorsv1alpha1.ReasonSuccess, cond.Reason)
				assert.Equal(t, `resolved to "quay.io/operatorhubio/prometheus@fake2.0.0"`, cond.Message)
			})
		}
	})
}

func TestOperatorDowngrade(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	ctx := context.Background()

	t.Run("enforce upgrade constraints", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			flagState bool
		}{
			{
				name:      "ForceSemverUpgradeConstraints feature gate enabled",
				flagState: true,
			},
			{
				name:      "ForceSemverUpgradeConstraints feature gate disabled",
				flagState: false,
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.ForceSemverUpgradeConstraints, tt.flagState)()
				defer func() {
					require.NoError(t, cl.DeleteAllOf(ctx, &operatorsv1alpha1.Operator{}))
					require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha1.BundleDeployment{}))
				}()

				opKey := types.NamespacedName{Name: fmt.Sprintf("operator-test-%s", rand.String(8))}
				operator := &operatorsv1alpha1.Operator{
					ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
					Spec: operatorsv1alpha1.OperatorSpec{
						PackageName: "prometheus",
						Version:     "1.0.1",
						Channel:     "beta",
					},
				}
				// Create an operator
				err := cl.Create(ctx, operator)
				require.NoError(t, err)

				// Run reconcile
				res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
				require.NoError(t, err)
				assert.Equal(t, ctrl.Result{}, res)

				// Refresh the operator after reconcile
				err = cl.Get(ctx, opKey, operator)
				require.NoError(t, err)

				// Checking the status fields
				assert.Equal(t, "quay.io/operatorhubio/prometheus@fake1.0.1", operator.Status.ResolvedBundleResource)

				// checking the expected conditions
				cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeResolved)
				require.NotNil(t, cond)
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
				assert.Equal(t, operatorsv1alpha1.ReasonSuccess, cond.Reason)
				assert.Equal(t, `resolved to "quay.io/operatorhubio/prometheus@fake1.0.1"`, cond.Message)

				// Invalid operation: can not downgrade
				operator.Spec.Version = "1.0.0"
				err = cl.Update(ctx, operator)
				require.NoError(t, err)

				// Run reconcile again
				res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
				require.Error(t, err)
				assert.Equal(t, ctrl.Result{}, res)

				// Refresh the operator after reconcile
				err = cl.Get(ctx, opKey, operator)
				require.NoError(t, err)

				// Checking the status fields
				// TODO: https://github.com/operator-framework/operator-controller/issues/320
				assert.Equal(t, "", operator.Status.ResolvedBundleResource)

				// checking the expected conditions
				cond = apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeResolved)
				require.NotNil(t, cond)
				assert.Equal(t, metav1.ConditionFalse, cond.Status)
				assert.Equal(t, operatorsv1alpha1.ReasonResolutionFailed, cond.Reason)
				assert.Contains(t, cond.Message, "constraints not satisfiable")
				assert.Contains(t, cond.Message, "installed package prometheus requires at least one of fake-catalog-prometheus-operatorhub/prometheus/beta/1.2.0, fake-catalog-prometheus-operatorhub/prometheus/beta/1.0.1\n")
			})
		}
	})

	t.Run("ignore upgrade constraints", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			flagState bool
		}{
			{
				name:      "ForceSemverUpgradeConstraints feature gate enabled",
				flagState: true,
			},
			{
				name:      "ForceSemverUpgradeConstraints feature gate disabled",
				flagState: false,
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.ForceSemverUpgradeConstraints, tt.flagState)()
				defer func() {
					require.NoError(t, cl.DeleteAllOf(ctx, &operatorsv1alpha1.Operator{}))
					require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha1.BundleDeployment{}))
				}()

				opKey := types.NamespacedName{Name: fmt.Sprintf("operator-test-%s", rand.String(8))}
				operator := &operatorsv1alpha1.Operator{
					ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
					Spec: operatorsv1alpha1.OperatorSpec{
						PackageName:             "prometheus",
						Version:                 "2.0.0",
						Channel:                 "beta",
						UpgradeConstraintPolicy: operatorsv1alpha1.UpgradeConstraintPolicyIgnore,
					},
				}
				// Create an operator
				err := cl.Create(ctx, operator)
				require.NoError(t, err)

				// Run reconcile
				res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
				require.NoError(t, err)
				assert.Equal(t, ctrl.Result{}, res)

				// Refresh the operator after reconcile
				err = cl.Get(ctx, opKey, operator)
				require.NoError(t, err)

				// Checking the status fields
				assert.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", operator.Status.ResolvedBundleResource)

				// checking the expected conditions
				cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeResolved)
				require.NotNil(t, cond)
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
				assert.Equal(t, operatorsv1alpha1.ReasonSuccess, cond.Reason)
				assert.Equal(t, `resolved to "quay.io/operatorhubio/prometheus@fake2.0.0"`, cond.Message)

				// We downgrade
				operator.Spec.Version = "1.0.0"
				err = cl.Update(ctx, operator)
				require.NoError(t, err)

				// Run reconcile again
				res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
				require.NoError(t, err)
				assert.Equal(t, ctrl.Result{}, res)

				// Refresh the operator after reconcile
				err = cl.Get(ctx, opKey, operator)
				require.NoError(t, err)

				// Checking the status fields
				assert.Equal(t, "quay.io/operatorhubio/prometheus@fake1.0.0", operator.Status.ResolvedBundleResource)

				// checking the expected conditions
				cond = apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeResolved)
				require.NotNil(t, cond)
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
				assert.Equal(t, operatorsv1alpha1.ReasonSuccess, cond.Reason)
				assert.Equal(t, `resolved to "quay.io/operatorhubio/prometheus@fake1.0.0"`, cond.Message)
			})
		}
	})
}

var (
	prometheusAlphaChannel = catalogmetadata.Channel{
		Channel: declcfg.Channel{
			Name:    "alpha",
			Package: "prometheus",
		},
	}
	prometheusBetaChannel = catalogmetadata.Channel{
		Channel: declcfg.Channel{
			Name:    "beta",
			Package: "prometheus",
			Entries: []declcfg.ChannelEntry{
				{
					Name: "operatorhub/prometheus/beta/1.0.0",
				},
				{
					Name:     "operatorhub/prometheus/beta/1.0.1",
					Replaces: "operatorhub/prometheus/beta/1.0.0",
				},
				{
					Name:     "operatorhub/prometheus/beta/1.2.0",
					Replaces: "operatorhub/prometheus/beta/1.0.1",
				},
				{
					Name:     "operatorhub/prometheus/beta/2.0.0",
					Replaces: "operatorhub/prometheus/beta/1.2.0",
				},
			},
		},
	}
	plainBetaChannel = catalogmetadata.Channel{
		Channel: declcfg.Channel{
			Name:    "beta",
			Package: "plain",
		},
	}
	badmediaBetaChannel = catalogmetadata.Channel{
		Channel: declcfg.Channel{
			Name:    "beta",
			Package: "badmedia",
		},
	}
)

var testBundleList = []*catalogmetadata.Bundle{
	{
		Bundle: declcfg.Bundle{
			Name:    "operatorhub/prometheus/alpha/0.37.0",
			Package: "prometheus",
			Image:   "quay.io/operatorhubio/prometheus@sha256:3e281e587de3d03011440685fc4fb782672beab044c1ebadc42788ce05a21c35",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName":"prometheus","version":"0.37.0"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[]`)},
			},
		},
		CatalogName: "fake-catalog",
		InChannels:  []*catalogmetadata.Channel{&prometheusAlphaChannel},
	},
	{
		Bundle: declcfg.Bundle{
			Name:    "operatorhub/prometheus/beta/1.0.0",
			Package: "prometheus",
			Image:   "quay.io/operatorhubio/prometheus@fake1.0.0",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName":"prometheus","version":"1.0.0"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[]`)},
			},
		},
		CatalogName: "fake-catalog",
		InChannels:  []*catalogmetadata.Channel{&prometheusBetaChannel},
	},
	{
		Bundle: declcfg.Bundle{
			Name:    "operatorhub/prometheus/beta/1.0.1",
			Package: "prometheus",
			Image:   "quay.io/operatorhubio/prometheus@fake1.0.1",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName":"prometheus","version":"1.0.1"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[]`)},
			},
		},
		CatalogName: "fake-catalog",
		InChannels:  []*catalogmetadata.Channel{&prometheusBetaChannel},
	},
	{
		Bundle: declcfg.Bundle{
			Name:    "operatorhub/prometheus/beta/1.2.0",
			Package: "prometheus",
			Image:   "quay.io/operatorhubio/prometheus@fake1.2.0",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName":"prometheus","version":"1.2.0"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[]`)},
			},
		},
		CatalogName: "fake-catalog",
		InChannels:  []*catalogmetadata.Channel{&prometheusBetaChannel},
	},
	{
		Bundle: declcfg.Bundle{
			Name:    "operatorhub/prometheus/beta/2.0.0",
			Package: "prometheus",
			Image:   "quay.io/operatorhubio/prometheus@fake2.0.0",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName":"prometheus","version":"2.0.0"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[]`)},
			},
		},
		CatalogName: "fake-catalog",
		InChannels:  []*catalogmetadata.Channel{&prometheusBetaChannel},
	},
	{
		Bundle: declcfg.Bundle{
			Name:    "operatorhub/plain/0.1.0",
			Package: "plain",
			Image:   "quay.io/operatorhub/plain@sha256:plain",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName":"plain","version":"0.1.0"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[]`)},
				{Type: "olm.bundle.mediatype", Value: json.RawMessage(`"plain+v0"`)},
			},
		},
		CatalogName: "fake-catalog",
		InChannels:  []*catalogmetadata.Channel{&plainBetaChannel},
	},
	{
		Bundle: declcfg.Bundle{
			Name:    "operatorhub/badmedia/0.1.0",
			Package: "badmedia",
			Image:   "quay.io/operatorhub/badmedia@sha256:badmedia",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName":"badmedia","version":"0.1.0"}`)},
				{Type: property.TypeGVK, Value: json.RawMessage(`[]`)},
				{Type: "olm.bundle.mediatype", Value: json.RawMessage(`"badmedia+v1"`)},
			},
		},
		CatalogName: "fake-catalog",
		InChannels:  []*catalogmetadata.Channel{&badmediaBetaChannel},
	},
}
