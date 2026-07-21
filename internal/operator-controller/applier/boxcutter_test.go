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

	rev, err := g.GenerateRevisionFromHelmRelease(t.Context(), helmRelease, ext, objectLabels, &applier.NamespaceConfig{Managed: true, Name: "test-namespace"})
	require.NoError(t, err)

	assert.Equal(t, "test-123-1", *rev.Name)
	assert.Equal(t, map[string]string{
		labels.OwnerKindKey: ocv1.ClusterExtensionKind,
		labels.OwnerNameKey: "test-123",
	}, rev.Labels)
	assert.Equal(t, map[string]string{
		"olm.operatorframework.io/bundle-name":      "my-bundle",
		"olm.operatorframework.io/bundle-reference": "bundle-ref",
		"olm.operatorframework.io/bundle-version":   "1.2.0",
		"olm.operatorframework.io/package-name":     "my-package",
	}, rev.Annotations)
	assert.Equal(t, ptr.To(ocv1.ClusterObjectSetLifecycleStateActive), rev.Spec.LifecycleState)
	assert.Equal(t, ptr.To(ocv1.CollisionProtectionNone), rev.Spec.CollisionProtection)
	assert.Equal(t, ptr.To(int64(1)), rev.Spec.Revision)

	// Verify phases - should have namespaces phase and configuration phase
	require.Len(t, rev.Spec.Phases, 2)

	// Verify namespace phase
	namespacesPhase := rev.Spec.Phases[0]
	assert.Equal(t, "namespaces", *namespacesPhase.Name)
	require.Len(t, namespacesPhase.Objects, 1)
	assert.Equal(t, "Namespace", namespacesPhase.Objects[0].Object.GetKind())
	assert.Equal(t, "test-namespace", namespacesPhase.Objects[0].Object.GetName())

	// Verify configuration phase
	configPhase := rev.Spec.Phases[1]
	assert.Equal(t, "configuration", *configPhase.Name)
	require.Len(t, configPhase.Objects, 2)
	assert.Equal(t, "ConfigMap", configPhase.Objects[0].Object.GetKind())
	assert.Equal(t, "Secret", configPhase.Objects[1].Object.GetKind())
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
	}, &applier.NamespaceConfig{Managed: true, Name: "test-namespace"})
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
			WithName(string(applier.PhaseNamespaces)).
			WithObjects(
				ocv1ac.ClusterObjectSetObject().
					WithObject(unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Namespace",
							"metadata": map[string]interface{}{
								"name":   "test-namespace",
								"labels": map[string]interface{}{},
							},
							"spec":   map[string]interface{}{},
							"status": map[string]interface{}{},
						},
					}),
			),
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

		rev, err := b.GenerateRevision(t.Context(), bundleFS, ext, map[string]string{}, map[string]string{}, nil)
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

		rev, err := b.GenerateRevision(t.Context(), bundleFS, ext, map[string]string{}, map[string]string{}, nil)
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

		rev, err := b.GenerateRevision(t.Context(), bundleFS, ext, map[string]string{}, map[string]string{}, nil)
		require.NoError(t, err)

		t.Log("by checking csv annotations are not added to the revision annotations")
		_, ok := rev.Annotations["olm.csv.annotation"]
		require.False(t, ok, "csv annotation should not be present in the revision annotations")
	})

	t.Run("errors getting bundle properties are surfaced", func(t *testing.T) {
		_, err := b.GenerateRevision(t.Context(), fstest.MapFS{}, ext, map[string]string{}, map[string]string{}, nil)
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

	_, err := b.GenerateRevision(t.Context(), dummyBundle, ext, map[string]string{}, map[string]string{}, nil)
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
	}, revAnnotations, &applier.NamespaceConfig{Managed: true, Name: "test-namespace"})
	require.NoError(t, err)
	t.Log("by checking the rendered objects contain the given object labels")
	for _, phase := range rev.Spec.Phases {
		for _, revObj := range phase.Objects {
			// Namespace objects only have objectLabels, not bundle object labels
			if revObj.Object.GetKind() == "Namespace" {
				require.Equal(t, map[string]string{
					"some": "value",
				}, revObj.Object.GetLabels())
			} else {
				require.Equal(t, map[string]string{
					"app":  "test-obj",
					"some": "value",
				}, revObj.Object.GetLabels())
			}
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

			rev, err := b.GenerateRevision(t.Context(), dummyBundle, ext, empty, empty, nil)
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
	}, map[string]string{}, map[string]string{}, nil)
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
				m.EXPECT().GenerateRevision(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string, nsConfig *applier.NamespaceConfig) (*ocv1ac.ClusterObjectSetApplyConfiguration, error) {
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
				m.EXPECT().GenerateRevisionFromHelmRelease(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
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
				m.EXPECT().GenerateRevision(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string, nsConfig *applier.NamespaceConfig) (*ocv1ac.ClusterObjectSetApplyConfiguration, error) {
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
				m.EXPECT().GenerateRevisionFromHelmRelease(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
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
				m.EXPECT().GenerateRevision(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string, nsConfig *applier.NamespaceConfig) (*ocv1ac.ClusterObjectSetApplyConfiguration, error) {
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
				m.EXPECT().GenerateRevisionFromHelmRelease(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
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
				m.EXPECT().GenerateRevision(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("render boom")).AnyTimes()
				m.EXPECT().GenerateRevisionFromHelmRelease(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
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
				m.EXPECT().GenerateRevision(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string, nsConfig *applier.NamespaceConfig) (*ocv1ac.ClusterObjectSetApplyConfiguration, error) {
						return ocv1ac.ClusterObjectSet("").
							WithAnnotations(revisionAnnotations).
							WithLabels(map[string]string{
								labels.OwnerNameKey: ext.Name,
							}).
							WithSpec(ocv1ac.ClusterObjectSetSpec()), nil
					}).AnyTimes()
				m.EXPECT().GenerateRevisionFromHelmRelease(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
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
				m.EXPECT().GenerateRevision(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string, nsConfig *applier.NamespaceConfig) (*ocv1ac.ClusterObjectSetApplyConfiguration, error) {
						return ocv1ac.ClusterObjectSet("").
							WithAnnotations(revisionAnnotations).
							WithLabels(map[string]string{
								labels.OwnerNameKey: ext.Name,
							}).
							WithSpec(ocv1ac.ClusterObjectSetSpec()), nil
					}).AnyTimes()
				m.EXPECT().GenerateRevisionFromHelmRelease(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
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
				m.EXPECT().GenerateRevision(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string, nsConfig *applier.NamespaceConfig) (*ocv1ac.ClusterObjectSetApplyConfiguration, error) {
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
				m.EXPECT().GenerateRevisionFromHelmRelease(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
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
			completed, status, err := boxcutter.Apply(t.Context(), testFS, ext, nil, revisionAnnotations, nil)

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
		m.EXPECT().GenerateRevisionFromHelmRelease(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, helmRelease *release.Release, e *ocv1.ClusterExtension, objectLabels map[string]string, nsConfig *applier.NamespaceConfig) (*ocv1ac.ClusterObjectSetApplyConfiguration, error) {
				return defaultHelmRevisionResult(e), nil
			}).AnyTimes()
		return m
	}

	t.Run("creates revision", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "test123"}, Spec: ocv1.ClusterExtensionSpec{Namespace: "test-namespace"},
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
			ObjectMeta: metav1.ObjectMeta{Name: "test123"}, Spec: ocv1.ClusterExtensionSpec{Namespace: "test-namespace"},
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
			ObjectMeta: metav1.ObjectMeta{Name: "test123"}, Spec: ocv1.ClusterExtensionSpec{Namespace: "test-namespace"},
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
			ObjectMeta: metav1.ObjectMeta{Name: "test123"}, Spec: ocv1.ClusterExtensionSpec{Namespace: "test-namespace"},
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
			ObjectMeta: metav1.ObjectMeta{Name: "test123"}, Spec: ocv1.ClusterExtensionSpec{Namespace: "test-namespace"},
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
			ObjectMeta: metav1.ObjectMeta{Name: "test123"}, Spec: ocv1.ClusterExtensionSpec{Namespace: "test-namespace"},
		}
		expectedRelease := &release.Release{
			Name:    "test123",
			Version: 2,
			Info:    &release.Info{Status: release.StatusDeployed},
		}

		ctrl := gomock.NewController(t)
		brb := mockapplier.NewMockClusterObjectSetGenerator(ctrl)
		brb.EXPECT().GenerateRevisionFromHelmRelease(gomock.Any(), expectedRelease, gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, helmRelease *release.Release, e *ocv1.ClusterExtension, objectLabels map[string]string, nsConfig *applier.NamespaceConfig) (*ocv1ac.ClusterObjectSetApplyConfiguration, error) {
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
			ObjectMeta: metav1.ObjectMeta{Name: "test123"}, Spec: ocv1.ClusterExtensionSpec{Namespace: "test-namespace"},
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
			ObjectMeta: metav1.ObjectMeta{Name: "test123"}, Spec: ocv1.ClusterExtensionSpec{Namespace: "test-namespace"},
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

func Test_SimpleRevisionGenerator_GenerateRevision_NamespaceInjection(t *testing.T) {
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
			ServiceAccount: ocv1.ServiceAccountReference{ //nolint:staticcheck // deprecated field used in test
				Name: "test-sa",
			},
		},
	}

	objectLabels := map[string]string{
		"test-label": "test-value",
	}

	t.Run("with namespace template", func(t *testing.T) {
		nsTemplate := `{
			"metadata": {
				"name": "template-name",
				"labels": {
					"template-label": "template-value",
					"security": "restricted"
				},
				"annotations": {
					"template-annotation": "template-annotation-value"
				}
			}
		}`

		bundleFS := bundlefs.Builder().
			WithPackageName("test-package").
			WithCSV(bundlecsv.Builder().
				WithName("test-csv").
				WithAnnotations(map[string]string{
					applier.AnnotationSuggestedNamespaceTemplate: nsTemplate,
				}).
				Build()).
			Build()

		// Parse the namespace template to pass via NamespaceConfig
		bundleAnnotations, err := applier.GetBundleAnnotations(bundleFS)
		require.NoError(t, err)
		parsedTemplate, err := applier.ParseNamespaceTemplate(bundleAnnotations)
		require.NoError(t, err)

		rev, err := b.GenerateRevision(t.Context(), bundleFS, ext, objectLabels, map[string]string{}, &applier.NamespaceConfig{Managed: true, Name: "test-namespace", Template: parsedTemplate})
		require.NoError(t, err)
		require.NotNil(t, rev)

		// Find the namespaces phase
		var namespacesPhase *ocv1ac.ClusterObjectSetPhaseApplyConfiguration
		for i := range rev.Spec.Phases {
			if *rev.Spec.Phases[i].Name == string(applier.PhaseNamespaces) {
				namespacesPhase = &rev.Spec.Phases[i]
				break
			}
		}

		require.NotNil(t, namespacesPhase, "namespaces phase should exist")
		require.Len(t, namespacesPhase.Objects, 1, "namespaces phase should contain exactly one object")

		nsObj := namespacesPhase.Objects[0].Object
		require.NotNil(t, nsObj)
		require.Equal(t, "v1", nsObj.GetAPIVersion())
		require.Equal(t, "Namespace", nsObj.GetKind())
		require.Equal(t, "test-namespace", nsObj.GetName(), "namespace name should match ext.Spec.Namespace, not template name")

		// Verify template labels are present
		labels := nsObj.GetLabels()
		require.Equal(t, "template-value", labels["template-label"])
		require.Equal(t, "restricted", labels["security"])
		// Verify objectLabels are also present
		require.Equal(t, "test-value", labels["test-label"])

		// Verify template annotations are present
		annotations := nsObj.GetAnnotations()
		require.Equal(t, "template-annotation-value", annotations["template-annotation"])
	})

	t.Run("without namespace template", func(t *testing.T) {
		rev, err := b.GenerateRevision(t.Context(), dummyBundle, ext, objectLabels, map[string]string{}, &applier.NamespaceConfig{Managed: true, Name: "test-namespace"})
		require.NoError(t, err)
		require.NotNil(t, rev)

		// Find the namespaces phase
		var namespacesPhase *ocv1ac.ClusterObjectSetPhaseApplyConfiguration
		for i := range rev.Spec.Phases {
			if *rev.Spec.Phases[i].Name == string(applier.PhaseNamespaces) {
				namespacesPhase = &rev.Spec.Phases[i]
				break
			}
		}

		require.NotNil(t, namespacesPhase, "namespaces phase should exist even without template")
		require.Len(t, namespacesPhase.Objects, 1, "namespaces phase should contain exactly one object")

		nsObj := namespacesPhase.Objects[0].Object
		require.NotNil(t, nsObj)
		require.Equal(t, "v1", nsObj.GetAPIVersion())
		require.Equal(t, "Namespace", nsObj.GetKind())
		require.Equal(t, "test-namespace", nsObj.GetName())

		// Verify objectLabels are present
		labels := nsObj.GetLabels()
		require.Equal(t, "test-value", labels["test-label"])

		// Should not have template-specific labels
		require.NotContains(t, labels, "template-label")
		require.NotContains(t, labels, "security")
	})
}

func Test_SimpleRevisionGenerator_GenerateRevision_NamespacePhaseCollisionProtection(t *testing.T) {
	ctrl := gomock.NewController(t)
	r := mockapplier.NewMockManifestProvider(ctrl)
	r.EXPECT().Get(gomock.Any(), gomock.Any()).Return([]client.Object{
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-service",
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
			ServiceAccount: ocv1.ServiceAccountReference{ //nolint:staticcheck // deprecated field used in test
				Name: "test-sa",
			},
		},
	}

	rev, err := b.GenerateRevision(t.Context(), dummyBundle, ext, map[string]string{}, map[string]string{}, &applier.NamespaceConfig{Managed: true, Name: "test-namespace"})
	require.NoError(t, err)
	require.NotNil(t, rev)

	t.Log("by checking the spec-level collision protection is set to Prevent")
	require.Equal(t, ptr.To(ocv1.CollisionProtectionPrevent), rev.Spec.CollisionProtection)

	// Find the namespaces phase
	var namespacesPhase *ocv1ac.ClusterObjectSetPhaseApplyConfiguration
	for i := range rev.Spec.Phases {
		if *rev.Spec.Phases[i].Name == string(applier.PhaseNamespaces) {
			namespacesPhase = &rev.Spec.Phases[i]
			break
		}
	}

	require.NotNil(t, namespacesPhase, "namespaces phase should exist")

	t.Log("by checking the namespaces phase inherits Prevent collision protection from spec (no explicit override)")
	require.Nil(t, namespacesPhase.CollisionProtection, "namespaces phase should inherit collision protection from spec")

	// Verify all phases inherit from spec (no explicit collision protection)
	for i := range rev.Spec.Phases {
		t.Logf("by checking phase %s does not have explicit collision protection", *rev.Spec.Phases[i].Name)
		require.Nil(t, rev.Spec.Phases[i].CollisionProtection, "all phases should inherit collision protection from spec")
	}
}

func Test_SimpleRevisionGenerator_GenerateRevisionFromHelmRelease_IncludesNamespace(t *testing.T) {
	g := &applier.SimpleRevisionGenerator{}

	helmRelease := &release.Release{
		Name:     "test-123",
		Manifest: `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"test-cm"}}`,
		Labels: map[string]string{
			labels.BundleNameKey:    "my-bundle",
			labels.PackageNameKey:   "my-package",
			labels.BundleVersionKey: "1.0.0",
		},
	}

	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: "test-123"},
		Spec: ocv1.ClusterExtensionSpec{
			Namespace:      "test-namespace",
			ServiceAccount: ocv1.ServiceAccountReference{Name: "test-sa"}, //nolint:staticcheck // deprecated field used in test
		},
	}

	rev, err := g.GenerateRevisionFromHelmRelease(t.Context(), helmRelease, ext, nil, &applier.NamespaceConfig{Managed: true, Name: "test-namespace"})
	require.NoError(t, err)

	var foundNS bool
	for _, phase := range rev.Spec.Phases {
		if *phase.Name == "namespaces" {
			for _, obj := range phase.Objects {
				if obj.Object.GetKind() == "Namespace" {
					foundNS = true
					assert.Equal(t, "test-namespace", obj.Object.GetName())
				}
			}
		}
	}
	assert.True(t, foundNS, "expected Namespace in Helm migration revision")
}

func Test_GenerateRevision_NamespacePhaseIsFirst(t *testing.T) {
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
				Name:      "test-deployment",
				Namespace: "test-ns",
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
			ServiceAccount: ocv1.ServiceAccountReference{ //nolint:staticcheck // deprecated field used in test
				Name: "test-sa",
			},
		},
	}

	rev, err := b.GenerateRevision(t.Context(), dummyBundle, ext, map[string]string{}, map[string]string{}, &applier.NamespaceConfig{Managed: true, Name: "test-namespace"})
	require.NoError(t, err)

	t.Log("by checking that phases are present")
	require.NotEmpty(t, rev.Spec.Phases, "revision should have at least one phase")

	t.Log("by checking that the first phase is the namespaces phase")
	firstPhase := rev.Spec.Phases[0]
	require.Equal(t, "namespaces", *firstPhase.Name, "first phase should be namespaces for proper deletion ordering")

	t.Log("by checking that the namespaces phase contains exactly one namespace object")
	require.Len(t, firstPhase.Objects, 1, "namespaces phase should contain exactly one object")

	t.Log("by checking that the namespace object has the correct name")
	nsObj := firstPhase.Objects[0].Object
	require.Equal(t, "Namespace", nsObj.GetKind())
	require.Equal(t, "test-namespace", nsObj.GetName(), "namespace name should match ext.Spec.Namespace")
}

func Test_GenerateRevision_COSHasOwnerLabels(t *testing.T) {
	ctrl := gomock.NewController(t)
	r := mockapplier.NewMockManifestProvider(ctrl)
	r.EXPECT().Get(gomock.Any(), gomock.Any()).Return([]client.Object{
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-configmap",
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
			ServiceAccount: ocv1.ServiceAccountReference{ //nolint:staticcheck // deprecated field used in test
				Name: "test-sa",
			},
		},
	}

	rev, err := b.GenerateRevision(t.Context(), dummyBundle, ext, map[string]string{}, map[string]string{}, nil)
	require.NoError(t, err)

	t.Log("by checking that the COS has owner-kind label")
	require.NotNil(t, rev.Labels, "COS should have labels")
	require.Equal(t, ocv1.ClusterExtensionKind, rev.Labels[labels.OwnerKindKey],
		"COS should have owner-kind label set to ClusterExtension")

	t.Log("by checking that the COS has owner-name label")
	require.Equal(t, "test-extension", rev.Labels[labels.OwnerNameKey],
		"COS should have owner-name label matching the ClusterExtension name")
}

func Test_ResolveNamespaceName_WithBundleFS(t *testing.T) {
	t.Run("resolves name from suggested-namespace-template annotation", func(t *testing.T) {
		nsTemplate := `{"metadata":{"name":"operator-ns","labels":{"pod-security.kubernetes.io/enforce":"privileged"}}}`
		bundleFS := bundlefs.Builder().
			WithPackageName("test-package").
			WithCSV(bundlecsv.Builder().
				WithName("test-csv").
				WithAnnotations(map[string]string{
					applier.AnnotationSuggestedNamespaceTemplate: nsTemplate,
				}).
				Build()).
			Build()

		annotations, err := applier.GetBundleAnnotations(bundleFS)
		require.NoError(t, err)

		name, template, err := applier.ResolveNamespaceName(annotations, "test-package")
		require.NoError(t, err)
		assert.Equal(t, "operator-ns", name)
		require.NotNil(t, template)
		assert.Equal(t, "privileged", template.Labels["pod-security.kubernetes.io/enforce"])
	})

	t.Run("resolves name from suggested-namespace annotation", func(t *testing.T) {
		bundleFS := bundlefs.Builder().
			WithPackageName("test-package").
			WithCSV(bundlecsv.Builder().
				WithName("test-csv").
				WithAnnotations(map[string]string{
					applier.AnnotationSuggestedNamespace: "my-operator-ns",
				}).
				Build()).
			Build()

		annotations, err := applier.GetBundleAnnotations(bundleFS)
		require.NoError(t, err)

		name, template, err := applier.ResolveNamespaceName(annotations, "test-package")
		require.NoError(t, err)
		assert.Equal(t, "my-operator-ns", name)
		assert.Nil(t, template)
	})

	t.Run("falls back to packageName-system", func(t *testing.T) {
		bundleFS := bundlefs.Builder().
			WithPackageName("test-package").
			WithCSV(bundlecsv.Builder().WithName("test-csv").Build()).
			Build()

		annotations, err := applier.GetBundleAnnotations(bundleFS)
		require.NoError(t, err)

		name, template, err := applier.ResolveNamespaceName(annotations, "test-package")
		require.NoError(t, err)
		assert.Equal(t, "test-package-system", name)
		assert.Nil(t, template)
	})

	t.Run("template takes priority over suggested-namespace", func(t *testing.T) {
		bundleFS := bundlefs.Builder().
			WithPackageName("test-package").
			WithCSV(bundlecsv.Builder().
				WithName("test-csv").
				WithAnnotations(map[string]string{
					applier.AnnotationSuggestedNamespaceTemplate: `{"metadata":{"name":"from-template"}}`,
					applier.AnnotationSuggestedNamespace:         "from-annotation",
				}).
				Build()).
			Build()

		annotations, err := applier.GetBundleAnnotations(bundleFS)
		require.NoError(t, err)

		name, _, err := applier.ResolveNamespaceName(annotations, "test-package")
		require.NoError(t, err)
		assert.Equal(t, "from-template", name)
	})
}
