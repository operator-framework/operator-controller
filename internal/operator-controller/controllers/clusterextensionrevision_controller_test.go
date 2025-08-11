package controllers_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"pkg.package-operator.run/boxcutter"
	"pkg.package-operator.run/boxcutter/machinery"
	machinerytypes "pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/controllers"
)

func Test_ClusterExtensionRevisionReconciler_Reconcile_RevisionProgression(t *testing.T) {
	const (
		clusterExtensionRevisionName = "test-ext-1"
	)

	testScheme := newScheme(t)

	for _, tc := range []struct {
		name           string
		existingObjs   func() []client.Object
		revisionResult machinery.RevisionResult
		validate       func(*testing.T, client.Client)
	}{
		{
			name:           "sets teardown finalizer",
			revisionResult: mockRevisionResult{},
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				rev1 := newTestClusterExtensionRevision(clusterExtensionRevisionName)
				require.NoError(t, controllerutil.SetControllerReference(ext, rev1, testScheme))
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
			name:           "set Available:False:InComplete status condition during rollout when no probe failures are detected",
			revisionResult: mockRevisionResult{},
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				rev1 := newTestClusterExtensionRevision(clusterExtensionRevisionName)
				require.NoError(t, controllerutil.SetControllerReference(ext, rev1, testScheme))
				return []client.Object{ext, rev1}
			},
			validate: func(t *testing.T, c client.Client) {
				rev := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{
					Name: clusterExtensionRevisionName,
				}, rev)
				require.NoError(t, err)
				cond := meta.FindStatusCondition(rev.Status.Conditions, "Available")
				require.NotNil(t, cond)
				require.Equal(t, metav1.ConditionFalse, cond.Status)
				require.Equal(t, "Incomplete", cond.Reason)
				require.Equal(t, "Revision has not been rolled out completely.", cond.Message)
				require.Equal(t, int64(1), cond.ObservedGeneration)
			},
		},
		{
			name: "set Available:False:ProbeFailure condition when probe failures are detected",
			revisionResult: mockRevisionResult{
				phases: []machinery.PhaseResult{
					mockPhaseResult{
						name:       "somephase",
						isComplete: false,
						objects: []machinery.ObjectResult{
							mockObjectResult{
								success: true,
								probes: map[string]machinery.ObjectProbeResult{
									boxcutter.ProgressProbeType: {
										Success: true,
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
								probes: map[string]machinery.ObjectProbeResult{
									boxcutter.ProgressProbeType: {
										Success: false,
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
								probes: map[string]machinery.ObjectProbeResult{
									boxcutter.ProgressProbeType: {
										Success: false,
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
				rev1 := newTestClusterExtensionRevision(clusterExtensionRevisionName)
				require.NoError(t, controllerutil.SetControllerReference(ext, rev1, testScheme))
				return []client.Object{ext, rev1}
			},
			validate: func(t *testing.T, c client.Client) {
				rev := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{
					Name: clusterExtensionRevisionName,
				}, rev)
				require.NoError(t, err)
				cond := meta.FindStatusCondition(rev.Status.Conditions, "Available")
				require.NotNil(t, cond)
				require.Equal(t, metav1.ConditionFalse, cond.Status)
				require.Equal(t, "ProbeFailure", cond.Reason)
				require.Equal(t, "Object Service.v1 my-namespace/my-service: something bad happened and something worse happened\nObject ConfigMap.v1 my-namespace/my-configmap: we have a problem", cond.Message)
				require.Equal(t, int64(1), cond.ObservedGeneration)
			},
		},
		{
			name: "set InTransition:True:InTransition condition while revision is transitioning",
			revisionResult: mockRevisionResult{
				inTransition: true,
			},
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				rev1 := newTestClusterExtensionRevision(clusterExtensionRevisionName)
				require.NoError(t, controllerutil.SetControllerReference(ext, rev1, testScheme))
				return []client.Object{ext, rev1}
			},
			validate: func(t *testing.T, c client.Client) {
				rev := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{
					Name: clusterExtensionRevisionName,
				}, rev)
				require.NoError(t, err)
				cond := meta.FindStatusCondition(rev.Status.Conditions, "InTransition")
				require.NotNil(t, cond)
				require.Equal(t, metav1.ConditionTrue, cond.Status)
				require.Equal(t, "InTransition", cond.Reason)
				require.Equal(t, "Rollout in progress.", cond.Message)
				require.Equal(t, int64(1), cond.ObservedGeneration)
			},
		},
		{
			name: "remove InTransition condition once transition rollout is finished",
			revisionResult: mockRevisionResult{
				inTransition: false,
			},
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				rev1 := newTestClusterExtensionRevision(clusterExtensionRevisionName)
				require.NoError(t, controllerutil.SetControllerReference(ext, rev1, testScheme))
				meta.SetStatusCondition(&rev1.Status.Conditions, metav1.Condition{
					Type:               "InTransition",
					Status:             metav1.ConditionTrue,
					Reason:             "InTransition",
					Message:            "some message",
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
				cond := meta.FindStatusCondition(rev.Status.Conditions, "InTransition")
				require.Nil(t, cond)
			},
		},
		{
			name: "set Available:True:Available and Succeeded:True:RolloutSuccess conditions on successful revision rollout",
			revisionResult: mockRevisionResult{
				isComplete: true,
			},
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				rev1 := newTestClusterExtensionRevision(clusterExtensionRevisionName)
				require.NoError(t, controllerutil.SetControllerReference(ext, rev1, testScheme))
				return []client.Object{ext, rev1}
			},
			validate: func(t *testing.T, c client.Client) {
				rev := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{
					Name: clusterExtensionRevisionName,
				}, rev)
				require.NoError(t, err)
				cond := meta.FindStatusCondition(rev.Status.Conditions, "Available")
				require.NotNil(t, cond)
				require.Equal(t, metav1.ConditionTrue, cond.Status)
				require.Equal(t, "Available", cond.Reason)
				require.Equal(t, "Object is available and passes all probes.", cond.Message)
				require.Equal(t, int64(1), cond.ObservedGeneration)

				cond = meta.FindStatusCondition(rev.Status.Conditions, "Succeeded")
				require.NotNil(t, cond)
				require.Equal(t, metav1.ConditionTrue, cond.Status)
				require.Equal(t, "RolloutSuccess", cond.Reason)
				require.Equal(t, "Revision succeeded rolling out.", cond.Message)
				require.Equal(t, int64(1), cond.ObservedGeneration)
			},
		},
		{
			name: "archive previous revisions on successful rollout",
			revisionResult: mockRevisionResult{
				isComplete: true,
			},
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				prevRev1 := newTestClusterExtensionRevision("prev-rev-1")
				require.NoError(t, controllerutil.SetControllerReference(ext, prevRev1, testScheme))
				prevRev2 := newTestClusterExtensionRevision("prev-rev-2")
				require.NoError(t, controllerutil.SetControllerReference(ext, prevRev2, testScheme))
				rev1 := newTestClusterExtensionRevision("test-ext-1")
				rev1.Spec.Previous = []ocv1.ClusterExtensionRevisionPrevious{
					{
						Name: "prev-rev-1",
						UID:  "prev-rev-1",
					}, {
						Name: "prev-rev-2",
						UID:  "prev-rev-2",
					},
				}
				require.NoError(t, controllerutil.SetControllerReference(ext, rev1, testScheme))
				return []client.Object{ext, prevRev1, prevRev2, rev1}
			},
			validate: func(t *testing.T, c client.Client) {
				rev := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{
					Name: "prev-rev-1",
				}, rev)
				require.NoError(t, err)
				require.Equal(t, ocv1.ClusterExtensionRevisionLifecycleStateArchived, rev.Spec.LifecycleState)

				err = c.Get(t.Context(), client.ObjectKey{
					Name: "prev-rev-2",
				}, rev)
				require.NoError(t, err)
				require.Equal(t, ocv1.ClusterExtensionRevisionLifecycleStateArchived, rev.Spec.LifecycleState)
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
			result, err := (&controllers.ClusterExtensionRevisionReconciler{
				Client: testClient,
				RevisionEngine: &mockRevisionEngine{
					reconcile: func(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionReconcileOption) (machinery.RevisionResult, error) {
						return tc.revisionResult, nil
					},
				},
			}).Reconcile(t.Context(), ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name: clusterExtensionRevisionName,
				},
			})

			// reconcile cluster extensionr evision
			require.Equal(t, ctrl.Result{}, result)
			require.NoError(t, err)

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
			rev1 := newTestClusterExtensionRevision(clusterExtensionRevisionName)
			require.NoError(t, controllerutil.SetControllerReference(ext, rev1, testScheme))

			// create extension and cluster extension
			testClient := fake.NewClientBuilder().
				WithScheme(testScheme).
				WithStatusSubresource(&ocv1.ClusterExtensionRevision{}).
				WithObjects(ext, rev1).
				Build()

			// reconcile cluster extension revision
			result, err := (&controllers.ClusterExtensionRevisionReconciler{
				Client: testClient,
				RevisionEngine: &mockRevisionEngine{
					reconcile: func(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionReconcileOption) (machinery.RevisionResult, error) {
						return tc.revisionResult, nil
					},
				},
			}).Reconcile(t.Context(), ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name: clusterExtensionRevisionName,
				},
			})

			// reconcile cluster extensionr evision
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
		expectedErr              string
	}{
		{
			name:           "teardown finalizer is removed",
			revisionResult: mockRevisionResult{},
			existingObjs: func() []client.Object {
				rev1 := newTestClusterExtensionRevision(clusterExtensionRevisionName)
				rev1.Finalizers = []string{
					"olm.operatorframework.io/teardown",
				}
				return []client.Object{rev1}
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
				rev1 := newTestClusterExtensionRevision(clusterExtensionRevisionName)
				rev1.Finalizers = []string{
					"olm.operatorframework.io/teardown",
				}
				rev1.DeletionTimestamp = &metav1.Time{Time: time.Now()}
				require.NoError(t, controllerutil.SetControllerReference(ext, rev1, testScheme))
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
				require.True(t, errors.IsNotFound(err))
			},
		},
		{
			name:           "surfaces tear down errors when deleted",
			revisionResult: mockRevisionResult{},
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				rev1 := newTestClusterExtensionRevision(clusterExtensionRevisionName)
				rev1.Finalizers = []string{
					"olm.operatorframework.io/teardown",
				}
				rev1.DeletionTimestamp = &metav1.Time{Time: time.Now()}
				require.NoError(t, controllerutil.SetControllerReference(ext, rev1, testScheme))
				return []client.Object{rev1, ext}
			},
			revisionEngineTeardownFn: func(t *testing.T) func(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionTeardownOption) (machinery.RevisionTeardownResult, error) {
				return func(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionTeardownOption) (machinery.RevisionTeardownResult, error) {
					return nil, fmt.Errorf("some teardown error")
				}
			},
			expectedErr: "some teardown error",
			validate: func(t *testing.T, c client.Client) {
				t.Log("cluster revision is not deleted and still contains finalizer")
				rev := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{
					Name: clusterExtensionRevisionName,
				}, rev)
				require.NoError(t, err)
				require.NotContains(t, "olm.operatorframework.io/teardown", rev.Finalizers)
			},
		},
		{
			name:           "revision is torn down when in archived state and finalizer is removed",
			revisionResult: mockRevisionResult{},
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				rev1 := newTestClusterExtensionRevision(clusterExtensionRevisionName)
				rev1.Finalizers = []string{
					"olm.operatorframework.io/teardown",
				}
				rev1.Spec.LifecycleState = ocv1.ClusterExtensionRevisionLifecycleStateArchived
				require.NoError(t, controllerutil.SetControllerReference(ext, rev1, testScheme))
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
				require.NotContains(t, "olm.operatorframework.io/teardown", rev.Finalizers)
			},
		},
		{
			name:           "surfaces revision teardown error when in archived state",
			revisionResult: mockRevisionResult{},
			existingObjs: func() []client.Object {
				ext := newTestClusterExtension()
				rev1 := newTestClusterExtensionRevision(clusterExtensionRevisionName)
				rev1.Finalizers = []string{
					"olm.operatorframework.io/teardown",
				}
				rev1.Spec.LifecycleState = ocv1.ClusterExtensionRevisionLifecycleStateArchived
				require.NoError(t, controllerutil.SetControllerReference(ext, rev1, testScheme))
				return []client.Object{rev1, ext}
			},
			revisionEngineTeardownFn: func(t *testing.T) func(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionTeardownOption) (machinery.RevisionTeardownResult, error) {
				return func(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionTeardownOption) (machinery.RevisionTeardownResult, error) {
					return nil, fmt.Errorf("some teardown error")
				}
			},
			expectedErr: "some teardown error",
			validate: func(t *testing.T, c client.Client) {
				t.Log("cluster revision is not deleted and still contains finalizer")
				rev := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{
					Name: clusterExtensionRevisionName,
				}, rev)
				require.NoError(t, err)
				require.NotContains(t, "olm.operatorframework.io/teardown", rev.Finalizers)
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
			result, err := (&controllers.ClusterExtensionRevisionReconciler{
				Client: testClient,
				RevisionEngine: &mockRevisionEngine{
					reconcile: func(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionReconcileOption) (machinery.RevisionResult, error) {
						return tc.revisionResult, nil
					},
					teardown: tc.revisionEngineTeardownFn(t),
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

func newTestClusterExtensionRevision(name string) *ocv1.ClusterExtensionRevision {
	return &ocv1.ClusterExtensionRevision{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			UID:        types.UID(name),
			Generation: int64(1),
		},
		Spec: ocv1.ClusterExtensionRevisionSpec{
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

func (m mockRevisionResult) InTransistion() bool {
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

func (m mockPhaseResult) InTransistion() bool {
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

type mockObjectResult struct {
	action  machinery.Action
	object  machinery.Object
	success bool
	probes  map[string]machinery.ObjectProbeResult
	string  string
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

func (m mockObjectResult) Probes() map[string]machinery.ObjectProbeResult {
	return m.probes
}

func (m mockObjectResult) String() string {
	return m.string
}

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
