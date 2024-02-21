package controllers_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		{"feature gate enabled and paused", true, true, func(t *testing.T, res ctrl.Result, err error, ext *ocv1alpha1.Extension) {
			assert.Equal(t, ctrl.Result{}, res)
			assert.NoError(t, err)
			assert.Equal(t, ocv1alpha1.ExtensionStatus{}, ext.Status)
		}},
		{"feature gate enabled and active", true, false, func(t *testing.T, res ctrl.Result, err error, ext *ocv1alpha1.Extension) {
			assert.Equal(t, ctrl.Result{}, res)
			assert.NoError(t, err)
			verifyExtensionInvariants(t, ext)
			assert.Empty(t, ext.Status.InstalledBundleResource)
			assert.Empty(t, ext.Status.ResolvedBundleResource)
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
					ServiceAccountName: "test-service-account",
					Source:             ocv1alpha1.ExtensionSource{Package: &ocv1alpha1.ExtensionSourcePackage{Name: "test-package"}},
				},
			}
			if tc.paused {
				ext.Spec.Managed = ocv1alpha1.ManagedStatePaused
			} else {
				ext.Spec.Managed = ocv1alpha1.ManagedStateActive
			}
			require.NoError(t, c.Create(ctx, ext))

			defer featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.EnableExtensionAPI, tc.featureGateEnabled)()
			res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})

			require.NoError(t, c.Get(ctx, extKey, ext))
			tc.assert(t, res, err, ext)
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
