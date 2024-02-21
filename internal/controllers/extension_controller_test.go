package controllers_test

import (
	"context"
	"fmt"
	"testing"

	rukpakv1alpha2 "github.com/operator-framework/rukpak/api/v1alpha2"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/conditionsets"
	"github.com/operator-framework/operator-controller/pkg/features"
)

// Describe: Extension Controller Test
func TestExtensionDoesNotExist(t *testing.T) {
	_, reconciler := newClientAndExtensionReconciler(t)

	t.Log("When the extension does not exist")
	t.Log("It returns no error")
	res, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "non-existent", Namespace: "non-existent"}})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)
}

func TestExtensionBadResources(t *testing.T) {
	cl, _ := newClientAndExtensionReconciler(t)
	ctx := context.Background()
	extKey := types.NamespacedName{
		Name:      fmt.Sprintf("extension-test-%s", rand.String(8)),
		Namespace: fmt.Sprintf("namespace-%s", rand.String(8)),
	}
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: extKey.Namespace,
		},
	}
	require.NoError(t, cl.Create(ctx, namespace))

	badExtensions := []ocv1alpha1.Extension{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "no-package-name", Namespace: extKey.Namespace},
			Spec: ocv1alpha1.ExtensionSpec{
				Source: ocv1alpha1.ExtensionSource{
					Package: &ocv1alpha1.ExtensionSourcePackage{
						Name: "",
					},
				},
				ServiceAccountName: "default",
			},
		}, {
			ObjectMeta: metav1.ObjectMeta{Name: "no-namespace"},
			Spec: ocv1alpha1.ExtensionSpec{
				Source: ocv1alpha1.ExtensionSource{
					Package: &ocv1alpha1.ExtensionSourcePackage{
						Name: fmt.Sprintf("non-existent-%s", rand.String(6)),
					},
				},
				ServiceAccountName: "default",
			},
		}, {
			ObjectMeta: metav1.ObjectMeta{Name: "no-service-account", Namespace: extKey.Namespace},
			Spec: ocv1alpha1.ExtensionSpec{
				Source: ocv1alpha1.ExtensionSource{
					Package: &ocv1alpha1.ExtensionSourcePackage{
						Name: fmt.Sprintf("non-existent-%s", rand.String(6)),
					},
				},
			},
		},
	}

	for _, e := range badExtensions {
		ext := e.DeepCopy()
		require.Error(t, cl.Create(ctx, ext), fmt.Sprintf("Failed on %q", e.ObjectMeta.GetName()))
	}

	invalidExtensions := []ocv1alpha1.Extension{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "no-source", Namespace: extKey.Namespace},
			Spec: ocv1alpha1.ExtensionSpec{
				ServiceAccountName: "default",
			},
		}, {
			ObjectMeta: metav1.ObjectMeta{Name: "no-package", Namespace: extKey.Namespace},
			Spec: ocv1alpha1.ExtensionSpec{
				Source:             ocv1alpha1.ExtensionSource{},
				ServiceAccountName: "default",
			},
		},
	}

	for _, e := range invalidExtensions {
		ext := e.DeepCopy()
		require.NoError(t, cl.Create(ctx, ext), fmt.Sprintf("Create failed on %q", e.GetObjectMeta().GetName()))
		name := types.NamespacedName{Name: e.GetObjectMeta().GetName(), Namespace: e.GetObjectMeta().GetNamespace()}

		cl, reconciler := newClientAndExtensionReconciler(t)
		res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: name})
		require.Equal(t, ctrl.Result{}, res)
		require.NoError(t, err)

		require.NoError(t, cl.Get(ctx, name, ext), fmt.Sprintf("Get failed on %q", e.ObjectMeta.GetName()))
		cond := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1alpha1.TypeResolved)
		require.NotNil(t, cond, fmt.Sprintf("Get condition failed on %q", ext.ObjectMeta.GetName()))
		require.Equal(t, metav1.ConditionUnknown, cond.Status, fmt.Sprintf("Get status check failed on %q", ext.ObjectMeta.GetName()))
		require.Equal(t, ocv1alpha1.ReasonResolutionUnknown, cond.Reason, fmt.Sprintf("Get status reason failed on %q", ext.ObjectMeta.GetName()))
		require.Equal(t, "extension feature is disabled", cond.Message)
		require.NoError(t, client.IgnoreNotFound(cl.Delete(ctx, ext)))
	}

	for _, e := range invalidExtensions {
		resetFeatureGate := featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.EnableExtensionAPI, true)
		ext := e.DeepCopy()
		require.NoError(t, cl.Create(ctx, ext), fmt.Sprintf("Create failed on %q", e.GetObjectMeta().GetName()))
		name := types.NamespacedName{Name: e.GetObjectMeta().GetName(), Namespace: e.GetObjectMeta().GetNamespace()}

		cl, reconciler := newClientAndExtensionReconciler(t)
		res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: name})
		require.Equal(t, ctrl.Result{}, res)
		require.NoError(t, err)

		require.NoError(t, cl.Get(ctx, name, ext), fmt.Sprintf("Get failed on %q", e.ObjectMeta.GetName()))
		cond := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1alpha1.TypeResolved)
		require.NotNil(t, cond, fmt.Sprintf("Get condition failed on %q", ext.ObjectMeta.GetName()))
		require.Equal(t, metav1.ConditionUnknown, cond.Status, fmt.Sprintf("Get status check failed on %q", ext.ObjectMeta.GetName()))
		require.Equal(t, ocv1alpha1.ReasonResolutionUnknown, cond.Reason, fmt.Sprintf("Get status reason failed on %q", ext.ObjectMeta.GetName()))
		require.Equal(t, "validation has not been attempted as spec is invalid", cond.Message)

		resetFeatureGate()
	}

	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.Extension{}, client.InNamespace(extKey.Namespace)))
	require.NoError(t, cl.Delete(ctx, namespace))
}

func TestExtensionInvalidSemverPastRegex(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.EnableExtensionAPI, true)()
	cl, reconciler := newClientAndExtensionReconciler(t)
	ctx := context.Background()
	t.Log("When an invalid semver is provided that bypasses the regex validation")
	extKey := types.NamespacedName{
		Name:      fmt.Sprintf("extension-test-%s", rand.String(8)),
		Namespace: fmt.Sprintf("namespace-%s", rand.String(8)),
	}
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: extKey.Namespace,
		},
	}
	require.NoError(t, cl.Create(ctx, namespace))

	t.Log("By injecting creating a client with the bad extension CR")
	pkgName := fmt.Sprintf("exists-%s", rand.String(6))
	extension := &ocv1alpha1.Extension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name, Namespace: extKey.Namespace},
		Spec: ocv1alpha1.ExtensionSpec{
			Source: ocv1alpha1.ExtensionSource{
				Package: &ocv1alpha1.ExtensionSourcePackage{
					Name:    pkgName,
					Version: "1.2.3-123abc_def", // bad semver that matches the regex on the CR validation
				},
			},
			ServiceAccountName: "default",
		},
	}

	// this bypasses client/server-side CR validation and allows us to test the reconciler's validation
	fakeClient := fake.NewClientBuilder().WithScheme(sch).WithObjects(extension).WithStatusSubresource(extension).Build()

	t.Log("By changing the reconciler client to the fake client")
	reconciler.Client = fakeClient

	t.Log("It should add an invalid spec condition and *not* re-enqueue for reconciliation")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching updated extension after reconcile")
	require.NoError(t, fakeClient.Get(ctx, extKey, extension))

	t.Log("By checking the status fields")
	require.Empty(t, extension.Status.ResolvedBundleResource)
	require.Empty(t, extension.Status.InstalledBundleResource)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(extension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonResolutionUnknown, cond.Reason)
	require.Equal(t, "validation has not been attempted as spec is invalid", cond.Message)
	cond = apimeta.FindStatusCondition(extension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "installation has not been attempted as spec is invalid", cond.Message)

	verifyExtensionInvariants(ctx, t, reconciler.Client, extension)
	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.Extension{}, client.InNamespace(extKey.Namespace)))
	require.NoError(t, cl.DeleteAllOf(ctx, &rukpakv1alpha2.BundleDeployment{}))
	require.NoError(t, cl.Delete(ctx, namespace))
}

func verifyExtensionInvariants(ctx context.Context, t *testing.T, c client.Client, ext *ocv1alpha1.Extension) {
	key := client.ObjectKeyFromObject(ext)
	require.NoError(t, c.Get(ctx, key, ext))

	verifyExtensionConditionsInvariants(t, ext)
}

func verifyExtensionConditionsInvariants(t *testing.T, ext *ocv1alpha1.Extension) {
	// Expect that the extension's set of conditions contains all defined
	// condition types for the Extension API. Every reconcile should always
	// ensure every condition type's status/reason/message reflects the state
	// read during _this_ reconcile call.
	require.Len(t, ext.Status.Conditions, len(conditionsets.ConditionTypes))
	for _, tt := range conditionsets.ConditionTypes {
		cond := apimeta.FindStatusCondition(ext.Status.Conditions, tt)
		require.NotNil(t, cond)
		require.NotEmpty(t, cond.Status)
		require.Contains(t, conditionsets.ConditionReasons, cond.Reason)
		require.Equal(t, ext.GetGeneration(), cond.ObservedGeneration)
	}
}
