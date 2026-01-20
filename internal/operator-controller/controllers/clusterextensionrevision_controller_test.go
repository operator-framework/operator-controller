package controllers_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"pkg.package-operator.run/boxcutter"
	"pkg.package-operator.run/boxcutter/machinery"
	machinerytypes "pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
)

const clusterExtensionRevisionName = "test-ext-1"

func Test_ClusterExtensionRevisionReconciler_Reconcile_RevisionReconciliation(t *testing.T) {
	testScheme := newScheme(t)

	for _, tc := range []struct {
		name                    string
		reconcilingRevisionName string
		existingObjs            func() []client.Object
		revisionResult          machinery.RevisionResult
		revisionReconcileErr    error
		validate                func(*testing.T, client.Client)
	}{
		{
			name:                    "sets teardown finalizer",
			reconcilingRevisionName: clusterExtensionRevisionName,
			revisionResult:          mockRevisionResult{},
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				rev1 := newTestClusterExtensionRevision(t, clusterExtensionRevisionName, ext, testScheme)
				return []client.Object{ext, rev1}
			},
			validate: func(t *testing.T, c client.Client) {
				rev := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{
					Name: clusterExtensionRevisionName,
				}, rev)
				require.NoError(t, err)
				require.Contains(t, rev.Finalizers, "olm.operatorframework.io/teardown")
			},
		},
		{
			name:                    "Available condition is not updated on error if its not already set",
			reconcilingRevisionName: clusterExtensionRevisionName,
			revisionResult:          mockRevisionResult{},
			revisionReconcileErr:    errors.New("some error"),
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				rev1 := newTestClusterExtensionRevision(t, clusterExtensionRevisionName, ext, testScheme)
				return []client.Object{ext, rev1}
			},
			validate: func(t *testing.T, c client.Client) {
				rev := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{
					Name: clusterExtensionRevisionName,
				}, rev)
				require.NoError(t, err)
				cond := meta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeAvailable)
				require.Nil(t, cond)
			},
		},
		{
			name:                    "Available condition is updated to Unknown on error if its been already set",
			reconcilingRevisionName: clusterExtensionRevisionName,
			revisionResult:          mockRevisionResult{},
			revisionReconcileErr:    errors.New("some error"),
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				rev1 := newTestClusterExtensionRevision(t, clusterExtensionRevisionName, ext, testScheme)
				meta.SetStatusCondition(&rev1.Status.Conditions, metav1.Condition{
					Type:               ocv1.ClusterExtensionRevisionTypeAvailable,
					Status:             metav1.ConditionTrue,
					Reason:             ocv1.ClusterExtensionRevisionReasonProbesSucceeded,
					Message:            "Revision 1.0.0 is rolled out.",
					ObservedGeneration: 1,
				})
				return []client.Object{ext, rev1}
			},
			validate: func(t *testing.T, c client.Client) {
				rev := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{
					Name: clusterExtensionRevisionName,
				}, rev)
				require.NoError(t, err)
				cond := meta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeAvailable)
				require.NotNil(t, cond)
				require.Equal(t, metav1.ConditionUnknown, cond.Status)
				require.Equal(t, ocv1.ClusterExtensionRevisionReasonReconciling, cond.Reason)
				require.Equal(t, "some error", cond.Message)
				require.Equal(t, int64(1), cond.ObservedGeneration)
			},
		},
		{
			name:                    "set Available:False:RollingOut status condition during rollout when no probe failures are detected",
			reconcilingRevisionName: clusterExtensionRevisionName,
			revisionResult:          mockRevisionResult{},
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				rev1 := newTestClusterExtensionRevision(t, clusterExtensionRevisionName, ext, testScheme)
				return []client.Object{ext, rev1}
			},
			validate: func(t *testing.T, c client.Client) {
				rev := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{
					Name: clusterExtensionRevisionName,
				}, rev)
				require.NoError(t, err)
				cond := meta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeAvailable)
				require.NotNil(t, cond)
				require.Equal(t, metav1.ConditionFalse, cond.Status)
				require.Equal(t, ocv1.ReasonRollingOut, cond.Reason)
				require.Equal(t, "Revision 1.0.0 is rolling out.", cond.Message)
				require.Equal(t, int64(1), cond.ObservedGeneration)
			},
		},
		{
			name:                    "set Available:False:ProbeFailure condition when probe failures are detected and revision is in transition",
			reconcilingRevisionName: clusterExtensionRevisionName,
			revisionResult: mockRevisionResult{
				inTransition: true,
				isComplete:   false,
				phases: []machinery.PhaseResult{
					mockPhaseResult{
						name:       "somephase",
						isComplete: false,
						objects: []machinery.ObjectResult{
							mockObjectResult{
								success: true,
								probes: machinerytypes.ProbeResultContainer{
									boxcutter.ProgressProbeType: {
										Status: machinerytypes.ProbeStatusTrue,
									},
								},
							},
							mockObjectResult{
								success: false,
								object: func() client.Object {
									obj := &corev1.Service{
										ObjectMeta: metav1.ObjectMeta{
											Name:      "my-service",
											Namespace: "my-namespace",
										},
									}
									obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Service"))
									return obj
								}(),
								probes: machinerytypes.ProbeResultContainer{
									boxcutter.ProgressProbeType: {
										Status: machinerytypes.ProbeStatusFalse,
										Messages: []string{
											"something bad happened",
											"something worse happened",
										},
									},
								},
							},
						},
					},
					mockPhaseResult{
						name:       "someotherphase",
						isComplete: false,
						objects: []machinery.ObjectResult{
							mockObjectResult{
								success: false,
								object: func() client.Object {
									obj := &corev1.ConfigMap{
										ObjectMeta: metav1.ObjectMeta{
											Name:      "my-configmap",
											Namespace: "my-namespace",
										},
									}
									obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
									return obj
								}(),
								probes: machinerytypes.ProbeResultContainer{
									boxcutter.ProgressProbeType: {
										Status: machinerytypes.ProbeStatusFalse,
										Messages: []string{
											"we have a problem",
										},
									},
								},
							},
						},
					},
				},
			},
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				rev1 := newTestClusterExtensionRevision(t, clusterExtensionRevisionName, ext, testScheme)
				return []client.Object{ext, rev1}
			},
			validate: func(t *testing.T, c client.Client) {
				rev := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{
					Name: clusterExtensionRevisionName,
				}, rev)
				require.NoError(t, err)
				cond := meta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeAvailable)
				require.NotNil(t, cond)
				require.Equal(t, metav1.ConditionFalse, cond.Status)
				require.Equal(t, ocv1.ClusterExtensionRevisionReasonProbeFailure, cond.Reason)
				require.Equal(t, "Object Service.v1 my-namespace/my-service: something bad happened and something worse happened\nObject ConfigMap.v1 my-namespace/my-configmap: we have a problem", cond.Message)
				require.Equal(t, int64(1), cond.ObservedGeneration)
			},
		},
		{
			name:                    "set Available:False:ProbeFailure condition when probe failures are detected and revision is not in transition",
			reconcilingRevisionName: clusterExtensionRevisionName,
			revisionResult: mockRevisionResult{
				inTransition: false,
				isComplete:   false,
				phases: []machinery.PhaseResult{
					mockPhaseResult{
						name:       "somephase",
						isComplete: false,
						objects: []machinery.ObjectResult{
							mockObjectResult{
								success: true,
								probes: machinerytypes.ProbeResultContainer{
									boxcutter.ProgressProbeType: machinerytypes.ProbeResult{
										Status: machinerytypes.ProbeStatusTrue,
									},
								},
							},
							mockObjectResult{
								success: false,
								object: func() client.Object {
									obj := &corev1.Service{
										ObjectMeta: metav1.ObjectMeta{
											Name:      "my-service",
											Namespace: "my-namespace",
										},
									}
									obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Service"))
									return obj
								}(),
								probes: machinerytypes.ProbeResultContainer{
									boxcutter.ProgressProbeType: machinerytypes.ProbeResult{
										Status: machinerytypes.ProbeStatusFalse,
										Messages: []string{
											"something bad happened",
											"something worse happened",
										},
									},
								},
							},
						},
					},
					mockPhaseResult{
						name:       "someotherphase",
						isComplete: false,
						objects: []machinery.ObjectResult{
							mockObjectResult{
								success: false,
								object: func() client.Object {
									obj := &corev1.ConfigMap{
										ObjectMeta: metav1.ObjectMeta{
											Name:      "my-configmap",
											Namespace: "my-namespace",
										},
									}
									obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
									return obj
								}(),
								probes: machinerytypes.ProbeResultContainer{
									boxcutter.ProgressProbeType: machinerytypes.ProbeResult{
										Status: machinerytypes.ProbeStatusFalse,
										Messages: []string{
											"we have a problem",
										},
									},
								},
							},
						},
					},
				},
			},
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				rev1 := newTestClusterExtensionRevision(t, clusterExtensionRevisionName, ext, testScheme)
				return []client.Object{ext, rev1}
			},
			validate: func(t *testing.T, c client.Client) {
				rev := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{
					Name: clusterExtensionRevisionName,
				}, rev)
				require.NoError(t, err)
				cond := meta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeAvailable)
				require.NotNil(t, cond)
				require.Equal(t, metav1.ConditionFalse, cond.Status)
				require.Equal(t, ocv1.ClusterExtensionRevisionReasonProbeFailure, cond.Reason)
				require.Equal(t, "Object Service.v1 my-namespace/my-service: something bad happened and something worse happened\nObject ConfigMap.v1 my-namespace/my-configmap: we have a problem", cond.Message)
				require.Equal(t, int64(1), cond.ObservedGeneration)
			},
		},
		{
			name:                    "set Progressing:True:Retrying when there's an error reconciling the revision",
			revisionReconcileErr:    errors.New("some error"),
			reconcilingRevisionName: clusterExtensionRevisionName,
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				rev1 := newTestClusterExtensionRevision(t, clusterExtensionRevisionName, ext, testScheme)
				return []client.Object{ext, rev1}
			},
			validate: func(t *testing.T, c client.Client) {
				rev := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{
					Name: clusterExtensionRevisionName,
				}, rev)
				require.NoError(t, err)
				cond := meta.FindStatusCondition(rev.Status.Conditions, ocv1.TypeProgressing)
				require.NotNil(t, cond)
				require.Equal(t, metav1.ConditionTrue, cond.Status)
				require.Equal(t, ocv1.ClusterExtensionRevisionReasonRetrying, cond.Reason)
				require.Equal(t, "some error", cond.Message)
				require.Equal(t, int64(1), cond.ObservedGeneration)
			},
		},
		{
			name: "set Progressing:True:RollingOut condition while revision is transitioning",
			revisionResult: mockRevisionResult{
				inTransition: true,
			},
			reconcilingRevisionName: clusterExtensionRevisionName,
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				rev1 := newTestClusterExtensionRevision(t, clusterExtensionRevisionName, ext, testScheme)
				return []client.Object{ext, rev1}
			},
			validate: func(t *testing.T, c client.Client) {
				rev := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{
					Name: clusterExtensionRevisionName,
				}, rev)
				require.NoError(t, err)
				cond := meta.FindStatusCondition(rev.Status.Conditions, ocv1.TypeProgressing)
				require.NotNil(t, cond)
				require.Equal(t, metav1.ConditionTrue, cond.Status)
				require.Equal(t, ocv1.ReasonRollingOut, cond.Reason)
				require.Equal(t, "Revision 1.0.0 is rolling out.", cond.Message)
				require.Equal(t, int64(1), cond.ObservedGeneration)
			},
		},
		{
			name: "set Progressing:True:Succeeded once transition rollout is finished",
			revisionResult: mockRevisionResult{
				inTransition: false,
			},
			reconcilingRevisionName: clusterExtensionRevisionName,
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				rev1 := newTestClusterExtensionRevision(t, clusterExtensionRevisionName, ext, testScheme)
				meta.SetStatusCondition(&rev1.Status.Conditions, metav1.Condition{
					Type:               ocv1.TypeProgressing,
					Status:             metav1.ConditionTrue,
					Reason:             ocv1.ReasonSucceeded,
					Message:            "Revision 1.0.0 is rolling out.",
					ObservedGeneration: 1,
				})
				return []client.Object{ext, rev1}
			},
			validate: func(t *testing.T, c client.Client) {
				rev := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{
					Name: clusterExtensionRevisionName,
				}, rev)
				require.NoError(t, err)
				cond := meta.FindStatusCondition(rev.Status.Conditions, ocv1.TypeProgressing)
				require.NotNil(t, cond)
				require.Equal(t, metav1.ConditionTrue, cond.Status)
				require.Equal(t, ocv1.ReasonSucceeded, cond.Reason)
				require.Equal(t, "Revision 1.0.0 has rolled out.", cond.Message)
				require.Equal(t, int64(1), cond.ObservedGeneration)
			},
		},
		{
			name: "set Available:True:ProbesSucceeded and Succeeded:True:Succeeded conditions on successful revision rollout",
			revisionResult: mockRevisionResult{
				isComplete: true,
			},
			reconcilingRevisionName: clusterExtensionRevisionName,
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				rev1 := newTestClusterExtensionRevision(t, clusterExtensionRevisionName, ext, testScheme)
				return []client.Object{ext, rev1}
			},
			validate: func(t *testing.T, c client.Client) {
				rev := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{
					Name: clusterExtensionRevisionName,
				}, rev)
				require.NoError(t, err)
				cond := meta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeAvailable)
				require.NotNil(t, cond)
				require.Equal(t, metav1.ConditionTrue, cond.Status)
				require.Equal(t, ocv1.ClusterExtensionRevisionReasonProbesSucceeded, cond.Reason)
				require.Equal(t, "Objects are available and pass all probes.", cond.Message)
				require.Equal(t, int64(1), cond.ObservedGeneration)

				cond = meta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeProgressing)
				require.NotNil(t, cond)
				require.Equal(t, metav1.ConditionTrue, cond.Status)
				require.Equal(t, ocv1.ReasonSucceeded, cond.Reason)
				require.Equal(t, "Revision 1.0.0 has rolled out.", cond.Message)
				require.Equal(t, int64(1), cond.ObservedGeneration)

				cond = meta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeSucceeded)
				require.NotNil(t, cond)
				require.Equal(t, metav1.ConditionTrue, cond.Status)
				require.Equal(t, ocv1.ReasonSucceeded, cond.Reason)
				require.Equal(t, "Revision succeeded rolling out.", cond.Message)
				require.Equal(t, int64(1), cond.ObservedGeneration)
			},
		},
		{
			name: "archive previous revisions on successful rollout",
			revisionResult: mockRevisionResult{
				isComplete: true,
			},
			reconcilingRevisionName: "test-ext-3",
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				prevRev1 := newTestClusterExtensionRevision(t, "test-ext-1", ext, testScheme)
				prevRev2 := newTestClusterExtensionRevision(t, "test-ext-2", ext, testScheme)
				rev := newTestClusterExtensionRevision(t, "test-ext-3", ext, testScheme)
				return []client.Object{ext, prevRev1, prevRev2, rev}
			},
			validate: func(t *testing.T, c client.Client) {
				rev := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{
					Name: "test-ext-1",
				}, rev)
				require.NoError(t, err)
				require.Equal(t, ocv1.ClusterExtensionRevisionLifecycleStateArchived, rev.Spec.LifecycleState)

				err = c.Get(t.Context(), client.ObjectKey{
					Name: "test-ext-2",
				}, rev)
				require.NoError(t, err)
				require.Equal(t, ocv1.ClusterExtensionRevisionLifecycleStateArchived, rev.Spec.LifecycleState)

				err = c.Get(t.Context(), client.ObjectKey{
					Name: "test-ext-3",
				}, rev)
				require.NoError(t, err)
				require.Equal(t, ocv1.ClusterExtensionRevisionLifecycleStateActive, rev.Spec.LifecycleState)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// create extension and cluster extension
			testClient := fake.NewClientBuilder().
				WithScheme(testScheme).
				WithStatusSubresource(&ocv1.ClusterExtensionRevision{}).
				WithObjects(tc.existingObjs()...).
				Build()

			// reconcile cluster extension revision
			mockEngine := &mockRevisionEngine{
				reconcile: func(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionReconcileOption) (machinery.RevisionResult, error) {
					return tc.revisionResult, tc.revisionReconcileErr
				},
			}
			result, err := (&controllers.ClusterExtensionRevisionReconciler{
				Client:                testClient,
				RevisionEngineFactory: &mockRevisionEngineFactory{engine: mockEngine},
				TrackingCache:         &mockTrackingCache{client: testClient},
			}).Reconcile(t.Context(), ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name: tc.reconcilingRevisionName,
				},
			})

			// reconcile cluster extension revision
			require.Equal(t, ctrl.Result{}, result)
			if tc.revisionReconcileErr == nil {
				require.NoError(t, err)
			} else {
				require.Contains(t, err.Error(), tc.revisionReconcileErr.Error())
			}

			// validate test case
			tc.validate(t, testClient)
		})
	}
}

func Test_ClusterExtensionRevisionReconciler_Reconcile_ValidationError_Retries(t *testing.T) {
	const (
		clusterExtensionName         = "test-ext"
		clusterExtensionRevisionName = "test-ext-1"
	)

	testScheme := newScheme(t)

	for _, tc := range []struct {
		name           string
		revisionResult machinery.RevisionResult
	}{
		{
			name: "retries on revision result validation error",
			revisionResult: mockRevisionResult{
				validationError: &validation.RevisionValidationError{
					RevisionName:   "test-ext-1",
					RevisionNumber: 1,
					Phases: []validation.PhaseValidationError{
						{
							PhaseName:  "everything",
							PhaseError: fmt.Errorf("some error"),
							Objects: []validation.ObjectValidationError{
								{
									ObjectRef: machinerytypes.ObjectRef{
										GroupVersionKind: schema.GroupVersionKind{
											Group:   "",
											Version: "v1",
											Kind:    "ConfigMap",
										},
										ObjectKey: client.ObjectKey{
											Name:      "my-configmap",
											Namespace: "my-namespace",
										},
									},
									Errors: []error{
										fmt.Errorf("is not a config"),
										fmt.Errorf("is not a map"),
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "retries on revision result phase validation error",
			revisionResult: mockRevisionResult{
				phases: []machinery.PhaseResult{
					mockPhaseResult{
						validationError: &validation.PhaseValidationError{
							PhaseName:  "everything",
							PhaseError: fmt.Errorf("some error"),
							Objects: []validation.ObjectValidationError{
								{
									ObjectRef: machinerytypes.ObjectRef{
										GroupVersionKind: schema.GroupVersionKind{
											Group:   "",
											Version: "v1",
											Kind:    "ConfigMap",
										},
										ObjectKey: client.ObjectKey{
											Name:      "my-configmap",
											Namespace: "my-namespace",
										},
									},
									Errors: []error{
										fmt.Errorf("is not a config"),
										fmt.Errorf("is not a map"),
									},
								},
							},
						},
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ext := newTestClusterExtension()
			rev1 := newTestClusterExtensionRevision(t, clusterExtensionRevisionName, ext, testScheme)

			// create extension and cluster extension
			testClient := fake.NewClientBuilder().
				WithScheme(testScheme).
				WithStatusSubresource(&ocv1.ClusterExtensionRevision{}).
				WithObjects(ext, rev1).
				Build()

			// reconcile cluster extension revision
			mockEngine := &mockRevisionEngine{
				reconcile: func(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionReconcileOption) (machinery.RevisionResult, error) {
					return tc.revisionResult, nil
				},
			}
			result, err := (&controllers.ClusterExtensionRevisionReconciler{
				Client:                testClient,
				RevisionEngineFactory: &mockRevisionEngineFactory{engine: mockEngine},
				TrackingCache:         &mockTrackingCache{client: testClient},
			}).Reconcile(t.Context(), ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name: clusterExtensionRevisionName,
				},
			})

			// reconcile cluster extension revision
			require.Equal(t, ctrl.Result{
				RequeueAfter: 10 * time.Second,
			}, result)
			require.NoError(t, err)
		})
	}
}

func Test_ClusterExtensionRevisionReconciler_Reconcile_Deletion(t *testing.T) {
	const (
		clusterExtensionRevisionName = "test-ext-1"
	)

	testScheme := newScheme(t)
	require.NoError(t, corev1.AddToScheme(testScheme))

	for _, tc := range []struct {
		name                     string
		existingObjs             func() []client.Object
		revisionResult           machinery.RevisionResult
		revisionEngineTeardownFn func(*testing.T) func(context.Context, machinerytypes.Revision, ...machinerytypes.RevisionTeardownOption) (machinery.RevisionTeardownResult, error)
		validate                 func(*testing.T, client.Client)
		trackingCacheFreeFn      func(context.Context, client.Object) error
		expectedErr              string
	}{
		{
			name:           "teardown finalizer is removed",
			revisionResult: mockRevisionResult{},
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				rev1 := newTestClusterExtensionRevision(t, clusterExtensionRevisionName, ext, testScheme)
				rev1.Finalizers = []string{
					"olm.operatorframework.io/teardown",
				}
				return []client.Object{ext, rev1}
			},
			validate: func(t *testing.T, c client.Client) {
				rev := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{
					Name: clusterExtensionRevisionName,
				}, rev)
				require.NoError(t, err)
				require.NotContains(t, "olm.operatorframework.io/teardown", rev.Finalizers)
			},
			revisionEngineTeardownFn: func(t *testing.T) func(context.Context, machinerytypes.Revision, ...machinerytypes.RevisionTeardownOption) (machinery.RevisionTeardownResult, error) {
				return nil
			},
		},
		{
			name:           "revision is torn down and deleted when deleted",
			revisionResult: mockRevisionResult{},
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				rev1 := newTestClusterExtensionRevision(t, clusterExtensionRevisionName, ext, testScheme)
				rev1.Finalizers = []string{
					"olm.operatorframework.io/teardown",
				}
				rev1.DeletionTimestamp = &metav1.Time{Time: time.Now()}
				return []client.Object{rev1, ext}
			},
			revisionEngineTeardownFn: func(t *testing.T) func(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionTeardownOption) (machinery.RevisionTeardownResult, error) {
				return func(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionTeardownOption) (machinery.RevisionTeardownResult, error) {
					return &mockRevisionTeardownResult{
						isComplete: true,
					}, nil
				}
			},
			validate: func(t *testing.T, c client.Client) {
				t.Log("cluster revision is deleted")
				rev := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{
					Name: clusterExtensionRevisionName,
				}, rev)
				require.Error(t, err)
				require.True(t, apierrors.IsNotFound(err))
			},
		},
		{
			name:           "set Available:Unknown:Reconciling and surface tracking cache cleanup errors when deleted",
			revisionResult: mockRevisionResult{},
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				rev1 := newTestClusterExtensionRevision(t, clusterExtensionRevisionName, ext, testScheme)
				rev1.Finalizers = []string{
					"olm.operatorframework.io/teardown",
				}
				rev1.DeletionTimestamp = &metav1.Time{Time: time.Now()}
				return []client.Object{rev1, ext}
			},
			revisionEngineTeardownFn: func(t *testing.T) func(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionTeardownOption) (machinery.RevisionTeardownResult, error) {
				return func(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionTeardownOption) (machinery.RevisionTeardownResult, error) {
					return &mockRevisionTeardownResult{
						isComplete: true,
					}, nil
				}
			},
			trackingCacheFreeFn: func(ctx context.Context, object client.Object) error {
				return fmt.Errorf("some tracking cache cleanup error")
			},
			expectedErr: "some tracking cache cleanup error",
			validate: func(t *testing.T, c client.Client) {
				t.Log("cluster revision is not deleted and still contains finalizer")
				rev := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{
					Name: clusterExtensionRevisionName,
				}, rev)
				require.NoError(t, err)
				cond := meta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeAvailable)
				require.NotNil(t, cond)
				require.Equal(t, metav1.ConditionUnknown, cond.Status)
				require.Equal(t, ocv1.ClusterExtensionRevisionReasonReconciling, cond.Reason)
				require.Equal(t, "some tracking cache cleanup error", cond.Message)
				require.Equal(t, int64(1), cond.ObservedGeneration)
			},
		},
		{
			name:           "set Available:Archived:Unknown and Progressing:False:Archived conditions when a revision is archived",
			revisionResult: mockRevisionResult{},
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				rev1 := newTestClusterExtensionRevision(t, clusterExtensionRevisionName, ext, testScheme)
				rev1.Finalizers = []string{
					"olm.operatorframework.io/teardown",
				}
				rev1.Spec.LifecycleState = ocv1.ClusterExtensionRevisionLifecycleStateArchived
				return []client.Object{rev1, ext}
			},
			revisionEngineTeardownFn: func(t *testing.T) func(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionTeardownOption) (machinery.RevisionTeardownResult, error) {
				return func(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionTeardownOption) (machinery.RevisionTeardownResult, error) {
					return &mockRevisionTeardownResult{
						isComplete: true,
					}, nil
				}
			},
			validate: func(t *testing.T, c client.Client) {
				rev := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{
					Name: clusterExtensionRevisionName,
				}, rev)
				require.NoError(t, err)
				cond := meta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeAvailable)
				require.NotNil(t, cond)
				require.Equal(t, metav1.ConditionUnknown, cond.Status)
				require.Equal(t, ocv1.ClusterExtensionRevisionReasonArchived, cond.Reason)
				require.Equal(t, "revision is archived", cond.Message)
				require.Equal(t, int64(1), cond.ObservedGeneration)

				cond = meta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeProgressing)
				require.NotNil(t, cond)
				require.Equal(t, metav1.ConditionFalse, cond.Status)
				require.Equal(t, ocv1.ClusterExtensionRevisionReasonArchived, cond.Reason)
				require.Equal(t, "revision is archived", cond.Message)
				require.Equal(t, int64(1), cond.ObservedGeneration)
			},
		},
		{
			name:           "revision is torn down when in archived state and finalizer is removed",
			revisionResult: mockRevisionResult{},
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				rev1 := newTestClusterExtensionRevision(t, clusterExtensionRevisionName, ext, testScheme)
				rev1.Finalizers = []string{
					"olm.operatorframework.io/teardown",
				}
				rev1.Spec.LifecycleState = ocv1.ClusterExtensionRevisionLifecycleStateArchived
				meta.SetStatusCondition(&rev1.Status.Conditions, metav1.Condition{
					Type:               ocv1.ClusterExtensionRevisionTypeAvailable,
					Status:             metav1.ConditionUnknown,
					Reason:             ocv1.ClusterExtensionRevisionReasonArchived,
					Message:            "revision is archived",
					ObservedGeneration: rev1.Generation,
				})
				meta.SetStatusCondition(&rev1.Status.Conditions, metav1.Condition{
					Type:               ocv1.ClusterExtensionRevisionTypeProgressing,
					Status:             metav1.ConditionFalse,
					Reason:             ocv1.ClusterExtensionRevisionReasonArchived,
					Message:            "revision is archived",
					ObservedGeneration: rev1.Generation,
				})
				return []client.Object{rev1, ext}
			},
			revisionEngineTeardownFn: func(t *testing.T) func(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionTeardownOption) (machinery.RevisionTeardownResult, error) {
				return func(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionTeardownOption) (machinery.RevisionTeardownResult, error) {
					return &mockRevisionTeardownResult{
						isComplete: true,
					}, nil
				}
			},
			validate: func(t *testing.T, c client.Client) {
				rev := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{
					Name: clusterExtensionRevisionName,
				}, rev)
				require.NoError(t, err)
				require.NotContains(t, rev.Finalizers, "olm.operatorframework.io/teardown")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// create extension and cluster extension
			testClient := fake.NewClientBuilder().
				WithScheme(testScheme).
				WithStatusSubresource(&ocv1.ClusterExtensionRevision{}).
				WithObjects(tc.existingObjs()...).
				Build()

			// reconcile cluster extension revision
			mockEngine := &mockRevisionEngine{
				reconcile: func(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionReconcileOption) (machinery.RevisionResult, error) {
					return tc.revisionResult, nil
				},
				teardown: tc.revisionEngineTeardownFn(t),
			}
			result, err := (&controllers.ClusterExtensionRevisionReconciler{
				Client:                testClient,
				RevisionEngineFactory: &mockRevisionEngineFactory{engine: mockEngine},
				TrackingCache: &mockTrackingCache{
					client: testClient,
					freeFn: tc.trackingCacheFreeFn,
				},
			}).Reconcile(t.Context(), ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name: clusterExtensionRevisionName,
				},
			})

			// reconcile cluster extension revision
			require.Equal(t, ctrl.Result{}, result)
			if tc.expectedErr != "" {
				require.Contains(t, err.Error(), tc.expectedErr)
			} else {
				require.NoError(t, err)
			}

			// validate test case
			tc.validate(t, testClient)
		})
	}
}

func Test_ClusterExtensionRevisionReconciler_Reconcile_ProgressDeadline(t *testing.T) {
	const (
		clusterExtensionRevisionName = "test-ext-1"
	)

	testScheme := newScheme(t)
	require.NoError(t, corev1.AddToScheme(testScheme))

	for _, tc := range []struct {
		name            string
		existingObjs    func() []client.Object
		revisionResult  machinery.RevisionResult
		validate        func(*testing.T, client.Client)
		reconcileErr    error
		reconcileResult ctrl.Result
	}{
		{
			name: "progressing set to false when progress deadline is exceeded",
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				rev1 := newTestClusterExtensionRevision(t, clusterExtensionRevisionName, ext, testScheme)
				rev1.Spec.ProgressDeadlineMinutes = 1
				rev1.CreationTimestamp = metav1.NewTime(time.Now().Add(-61 * time.Second))
				return []client.Object{rev1, ext}
			},
			revisionResult: &mockRevisionResult{
				inTransition: true,
			},
			validate: func(t *testing.T, c client.Client) {
				rev := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{
					Name: clusterExtensionRevisionName,
				}, rev)
				require.NoError(t, err)
				cnd := meta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeProgressing)
				require.Equal(t, metav1.ConditionFalse, cnd.Status)
				require.Equal(t, ocv1.ReasonProgressDeadlineExceeded, cnd.Reason)
			},
		},
		{
			name: "requeue after progressDeadline time for final progression deadline check",
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				rev1 := newTestClusterExtensionRevision(t, clusterExtensionRevisionName, ext, testScheme)
				rev1.Spec.ProgressDeadlineMinutes = 1
				rev1.CreationTimestamp = metav1.NewTime(time.Now())
				return []client.Object{rev1, ext}
			},
			revisionResult: &mockRevisionResult{
				inTransition: true,
			},
			reconcileResult: ctrl.Result{RequeueAfter: 1 * time.Minute},
			validate: func(t *testing.T, c client.Client) {
				rev := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{
					Name: clusterExtensionRevisionName,
				}, rev)
				require.NoError(t, err)
				cnd := meta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeProgressing)
				require.Equal(t, metav1.ConditionTrue, cnd.Status)
				require.Equal(t, ocv1.ReasonRollingOut, cnd.Reason)
			},
		},
		{
			name: "no progression deadline checks on revision recovery",
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				rev1 := newTestClusterExtensionRevision(t, clusterExtensionRevisionName, ext, testScheme)
				rev1.Spec.ProgressDeadlineMinutes = 1
				rev1.CreationTimestamp = metav1.NewTime(time.Now().Add(-2 * time.Minute))
				meta.SetStatusCondition(&rev1.Status.Conditions, metav1.Condition{
					Type:               ocv1.ClusterExtensionRevisionTypeProgressing,
					Status:             metav1.ConditionTrue,
					Reason:             ocv1.ReasonSucceeded,
					ObservedGeneration: rev1.Generation,
				})
				meta.SetStatusCondition(&rev1.Status.Conditions, metav1.Condition{
					Type:               ocv1.ClusterExtensionRevisionTypeSucceeded,
					Status:             metav1.ConditionTrue,
					Reason:             ocv1.ReasonSucceeded,
					ObservedGeneration: rev1.Generation,
				})
				return []client.Object{rev1, ext}
			},
			revisionResult: &mockRevisionResult{
				inTransition: true,
			},
			validate: func(t *testing.T, c client.Client) {
				rev := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{
					Name: clusterExtensionRevisionName,
				}, rev)
				require.NoError(t, err)
				cnd := meta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeProgressing)
				require.Equal(t, metav1.ConditionTrue, cnd.Status)
				require.Equal(t, ocv1.ReasonRollingOut, cnd.Reason)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// create extension and cluster extension
			testClient := fake.NewClientBuilder().
				WithScheme(testScheme).
				WithStatusSubresource(&ocv1.ClusterExtensionRevision{}).
				WithObjects(tc.existingObjs()...).
				Build()

			// reconcile cluster extension revision
			mockEngine := &mockRevisionEngine{
				reconcile: func(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionReconcileOption) (machinery.RevisionResult, error) {
					return tc.revisionResult, nil
				},
			}
			result, err := (&controllers.ClusterExtensionRevisionReconciler{
				Client:                testClient,
				RevisionEngineFactory: &mockRevisionEngineFactory{engine: mockEngine},
				TrackingCache: &mockTrackingCache{
					client: testClient,
				},
			}).Reconcile(t.Context(), ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name: clusterExtensionRevisionName,
				},
			})
			require.Equal(t, tc.reconcileResult, result)
			require.Equal(t, tc.reconcileErr, err)

			tc.validate(t, testClient)
		})
	}
}

func newTestClusterExtension() *ocv1.ClusterExtension {
	return &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ext",
			UID:  "test-ext",
		},
		Spec: ocv1.ClusterExtensionSpec{
			Namespace: "some-namespace",
			ServiceAccount: ocv1.ServiceAccountReference{
				Name: "service-account",
			},
			Source: ocv1.SourceConfig{
				SourceType: ocv1.SourceTypeCatalog,
				Catalog: &ocv1.CatalogFilter{
					PackageName: "some-package",
				},
			},
		},
	}
}

func newTestClusterExtensionRevision(t *testing.T, revisionName string, ext *ocv1.ClusterExtension, scheme *runtime.Scheme) *ocv1.ClusterExtensionRevision {
	t.Helper()

	// Extract revision number from name (e.g., "rev-1" -> 1, "test-ext-10" -> 10)
	revNum := controllers.ExtractRevisionNumber(t, revisionName)

	rev := &ocv1.ClusterExtensionRevision{
		ObjectMeta: metav1.ObjectMeta{
			Name:       revisionName,
			UID:        types.UID(revisionName),
			Generation: int64(1),
			Annotations: map[string]string{
				labels.PackageNameKey:             "some-package",
				labels.BundleNameKey:              "some-package.v1.0.0",
				labels.BundleReferenceKey:         "registry.io/some-repo/some-package:v1.0.0",
				labels.BundleVersionKey:           "1.0.0",
				labels.ServiceAccountNameKey:      ext.Spec.ServiceAccount.Name,
				labels.ServiceAccountNamespaceKey: ext.Spec.Namespace,
			},
			Labels: map[string]string{
				labels.OwnerNameKey: "test-ext",
			},
		},
		Spec: ocv1.ClusterExtensionRevisionSpec{
			LifecycleState: ocv1.ClusterExtensionRevisionLifecycleStateActive,
			Revision:       revNum,
			Phases: []ocv1.ClusterExtensionRevisionPhase{
				{
					Name: "everything",
					Objects: []ocv1.ClusterExtensionRevisionObject{
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "ConfigMap",
									"data": map[string]interface{}{
										"foo": "bar",
									},
								},
							},
						},
					},
				},
			},
		},
	}
	require.NoError(t, controllerutil.SetControllerReference(ext, rev, scheme))
	return rev
}

type mockRevisionEngine struct {
	teardown  func(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionTeardownOption) (machinery.RevisionTeardownResult, error)
	reconcile func(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionReconcileOption) (machinery.RevisionResult, error)
}

func (m mockRevisionEngine) Teardown(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionTeardownOption) (machinery.RevisionTeardownResult, error) {
	return m.teardown(ctx, rev)
}

func (m mockRevisionEngine) Reconcile(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionReconcileOption) (machinery.RevisionResult, error) {
	return m.reconcile(ctx, rev)
}

// mockRevisionEngineFactory creates mock RevisionEngines for testing
type mockRevisionEngineFactory struct {
	engine    controllers.RevisionEngine
	createErr error
}

func (f *mockRevisionEngineFactory) CreateRevisionEngine(ctx context.Context, rev *ocv1.ClusterExtensionRevision) (controllers.RevisionEngine, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	return f.engine, nil
}

type mockRevisionResult struct {
	validationError *validation.RevisionValidationError
	phases          []machinery.PhaseResult
	inTransition    bool
	isComplete      bool
	hasProgressed   bool
	string          string
}

func (m mockRevisionResult) GetValidationError() *validation.RevisionValidationError {
	return m.validationError
}

func (m mockRevisionResult) GetPhases() []machinery.PhaseResult {
	return m.phases
}

func (m mockRevisionResult) InTransition() bool {
	return m.inTransition
}

func (m mockRevisionResult) IsComplete() bool {
	return m.isComplete
}

func (m mockRevisionResult) HasProgressed() bool {
	return m.hasProgressed
}

func (m mockRevisionResult) String() string {
	return m.string
}

var _ machinery.PhaseResult = &mockPhaseResult{}

type mockPhaseResult struct {
	name            string
	validationError *validation.PhaseValidationError
	objects         []machinery.ObjectResult
	inTransition    bool
	isComplete      bool
	hasProgressed   bool
	string          string
}

func (m mockPhaseResult) GetName() string {
	return m.name
}

func (m mockPhaseResult) GetValidationError() *validation.PhaseValidationError {
	return m.validationError
}

func (m mockPhaseResult) GetObjects() []machinery.ObjectResult {
	return m.objects
}

func (m mockPhaseResult) InTransition() bool {
	return m.inTransition
}

func (m mockPhaseResult) IsComplete() bool {
	return m.isComplete
}

func (m mockPhaseResult) HasProgressed() bool {
	return m.hasProgressed
}

func (m mockPhaseResult) String() string {
	return m.string
}

var _ machinery.ObjectResult = &mockObjectResult{}

type mockObjectResult struct {
	action   machinery.Action
	object   machinery.Object
	success  bool
	complete bool
	paused   bool
	probes   machinerytypes.ProbeResultContainer
	string   string
}

func (m mockObjectResult) ProbeResults() machinerytypes.ProbeResultContainer {
	return m.probes
}

func (m mockObjectResult) IsComplete() bool {
	return m.complete
}

func (m mockObjectResult) IsPaused() bool {
	return m.paused
}

func (m mockObjectResult) Action() machinery.Action {
	return m.action
}

func (m mockObjectResult) Object() machinery.Object {
	return m.object
}

func (m mockObjectResult) Success() bool {
	return m.success
}

func (m mockObjectResult) String() string {
	return m.string
}

var _ machinery.RevisionTeardownResult = mockRevisionTeardownResult{}

type mockRevisionTeardownResult struct {
	phases            []machinery.PhaseTeardownResult
	isComplete        bool
	waitingPhaseNames []string
	activePhaseName   string
	phaseIsActive     bool
	gonePhaseNames    []string
	string            string
}

func (m mockRevisionTeardownResult) GetPhases() []machinery.PhaseTeardownResult {
	return m.phases
}

func (m mockRevisionTeardownResult) IsComplete() bool {
	return m.isComplete
}

func (m mockRevisionTeardownResult) GetWaitingPhaseNames() []string {
	return m.waitingPhaseNames
}

func (m mockRevisionTeardownResult) GetActivePhaseName() (string, bool) {
	return m.activePhaseName, m.phaseIsActive
}

func (m mockRevisionTeardownResult) GetGonePhaseNames() []string {
	return m.gonePhaseNames
}

func (m mockRevisionTeardownResult) String() string {
	return m.string
}

type mockTrackingCache struct {
	client client.Client
	freeFn func(context.Context, client.Object) error
}

func (m *mockTrackingCache) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return m.client.Get(ctx, key, obj, opts...)
}

func (m *mockTrackingCache) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return m.client.List(ctx, list, opts...)
}

func (m *mockTrackingCache) Source(handler handler.EventHandler, predicates ...predicate.Predicate) source.Source {
	panic("not implemented")
}

func (m *mockTrackingCache) Watch(ctx context.Context, user client.Object, gvks sets.Set[schema.GroupVersionKind]) error {
	return nil
}

func (m *mockTrackingCache) Free(ctx context.Context, user client.Object) error {
	if m.freeFn != nil {
		return m.freeFn(ctx, user)
	}
	return nil
}

func Test_ClusterExtensionRevisionReconciler_getScopedClient_Errors(t *testing.T) {
	testScheme := newScheme(t)

	t.Run("works with serviceAccount annotation and without owner label", func(t *testing.T) {
		rev := &ocv1.ClusterExtensionRevision{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "test-rev-1",
				Labels: map[string]string{},
				Annotations: map[string]string{
					labels.ServiceAccountNameKey:      "test-sa",
					labels.ServiceAccountNamespaceKey: "test-ns",
					labels.BundleVersionKey:           "1.0.0",
				},
			},
			Spec: ocv1.ClusterExtensionRevisionSpec{
				LifecycleState: ocv1.ClusterExtensionRevisionLifecycleStateActive,
				Revision:       1,
				Phases:         []ocv1.ClusterExtensionRevisionPhase{},
			},
		}

		testClient := fake.NewClientBuilder().
			WithScheme(testScheme).
			WithStatusSubresource(&ocv1.ClusterExtensionRevision{}).
			WithObjects(rev).
			Build()

		mockEngine := &mockRevisionEngine{
			reconcile: func(ctx context.Context, r machinerytypes.Revision, opts ...machinerytypes.RevisionReconcileOption) (machinery.RevisionResult, error) {
				return &mockRevisionResult{}, nil
			},
		}

		reconciler := &controllers.ClusterExtensionRevisionReconciler{
			Client:                testClient,
			RevisionEngineFactory: &mockRevisionEngineFactory{engine: mockEngine},
			TrackingCache:         &mockTrackingCache{client: testClient},
		}

		_, err := reconciler.Reconcile(t.Context(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "test-rev-1"},
		})

		require.NoError(t, err)
	})

	t.Run("missing serviceAccount annotation", func(t *testing.T) {
		rev := &ocv1.ClusterExtensionRevision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-rev-1",
				Annotations: map[string]string{
					labels.BundleVersionKey: "1.0.0",
				},
			},
			Spec: ocv1.ClusterExtensionRevisionSpec{
				LifecycleState: ocv1.ClusterExtensionRevisionLifecycleStateActive,
				Revision:       1,
				Phases:         []ocv1.ClusterExtensionRevisionPhase{},
			},
		}

		testClient := fake.NewClientBuilder().
			WithScheme(testScheme).
			WithObjects(rev).
			Build()

		failingFactory := &mockRevisionEngineFactory{
			createErr: errors.New("missing serviceAccount name annotation"),
		}

		reconciler := &controllers.ClusterExtensionRevisionReconciler{
			Client:                testClient,
			RevisionEngineFactory: failingFactory,
			TrackingCache:         &mockTrackingCache{client: testClient},
		}

		_, err := reconciler.Reconcile(t.Context(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "test-rev-1"},
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "serviceAccount")
	})

	t.Run("factory fails to create engine", func(t *testing.T) {
		ext := newTestClusterExtension()
		rev := newTestClusterExtensionRevision(t, "test-rev", ext, testScheme)

		testClient := fake.NewClientBuilder().
			WithScheme(testScheme).
			WithObjects(ext, rev).
			Build()

		failingFactory := &mockRevisionEngineFactory{
			createErr: errors.New("token getter failed"),
		}

		reconciler := &controllers.ClusterExtensionRevisionReconciler{
			Client:                testClient,
			RevisionEngineFactory: failingFactory,
			TrackingCache:         &mockTrackingCache{client: testClient},
		}

		_, err := reconciler.Reconcile(t.Context(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "test-rev"},
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to create revision engine")
		require.Contains(t, err.Error(), "token getter failed")
	})
}
