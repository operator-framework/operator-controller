package controllers_test

import (
	"context"
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
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/operator-framework/operator-controller/internal/controllers"

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
			assert.Equal(t, ocv1alpha1.ExtensionStatus{Paused: true}, ext.Status)
		}},
		{"feature gate enabled and active", true, false, func(t *testing.T, res ctrl.Result, err error, ext *ocv1alpha1.Extension) {
			assert.Equal(t, ctrl.Result{}, res)
			assert.NoError(t, err)
			verifyExtensionInvariants(t, ext)
			assert.False(t, ext.Status.Paused)
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

func TestMapAppCondtitionToStatus(t *testing.T) {
	testCases := []struct {
		name     string
		app      *kappctrlv1alpha1.App
		ext      *ocv1alpha1.Extension
		expected *ocv1alpha1.Extension
	}{
		{
			name: "preserve existing conditions on extension while reconciling",
			app: &kappctrlv1alpha1.App{
				Status: kappctrlv1alpha1.AppStatus{
					GenericStatus: kappctrlv1alpha1.GenericStatus{
						//						ObservedGeneration: 0,
						FriendlyDescription: "Paused/Cancelled",
					},
				},
			},
			ext: &ocv1alpha1.Extension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: ocv1alpha1.ExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:               ocv1alpha1.TypeDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeInstalled,
							Status:             metav1.ConditionUnknown,
							Reason:             ocv1alpha1.ReasonInstallationStatusUnknown,
							Message:            "install status unknown",
							ObservedGeneration: 1,
						},
					},
				},
			},
			expected: &ocv1alpha1.Extension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: ocv1alpha1.ExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:               ocv1alpha1.TypeDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeInstalled,
							Status:             metav1.ConditionUnknown,
							Reason:             ocv1alpha1.ReasonInstallationStatusUnknown,
							Message:            "Paused/Cancelled",
							ObservedGeneration: 1,
						},
					},
				},
			},
		},
		{
			name: "show UsefulErrorMessage when referenced by FriendlyErrorMessage",
			app: &kappctrlv1alpha1.App{
				Status: kappctrlv1alpha1.AppStatus{
					GenericStatus: kappctrlv1alpha1.GenericStatus{
						Conditions: []kappctrlv1alpha1.Condition{{
							Type:    kappctrlv1alpha1.ReconcileFailed,
							Status:  corev1.ConditionTrue,
							Message: "Reconcile Failed",
						}},
						FriendlyDescription: "Reconcile Error (see .status.usefulErrorMessage for details)",
						UsefulErrorMessage:  "Deployment Error: Exit Status 1",
					},
				},
			},
			ext: &ocv1alpha1.Extension{
				Status: ocv1alpha1.ExtensionStatus{
					InstalledBundleResource: "test-bundle",
					Conditions: []metav1.Condition{
						{
							Type:    ocv1alpha1.TypeInstalled,
							Status:  metav1.ConditionTrue,
							Reason:  ocv1alpha1.ReasonSuccess,
							Message: "Success",
						},
					},
				},
			},
			expected: &ocv1alpha1.Extension{
				Status: ocv1alpha1.ExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:    ocv1alpha1.TypeInstalled,
							Status:  metav1.ConditionFalse,
							Reason:  ocv1alpha1.ReasonInstallationFailed,
							Message: "Deployment Error: Exit Status 1",
						},
					},
				},
			},
		},
		{
			name: "show FriendlyErrorMessage when present",
			app: &kappctrlv1alpha1.App{
				Status: kappctrlv1alpha1.AppStatus{
					GenericStatus: kappctrlv1alpha1.GenericStatus{
						Conditions: []kappctrlv1alpha1.Condition{{
							Type:    kappctrlv1alpha1.DeleteFailed,
							Status:  corev1.ConditionTrue,
							Message: "Delete Failed",
						}},
						FriendlyDescription: "Delete Error: Timed out",
						UsefulErrorMessage:  "Timed out after 5m",
					},
				},
			},
			ext: &ocv1alpha1.Extension{
				Status: ocv1alpha1.ExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:    ocv1alpha1.TypeInstalled,
							Status:  metav1.ConditionUnknown,
							Reason:  ocv1alpha1.ReasonDeleting,
							Message: "Deleting",
						},
					},
				},
			},
			expected: &ocv1alpha1.Extension{
				Status: ocv1alpha1.ExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:    ocv1alpha1.TypeInstalled,
							Status:  metav1.ConditionUnknown,
							Reason:  ocv1alpha1.ReasonDeleteFailed,
							Message: "Delete Error: Timed out",
						},
					},
				},
			},
		},
		{
			name: "update status without message when none exist on App",
			app: &kappctrlv1alpha1.App{
				Status: kappctrlv1alpha1.AppStatus{
					GenericStatus: kappctrlv1alpha1.GenericStatus{
						Conditions: []kappctrlv1alpha1.Condition{{
							Type:   kappctrlv1alpha1.Deleting,
							Status: corev1.ConditionTrue,
						}},
					},
				},
			},
			ext: &ocv1alpha1.Extension{
				Status: ocv1alpha1.ExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:    ocv1alpha1.TypeInstalled,
							Status:  metav1.ConditionUnknown,
							Reason:  ocv1alpha1.ReasonInstallationStatusUnknown,
							Message: "Reconciling",
						},
					},
				},
			},
			expected: &ocv1alpha1.Extension{
				Status: ocv1alpha1.ExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:   ocv1alpha1.TypeInstalled,
							Status: metav1.ConditionUnknown,
							Reason: ocv1alpha1.ReasonDeleting,
						},
					},
				},
			},
		},
		{
			name: "set default installed condition for empty app status",
			app:  &kappctrlv1alpha1.App{},
			ext: &ocv1alpha1.Extension{
				Status: ocv1alpha1.ExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:    ocv1alpha1.TypeInstalled,
							Status:  metav1.ConditionUnknown,
							Reason:  ocv1alpha1.ReasonInstallationStatusUnknown,
							Message: "Reconciling",
						},
					},
				},
			},
			expected: &ocv1alpha1.Extension{
				Status: ocv1alpha1.ExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:    ocv1alpha1.TypeInstalled,
							Status:  metav1.ConditionUnknown,
							Reason:  ocv1alpha1.ReasonInstallationStatusUnknown,
							Message: "install status unknown",
						},
					},
				},
			},
		},
	}

	for _, tt := range testCases {
		controllers.MapAppStatusToCondition(tt.app, tt.ext)
		for i := range tt.ext.Status.Conditions {
			//unset transition time for comparison
			tt.ext.Status.Conditions[i].LastTransitionTime = metav1.Time{}
		}
		assert.Equal(t, tt.expected, tt.ext, tt.name)
	}
}
