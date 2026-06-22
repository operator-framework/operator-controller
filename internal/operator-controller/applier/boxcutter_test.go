package applier_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	k8scheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	ocv1ac "github.com/operator-framework/operator-controller/applyconfigurations/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
	bundlecsv "github.com/operator-framework/operator-controller/internal/testing/bundle/csv"
	bundlefs "github.com/operator-framework/operator-controller/internal/testing/bundle/fs"
	mockapplier "github.com/operator-framework/operator-controller/internal/testutil/mock/applier"
	mockctrlclient "github.com/operator-framework/operator-controller/internal/testutil/mock/ctrlclient"
)

var (
	dummyBundle = bundlefs.Builder().
		WithPackageName("test-package").
		WithCSV(bundlecsv.Builder().WithName("test-csv").Build()).
		Build()
)

func Test_SimpleRevisionGenerator_GenerateRevisionFromHelmRelease(t *testing.T) {
	g := &applier.SimpleRevisionGenerator{}

	helmRelease := &release.Release{
		Name: "test-123",
		Manifest: strings.Join(strings.Fields(`
		{
			"apiVersion":"v1",
			"kind":"ConfigMap",
			"metadata":{
				"finalizers":["test"],
				"ownerReferences":[{"kind":"TestOwner"}],
				"creationTimestamp":{"time":"0"},
				"uid":"1a2b3c4d",
				"resourceVersion":"12345",
				"generation":123,
				"managedFields":[{"manager":"test-manager"}],
				"deletionTimestamp":{"time":"0"},
				"deletionGracePeriodSeconds":30,
			}, "status": {
				"replicas": 3,
			}
		}`), "") + "\n" + `{"apiVersion":"v1","kind":"Secret"}` + "\n",
		Labels: map[string]string{
			labels.BundleNameKey:      "my-bundle",
			labels.PackageNameKey:     "my-package",
			labels.BundleVersionKey:   "1.2.0",
			labels.BundleReferenceKey: "bundle-ref",
		},
	}

	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-123",
		},
		Spec: ocv1.ClusterExtensionSpec{
			Namespace: "test-namespace",
		},
	}

	objectLabels := map[string]string{
		"my-label": "my-value",
	}

	rev, err := g.GenerateRevisionFromHelmRelease(t.Context(), helmRelease, ext, objectLabels)
	require.NoError(t, err)

	expected := ocv1ac.ClusterObjectSet("test-123-1").
		WithAnnotations(map[string]string{
			"olm.operatorframework.io/bundle-name":      "my-bundle",
			"olm.operatorframework.io/bundle-reference": "bundle-ref",
			"olm.operatorframework.io/bundle-version":   "1.2.0",
			"olm.operatorframework.io/package-name":     "my-package",
		}).
		WithLabels(map[string]string{
			labels.OwnerKindKey: ocv1.ClusterExtensionKind,
			labels.OwnerNameKey: "test-123",
		}).
		WithSpec(ocv1ac.ClusterObjectSetSpec().
			WithLifecycleState(ocv1.ClusterObjectSetLifecycleStateActive).
			WithCollisionProtection(ocv1.CollisionProtectionNone).
			WithRevision(1).
			WithPhases(
				ocv1ac.ClusterObjectSetPhase().
					WithName("configuration").
					WithObjects(
						ocv1ac.ClusterObjectSetObject().
							WithObject(unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "ConfigMap",
									"metadata": map[string]interface{}{
										"labels": map[string]interface{}{
											"my-label": "my-value",
										},
										"annotations": map[string]interface{}{
											"olm.operatorframework.io/bundle-version": "1.2.0",
											"olm.operatorframework.io/package-name":   "my-package",
										},
									},
								},
							}),
						ocv1ac.ClusterObjectSetObject().
							WithObject(unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "Secret",
									"metadata": map[string]interface{}{
										"labels": map[string]interface{}{
											"my-label": "my-value",
										},
										"annotations": map[string]interface{}{
											"olm.operatorframework.io/bundle-version": "1.2.0",
											"olm.operatorframework.io/package-name":   "my-package",
										},
									},
								},
							}),
					)),
		)
	assert.Equal(t, expected.Name, rev.Name)
	assert.Equal(t, expected.Labels, rev.Labels)
	assert.Equal(t, expected.Annotations, rev.Annotations)
	assert.Equal(t, expected.Spec.LifecycleState, rev.Spec.LifecycleState)
	assert.Equal(t, expected.Spec.CollisionProtection, rev.Spec.CollisionProtection)
	assert.Equal(t, expected.Spec.Revision, rev.Spec.Revision)
	assert.Equal(t, expected.Spec.Phases, rev.Spec.Phases)
}

func Test_SimpleRevisionGenerator_GenerateRevision(t *testing.T) {
	ctrl := gomock.NewController(t)
	r := mockapplier.NewMockManifestProvider(ctrl)
	r.EXPECT().Get(gomock.Any(), gomock.Any()).Return([]client.Object{
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-service",
			},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "test-deployment",
				Namespace:   "test-ns",
				Labels:      map[string]string{"my-label": "my-label-value"},
				Annotations: map[string]string{"my-annotation": "my-annotation-value"},
				// Fields to be sanitized
				Finalizers:                 []string{"test"},
				OwnerReferences:            []metav1.OwnerReference{{Kind: "TestOwner"}},
				CreationTimestamp:          metav1.Time{Time: metav1.Now().Time},
				UID:                        "1a2b3c4d",
				ResourceVersion:            "12345",
				Generation:                 123,
				ManagedFields:              []metav1.ManagedFieldsEntry{{Manager: "test-manager"}},
				DeletionTimestamp:          &metav1.Time{Time: metav1.Now().Time},
				DeletionGracePeriodSeconds: func(i int64) *int64 { return &i }(30),
			}, Status: appsv1.DeploymentStatus{
				Replicas: 3,
			},
		},
	}, nil).AnyTimes()

	b := applier.SimpleRevisionGenerator{
		Scheme:           k8scheme.Scheme,
		ManifestProvider: r,
	}

	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-extension",
		},
		Spec: ocv1.ClusterExtensionSpec{
			Namespace: "test-namespace",
		},
	}

	rev, err := b.GenerateRevision(t.Context(), dummyBundle, ext, map[string]string{}, map[string]string{
		labels.BundleVersionKey: "1.0.0",
		labels.PackageNameKey:   "test-package",
	})
	require.NoError(t, err)

	t.Log("by checking the olm.operatorframework.io/owner-name and owner-kind labels are set")
	require.Equal(t, map[string]string{
		labels.OwnerKindKey: ocv1.ClusterExtensionKind,
		labels.OwnerNameKey: "test-extension",
	}, rev.Labels)
	t.Log("by checking the revision number is not set (defaults to zero value)")
	require.Nil(t, rev.Spec.Revision)
	t.Log("by checking the spec-level collisionProtection is set")
	require.Equal(t, ptr.To(ocv1.CollisionProtectionPrevent), rev.Spec.CollisionProtection)
	t.Log("by checking the rendered objects are present in the correct phases")
	require.Equal(t, []ocv1ac.ClusterObjectSetPhaseApplyConfiguration{
		*ocv1ac.ClusterObjectSetPhase().
			WithName(string(applier.PhaseInfrastructure)).
			WithObjects(
				ocv1ac.ClusterObjectSetObject().
					WithObject(unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Service",
							"metadata": map[string]interface{}{
								"name": "test-service",
								"annotations": map[string]interface{}{
									"olm.operatorframework.io/bundle-version": "1.0.0",
									"olm.operatorframework.io/package-name":   "test-package",
								},
							},
							"spec": map[string]interface{}{},
						},
					}),
			),
		*ocv1ac.ClusterObjectSetPhase().
			WithName(string(applier.PhaseDeploy)).
			WithObjects(
				ocv1ac.ClusterObjectSetObject().
					WithObject(unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "apps/v1",
							"kind":       "Deployment",
							"metadata": map[string]interface{}{
								"name":      "test-deployment",
								"namespace": "test-ns",
								"labels": map[string]interface{}{
									"my-label": "my-label-value",
								},
								"annotations": map[string]interface{}{
									"my-annotation": "my-annotation-value",
									"olm.operatorframework.io/bundle-version": "1.0.0",
									"olm.operatorframework.io/package-name":   "test-package",
								},
							},
							"spec": map[string]interface{}{
								"selector": nil,
								"template": map[string]interface{}{
									"metadata": map[string]interface{}{},
									"spec": map[string]interface{}{
										"containers": nil,
									},
								},
								"strategy": map[string]interface{}{},
							},
						},
					}),
			),
	}, rev.Spec.Phases)
}

func Test_SimpleRevisionGenerator_GenerateRevision_BundleAnnotations(t *testing.T) {
	ctrl := gomock.NewController(t)
	r := mockapplier.NewMockManifestProvider(ctrl)
	r.EXPECT().Get(gomock.Any(), gomock.Any()).Return([]client.Object{}, nil).AnyTimes()

	b := applier.SimpleRevisionGenerator{
		Scheme:           k8scheme.Scheme,
		ManifestProvider: r,
	}

	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-extension",
		},
		Spec: ocv1.ClusterExtensionSpec{
			Namespace: "test-namespace",
		},
	}

	t.Run("bundle properties are copied to the olm.properties annotation", func(t *testing.T) {
		bundleFS := bundlefs.Builder().
			WithPackageName("test-package").
			WithBundleProperty("olm.bundle.property", "some-value").
			WithBundleProperty("olm.another.bundle.property", "some-other-value").
			WithCSV(bundlecsv.Builder().WithName("test-csv").Build()).
			Build()

		rev, err := b.GenerateRevision(t.Context(), bundleFS, ext, map[string]string{}, map[string]string{})
		require.NoError(t, err)

		t.Log("by checking bundle properties are added to the revision annotations")
		require.NotNil(t, rev.Annotations)
		require.JSONEq(t, `[{"type":"olm.bundle.property","value":"some-value"},{"type":"olm.another.bundle.property","value":"some-other-value"}]`, rev.Annotations["olm.properties"])
	})

	t.Run("olm.properties should not be present if there are no bundle properties", func(t *testing.T) {
		bundleFS := bundlefs.Builder().
			WithPackageName("test-package").
			WithCSV(bundlecsv.Builder().WithName("test-csv").Build()).
			Build()

		rev, err := b.GenerateRevision(t.Context(), bundleFS, ext, map[string]string{}, map[string]string{})
		require.NoError(t, err)

		t.Log("by checking olm.properties is not present in the revision annotations")
		_, ok := rev.Annotations["olm.properties"]
		require.False(t, ok, "olm.properties should not be present in the revision annotations")
	})

	t.Run("csv annotations are not added to the revision annotations", func(t *testing.T) {
		bundleFS := bundlefs.Builder().
			WithPackageName("test-package").
			WithBundleProperty("olm.bundle.property", "some-value").
			WithCSV(bundlecsv.Builder().
				WithName("test-csv").
				WithAnnotations(map[string]string{
					"some.csv.annotation": "some-other-value",
				}).
				Build()).
			Build()

		rev, err := b.GenerateRevision(t.Context(), bundleFS, ext, map[string]string{}, map[string]string{})
		require.NoError(t, err)

		t.Log("by checking csv annotations are not added to the revision annotations")
		_, ok := rev.Annotations["olm.csv.annotation"]
		require.False(t, ok, "csv annotation should not be present in the revision annotations")
	})

	t.Run("errors getting bundle properties are surfaced", func(t *testing.T) {
		_, err := b.GenerateRevision(t.Context(), fstest.MapFS{}, ext, map[string]string{}, map[string]string{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "metadata/annotations.yaml: file does not exist")
	})
}

func Test_SimpleRevisionGenerator_Renderer_Integration(t *testing.T) {
	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-extension",
		},
	}
	ctrl := gomock.NewController(t)
	r := mockapplier.NewMockManifestProvider(ctrl)
	r.EXPECT().Get(dummyBundle, ext).DoAndReturn(
		func(b fs.FS, e *ocv1.ClusterExtension) ([]client.Object, error) {
			t.Log("by checking renderer was called with the correct parameters")
			require.Equal(t, dummyBundle, b)
			require.Equal(t, ext, e)
			return nil, nil
		}).AnyTimes()
	b := applier.SimpleRevisionGenerator{
		Scheme:           k8scheme.Scheme,
		ManifestProvider: r,
	}

	_, err := b.GenerateRevision(t.Context(), dummyBundle, ext, map[string]string{}, map[string]string{})
	require.NoError(t, err)
}

func Test_SimpleRevisionGenerator_AppliesObjectLabelsAndRevisionAnnotations(t *testing.T) {
	renderedObjs := []client.Object{
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-service",
				Labels: map[string]string{
					"app": "test-obj",
				},
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-configmap",
				Labels: map[string]string{
					"app": "test-obj",
				},
			},
		},
	}
	ctrl := gomock.NewController(t)
	r := mockapplier.NewMockManifestProvider(ctrl)
	r.EXPECT().Get(gomock.Any(), gomock.Any()).Return(renderedObjs, nil).AnyTimes()

	b := applier.SimpleRevisionGenerator{
		Scheme:           k8scheme.Scheme,
		ManifestProvider: r,
	}

	revAnnotations := map[string]string{
		"other": "value",
	}

	rev, err := b.GenerateRevision(t.Context(), dummyBundle, &ocv1.ClusterExtension{
		Spec: ocv1.ClusterExtensionSpec{
			Namespace: "test-namespace",
		},
	}, map[string]string{
		"some": "value",
	}, revAnnotations)
	require.NoError(t, err)
	t.Log("by checking the rendered objects contain the given object labels")
	for _, phase := range rev.Spec.Phases {
		for _, revObj := range phase.Objects {
			require.Equal(t, map[string]string{
				"app":  "test-obj",
				"some": "value",
			}, revObj.Object.GetLabels())
		}
	}
	t.Log("by checking the generated revision contain the given annotations")
	require.Equal(t, revAnnotations, rev.Annotations)
}

func Test_SimpleRevisionGenerator_PropagatesProgressDeadlineMinutes(t *testing.T) {
	ctrl := gomock.NewController(t)
	r := mockapplier.NewMockManifestProvider(ctrl)
	r.EXPECT().Get(gomock.Any(), gomock.Any()).Return([]client.Object{}, nil).AnyTimes()

	b := applier.SimpleRevisionGenerator{
		Scheme:           k8scheme.Scheme,
		ManifestProvider: r,
	}

	type args struct {
		progressDeadlineMinutes *int32
	}
	type want struct {
		progressDeadlineMinutes *int32
	}
	type testCase struct {
		args args
		want want
	}
	for name, tc := range map[string]testCase{
		"propagates when set": {
			args: args{
				progressDeadlineMinutes: ptr.To(int32(10)),
			},
			want: want{
				progressDeadlineMinutes: ptr.To(int32(10)),
			},
		},
		"do not propagate when unset": {
			want: want{
				progressDeadlineMinutes: nil,
			},
		},
	} {
		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-extension",
			},
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "test-namespace",
			},
		}
		empty := map[string]string{}
		t.Run(name, func(t *testing.T) {
			if pd := tc.args.progressDeadlineMinutes; pd != nil {
				ext.Spec.ProgressDeadlineMinutes = *pd
			}

			rev, err := b.GenerateRevision(t.Context(), dummyBundle, ext, empty, empty)
			require.NoError(t, err)
			require.Equal(t, tc.want.progressDeadlineMinutes, rev.Spec.ProgressDeadlineMinutes)
		})
	}
}

func Test_SimpleRevisionGenerator_Failure(t *testing.T) {
	ctrl := gomock.NewController(t)
	r := mockapplier.NewMockManifestProvider(ctrl)
	r.EXPECT().Get(gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("some-error")).AnyTimes()

	b := applier.SimpleRevisionGenerator{
		Scheme:           k8scheme.Scheme,
		ManifestProvider: r,
	}

	rev, err := b.GenerateRevision(t.Context(), fstest.MapFS{}, &ocv1.ClusterExtension{
		Spec: ocv1.ClusterExtensionSpec{
			Namespace: "test-namespace",
		},
	}, map[string]string{}, map[string]string{})
	require.Nil(t, rev)
	t.Log("by checking rendering errors are propagated")
	require.Error(t, err)
	require.Contains(t, err.Error(), "some-error")
}

func TestBoxcutter_Apply(t *testing.T) {
	testScheme := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(testScheme))
	require.NoError(t, corev1.AddToScheme(testScheme))

	// This is the revision that the mock builder will produce by default.
	// We calculate its hash to use in the tests.
	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ext",
			UID:  "test-uid",
		},
	}
	defaultDesiredRevision := &ocv1.ClusterObjectSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ext-1",
			UID:  "rev-uid-1",
			Labels: map[string]string{
				labels.OwnerNameKey: ext.Name,
			},
		},
		Spec: ocv1.ClusterObjectSetSpec{
			Revision: 1,
			Phases: []ocv1.ClusterObjectSetPhase{
				{
					Name: string(applier.PhaseDeploy),
					Objects: []ocv1.ClusterObjectSetObject{
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "ConfigMap",
									"metadata": map[string]interface{}{
										"name": "test-cm",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	allowedRevisionValue := func(revNum int64) *interceptor.Funcs {
		return &interceptor.Funcs{
			Apply: func(ctx context.Context, client client.WithWatch, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
				cos, ok := obj.(*ocv1ac.ClusterObjectSetApplyConfiguration)
				if !ok {
					return fmt.Errorf("expected ClusterObjectSetApplyConfiguration, got %T", obj)
				}
				if cos.Spec == nil || cos.Spec.Revision == nil || *cos.Spec.Revision != revNum {
					gk := ocv1.SchemeGroupVersion.WithKind("ClusterObjectSet").GroupKind()
					name := ""
					if n := cos.GetName(); n != nil {
						name = *n
					}
					return apierrors.NewInvalid(gk, name, field.ErrorList{field.Invalid(field.NewPath("spec.phases"), "immutable", "spec.phases is immutable")})
				}
				return client.Apply(ctx, obj, opts...)
			},
		}
	}
	testCases := []struct {
		name             string
		mockBuilder      func(t *testing.T) applier.ClusterObjectSetGenerator
		existingObjs     []client.Object
		expectedErr      string
		validate         func(t *testing.T, c client.Client)
		clientIterceptor *interceptor.Funcs
	}{
		{
			name: "first revision",
			mockBuilder: func(t *testing.T) applier.ClusterObjectSetGenerator {
				ctrl := gomock.NewController(t)
				m := mockapplier.NewMockClusterObjectSetGenerator(ctrl)
				m.EXPECT().GenerateRevision(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (*ocv1ac.ClusterObjectSetApplyConfiguration, error) {
						return ocv1ac.ClusterObjectSet("").
							WithAnnotations(revisionAnnotations).
							WithLabels(map[string]string{
								labels.OwnerNameKey: ext.Name,
							}).
							WithSpec(ocv1ac.ClusterObjectSetSpec().
								WithPhases(
									ocv1ac.ClusterObjectSetPhase().
										WithName(string(applier.PhaseDeploy)).
										WithObjects(
											ocv1ac.ClusterObjectSetObject().
												WithObject(unstructured.Unstructured{
													Object: map[string]interface{}{
														"apiVersion": "v1",
														"kind":       "ConfigMap",
														"metadata": map[string]interface{}{
															"name": "test-cm",
														},
													},
												}),
										),
								),
							), nil
					}).AnyTimes()
				m.EXPECT().GenerateRevisionFromHelmRelease(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
				return m
			},
			validate: func(t *testing.T, c client.Client) {
				revList := &ocv1.ClusterObjectSetList{}
				err := c.List(t.Context(), revList, client.MatchingLabels{labels.OwnerNameKey: ext.Name})
				require.NoError(t, err)
				require.Len(t, revList.Items, 1)

				rev := revList.Items[0]
				assert.Equal(t, "test-ext-1", rev.Name)
				assert.Equal(t, int64(1), rev.Spec.Revision)
				assert.Len(t, rev.OwnerReferences, 1)
				assert.Equal(t, ext.Name, rev.OwnerReferences[0].Name)
				assert.Equal(t, ext.UID, rev.OwnerReferences[0].UID)
			},
		},
		{
			name: "no change, revision exists",
			mockBuilder: func(t *testing.T) applier.ClusterObjectSetGenerator {
				ctrl := gomock.NewController(t)
				m := mockapplier.NewMockClusterObjectSetGenerator(ctrl)
				m.EXPECT().GenerateRevision(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (*ocv1ac.ClusterObjectSetApplyConfiguration, error) {
						return ocv1ac.ClusterObjectSet("").
							WithAnnotations(revisionAnnotations).
							WithLabels(map[string]string{
								labels.OwnerNameKey: ext.Name,
							}).
							WithSpec(ocv1ac.ClusterObjectSetSpec().
								WithPhases(
									ocv1ac.ClusterObjectSetPhase().
										WithName(string(applier.PhaseDeploy)).
										WithObjects(
											ocv1ac.ClusterObjectSetObject().
												WithObject(unstructured.Unstructured{
													Object: map[string]interface{}{
														"apiVersion": "v1",
														"kind":       "ConfigMap",
														"metadata": map[string]interface{}{
															"name": "test-cm",
														},
													},
												}),
										),
								),
							), nil
					}).AnyTimes()
				m.EXPECT().GenerateRevisionFromHelmRelease(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
				return m
			},
			existingObjs: []client.Object{
				defaultDesiredRevision,
			},
			validate: func(t *testing.T, c client.Client) {
				revList := &ocv1.ClusterObjectSetList{}
				err := c.List(context.Background(), revList, client.MatchingLabels{labels.OwnerNameKey: ext.Name})
				require.NoError(t, err)
				// No new revision should be created
				require.Len(t, revList.Items, 1)
				assert.Equal(t, "test-ext-1", revList.Items[0].Name)
			},
		},
		{
			name: "new revision created when objects in new revision are different",
			mockBuilder: func(t *testing.T) applier.ClusterObjectSetGenerator {
				ctrl := gomock.NewController(t)
				m := mockapplier.NewMockClusterObjectSetGenerator(ctrl)
				m.EXPECT().GenerateRevision(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (*ocv1ac.ClusterObjectSetApplyConfiguration, error) {
						return ocv1ac.ClusterObjectSet("").
							WithAnnotations(revisionAnnotations).
							WithLabels(map[string]string{
								labels.OwnerNameKey: ext.Name,
							}).
							WithSpec(ocv1ac.ClusterObjectSetSpec().
								WithPhases(
									ocv1ac.ClusterObjectSetPhase().
										WithName(string(applier.PhaseDeploy)).
										WithObjects(
											ocv1ac.ClusterObjectSetObject().
												WithObject(unstructured.Unstructured{
													Object: map[string]interface{}{
														"apiVersion": "v1",
														"kind":       "Secret",
														"metadata": map[string]interface{}{
															"name": "new-secret",
														},
													},
												}),
										),
								),
							), nil
					}).AnyTimes()
				m.EXPECT().GenerateRevisionFromHelmRelease(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
				return m
			},
			clientIterceptor: allowedRevisionValue(2),
			existingObjs: []client.Object{
				defaultDesiredRevision,
			},
			validate: func(t *testing.T, c client.Client) {
				revList := &ocv1.ClusterObjectSetList{}
				err := c.List(context.Background(), revList, client.MatchingLabels{labels.OwnerNameKey: ext.Name})
				require.NoError(t, err)
				require.Len(t, revList.Items, 2)

				// Find the new revision (rev 2)
				var newRev ocv1.ClusterObjectSet
				for _, r := range revList.Items {
					if r.Spec.Revision == 2 {
						newRev = r
						break
					}
				}
				require.NotNil(t, newRev)

				assert.Equal(t, "test-ext-2", newRev.Name)
				assert.Equal(t, int64(2), newRev.Spec.Revision)
			},
		},
		{
			name: "error from GenerateRevision",
			mockBuilder: func(t *testing.T) applier.ClusterObjectSetGenerator {
				ctrl := gomock.NewController(t)
				m := mockapplier.NewMockClusterObjectSetGenerator(ctrl)
				m.EXPECT().GenerateRevision(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("render boom")).AnyTimes()
				m.EXPECT().GenerateRevisionFromHelmRelease(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
				return m
			},
			expectedErr: "render boom",
			validate: func(t *testing.T, c client.Client) {
				// Ensure no revisions were created
				revList := &ocv1.ClusterObjectSetList{}
				err := c.List(context.Background(), revList, client.MatchingLabels{labels.OwnerNameKey: ext.Name})
				require.NoError(t, err)
				assert.Empty(t, revList.Items)
			},
		},
		{
			name: "keep at most 5 past revisions",
			mockBuilder: func(t *testing.T) applier.ClusterObjectSetGenerator {
				ctrl := gomock.NewController(t)
				m := mockapplier.NewMockClusterObjectSetGenerator(ctrl)
				m.EXPECT().GenerateRevision(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (*ocv1ac.ClusterObjectSetApplyConfiguration, error) {
						return ocv1ac.ClusterObjectSet("").
							WithAnnotations(revisionAnnotations).
							WithLabels(map[string]string{
								labels.OwnerNameKey: ext.Name,
							}).
							WithSpec(ocv1ac.ClusterObjectSetSpec()), nil
					}).AnyTimes()
				m.EXPECT().GenerateRevisionFromHelmRelease(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
				return m
			},
			existingObjs: []client.Object{
				&ocv1.ClusterObjectSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rev-1",
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterObjectSetSpec{
						LifecycleState: ocv1.ClusterObjectSetLifecycleStateArchived,
						Revision:       1,
					},
				},
				&ocv1.ClusterObjectSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rev-2",
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterObjectSetSpec{
						LifecycleState: ocv1.ClusterObjectSetLifecycleStateArchived,
						Revision:       2,
					},
				},
				&ocv1.ClusterObjectSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rev-3",
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterObjectSetSpec{
						LifecycleState: ocv1.ClusterObjectSetLifecycleStateArchived,
						Revision:       3,
					},
				},
				&ocv1.ClusterObjectSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rev-4",
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterObjectSetSpec{
						LifecycleState: ocv1.ClusterObjectSetLifecycleStateArchived,
						Revision:       4,
					},
				},
				&ocv1.ClusterObjectSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rev-5",
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterObjectSetSpec{
						LifecycleState: ocv1.ClusterObjectSetLifecycleStateArchived,
						Revision:       5,
					},
				},
				&ocv1.ClusterObjectSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rev-6",
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterObjectSetSpec{
						LifecycleState: ocv1.ClusterObjectSetLifecycleStateArchived,
						Revision:       6,
					},
				},
			},
			clientIterceptor: allowedRevisionValue(7),
			validate: func(t *testing.T, c client.Client) {
				rev1 := &ocv1.ClusterObjectSet{}
				err := c.Get(t.Context(), client.ObjectKey{Name: "rev-1"}, rev1)
				require.Error(t, err)
				assert.True(t, apierrors.IsNotFound(err))

				// Verify garbage collection: should only keep the limit + 1 (current) revisions
				revList := &ocv1.ClusterObjectSetList{}
				err = c.List(t.Context(), revList)
				require.NoError(t, err)
				// Should have ClusterObjectSetRetentionLimit (5) + current (1) = 6 revisions max
				assert.LessOrEqual(t, len(revList.Items), applier.ClusterObjectSetRetentionLimit+1)
			},
		},
		{
			name: "keep active revisions when they are out of limit",
			mockBuilder: func(t *testing.T) applier.ClusterObjectSetGenerator {
				ctrl := gomock.NewController(t)
				m := mockapplier.NewMockClusterObjectSetGenerator(ctrl)
				m.EXPECT().GenerateRevision(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (*ocv1ac.ClusterObjectSetApplyConfiguration, error) {
						return ocv1ac.ClusterObjectSet("").
							WithAnnotations(revisionAnnotations).
							WithLabels(map[string]string{
								labels.OwnerNameKey: ext.Name,
							}).
							WithSpec(ocv1ac.ClusterObjectSetSpec()), nil
					}).AnyTimes()
				m.EXPECT().GenerateRevisionFromHelmRelease(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
				return m
			},
			existingObjs: []client.Object{
				&ocv1.ClusterObjectSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rev-1",
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterObjectSetSpec{
						LifecycleState: ocv1.ClusterObjectSetLifecycleStateArchived,
						Revision:       1,
					},
				},
				&ocv1.ClusterObjectSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rev-2",
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterObjectSetSpec{
						// index beyond the retention limit but active; should be preserved
						LifecycleState: ocv1.ClusterObjectSetLifecycleStateActive,
						Revision:       2,
					},
				},
				&ocv1.ClusterObjectSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rev-3",
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterObjectSetSpec{
						LifecycleState: ocv1.ClusterObjectSetLifecycleStateActive,
						Revision:       3,
					},
				},
				&ocv1.ClusterObjectSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rev-4",
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterObjectSetSpec{
						// archived but should be preserved since it is within the limit
						LifecycleState: ocv1.ClusterObjectSetLifecycleStateArchived,
						Revision:       4,
					},
				},
				&ocv1.ClusterObjectSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rev-5",
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterObjectSetSpec{
						LifecycleState: ocv1.ClusterObjectSetLifecycleStateActive,
						Revision:       5,
					},
				},
				&ocv1.ClusterObjectSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rev-6",
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterObjectSetSpec{
						LifecycleState: ocv1.ClusterObjectSetLifecycleStateActive,
						Revision:       6,
					},
				},
				&ocv1.ClusterObjectSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rev-7",
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterObjectSetSpec{
						LifecycleState: ocv1.ClusterObjectSetLifecycleStateActive,
						Revision:       7,
					},
				},
			},
			clientIterceptor: allowedRevisionValue(8),
			validate: func(t *testing.T, c client.Client) {
				rev1 := &ocv1.ClusterObjectSet{}
				err := c.Get(t.Context(), client.ObjectKey{Name: "rev-1"}, rev1)
				require.Error(t, err)
				assert.True(t, apierrors.IsNotFound(err))

				rev2 := &ocv1.ClusterObjectSet{}
				err = c.Get(t.Context(), client.ObjectKey{Name: "rev-2"}, rev2)
				require.NoError(t, err)

				// Verify active revisions are kept even if beyond the limit
				rev4 := &ocv1.ClusterObjectSet{}
				err = c.Get(t.Context(), client.ObjectKey{Name: "rev-4"}, rev4)
				require.NoError(t, err, "active revision 4 should still exist even though it's beyond the limit")
			},
		},
		{
			name: "annotation-only update (same phases, different annotations)",
			mockBuilder: func(t *testing.T) applier.ClusterObjectSetGenerator {
				ctrl := gomock.NewController(t)
				m := mockapplier.NewMockClusterObjectSetGenerator(ctrl)
				m.EXPECT().GenerateRevision(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (*ocv1ac.ClusterObjectSetApplyConfiguration, error) {
						return ocv1ac.ClusterObjectSet("").
							WithAnnotations(revisionAnnotations).
							WithLabels(map[string]string{
								labels.OwnerNameKey: ext.Name,
							}).
							WithSpec(ocv1ac.ClusterObjectSetSpec().
								WithPhases(
									ocv1ac.ClusterObjectSetPhase().
										WithName(string(applier.PhaseDeploy)).
										WithObjects(
											ocv1ac.ClusterObjectSetObject().
												WithObject(unstructured.Unstructured{
													Object: map[string]interface{}{
														"apiVersion": "v1",
														"kind":       "ConfigMap",
														"metadata": map[string]interface{}{
															"name": "test-cm",
														},
													},
												}),
										),
								),
							), nil
					}).AnyTimes()
				m.EXPECT().GenerateRevisionFromHelmRelease(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
				return m
			},
			existingObjs: []client.Object{
				ext,
				&ocv1.ClusterObjectSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-ext-1",
						Annotations: map[string]string{
							labels.BundleVersionKey: "1.0.0",
							labels.PackageNameKey:   "test-package",
						},
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterObjectSetSpec{
						Revision: 1,
						Phases: []ocv1.ClusterObjectSetPhase{
							{
								Name: string(applier.PhaseDeploy),
								Objects: []ocv1.ClusterObjectSetObject{
									{
										Object: unstructured.Unstructured{
											Object: map[string]interface{}{
												"apiVersion": "v1",
												"kind":       "ConfigMap",
												"metadata": map[string]interface{}{
													"name": "test-cm",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			validate: func(t *testing.T, c client.Client) {
				revList := &ocv1.ClusterObjectSetList{}
				err := c.List(context.Background(), revList, client.MatchingLabels{labels.OwnerNameKey: ext.Name})
				require.NoError(t, err)
				// Should still be only 1 revision (in-place update, not new revision)
				require.Len(t, revList.Items, 1)

				rev := revList.Items[0]
				assert.Equal(t, "test-ext-1", rev.Name)
				assert.Equal(t, int64(1), rev.Spec.Revision)
				// Verify annotations were updated
				assert.Equal(t, "1.0.1", rev.Annotations[labels.BundleVersionKey])
				assert.Equal(t, "test-package", rev.Annotations[labels.PackageNameKey])
				// Verify owner label is still present
				assert.Equal(t, ext.Name, rev.Labels[labels.OwnerNameKey])
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			cb := fake.NewClientBuilder().WithScheme(testScheme).WithObjects(tc.existingObjs...)
			if tc.clientIterceptor != nil {
				cb.WithInterceptorFuncs(*tc.clientIterceptor)
			}
			fakeClient := cb.Build()

			boxcutter := &applier.Boxcutter{
				Client:            fakeClient,
				Scheme:            testScheme,
				RevisionGenerator: tc.mockBuilder(t),
				FieldOwner:        "test-owner",
				SystemNamespace:   "olmv1-system",
			}

			// We need a dummy fs.FS
			testFS := fstest.MapFS{}

			// Execute
			revisionAnnotations := map[string]string{}
			if tc.name == "annotation-only update (same phases, different annotations)" {
				// For annotation-only update test, pass NEW annotations
				revisionAnnotations = map[string]string{
					labels.BundleVersionKey: "1.0.1",
					labels.PackageNameKey:   "test-package",
				}
			}
			completed, status, err := boxcutter.Apply(t.Context(), testFS, ext, nil, revisionAnnotations)

			// Assert
			if tc.expectedErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedErr)
				assert.False(t, completed)
				assert.Empty(t, status)
			} else {
				require.NoError(t, err)
				assert.True(t, completed)
				assert.Empty(t, status)
			}

			if tc.validate != nil {
				// For the client create error, we need a client that *will* error.
				// Since we can't do that easily, we will skip validation for that specific path
				// as the state won't be what we expect.
				if tc.name != "error from client create" {
					tc.validate(t, fakeClient)
				}
			}
		})
	}
}

func TestBoxcutterStorageMigrator(t *testing.T) {
	// defaultHelmRevisionResult returns the default result for GenerateRevisionFromHelmRelease in storage migrator tests.
	defaultHelmRevisionResult := func(ext *ocv1.ClusterExtension) *ocv1ac.ClusterObjectSetApplyConfiguration {
		return ocv1ac.ClusterObjectSet("test-revision").
			WithLabels(map[string]string{
				labels.OwnerNameKey: ext.Name,
			}).
			WithSpec(ocv1ac.ClusterObjectSetSpec())
	}

	// newStorageMigratorGenerator creates a gomock ClusterObjectSetGenerator for storage migrator tests.
	// GenerateRevision is not expected to be called. GenerateRevisionFromHelmRelease returns the default result.
	newStorageMigratorGenerator := func(t *testing.T) *mockapplier.MockClusterObjectSetGenerator {
		ctrl := gomock.NewController(t)
		m := mockapplier.NewMockClusterObjectSetGenerator(ctrl)
		m.EXPECT().GenerateRevisionFromHelmRelease(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, helmRelease *release.Release, e *ocv1.ClusterExtension, objectLabels map[string]string) (*ocv1ac.ClusterObjectSetApplyConfiguration, error) {
				return defaultHelmRevisionResult(e), nil
			}).AnyTimes()
		return m
	}

	t.Run("creates revision", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "test123"},
		}
		ctrl := gomock.NewController(t)
		brb := newStorageMigratorGenerator(t)
		mag := newMockActionGetter(ctrl, mockActionGetterConfig{
			currentRel: &release.Release{
				Name: "test123",
				Info: &release.Info{Status: release.StatusDeployed},
			},
		})
		mockClient := mockctrlclient.NewMockClient(ctrl)
		mockStatusWriter := mockctrlclient.NewMockSubResourceWriter(ctrl)
		mockClient.EXPECT().Status().Return(mockStatusWriter).AnyTimes()

		sm := &applier.BoxcutterStorageMigrator{
			RevisionGenerator:  brb,
			ActionClientGetter: mag,
			Client:             mockClient,
			Scheme:             testScheme,
			FieldOwner:         "test-owner",
		}

		mockClient.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil)
		mockClient.EXPECT().Apply(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
				// Verify the migration marker label is set before apply
				rev := obj.(*ocv1ac.ClusterObjectSetApplyConfiguration)
				require.Equal(t, "true", rev.Labels[labels.MigratedFromHelmKey], "Migration marker label should be set")
				return nil
			})
		mockClient.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				// Simulate Get() returning the created revision with server-managed fields
				rev := obj.(*ocv1.ClusterObjectSet)
				rev.Name = "test-revision"
				rev.Generation = 1
				rev.ResourceVersion = "1"
				rev.Labels = map[string]string{
					labels.MigratedFromHelmKey: "true",
				}
				return nil
			})

		var updatedObj client.Object
		mockStatusWriter.EXPECT().Update(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				updatedObj = obj
				return nil
			})

		err := sm.Migrate(t.Context(), ext, map[string]string{"my-label": "my-value"})
		require.NoError(t, err)

		// Verify the migrated revision has Succeeded=True status with Succeeded reason and a migration message
		require.NotNil(t, updatedObj, "Updated object should not be nil")

		rev, ok := updatedObj.(*ocv1.ClusterObjectSet)
		require.True(t, ok, "Updated object should be a ClusterObjectSet")

		succeededCond := apimeta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterObjectSetTypeSucceeded)
		require.NotNil(t, succeededCond, "Succeeded condition should be set")
		assert.Equal(t, metav1.ConditionTrue, succeededCond.Status, "Succeeded condition should be True")
		assert.Equal(t, ocv1.ReasonSucceeded, succeededCond.Reason, "Reason should be Succeeded")
		assert.Equal(t, "Revision succeeded - migrated from Helm release", succeededCond.Message, "Message should indicate Helm migration")
		assert.Equal(t, int64(1), succeededCond.ObservedGeneration, "ObservedGeneration should match revision generation")
	})

	t.Run("does not create revision when revisions exist", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "test123"},
		}
		// GenerateRevisionFromHelmRelease should not be called when revisions already exist
		ctrl := gomock.NewController(t)
		brb := mockapplier.NewMockClusterObjectSetGenerator(ctrl)
		mag := newMockActionGetter(ctrl, mockActionGetterConfig{})

		mockClient := mockctrlclient.NewMockClient(ctrl)

		sm := &applier.BoxcutterStorageMigrator{
			RevisionGenerator:  brb,
			ActionClientGetter: mag,
			Client:             mockClient,
			Scheme:             testScheme,
			FieldOwner:         "test-owner",
		}

		existingRev := ocv1.ClusterObjectSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-revision",
				Generation: 2,
				Labels: map[string]string{
					labels.MigratedFromHelmKey: "true",
				},
			},
			Spec: ocv1.ClusterObjectSetSpec{
				Revision: 1, // Migration creates revision 1
			},
			Status: ocv1.ClusterObjectSetStatus{
				Conditions: []metav1.Condition{
					{
						Type:   ocv1.ClusterObjectSetTypeSucceeded,
						Status: metav1.ConditionTrue,
						Reason: ocv1.ReasonSucceeded,
					},
				},
			},
		}

		mockClient.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
				cosList := list.(*ocv1.ClusterObjectSetList)
				cosList.Items = []ocv1.ClusterObjectSet{existingRev}
				return nil
			})

		err := sm.Migrate(t.Context(), ext, map[string]string{"my-label": "my-value"})
		require.NoError(t, err)
	})

	t.Run("sets status when revision exists but status is missing", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "test123"},
		}
		ctrl := gomock.NewController(t)
		brb := mockapplier.NewMockClusterObjectSetGenerator(ctrl)
		mag := newMockActionGetter(ctrl, mockActionGetterConfig{})

		mockClient := mockctrlclient.NewMockClient(ctrl)
		mockStatusWriter := mockctrlclient.NewMockSubResourceWriter(ctrl)
		mockClient.EXPECT().Status().Return(mockStatusWriter).AnyTimes()

		sm := &applier.BoxcutterStorageMigrator{
			RevisionGenerator:  brb,
			ActionClientGetter: mag,
			Client:             mockClient,
			Scheme:             testScheme,
			FieldOwner:         "test-owner",
		}

		existingRev := ocv1.ClusterObjectSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-revision",
				Generation: 2,
				Labels: map[string]string{
					labels.MigratedFromHelmKey: "true",
				},
			},
			Spec: ocv1.ClusterObjectSetSpec{
				Revision: 1, // Migration creates revision 1
			},
			// Status is empty - simulating the case where creation succeeded but status update failed
		}

		mockClient.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
				cosList := list.(*ocv1.ClusterObjectSetList)
				cosList.Items = []ocv1.ClusterObjectSet{existingRev}
				return nil
			})

		mockClient.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				rev := obj.(*ocv1.ClusterObjectSet)
				*rev = existingRev
				return nil
			})

		var updatedObj client.Object
		mockStatusWriter.EXPECT().Update(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				updatedObj = obj
				return nil
			})

		err := sm.Migrate(t.Context(), ext, map[string]string{"my-label": "my-value"})
		require.NoError(t, err)

		// Verify the status was set
		require.NotNil(t, updatedObj, "Updated object should not be nil")

		rev, ok := updatedObj.(*ocv1.ClusterObjectSet)
		require.True(t, ok, "Updated object should be a ClusterObjectSet")

		succeededCond := apimeta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterObjectSetTypeSucceeded)
		require.NotNil(t, succeededCond, "Succeeded condition should be set")
		assert.Equal(t, metav1.ConditionTrue, succeededCond.Status, "Succeeded condition should be True")
		assert.Equal(t, ocv1.ReasonSucceeded, succeededCond.Reason, "Reason should be Succeeded")
	})

	t.Run("updates status from False to True for migrated revision", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "test123"},
		}
		ctrl := gomock.NewController(t)
		brb := mockapplier.NewMockClusterObjectSetGenerator(ctrl)
		mag := newMockActionGetter(ctrl, mockActionGetterConfig{})

		mockClient := mockctrlclient.NewMockClient(ctrl)
		mockStatusWriter := mockctrlclient.NewMockSubResourceWriter(ctrl)
		mockClient.EXPECT().Status().Return(mockStatusWriter).AnyTimes()

		sm := &applier.BoxcutterStorageMigrator{
			RevisionGenerator:  brb,
			ActionClientGetter: mag,
			Client:             mockClient,
			Scheme:             testScheme,
			FieldOwner:         "test-owner",
		}

		// Migrated revision with Succeeded=False (e.g., from a previous failed status update attempt)
		// This simulates a revision whose Succeeded condition should be corrected from False to True during migration.
		existingRev := ocv1.ClusterObjectSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-revision",
				Generation: 2,
				Labels: map[string]string{
					labels.MigratedFromHelmKey: "true",
				},
			},
			Spec: ocv1.ClusterObjectSetSpec{
				Revision: 1,
			},
			Status: ocv1.ClusterObjectSetStatus{
				Conditions: []metav1.Condition{
					{
						Type:   ocv1.ClusterObjectSetTypeSucceeded,
						Status: metav1.ConditionFalse, // Important: False, not missing
						Reason: "InProgress",
					},
				},
			},
		}

		mockClient.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
				cosList := list.(*ocv1.ClusterObjectSetList)
				cosList.Items = []ocv1.ClusterObjectSet{existingRev}
				return nil
			})

		mockClient.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				rev := obj.(*ocv1.ClusterObjectSet)
				*rev = existingRev
				return nil
			})

		var updatedObj client.Object
		mockStatusWriter.EXPECT().Update(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				updatedObj = obj
				return nil
			})

		err := sm.Migrate(t.Context(), ext, map[string]string{"my-label": "my-value"})
		require.NoError(t, err)

		// Verify the status was updated from False to True
		require.NotNil(t, updatedObj, "Updated object should not be nil")

		rev, ok := updatedObj.(*ocv1.ClusterObjectSet)
		require.True(t, ok, "Updated object should be a ClusterObjectSet")

		succeededCond := apimeta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterObjectSetTypeSucceeded)
		require.NotNil(t, succeededCond, "Succeeded condition should be set")
		assert.Equal(t, metav1.ConditionTrue, succeededCond.Status, "Succeeded condition should be updated to True")
		assert.Equal(t, ocv1.ReasonSucceeded, succeededCond.Reason, "Reason should be Succeeded")
	})

	t.Run("does not set status on non-migrated revision 1", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "test123"},
		}
		ctrl := gomock.NewController(t)
		brb := mockapplier.NewMockClusterObjectSetGenerator(ctrl)
		mag := newMockActionGetter(ctrl, mockActionGetterConfig{})

		mockClient := mockctrlclient.NewMockClient(ctrl)

		sm := &applier.BoxcutterStorageMigrator{
			RevisionGenerator:  brb,
			ActionClientGetter: mag,
			Client:             mockClient,
			Scheme:             testScheme,
			FieldOwner:         "test-owner",
		}

		// Revision 1 created by normal Boxcutter operation (no migration label)
		// This simulates the first rollout - status should NOT be set as it may still be in progress
		existingRev := ocv1.ClusterObjectSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-revision",
				Generation: 2,
				// No migration label - this is a normal Boxcutter revision
			},
			Spec: ocv1.ClusterObjectSetSpec{
				Revision: 1,
			},
		}

		mockClient.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
				cosList := list.(*ocv1.ClusterObjectSetList)
				cosList.Items = []ocv1.ClusterObjectSet{existingRev}
				return nil
			})

		// The migration flow calls Get() to re-fetch the revision before checking its status.
		// Even for non-migrated revisions, Get() is called to determine if status needs to be set.
		mockClient.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				rev := obj.(*ocv1.ClusterObjectSet)
				*rev = existingRev
				return nil
			})

		// Status() and Update() should NOT be called for non-migrated revisions.
		// gomock will fail if any unexpected calls are made.

		err := sm.Migrate(t.Context(), ext, map[string]string{"my-label": "my-value"})
		require.NoError(t, err)
	})

	t.Run("migrates from most recent deployed release when latest is failed", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "test123"},
		}
		expectedRelease := &release.Release{
			Name:    "test123",
			Version: 2,
			Info:    &release.Info{Status: release.StatusDeployed},
		}

		ctrl := gomock.NewController(t)
		brb := mockapplier.NewMockClusterObjectSetGenerator(ctrl)
		brb.EXPECT().GenerateRevisionFromHelmRelease(gomock.Any(), expectedRelease, gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, helmRelease *release.Release, e *ocv1.ClusterExtension, objectLabels map[string]string) (*ocv1ac.ClusterObjectSetApplyConfiguration, error) {
				return defaultHelmRevisionResult(e), nil
			}).Times(1)

		mag := newMockActionGetter(ctrl, mockActionGetterConfig{
			currentRel: &release.Release{
				Name:    "test123",
				Version: 3,
				Info:    &release.Info{Status: release.StatusFailed},
			},
			history: []*release.Release{
				{
					Name:    "test123",
					Version: 3,
					Info:    &release.Info{Status: release.StatusFailed},
				},
				expectedRelease,
				{
					Name:    "test123",
					Version: 1,
					Info:    &release.Info{Status: release.StatusSuperseded},
				},
			},
		})

		mockClient := mockctrlclient.NewMockClient(ctrl)
		mockStatusWriter := mockctrlclient.NewMockSubResourceWriter(ctrl)
		mockClient.EXPECT().Status().Return(mockStatusWriter).AnyTimes()

		sm := &applier.BoxcutterStorageMigrator{
			RevisionGenerator:  brb,
			ActionClientGetter: mag,
			Client:             mockClient,
			Scheme:             testScheme,
			FieldOwner:         "test-owner",
		}

		mockClient.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil)

		mockClient.EXPECT().Apply(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
				// Verify the migration marker label is set before apply
				rev := obj.(*ocv1ac.ClusterObjectSetApplyConfiguration)
				require.Equal(t, "true", rev.Labels[labels.MigratedFromHelmKey], "Migration marker label should be set")
				return nil
			})

		mockClient.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				rev := obj.(*ocv1.ClusterObjectSet)
				rev.Name = "test-revision"
				rev.Generation = 1
				rev.ResourceVersion = "1"
				rev.Labels = map[string]string{
					labels.MigratedFromHelmKey: "true",
				}
				return nil
			})

		var updatedObj client.Object
		mockStatusWriter.EXPECT().Update(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				updatedObj = obj
				return nil
			})

		err := sm.Migrate(t.Context(), ext, map[string]string{"my-label": "my-value"})
		require.NoError(t, err)

		// Verify the migrated revision has Succeeded=True status
		require.NotNil(t, updatedObj, "Updated object should not be nil")

		rev, ok := updatedObj.(*ocv1.ClusterObjectSet)
		require.True(t, ok, "Updated object should be a ClusterObjectSet")

		succeededCond := apimeta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterObjectSetTypeSucceeded)
		require.NotNil(t, succeededCond, "Succeeded condition should be set")
		assert.Equal(t, metav1.ConditionTrue, succeededCond.Status, "Succeeded condition should be True")
	})

	t.Run("does not create revision when helm release is not deployed and no deployed history", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "test123"},
		}
		ctrl := gomock.NewController(t)
		// GenerateRevisionFromHelmRelease should NOT be called when no deployed release exists
		brb := mockapplier.NewMockClusterObjectSetGenerator(ctrl)
		mag := newMockActionGetter(ctrl, mockActionGetterConfig{
			currentRel: &release.Release{
				Name: "test123",
				Info: &release.Info{Status: release.StatusFailed},
			},
			history: []*release.Release{
				{
					Name:    "test123",
					Version: 2,
					Info:    &release.Info{Status: release.StatusFailed},
				},
				{
					Name:    "test123",
					Version: 1,
					Info:    &release.Info{Status: release.StatusFailed},
				},
			},
		})

		mockClient := mockctrlclient.NewMockClient(ctrl)

		sm := &applier.BoxcutterStorageMigrator{
			RevisionGenerator:  brb,
			ActionClientGetter: mag,
			Client:             mockClient,
			Scheme:             testScheme,
			FieldOwner:         "test-owner",
		}

		mockClient.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil)

		err := sm.Migrate(t.Context(), ext, map[string]string{"my-label": "my-value"})
		require.NoError(t, err)
		// gomock will verify GenerateRevisionFromHelmRelease was NOT called (no expectation set)
	})

	t.Run("does not create revision when no helm release", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "test123"},
		}
		ctrl := gomock.NewController(t)
		brb := mockapplier.NewMockClusterObjectSetGenerator(ctrl)
		mag := newMockActionGetter(ctrl, mockActionGetterConfig{
			getClientErr: driver.ErrReleaseNotFound,
		})

		mockClient := mockctrlclient.NewMockClient(ctrl)

		sm := &applier.BoxcutterStorageMigrator{
			RevisionGenerator:  brb,
			ActionClientGetter: mag,
			Client:             mockClient,
			Scheme:             testScheme,
			FieldOwner:         "test-owner",
		}

		mockClient.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil)

		err := sm.Migrate(t.Context(), ext, map[string]string{"my-label": "my-value"})
		require.NoError(t, err)
	})
}
