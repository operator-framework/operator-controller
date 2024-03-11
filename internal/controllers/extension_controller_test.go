package controllers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kappctrlv1alpha1 "github.com/vmware-tanzu/carvel-kapp-controller/pkg/apis/kappctrl/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	"github.com/operator-framework/operator-controller/internal/conditionsets"
	"github.com/operator-framework/operator-controller/pkg/features"
)

const (
	testServiceAccount = "test-sa"
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

func TestExtensionReconcileFeatureGateDisabled(t *testing.T) {
	c, reconciler := newClientAndExtensionReconciler(t)
	ctx := context.Background()

	testCases := []struct {
		name               string
		featureGateEnabled bool
		paused             bool
		assert             func(*testing.T, ctrl.Result, error, *ocv1alpha1.Extension)
	}{
		{"feature gate disabled", false, false, func(t *testing.T, res ctrl.Result, err error, ext *ocv1alpha1.Extension) {
			assert.Equal(t, ctrl.Result{}, res)
			assert.NoError(t, err)
			verifyExtensionInvariants(t, ext)
			assert.Empty(t, ext.Status.InstalledBundleResource)
			assert.Empty(t, ext.Status.ResolvedBundleResource)
			for _, cond := range ext.Status.Conditions {
				assert.Equal(t, metav1.ConditionUnknown, cond.Status)
				assert.Equal(t, "extension feature is disabled", cond.Message)
			}
		}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			extKey := types.NamespacedName{Name: fmt.Sprintf("extension-test-%s", rand.String(8)), Namespace: "default"}
			ext := &ocv1alpha1.Extension{
				ObjectMeta: metav1.ObjectMeta{Name: extKey.Name, Namespace: extKey.Namespace},
				Spec: ocv1alpha1.ExtensionSpec{
					Paused:             tc.paused,
					ServiceAccountName: testServiceAccount,
					Source:             ocv1alpha1.ExtensionSource{SourceType: ocv1alpha1.SourceTypePackage, Package: &ocv1alpha1.ExtensionSourcePackage{Name: "test-package"}},
				},
			}
			require.NoError(t, c.Create(ctx, ext))

			defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.EnableExtensionAPI, tc.featureGateEnabled)()
			res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})

			require.NoError(t, c.Get(ctx, extKey, ext))
			tc.assert(t, res, err, ext)
		})
	}
}

func TestExtensionNonExistentPackage(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.EnableExtensionAPI, true)()

	cl, reconciler := newClientAndExtensionReconciler(t)
	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("extension-test-%s", rand.String(8)), Namespace: fmt.Sprintf("test-namespace-%s", rand.String(8))}

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: extKey.Namespace}}
	t.Log("Create namespace for extension")
	require.NoError(t, cl.Create(ctx, ns))
	defer func() {
		t.Logf("Cleaning up namespace %s", extKey.Namespace)
		require.NoError(t, cl.Delete(ctx, ns))
	}()

	t.Log("When the cluster extension specifies a non-existent package")
	t.Log("By initializing cluster state")
	pkgName := fmt.Sprintf("non-existent-%s", rand.String(6))
	extension := &ocv1alpha1.Extension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name, Namespace: extKey.Namespace},
		Spec: ocv1alpha1.ExtensionSpec{
			ServiceAccountName: "test-sa",
			Source: ocv1alpha1.ExtensionSource{
				SourceType: ocv1alpha1.SourceTypePackage,
				Package: &ocv1alpha1.ExtensionSourcePackage{
					Name: pkgName,
				},
			}},
	}
	require.NoError(t, cl.Create(ctx, extension))
	defer func() {
		t.Logf("Cleaning up extensions %s", extKey.Namespace)
		require.NoError(t, cl.Delete(ctx, extension))
	}()

	t.Log("It sets resolution failure status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.EqualError(t, err, fmt.Sprintf("no package %q found", pkgName))

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, extension))

	t.Log("By checking the status fields")
	require.Empty(t, extension.Status.ResolvedBundleResource)
	require.Empty(t, extension.Status.InstalledBundleResource)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(extension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonResolutionFailed, cond.Reason)
	require.Equal(t, fmt.Sprintf("no package %q found", pkgName), cond.Message)

	verifyExtensionInvariants(t, extension)
}

func TestExtensionNonExistentVersion(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.EnableExtensionAPI, true)()
	cl, reconciler := newClientAndExtensionReconciler(t)
	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("extension-test-%s", rand.String(8)), Namespace: fmt.Sprintf("test-namespace-%s", rand.String(8))}

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: extKey.Namespace}}
	t.Log("Create namespace for extension")
	require.NoError(t, cl.Create(ctx, ns))
	defer func() {
		t.Logf("Cleaning up namespace %s", extKey.Namespace)
		require.NoError(t, cl.Delete(ctx, ns))
	}()

	t.Log("When the extension specifies a version that does not exist")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	extension := &ocv1alpha1.Extension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name, Namespace: extKey.Namespace},
		Spec: ocv1alpha1.ExtensionSpec{
			ServiceAccountName: testServiceAccount,
			Source: ocv1alpha1.ExtensionSource{
				SourceType: ocv1alpha1.SourceTypePackage,
				Package: &ocv1alpha1.ExtensionSourcePackage{
					Name:    pkgName,
					Version: "0.50.0",
				},
			}},
	}
	require.NoError(t, cl.Create(ctx, extension))
	defer func() {
		t.Logf("Cleaning up extensions %s", extKey.Namespace)
		require.NoError(t, cl.Delete(ctx, extension))
	}()

	t.Log("It sets resolution failure status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.EqualError(t, err, fmt.Sprintf(`no package %q matching version "0.50.0" found`, pkgName))

	t.Log("By fetching updated extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, extension))

	t.Log("By checking the status fields")
	require.Empty(t, extension.Status.ResolvedBundleResource)
	require.Empty(t, extension.Status.InstalledBundleResource)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(extension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonResolutionFailed, cond.Reason)
	require.Equal(t, fmt.Sprintf(`no package %q matching version "0.50.0" found`, pkgName), cond.Message)
	cond = apimeta.FindStatusCondition(extension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "installation has not been attempted as resolution failed", cond.Message)

	verifyExtensionConditionsInvariants(t, extension)
}

func TestExtensionAppDoesNotExist(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.EnableExtensionAPI, true)()
	cl, reconciler := newClientAndExtensionReconciler(t)
	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("extension-test-%s", rand.String(8)), Namespace: fmt.Sprintf("test-namespace-%s", rand.String(8))}
	const pkgName = "prometheus"

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: extKey.Namespace}}
	t.Log("Create namespace for extension")
	require.NoError(t, cl.Create(ctx, ns))
	defer func() {
		t.Logf("Cleaning up namespace %s", extKey.Namespace)
		require.NoError(t, cl.Delete(ctx, ns))
	}()

	t.Log("When the extension specifies a valid available package")
	t.Log("By initializing cluster state")
	extension := &ocv1alpha1.Extension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name, Namespace: extKey.Namespace},
		Spec: ocv1alpha1.ExtensionSpec{
			ServiceAccountName: testServiceAccount,
			Source: ocv1alpha1.ExtensionSource{
				SourceType: ocv1alpha1.SourceTypePackage,
				Package: &ocv1alpha1.ExtensionSourcePackage{
					Name: pkgName,
				},
			}},
	}
	require.NoError(t, cl.Create(ctx, extension))
	defer func() {
		t.Logf("Cleaning up extensions %s", extKey.Namespace)
		require.NoError(t, cl.Delete(ctx, extension))
	}()

	t.Log("When the App does not exist")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, extension))

	t.Log("It results in the expected App")
	app := &kappctrlv1alpha1.App{}
	require.NoError(t, cl.Get(ctx, extKey, app))
	require.NotEmpty(t, app.Spec.Fetch)
	require.Len(t, app.Spec.Fetch, 1)
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", app.Spec.Fetch[0].Image.URL)

	t.Log("It sets the resolvedBundleResource status field")
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", extension.Status.ResolvedBundleResource)

	t.Log("It sets the InstalledBundleResource status field")
	require.Empty(t, extension.Status.InstalledBundleResource)

	t.Log("It sets the status on the cluster extension")
	cond := apimeta.FindStatusCondition(extension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)

	cond = apimeta.FindStatusCondition(extension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "install status unknown", cond.Message)

	verifyExtensionConditionsInvariants(t, extension)
}

func TestExtensionAppOutOfDate(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.EnableExtensionAPI, true)()
	cl, reconciler := newClientAndExtensionReconciler(t)
	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("extension-test-%s", rand.String(8)), Namespace: fmt.Sprintf("test-namespace-%s", rand.String(8))}
	const pkgName = "prometheus"

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: extKey.Namespace}}
	t.Log("Create namespace for extension")
	require.NoError(t, cl.Create(ctx, ns))
	defer func() {
		t.Logf("Cleaning up namespace %s", extKey.Namespace)
		require.NoError(t, cl.Delete(ctx, ns))
	}()

	t.Log("When the extension specifies a valid available package")
	t.Log("By initializing cluster state")
	extension := &ocv1alpha1.Extension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name, Namespace: extKey.Namespace},
		Spec: ocv1alpha1.ExtensionSpec{
			ServiceAccountName: testServiceAccount,
			Source: ocv1alpha1.ExtensionSource{
				SourceType: ocv1alpha1.SourceTypePackage,
				Package: &ocv1alpha1.ExtensionSourcePackage{
					Name: pkgName,
				},
			}},
	}
	require.NoError(t, cl.Create(ctx, extension))
	defer func() {
		t.Logf("Cleaning up extensions %s", extKey.Namespace)
		require.NoError(t, cl.Delete(ctx, extension))
	}()

	t.Log("When the expected App already exists")
	t.Log("When the App spec is out of date")
	t.Log("By patching the existing App")

	app := &kappctrlv1alpha1.App{
		ObjectMeta: metav1.ObjectMeta{
			Name:      extension.Name,
			Namespace: extension.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "kappctrl.k14s.io/v1alpha1",
					Kind:               "App",
					Name:               extension.Name,
					UID:                extension.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			},
		},
		Spec: kappctrlv1alpha1.AppSpec{
			ServiceAccountName: testServiceAccount,
			Fetch: []kappctrlv1alpha1.AppFetch{
				{
					Image: &kappctrlv1alpha1.AppFetchImage{
						URL: "quay.io/operatorhubio/prometheus@fake2.0.0",
					},
				},
			},
			Template: []kappctrlv1alpha1.AppTemplate{},
			Deploy: []kappctrlv1alpha1.AppDeploy{
				{
					Kapp: &kappctrlv1alpha1.AppDeployKapp{},
				},
			},
		},
	}

	t.Log("By modifying the App spec and creating the object")
	app.Spec.Fetch = []kappctrlv1alpha1.AppFetch{
		{
			Image: &kappctrlv1alpha1.AppFetchImage{
				URL: "quay.io/operatorhubio/prometheussomething@fake2.0.1",
			},
		},
	}
	require.NoError(t, cl.Create(ctx, app))

	t.Log("It results in the expected App")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching updated extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, extension))

	t.Log("By checking the expected App spec")
	app = &kappctrlv1alpha1.App{}
	require.NoError(t, cl.Get(ctx, extKey, app))
	require.NotEmpty(t, app.Spec.Fetch)
	require.Len(t, app.Spec.Fetch, 1)
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", app.Spec.Fetch[0].Image.URL)

	t.Log("By checking the status fields")
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", extension.Status.ResolvedBundleResource)
	require.Empty(t, extension.Status.InstalledBundleResource)

	t.Log("By checking the expected status conditions")
	cond := apimeta.FindStatusCondition(extension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)
	cond = apimeta.FindStatusCondition(extension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "install status unknown", cond.Message)

	verifyExtensionConditionsInvariants(t, extension)
}

func TestExtensionAppUpToDate(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.EnableExtensionAPI, true)()
	cl, reconciler := newClientAndExtensionReconciler(t)
	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("extension-test-%s", rand.String(8)), Namespace: fmt.Sprintf("test-namespace-%s", rand.String(8))}
	const pkgName = "prometheus"

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: extKey.Namespace}}
	t.Log("Create namespace for extension")
	require.NoError(t, cl.Create(ctx, ns))
	defer func() {
		t.Logf("Cleaning up namespace %s", extKey.Namespace)
		require.NoError(t, cl.Delete(ctx, ns))
	}()

	t.Log("When the extension specifies a valid available package")
	t.Log("By initializing cluster state")
	extension := &ocv1alpha1.Extension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name, Namespace: extKey.Namespace},
		Spec: ocv1alpha1.ExtensionSpec{
			ServiceAccountName: testServiceAccount,
			Source: ocv1alpha1.ExtensionSource{
				SourceType: ocv1alpha1.SourceTypePackage,
				Package: &ocv1alpha1.ExtensionSourcePackage{
					Name: pkgName,
				},
			}},
	}
	require.NoError(t, cl.Create(ctx, extension))
	defer func() {
		t.Logf("Cleaning up extensions %s", extKey.Namespace)
		require.NoError(t, cl.Delete(ctx, extension))
	}()

	t.Log("When the app already exists")
	t.Log("When the App spec is up-to-date")
	t.Log("By patching the existing App")

	app := &kappctrlv1alpha1.App{
		ObjectMeta: metav1.ObjectMeta{
			Name:      extension.Name,
			Namespace: extension.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "kappctrl.k14s.io/v1alpha1",
					Kind:               "App",
					Name:               extension.Name,
					UID:                extension.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			},
		},
		Spec: kappctrlv1alpha1.AppSpec{
			ServiceAccountName: testServiceAccount,
			Fetch: []kappctrlv1alpha1.AppFetch{
				{
					Image: &kappctrlv1alpha1.AppFetchImage{
						URL: "quay.io/operatorhubio/prometheus@fake2.0.0",
					},
				},
			},
			Template: []kappctrlv1alpha1.AppTemplate{},
			Deploy: []kappctrlv1alpha1.AppDeploy{
				{
					Kapp: &kappctrlv1alpha1.AppDeployKapp{},
				},
			},
		},
	}

	require.NoError(t, cl.Create(ctx, app))
	app.Status.ObservedGeneration = app.GetGeneration()

	t.Log("When the App status is mapped to the expected Extension status")
	t.Log("It verifies extension status when app is waiting to be created")
	t.Log("By updating the status of app")
	require.NoError(t, cl.Status().Update(ctx, app))

	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching the updated cluster extension after reconcile")
	ext := &ocv1alpha1.Extension{}
	require.NoError(t, cl.Get(ctx, extKey, ext))

	t.Log("By checking the status fields")
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", ext.Status.ResolvedBundleResource)
	require.Empty(t, ext.Status.InstalledBundleResource)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)
	cond = apimeta.FindStatusCondition(ext.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "install status unknown", cond.Message)

	// TODO: Add tests to verify the mapping of status between App and Extension.
	// Patch App's statuses and verify if the right one appears on the Extension.
}

func TestExtensionExpectedApp(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.EnableExtensionAPI, true)()
	cl, reconciler := newClientAndExtensionReconciler(t)
	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("extension-test-%s", rand.String(8)), Namespace: fmt.Sprintf("test-namespace-%s", rand.String(8))}
	const pkgName = "prometheus"

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: extKey.Namespace}}
	t.Log("Create namespace for extension")
	require.NoError(t, cl.Create(ctx, ns))
	defer func() {
		t.Logf("Cleaning up namespace %s", extKey.Namespace)
		require.NoError(t, cl.Delete(ctx, ns))
	}()

	t.Log("When the extension specifies a valid available package")
	t.Log("By initializing cluster state")
	extension := &ocv1alpha1.Extension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name, Namespace: extKey.Namespace},
		Spec: ocv1alpha1.ExtensionSpec{
			ServiceAccountName: testServiceAccount,
			Source: ocv1alpha1.ExtensionSource{
				SourceType: ocv1alpha1.SourceTypePackage,
				Package: &ocv1alpha1.ExtensionSourcePackage{
					Name: pkgName,
				},
			}},
	}
	require.NoError(t, cl.Create(ctx, extension))
	defer func() {
		t.Logf("Cleaning up extensions %s", extKey.Namespace)
		require.NoError(t, cl.Delete(ctx, extension))
	}()

	t.Log("When an out-of-date App exists")
	t.Log("By creating the expected App")
	app := &kappctrlv1alpha1.App{
		ObjectMeta: metav1.ObjectMeta{
			Name:      extension.Name,
			Namespace: extension.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "kappctrl.k14s.io/v1alpha1",
					Kind:               "App",
					Name:               extension.Name,
					UID:                extension.UID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			},
		},
		Spec: kappctrlv1alpha1.AppSpec{
			ServiceAccountName: testServiceAccount,
			Fetch: []kappctrlv1alpha1.AppFetch{
				{
					Image: &kappctrlv1alpha1.AppFetchImage{
						URL: "quay.io/operatorhubio/prometheus@fake2.0.0",
					},
				},
			},
			Template: []kappctrlv1alpha1.AppTemplate{},
			Deploy: []kappctrlv1alpha1.AppDeploy{
				{
					Kapp: &kappctrlv1alpha1.AppDeployKapp{},
				},
			},
		},
	}
	require.NoError(t, cl.Create(ctx, app))

	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching updated extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, extension))

	t.Log("It results in the expected App")
	app = &kappctrlv1alpha1.App{}
	require.NoError(t, cl.Get(ctx, extKey, app))
	require.NotEmpty(t, app.Spec.Fetch)
	require.Len(t, app.Spec.Fetch, 1)
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", app.Spec.Fetch[0].Image.URL)

	t.Log("It sets the resolvedBundleResource status field")
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", extension.Status.ResolvedBundleResource)

	t.Log("It sets the InstalledBundleResource status field")
	require.Empty(t, extension.Status.InstalledBundleResource)

	t.Log("It sets resolution to unknown status")
	cond := apimeta.FindStatusCondition(extension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)
	cond = apimeta.FindStatusCondition(extension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "install status unknown", cond.Message)

	verifyExtensionConditionsInvariants(t, extension)
}

func TestExtensionDuplicatePackage(t *testing.T) {
	t.Skip("Include this test after resolution logic is modified to not contain duplicate packages.")

	defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.EnableExtensionAPI, true)()
	cl, reconciler := newClientAndExtensionReconciler(t)
	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("extension-test-%s", rand.String(8)), Namespace: fmt.Sprintf("test-namespace-%s", rand.String(8))}
	const pkgName = "prometheus"

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: extKey.Namespace}}
	t.Log("Create namespace for extension")
	require.NoError(t, cl.Create(ctx, ns))
	defer func() {
		t.Logf("Cleaning up namespace %s", extKey.Namespace)
		require.NoError(t, cl.Delete(ctx, ns))
	}()

	t.Log("When the extension specifies a duplicate package")
	t.Log("By initializing cluster state")
	dupExtension := &ocv1alpha1.Extension{
		ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("orig-%s", extKey.Name), Namespace: extKey.Namespace},
		Spec: ocv1alpha1.ExtensionSpec{
			ServiceAccountName: testServiceAccount,
			Source: ocv1alpha1.ExtensionSource{
				SourceType: ocv1alpha1.SourceTypePackage,
				Package: &ocv1alpha1.ExtensionSourcePackage{
					Name: pkgName,
				},
			}},
	}
	require.NoError(t, cl.Create(ctx, dupExtension))
	defer func() {
		t.Logf("Cleaning up extensions %s", extKey.Namespace)
		require.NoError(t, cl.Delete(ctx, dupExtension))
	}()

	extension := &ocv1alpha1.Extension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name, Namespace: extKey.Namespace},
		Spec: ocv1alpha1.ExtensionSpec{
			ServiceAccountName: testServiceAccount,
			Source: ocv1alpha1.ExtensionSource{
				SourceType: ocv1alpha1.SourceTypePackage,
				Package: &ocv1alpha1.ExtensionSourcePackage{
					Name: pkgName,
				},
			}},
	}
	require.NoError(t, cl.Create(ctx, extension))
	defer func() {
		t.Logf("Cleaning up extensions %s", extKey.Namespace)
		require.NoError(t, cl.Delete(ctx, extension))
	}()

	t.Log("It sets resolution failure status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.EqualError(t, err, `duplicate identifier "required package prometheus" in input`)
}

func TestExtensionChannelVersionExists(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.EnableExtensionAPI, true)()
	cl, reconciler := newClientAndExtensionReconciler(t)
	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("extension-test-%s", rand.String(8)), Namespace: fmt.Sprintf("test-namespace-%s", rand.String(8))}

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: extKey.Namespace}}
	t.Log("Create namespace for extension")
	require.NoError(t, cl.Create(ctx, ns))
	defer func() {
		t.Logf("Cleaning up namespace %s", extKey.Namespace)
		require.NoError(t, cl.Delete(ctx, ns))
	}()

	t.Log("When the extension specifies a channel with version that exist")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgVer := "1.0.0"
	pkgChan := "beta"
	extension := &ocv1alpha1.Extension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name, Namespace: extKey.Namespace},
		Spec: ocv1alpha1.ExtensionSpec{
			ServiceAccountName: testServiceAccount,
			Source: ocv1alpha1.ExtensionSource{
				SourceType: ocv1alpha1.SourceTypePackage,
				Package: &ocv1alpha1.ExtensionSourcePackage{
					Name:    pkgName,
					Version: pkgVer,
					Channel: pkgChan,
				},
			}},
	}
	require.NoError(t, cl.Create(ctx, extension))
	defer func() {
		t.Logf("Cleaning up extensions %s", extKey.Namespace)
		require.NoError(t, cl.Delete(ctx, extension))
	}()

	t.Log("It sets resolution success status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching updated extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, extension))

	t.Log("By checking the status fields")
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake1.0.0", extension.Status.ResolvedBundleResource)
	require.Empty(t, extension.Status.InstalledBundleResource)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(extension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake1.0.0\"", cond.Message)
	cond = apimeta.FindStatusCondition(extension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "install status unknown", cond.Message)

	t.Log("By fetching the App")
	app := &kappctrlv1alpha1.App{}
	require.NoError(t, cl.Get(ctx, extKey, app))
	require.NotEmpty(t, app.Spec.Fetch)
	require.Len(t, app.Spec.Fetch, 1)
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake1.0.0", app.Spec.Fetch[0].Image.URL)

	verifyExtensionConditionsInvariants(t, extension)
}

func TestExtensionChannelExistsNoVersion(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.EnableExtensionAPI, true)()
	cl, reconciler := newClientAndExtensionReconciler(t)
	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("extension-test-%s", rand.String(8)), Namespace: fmt.Sprintf("test-namespace-%s", rand.String(8))}

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: extKey.Namespace}}
	t.Log("Create namespace for extension")
	require.NoError(t, cl.Create(ctx, ns))
	defer func() {
		t.Logf("Cleaning up namespace %s", extKey.Namespace)
		require.NoError(t, cl.Delete(ctx, ns))
	}()

	t.Log("When the extension specifies a package that exists within a channel but no version specified")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgVer := ""
	pkgChan := "beta"
	extension := &ocv1alpha1.Extension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name, Namespace: extKey.Namespace},
		Spec: ocv1alpha1.ExtensionSpec{
			ServiceAccountName: testServiceAccount,
			Source: ocv1alpha1.ExtensionSource{
				SourceType: ocv1alpha1.SourceTypePackage,
				Package: &ocv1alpha1.ExtensionSourcePackage{
					Name:    pkgName,
					Version: pkgVer,
					Channel: pkgChan,
				},
			}},
	}
	require.NoError(t, cl.Create(ctx, extension))
	defer func() {
		t.Logf("Cleaning up extensions %s", extKey.Namespace)
		require.NoError(t, cl.Delete(ctx, extension))
	}()

	t.Log("It sets resolution success status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)
	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, extension))

	t.Log("By checking the status fields")
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", extension.Status.ResolvedBundleResource)
	require.Empty(t, extension.Status.InstalledBundleResource)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(extension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, cond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake2.0.0\"", cond.Message)
	cond = apimeta.FindStatusCondition(extension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "install status unknown", cond.Message)

	t.Log("By fetching the App")
	app := &kappctrlv1alpha1.App{}
	require.NoError(t, cl.Get(ctx, extKey, app))
	require.NotEmpty(t, app.Spec.Fetch)
	require.Len(t, app.Spec.Fetch, 1)
	require.Equal(t, "quay.io/operatorhubio/prometheus@fake2.0.0", app.Spec.Fetch[0].Image.URL)

	verifyExtensionConditionsInvariants(t, extension)
}

func TestExtensionVersionNoChannel(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.EnableExtensionAPI, true)()
	cl, reconciler := newClientAndExtensionReconciler(t)
	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("extension-test-%s", rand.String(8)), Namespace: fmt.Sprintf("test-namespace-%s", rand.String(8))}

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: extKey.Namespace}}
	t.Log("Create namespace for extension")
	require.NoError(t, cl.Create(ctx, ns))
	defer func() {
		t.Logf("Cleaning up namespace %s", extKey.Namespace)
		require.NoError(t, cl.Delete(ctx, ns))
	}()

	t.Log("When the extension specifies a package version in a channel that does not exist")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgVer := "0.47.0"
	pkgChan := "alpha"
	extension := &ocv1alpha1.Extension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name, Namespace: extKey.Namespace},
		Spec: ocv1alpha1.ExtensionSpec{
			ServiceAccountName: testServiceAccount,
			Source: ocv1alpha1.ExtensionSource{
				SourceType: ocv1alpha1.SourceTypePackage,
				Package: &ocv1alpha1.ExtensionSourcePackage{
					Name:    pkgName,
					Version: pkgVer,
					Channel: pkgChan,
				},
			}},
	}
	require.NoError(t, cl.Create(ctx, extension))
	defer func() {
		t.Logf("Cleaning up extensions %s", extKey.Namespace)
		require.NoError(t, cl.Delete(ctx, extension))
	}()

	t.Log("It sets resolution failure status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.EqualError(t, err, fmt.Sprintf("no package %q matching version %q found in channel %q", pkgName, pkgVer, pkgChan))

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, extension))

	t.Log("By checking the status fields")
	require.Empty(t, extension.Status.ResolvedBundleResource)
	require.Empty(t, extension.Status.InstalledBundleResource)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(extension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonResolutionFailed, cond.Reason)
	require.Equal(t, fmt.Sprintf("no package %q matching version %q found in channel %q", pkgName, pkgVer, pkgChan), cond.Message)
	cond = apimeta.FindStatusCondition(extension.Status.Conditions, ocv1alpha1.TypeInstalled)

	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "installation has not been attempted as resolution failed", cond.Message)

	verifyExtensionConditionsInvariants(t, extension)
}

func TestExtensionNoChannel(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.EnableExtensionAPI, true)()
	cl, reconciler := newClientAndExtensionReconciler(t)
	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("extension-test-%s", rand.String(8)), Namespace: fmt.Sprintf("test-namespace-%s", rand.String(8))}

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: extKey.Namespace}}
	t.Log("Create namespace for extension")
	require.NoError(t, cl.Create(ctx, ns))
	defer func() {
		t.Logf("Cleaning up namespace %s", extKey.Namespace)
		require.NoError(t, cl.Delete(ctx, ns))
	}()

	t.Log("When the extension specifies a package in a channel that does not exist")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgChan := "non-existent"
	extension := &ocv1alpha1.Extension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name, Namespace: extKey.Namespace},
		Spec: ocv1alpha1.ExtensionSpec{
			ServiceAccountName: testServiceAccount,
			Source: ocv1alpha1.ExtensionSource{
				SourceType: ocv1alpha1.SourceTypePackage,
				Package: &ocv1alpha1.ExtensionSourcePackage{
					Name:    pkgName,
					Channel: pkgChan,
				},
			}},
	}
	require.NoError(t, cl.Create(ctx, extension))
	defer func() {
		t.Logf("Cleaning up extensions %s", extKey.Namespace)
		require.NoError(t, cl.Delete(ctx, extension))
	}()

	t.Log("It sets resolution failure status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.EqualError(t, err, fmt.Sprintf("no package %q found in channel %q", pkgName, pkgChan))

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, extension))

	t.Log("By checking the status fields")
	require.Empty(t, extension.Status.ResolvedBundleResource)
	require.Empty(t, extension.Status.InstalledBundleResource)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(extension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonResolutionFailed, cond.Reason)
	require.Equal(t, fmt.Sprintf("no package %q found in channel %q", pkgName, pkgChan), cond.Message)
	cond = apimeta.FindStatusCondition(extension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "installation has not been attempted as resolution failed", cond.Message)

	verifyExtensionConditionsInvariants(t, extension)
}

func TestExtensionNoVersion(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.EnableExtensionAPI, true)()
	cl, reconciler := newClientAndExtensionReconciler(t)
	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("extension-test-%s", rand.String(8)), Namespace: fmt.Sprintf("test-namespace-%s", rand.String(8))}

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: extKey.Namespace}}
	t.Log("Create namespace for extension")
	require.NoError(t, cl.Create(ctx, ns))
	defer func() {
		t.Logf("Cleaning up namespace %s", extKey.Namespace)
		require.NoError(t, cl.Delete(ctx, ns))
	}()

	t.Log("When the extension specifies a package version that does not exist in the channel")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgVer := "0.57.0"
	pkgChan := "non-existent"
	extension := &ocv1alpha1.Extension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name, Namespace: extKey.Namespace},
		Spec: ocv1alpha1.ExtensionSpec{
			ServiceAccountName: testServiceAccount,
			Source: ocv1alpha1.ExtensionSource{
				SourceType: ocv1alpha1.SourceTypePackage,
				Package: &ocv1alpha1.ExtensionSourcePackage{
					Name:    pkgName,
					Version: pkgVer,
					Channel: pkgChan,
				},
			}},
	}
	require.NoError(t, cl.Create(ctx, extension))
	defer func() {
		t.Logf("Cleaning up extensions %s", extKey.Namespace)
		require.NoError(t, cl.Delete(ctx, extension))
	}()

	t.Log("It sets resolution failure status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.EqualError(t, err, fmt.Sprintf("no package %q matching version %q found in channel %q", pkgName, pkgVer, pkgChan))

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, extension))

	t.Log("By checking the status fields")
	require.Empty(t, extension.Status.ResolvedBundleResource)
	require.Empty(t, extension.Status.InstalledBundleResource)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(extension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonResolutionFailed, cond.Reason)
	require.Equal(t, fmt.Sprintf("no package %q matching version %q found in channel %q", pkgName, pkgVer, pkgChan), cond.Message)
	cond = apimeta.FindStatusCondition(extension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationStatusUnknown, cond.Reason)
	require.Equal(t, "installation has not been attempted as resolution failed", cond.Message)

	verifyExtensionConditionsInvariants(t, extension)
}

func verifyExtensionInvariants(t *testing.T, ext *ocv1alpha1.Extension) {
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

// Using a separate list of bundles for test that do not break existing cluster extension tests.
var testBundleListForExtension = []*catalogmetadata.Bundle{
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
				{Type: "olm.bundle.mediatype", Value: json.RawMessage(`"plain+v0"`)},
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
				{Type: "olm.bundle.mediatype", Value: json.RawMessage(`"plain+v0"`)},
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
