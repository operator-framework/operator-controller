package controllers_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	carvelv1alpha1 "github.com/vmware-tanzu/carvel-kapp-controller/pkg/apis/kappctrl/v1alpha1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	ctrl "sigs.k8s.io/controller-runtime"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
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

func TestExtensionReconcile(t *testing.T) {
	t.Skip("Skipping this till kapp-controller test setup is implemented.")
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
			assert.Empty(t, ext.Status.InstalledBundle)
			assert.Empty(t, ext.Status.ResolvedBundle)
			for _, cond := range ext.Status.Conditions {
				assert.Equal(t, metav1.ConditionUnknown, cond.Status)
				assert.Equal(t, "extension feature is disabled", cond.Message)
			}
		}},
		{"feature gate enabled and paused", true, true, func(t *testing.T, res ctrl.Result, err error, ext *ocv1alpha1.Extension) {
			assert.Equal(t, ctrl.Result{}, res)
			assert.NoError(t, err)
			assert.Equal(t, ocv1alpha1.ExtensionStatus{Paused: true}, ext.Status)
		}},
		{"feature gate enabled and active", true, false, func(t *testing.T, res ctrl.Result, err error, ext *ocv1alpha1.Extension) {
			assert.Equal(t, ctrl.Result{}, res)
			assert.NoError(t, err)
			verifyExtensionInvariants(t, ext)
			assert.False(t, ext.Status.Paused)
			assert.Empty(t, ext.Status.InstalledBundle)
			assert.Empty(t, ext.Status.ResolvedBundle)
			for _, cond := range ext.Status.Conditions {
				assert.Equal(t, metav1.ConditionUnknown, cond.Status)
				assert.Equal(t, "the Extension interface is not fully implemented", cond.Message)
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
					ServiceAccountName: "test-service-account",
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

func TestExtensionResolve(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.EnableExtensionAPI, true)()
	ctx := context.Background()

	testCases := []struct {
		name                    string
		packageName             string
		packageVersion          string
		upgradeConstraintPolicy ocv1alpha1.UpgradeConstraintPolicy
		wantErr                 error
		existingApp             *carvelv1alpha1.App
		wantCondition           metav1.Condition
	}{
		{
			name:           "basic install with specified version",
			packageName:    "prometheus",
			packageVersion: "0.37.0",
			wantCondition: metav1.Condition{
				Status:  metav1.ConditionTrue,
				Message: `resolved to "quay.io/operatorhubio/prometheus@sha256:3e281e587de3d03011440685fc4fb782672beab044c1ebadc42788ce05a21c35"`,
			},
		},
		{
			name:           "existing App of same version",
			packageName:    "prometheus",
			packageVersion: "0.37.0",
			existingApp: &carvelv1alpha1.App{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Annotations: map[string]string{
						"olm.operatorframework.io/bundleVersion": "0.37.0",
					},
				},
				Spec: carvelv1alpha1.AppSpec{},
			},
			wantCondition: metav1.Condition{
				Status:  metav1.ConditionTrue,
				Message: `resolved to "quay.io/operatorhubio/prometheus@sha256:3e281e587de3d03011440685fc4fb782672beab044c1ebadc42788ce05a21c35"`,
			},
		},
		{
			name:           "existing App of higher version than requested",
			packageName:    "prometheus",
			packageVersion: "0.37.0",
			existingApp: &carvelv1alpha1.App{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Annotations: map[string]string{
						"olm.operatorframework.io/bundleVersion": "0.38.0",
					},
				},
				Spec: carvelv1alpha1.AppSpec{},
			},
			wantErr: fmt.Errorf("no package \"prometheus\" matching version \"0.37.0\" which upgrades currently installed version \"0.38.0\" found"),
			wantCondition: metav1.Condition{
				Status:  metav1.ConditionFalse,
				Message: `no package "prometheus" matching version "0.37.0" which upgrades currently installed version "0.38.0" found`,
			},
		},
		{
			name:                    "downgrade with UpgradeConstraintPolicy of 'Ignore'",
			packageName:             "prometheus",
			packageVersion:          "0.37.0",
			upgradeConstraintPolicy: ocv1alpha1.UpgradeConstraintPolicyIgnore,
			existingApp: &carvelv1alpha1.App{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Annotations: map[string]string{
						"olm.operatorframework.io/bundleVersion": "0.38.0",
					},
				},
				Spec: carvelv1alpha1.AppSpec{},
			},
			wantCondition: metav1.Condition{
				Status:  metav1.ConditionTrue,
				Message: `resolved to "quay.io/operatorhubio/prometheus@sha256:3e281e587de3d03011440685fc4fb782672beab044c1ebadc42788ce05a21c35"`,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c, reconciler := newClientAndExtensionReconciler(t)
			extName := fmt.Sprintf("extension-test-%s", rand.String(8))
			ext := &ocv1alpha1.Extension{
				ObjectMeta: metav1.ObjectMeta{Name: extName, Namespace: "default"},
				Spec: ocv1alpha1.ExtensionSpec{
					ServiceAccountName: testServiceAccount,
					Source: ocv1alpha1.ExtensionSource{
						SourceType: ocv1alpha1.SourceTypePackage,
						Package: &ocv1alpha1.ExtensionSourcePackage{
							Name:                    tc.packageName,
							Version:                 tc.packageVersion,
							UpgradeConstraintPolicy: tc.upgradeConstraintPolicy,
						},
					},
				},
			}
			if tc.existingApp != nil {
				tc.existingApp.Name = extName
				require.NoError(t, c.Create(ctx, tc.existingApp))
			}

			require.NoError(t, c.Create(ctx, ext))
			extNN := types.NamespacedName{Name: ext.GetName(), Namespace: ext.GetNamespace()}

			res, reconcileErr := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extNN})
			assert.Equal(t, reconcileErr, tc.wantErr)
			if tc.wantErr != nil {
				err := c.Get(ctx, extNN, ext)
				assert.NoError(t, err)

				assert.Equal(t, ctrl.Result{}, res)

				condition := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1alpha1.TypeResolved)
				assert.NotNil(t, condition)
				assert.Equal(t, tc.wantCondition.Status, condition.Status)
				assert.Equal(t, tc.wantCondition.Message, condition.Message)
			}
		})
	}
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
