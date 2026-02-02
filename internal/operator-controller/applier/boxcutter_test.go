package applier_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	k8scheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/operator-controller/authorization"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing/bundlefs"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing/clusterserviceversion"
)

var (
	dummyBundle = bundlefs.Builder().
		WithPackageName("test-package").
		WithCSV(clusterserviceversion.Builder().WithName("test-csv").Build()).
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
			ServiceAccount: ocv1.ServiceAccountReference{
				Name: "test-sa",
			},
		},
	}

	objectLabels := map[string]string{
		"my-label": "my-value",
	}

	rev, err := g.GenerateRevisionFromHelmRelease(t.Context(), helmRelease, ext, objectLabels)
	require.NoError(t, err)

	assert.Equal(t, &ocv1.ClusterExtensionRevision{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-123-1",
			Annotations: map[string]string{
				"olm.operatorframework.io/bundle-name":               "my-bundle",
				"olm.operatorframework.io/bundle-reference":          "bundle-ref",
				"olm.operatorframework.io/bundle-version":            "1.2.0",
				"olm.operatorframework.io/package-name":              "my-package",
				"olm.operatorframework.io/service-account-name":      "test-sa",
				"olm.operatorframework.io/service-account-namespace": "test-namespace",
			},
			Labels: map[string]string{
				labels.OwnerNameKey: "test-123",
			},
		},
		Spec: ocv1.ClusterExtensionRevisionSpec{
			LifecycleState: ocv1.ClusterExtensionRevisionLifecycleStateActive,
			Revision:       1,
			Phases: []ocv1.ClusterExtensionRevisionPhase{
				{
					Name: "deploy",
					Objects: []ocv1.ClusterExtensionRevisionObject{
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "ConfigMap",
									"metadata": map[string]interface{}{
										"labels": map[string]interface{}{
											"my-label": "my-value",
										},
									},
								},
							},
							CollisionProtection: ocv1.CollisionProtectionNone,
						},
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "Secret",
									"metadata": map[string]interface{}{
										"labels": map[string]interface{}{
											"my-label": "my-value",
										},
									},
								},
							},
							CollisionProtection: ocv1.CollisionProtectionNone,
						},
					},
				},
			},
		},
	}, rev)
}

func Test_SimpleRevisionGenerator_GenerateRevision(t *testing.T) {
	r := &FakeManifestProvider{
		GetFn: func(_ fs.FS, _ *ocv1.ClusterExtension) ([]client.Object, error) {
			return []client.Object{
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
			}, nil
		},
	}

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
			ServiceAccount: ocv1.ServiceAccountReference{
				Name: "test-sa",
			},
		},
	}

	rev, err := b.GenerateRevision(t.Context(), dummyBundle, ext, map[string]string{}, map[string]string{})
	require.NoError(t, err)

	t.Log("by checking the olm.operatorframework.io/owner-name label is set to the name of the ClusterExtension")
	require.Equal(t, map[string]string{
		labels.OwnerNameKey: "test-extension",
	}, rev.Labels)
	t.Log("by checking the revision number is 0")
	require.Equal(t, int64(0), rev.Spec.Revision)
	t.Log("by checking the rendered objects are present in the correct phases")
	require.Equal(t, []ocv1.ClusterExtensionRevisionPhase{
		{
			Name: string(applier.PhaseDeploy),
			Objects: []ocv1.ClusterExtensionRevisionObject{
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Service",
							"metadata": map[string]interface{}{
								"name": "test-service",
							},
							"spec": map[string]interface{}{},
						},
					},
				},
				{
					Object: unstructured.Unstructured{
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
					},
				},
			},
		},
	}, rev.Spec.Phases)
}

func Test_SimpleRevisionGenerator_GenerateRevision_BundleAnnotations(t *testing.T) {
	r := &FakeManifestProvider{
		GetFn: func(_ fs.FS, _ *ocv1.ClusterExtension) ([]client.Object, error) {
			return []client.Object{}, nil
		},
	}

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
			ServiceAccount: ocv1.ServiceAccountReference{
				Name: "test-sa",
			},
		},
	}

	t.Run("bundle properties are copied to the olm.properties annotation", func(t *testing.T) {
		bundleFS := bundlefs.Builder().
			WithPackageName("test-package").
			WithBundleProperty("olm.bundle.property", "some-value").
			WithBundleProperty("olm.another.bundle.property", "some-other-value").
			WithCSV(clusterserviceversion.Builder().WithName("test-csv").Build()).
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
			WithCSV(clusterserviceversion.Builder().WithName("test-csv").Build()).
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
			WithCSV(clusterserviceversion.Builder().
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
	r := &FakeManifestProvider{
		GetFn: func(b fs.FS, e *ocv1.ClusterExtension) ([]client.Object, error) {
			t.Log("by checking renderer was called with the correct parameters")
			require.Equal(t, dummyBundle, b)
			require.Equal(t, ext, e)
			return nil, nil
		},
	}
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
	r := &FakeManifestProvider{
		GetFn: func(b fs.FS, e *ocv1.ClusterExtension) ([]client.Object, error) {
			return renderedObjs, nil
		},
	}

	b := applier.SimpleRevisionGenerator{
		Scheme:           k8scheme.Scheme,
		ManifestProvider: r,
	}

	revAnnotations := map[string]string{
		"other": "value",
	}

	rev, err := b.GenerateRevision(t.Context(), dummyBundle, &ocv1.ClusterExtension{
		Spec: ocv1.ClusterExtensionSpec{
			Namespace:      "test-namespace",
			ServiceAccount: ocv1.ServiceAccountReference{Name: "test-sa"},
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
	r := &FakeManifestProvider{
		GetFn: func(b fs.FS, e *ocv1.ClusterExtension) ([]client.Object, error) {
			return []client.Object{}, nil
		},
	}

	b := applier.SimpleRevisionGenerator{
		Scheme:           k8scheme.Scheme,
		ManifestProvider: r,
	}

	type args struct {
		progressDeadlineMinutes *int32
	}
	type want struct {
		progressDeadlineMinutes int32
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
				progressDeadlineMinutes: 10,
			},
		},
		"do not propagate when unset": {
			want: want{
				progressDeadlineMinutes: 0,
			},
		},
	} {
		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-extension",
			},
			Spec: ocv1.ClusterExtensionSpec{
				Namespace:      "test-namespace",
				ServiceAccount: ocv1.ServiceAccountReference{Name: "test-sa"},
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
	r := &FakeManifestProvider{
		GetFn: func(b fs.FS, e *ocv1.ClusterExtension) ([]client.Object, error) {
			return nil, fmt.Errorf("some-error")
		},
	}
	b := applier.SimpleRevisionGenerator{
		Scheme:           k8scheme.Scheme,
		ManifestProvider: r,
	}

	rev, err := b.GenerateRevision(t.Context(), fstest.MapFS{}, &ocv1.ClusterExtension{
		Spec: ocv1.ClusterExtensionSpec{
			Namespace:      "test-namespace",
			ServiceAccount: ocv1.ServiceAccountReference{Name: "test-sa"},
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

	// This is the revision that the mock builder will produce by default.
	// We calculate its hash to use in the tests.
	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ext",
			UID:  "test-uid",
		},
	}
	defaultDesiredRevision := &ocv1.ClusterExtensionRevision{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ext-1",
			UID:  "rev-uid-1",
			Labels: map[string]string{
				labels.OwnerNameKey: ext.Name,
			},
		},
		Spec: ocv1.ClusterExtensionRevisionSpec{
			Revision: 1,
			Phases: []ocv1.ClusterExtensionRevisionPhase{
				{
					Name: string(applier.PhaseDeploy),
					Objects: []ocv1.ClusterExtensionRevisionObject{
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
			Patch: func(ctx context.Context, client client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				cer, ok := obj.(*ocv1.ClusterExtensionRevision)
				if !ok {
					return fmt.Errorf("expected ClusterExtensionRevision, got %T", obj)
				}
				fmt.Println(cer.Spec.Revision)
				if cer.Spec.Revision != revNum {
					fmt.Println("AAA")
					return apierrors.NewInvalid(cer.GroupVersionKind().GroupKind(), cer.GetName(), field.ErrorList{field.Invalid(field.NewPath("spec.phases"), "immutable", "spec.phases is immutable")})
				}
				return client.Patch(ctx, obj, patch, opts...)
			},
		}
	}
	testCases := []struct {
		name             string
		mockBuilder      applier.ClusterExtensionRevisionGenerator
		existingObjs     []client.Object
		expectedErr      string
		validate         func(t *testing.T, c client.Client)
		clientIterceptor *interceptor.Funcs
	}{
		{
			name: "first revision",
			mockBuilder: &mockBundleRevisionBuilder{
				makeRevisionFunc: func(ctx context.Context, bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (*ocv1.ClusterExtensionRevision, error) {
					return &ocv1.ClusterExtensionRevision{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: revisionAnnotations,
							Labels: map[string]string{
								labels.OwnerNameKey: ext.Name,
							},
						},
						Spec: ocv1.ClusterExtensionRevisionSpec{
							Phases: []ocv1.ClusterExtensionRevisionPhase{
								{
									Name: string(applier.PhaseDeploy),
									Objects: []ocv1.ClusterExtensionRevisionObject{
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
					}, nil
				},
			},
			validate: func(t *testing.T, c client.Client) {
				revList := &ocv1.ClusterExtensionRevisionList{}
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
			mockBuilder: &mockBundleRevisionBuilder{
				makeRevisionFunc: func(ctx context.Context, bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (*ocv1.ClusterExtensionRevision, error) {
					return &ocv1.ClusterExtensionRevision{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: revisionAnnotations,
							Labels: map[string]string{
								labels.OwnerNameKey: ext.Name,
							},
						},
						Spec: ocv1.ClusterExtensionRevisionSpec{
							Phases: []ocv1.ClusterExtensionRevisionPhase{
								{
									Name: string(applier.PhaseDeploy),
									Objects: []ocv1.ClusterExtensionRevisionObject{
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
					}, nil
				},
			},
			existingObjs: []client.Object{
				defaultDesiredRevision,
			},
			validate: func(t *testing.T, c client.Client) {
				revList := &ocv1.ClusterExtensionRevisionList{}
				err := c.List(context.Background(), revList, client.MatchingLabels{labels.OwnerNameKey: ext.Name})
				require.NoError(t, err)
				// No new revision should be created
				require.Len(t, revList.Items, 1)
				assert.Equal(t, "test-ext-1", revList.Items[0].Name)
			},
		},
		{
			name: "new revision created when objects in new revision are different",
			mockBuilder: &mockBundleRevisionBuilder{
				makeRevisionFunc: func(ctx context.Context, bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (*ocv1.ClusterExtensionRevision, error) {
					return &ocv1.ClusterExtensionRevision{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: revisionAnnotations,
							Labels: map[string]string{
								labels.OwnerNameKey: ext.Name,
							},
						},
						Spec: ocv1.ClusterExtensionRevisionSpec{
							Phases: []ocv1.ClusterExtensionRevisionPhase{
								{
									Name: string(applier.PhaseDeploy),
									Objects: []ocv1.ClusterExtensionRevisionObject{
										{
											Object: unstructured.Unstructured{
												Object: map[string]interface{}{
													"apiVersion": "v1",
													"kind":       "Secret",
													"metadata": map[string]interface{}{
														"name": "new-secret",
													},
												},
											},
										},
									},
								},
							},
						},
					}, nil
				},
			},
			clientIterceptor: allowedRevisionValue(2),
			existingObjs: []client.Object{
				defaultDesiredRevision,
			},
			validate: func(t *testing.T, c client.Client) {
				revList := &ocv1.ClusterExtensionRevisionList{}
				err := c.List(context.Background(), revList, client.MatchingLabels{labels.OwnerNameKey: ext.Name})
				require.NoError(t, err)
				require.Len(t, revList.Items, 2)

				// Find the new revision (rev 2)
				var newRev ocv1.ClusterExtensionRevision
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
			mockBuilder: &mockBundleRevisionBuilder{
				makeRevisionFunc: func(ctx context.Context, bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (*ocv1.ClusterExtensionRevision, error) {
					return nil, errors.New("render boom")
				},
			},
			expectedErr: "render boom",
			validate: func(t *testing.T, c client.Client) {
				// Ensure no revisions were created
				revList := &ocv1.ClusterExtensionRevisionList{}
				err := c.List(context.Background(), revList, client.MatchingLabels{labels.OwnerNameKey: ext.Name})
				require.NoError(t, err)
				assert.Empty(t, revList.Items)
			},
		},
		{
			name: "keep at most 5 past revisions",
			mockBuilder: &mockBundleRevisionBuilder{
				makeRevisionFunc: func(ctx context.Context, bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (*ocv1.ClusterExtensionRevision, error) {
					return &ocv1.ClusterExtensionRevision{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: revisionAnnotations,
							Labels: map[string]string{
								labels.OwnerNameKey: ext.Name,
							},
						},
						Spec: ocv1.ClusterExtensionRevisionSpec{},
					}, nil
				},
			},
			existingObjs: []client.Object{
				&ocv1.ClusterExtensionRevision{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rev-1",
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterExtensionRevisionSpec{
						LifecycleState: ocv1.ClusterExtensionRevisionLifecycleStateArchived,
						Revision:       1,
					},
				},
				&ocv1.ClusterExtensionRevision{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rev-2",
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterExtensionRevisionSpec{
						LifecycleState: ocv1.ClusterExtensionRevisionLifecycleStateArchived,
						Revision:       2,
					},
				},
				&ocv1.ClusterExtensionRevision{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rev-3",
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterExtensionRevisionSpec{
						LifecycleState: ocv1.ClusterExtensionRevisionLifecycleStateArchived,
						Revision:       3,
					},
				},
				&ocv1.ClusterExtensionRevision{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rev-4",
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterExtensionRevisionSpec{
						LifecycleState: ocv1.ClusterExtensionRevisionLifecycleStateArchived,
						Revision:       4,
					},
				},
				&ocv1.ClusterExtensionRevision{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rev-5",
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterExtensionRevisionSpec{
						LifecycleState: ocv1.ClusterExtensionRevisionLifecycleStateArchived,
						Revision:       5,
					},
				},
				&ocv1.ClusterExtensionRevision{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rev-6",
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterExtensionRevisionSpec{
						LifecycleState: ocv1.ClusterExtensionRevisionLifecycleStateArchived,
						Revision:       6,
					},
				},
			},
			clientIterceptor: allowedRevisionValue(7),
			validate: func(t *testing.T, c client.Client) {
				rev1 := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{Name: "rev-1"}, rev1)
				require.Error(t, err)
				assert.True(t, apierrors.IsNotFound(err))

				// Verify garbage collection: should only keep the limit + 1 (current) revisions
				revList := &ocv1.ClusterExtensionRevisionList{}
				err = c.List(t.Context(), revList)
				require.NoError(t, err)
				// Should have ClusterExtensionRevisionRetentionLimit (5) + current (1) = 6 revisions max
				assert.LessOrEqual(t, len(revList.Items), applier.ClusterExtensionRevisionRetentionLimit+1)
			},
		},
		{
			name: "keep active revisions when they are out of limit",
			mockBuilder: &mockBundleRevisionBuilder{
				makeRevisionFunc: func(ctx context.Context, bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (*ocv1.ClusterExtensionRevision, error) {
					return &ocv1.ClusterExtensionRevision{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: revisionAnnotations,
							Labels: map[string]string{
								labels.OwnerNameKey: ext.Name,
							},
						},
						Spec: ocv1.ClusterExtensionRevisionSpec{},
					}, nil
				},
			},
			existingObjs: []client.Object{
				&ocv1.ClusterExtensionRevision{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rev-1",
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterExtensionRevisionSpec{
						LifecycleState: ocv1.ClusterExtensionRevisionLifecycleStateArchived,
						Revision:       1,
					},
				},
				&ocv1.ClusterExtensionRevision{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rev-2",
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterExtensionRevisionSpec{
						// index beyond the retention limit but active; should be preserved
						LifecycleState: ocv1.ClusterExtensionRevisionLifecycleStateActive,
						Revision:       2,
					},
				},
				&ocv1.ClusterExtensionRevision{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rev-3",
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterExtensionRevisionSpec{
						LifecycleState: ocv1.ClusterExtensionRevisionLifecycleStateActive,
						Revision:       3,
					},
				},
				&ocv1.ClusterExtensionRevision{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rev-4",
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterExtensionRevisionSpec{
						// archived but should be preserved since it is within the limit
						LifecycleState: ocv1.ClusterExtensionRevisionLifecycleStateArchived,
						Revision:       4,
					},
				},
				&ocv1.ClusterExtensionRevision{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rev-5",
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterExtensionRevisionSpec{
						LifecycleState: ocv1.ClusterExtensionRevisionLifecycleStateActive,
						Revision:       5,
					},
				},
				&ocv1.ClusterExtensionRevision{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rev-6",
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterExtensionRevisionSpec{
						LifecycleState: ocv1.ClusterExtensionRevisionLifecycleStateActive,
						Revision:       6,
					},
				},
				&ocv1.ClusterExtensionRevision{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rev-7",
						Labels: map[string]string{
							labels.OwnerNameKey: ext.Name,
						},
					},
					Spec: ocv1.ClusterExtensionRevisionSpec{
						LifecycleState: ocv1.ClusterExtensionRevisionLifecycleStateActive,
						Revision:       7,
					},
				},
			},
			clientIterceptor: allowedRevisionValue(8),
			validate: func(t *testing.T, c client.Client) {
				rev1 := &ocv1.ClusterExtensionRevision{}
				err := c.Get(t.Context(), client.ObjectKey{Name: "rev-1"}, rev1)
				require.Error(t, err)
				assert.True(t, apierrors.IsNotFound(err))

				rev2 := &ocv1.ClusterExtensionRevision{}
				err = c.Get(t.Context(), client.ObjectKey{Name: "rev-2"}, rev2)
				require.NoError(t, err)

				// Verify active revisions are kept even if beyond the limit
				rev4 := &ocv1.ClusterExtensionRevision{}
				err = c.Get(t.Context(), client.ObjectKey{Name: "rev-4"}, rev4)
				require.NoError(t, err, "active revision 4 should still exist even though it's beyond the limit")
			},
		},
		{
			name: "annotation-only update (same phases, different annotations)",
			mockBuilder: &mockBundleRevisionBuilder{
				makeRevisionFunc: func(ctx context.Context, bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (*ocv1.ClusterExtensionRevision, error) {
					return &ocv1.ClusterExtensionRevision{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: revisionAnnotations,
							Labels: map[string]string{
								labels.OwnerNameKey: ext.Name,
							},
						},
						Spec: ocv1.ClusterExtensionRevisionSpec{
							Phases: []ocv1.ClusterExtensionRevisionPhase{
								{
									Name: string(applier.PhaseDeploy),
									Objects: []ocv1.ClusterExtensionRevisionObject{
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
					}, nil
				},
			},
			existingObjs: []client.Object{
				ext,
				&ocv1.ClusterExtensionRevision{
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
					Spec: ocv1.ClusterExtensionRevisionSpec{
						Revision: 1,
						Phases: []ocv1.ClusterExtensionRevisionPhase{
							{
								Name: string(applier.PhaseDeploy),
								Objects: []ocv1.ClusterExtensionRevisionObject{
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
				revList := &ocv1.ClusterExtensionRevisionList{}
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
				RevisionGenerator: tc.mockBuilder,
				FieldOwner:        "test-owner",
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

func Test_PreAuthorizer_Integration(t *testing.T) {
	testScheme := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(testScheme))

	// This is the revision that the mock builder will produce by default.
	// We calculate its hash to use in the tests.
	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ext",
			UID:  "test-uid",
		},
		Spec: ocv1.ClusterExtensionSpec{
			Namespace: "test-namespace",
			ServiceAccount: ocv1.ServiceAccountReference{
				Name: "test-sa",
			},
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(testScheme).Build()
	dummyGenerator := &mockBundleRevisionBuilder{
		makeRevisionFunc: func(ctx context.Context, bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotation map[string]string) (*ocv1.ClusterExtensionRevision, error) {
			return &ocv1.ClusterExtensionRevision{
				Spec: ocv1.ClusterExtensionRevisionSpec{
					Phases: []ocv1.ClusterExtensionRevisionPhase{
						{
							Name: "some-phase",
							Objects: []ocv1.ClusterExtensionRevisionObject{
								{
									Object: unstructured.Unstructured{
										Object: map[string]interface{}{
											"apiVersion": "v1",
											"kind":       "ConfigMap",
											"data": map[string]string{
												"test-data": "test-data",
											},
										},
									},
								},
							},
						},
					},
				},
			}, nil
		},
	}
	dummyBundleFs := fstest.MapFS{}
	revisionAnnotations := map[string]string{}

	for _, tc := range []struct {
		name          string
		preAuthorizer func(t *testing.T) authorization.PreAuthorizer
		validate      func(t *testing.T, err error)
	}{
		{
			name: "preauthorizer called with correct parameters",
			preAuthorizer: func(t *testing.T) authorization.PreAuthorizer {
				return &mockPreAuthorizer{
					fn: func(ctx context.Context, user user.Info, reader io.Reader, additionalRequiredPerms ...authorization.UserAuthorizerAttributesFactory) ([]authorization.ScopedPolicyRules, error) {
						require.Equal(t, "system:serviceaccount:test-namespace:test-sa", user.GetName())
						require.Empty(t, user.GetUID())
						require.Nil(t, user.GetExtra())
						require.Empty(t, user.GetGroups())

						t.Log("has correct additional permissions")
						require.Len(t, additionalRequiredPerms, 1)
						perms := additionalRequiredPerms[0](user)

						require.Len(t, perms, 1)
						require.Equal(t, authorizer.AttributesRecord{
							User:            user,
							Name:            "test-ext-1",
							APIGroup:        "olm.operatorframework.io",
							APIVersion:      "v1",
							Resource:        "clusterextensionrevisions/finalizers",
							ResourceRequest: true,
							Verb:            "update",
						}, perms[0])

						t.Log("has correct manifest reader")
						manifests, err := io.ReadAll(reader)
						require.NoError(t, err)
						require.Equal(t, "---\napiVersion: v1\ndata:\n  test-data: test-data\nkind: ConfigMap\n", string(manifests))
						return nil, nil
					},
				}
			},
		}, {
			name: "preauthorizer errors are returned",
			preAuthorizer: func(t *testing.T) authorization.PreAuthorizer {
				return &mockPreAuthorizer{
					fn: func(ctx context.Context, user user.Info, reader io.Reader, additionalRequiredPerms ...authorization.UserAuthorizerAttributesFactory) ([]authorization.ScopedPolicyRules, error) {
						return nil, errors.New("test error")
					},
				}
			},
			validate: func(t *testing.T, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "pre-authorization failed")
				require.Contains(t, err.Error(), "authorization evaluation error: test error")
			},
		}, {
			name: "preauthorizer missing permissions are returned as an error",
			preAuthorizer: func(t *testing.T) authorization.PreAuthorizer {
				return &mockPreAuthorizer{
					fn: func(ctx context.Context, user user.Info, reader io.Reader, additionalRequiredPerms ...authorization.UserAuthorizerAttributesFactory) ([]authorization.ScopedPolicyRules, error) {
						return []authorization.ScopedPolicyRules{
							{
								Namespace: "",
								MissingRules: []rbacv1.PolicyRule{
									{
										APIGroups: []string{""},
										Resources: []string{"pods"},
										Verbs:     []string{"get", "list", "watch"},
									},
								},
							},
						}, nil
					},
				}
			},
			validate: func(t *testing.T, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "pre-authorization failed")
				require.Contains(t, err.Error(), "service account requires the following permissions")
				require.Contains(t, err.Error(), "Resources:[pods]")
				require.Contains(t, err.Error(), "Verbs:[get,list,watch]")
			},
		}, {
			name: "preauthorizer missing permissions and errors are combined and returned as an error",
			preAuthorizer: func(t *testing.T) authorization.PreAuthorizer {
				return &mockPreAuthorizer{
					fn: func(ctx context.Context, user user.Info, reader io.Reader, additionalRequiredPerms ...authorization.UserAuthorizerAttributesFactory) ([]authorization.ScopedPolicyRules, error) {
						return []authorization.ScopedPolicyRules{
							{
								Namespace: "",
								MissingRules: []rbacv1.PolicyRule{
									{
										APIGroups: []string{""},
										Resources: []string{"pods"},
										Verbs:     []string{"get", "list", "watch"},
									},
								},
							},
						}, errors.New("test error")
					},
				}
			},
			validate: func(t *testing.T, err error) {
				require.Error(t, err)
				require.Contains(t, err.Error(), "pre-authorization failed")
				require.Contains(t, err.Error(), "service account requires the following permissions")
				require.Contains(t, err.Error(), "Resources:[pods]")
				require.Contains(t, err.Error(), "Verbs:[get,list,watch]")
				require.Contains(t, err.Error(), "authorization evaluation error: test error")
			},
		}, {
			name: "successful call to preauthorizer does not block applier",
			preAuthorizer: func(t *testing.T) authorization.PreAuthorizer {
				return &mockPreAuthorizer{
					fn: func(ctx context.Context, user user.Info, reader io.Reader, additionalRequiredPerms ...authorization.UserAuthorizerAttributesFactory) ([]authorization.ScopedPolicyRules, error) {
						return nil, nil
					},
				}
			},
			validate: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			boxcutter := &applier.Boxcutter{
				Client:            fakeClient,
				Scheme:            testScheme,
				FieldOwner:        "test-owner",
				RevisionGenerator: dummyGenerator,
				PreAuthorizer:     tc.preAuthorizer(t),
			}
			completed, status, err := boxcutter.Apply(t.Context(), dummyBundleFs, ext, nil, revisionAnnotations)
			if tc.validate != nil {
				tc.validate(t, err)
			}
			_ = completed
			_ = status
		})
	}
}

func TestBoxcutterStorageMigrator(t *testing.T) {
	t.Run("creates revision", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))

		brb := &mockBundleRevisionBuilder{}
		mag := &mockActionGetter{
			currentRel: &release.Release{
				Name: "test123",
				Info: &release.Info{Status: release.StatusDeployed},
			},
		}
		client := &clientMock{}
		sm := &applier.BoxcutterStorageMigrator{
			RevisionGenerator:  brb,
			ActionClientGetter: mag,
			Client:             client,
			Scheme:             testScheme,
		}

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "test123"},
		}

		client.
			On("List", mock.Anything, mock.AnythingOfType("*v1.ClusterExtensionRevisionList"), mock.Anything).
			Return(nil)
		client.
			On("Create", mock.Anything, mock.AnythingOfType("*v1.ClusterExtensionRevision"), mock.Anything).
			Once().
			Run(func(args mock.Arguments) {
				// Verify the migration marker label is set before creation
				rev := args.Get(1).(*ocv1.ClusterExtensionRevision)
				require.Equal(t, "true", rev.Labels[labels.MigratedFromHelmKey], "Migration marker label should be set")

				// Simulate real Kubernetes behavior: Create() populates server-managed fields
				rev.Generation = 1
				rev.ResourceVersion = "1"
			}).
			Return(nil)
		client.
			On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1.ClusterExtensionRevision"), mock.Anything).
			Once().
			Run(func(args mock.Arguments) {
				// Simulate Get() returning the created revision with server-managed fields
				rev := args.Get(2).(*ocv1.ClusterExtensionRevision)
				rev.Generation = 1
				rev.ResourceVersion = "1"
			}).
			Return(nil)

		err := sm.Migrate(t.Context(), ext, map[string]string{"my-label": "my-value"})
		require.NoError(t, err)

		client.AssertExpectations(t)

		// Verify the migrated revision has Succeeded=True status with Succeeded reason and a migration message
		statusWriter := client.Status().(*statusWriterMock)
		require.True(t, statusWriter.updateCalled, "Status().Update() should be called during migration")
		require.NotNil(t, statusWriter.updatedObj, "Updated object should not be nil")

		rev, ok := statusWriter.updatedObj.(*ocv1.ClusterExtensionRevision)
		require.True(t, ok, "Updated object should be a ClusterExtensionRevision")

		succeededCond := apimeta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeSucceeded)
		require.NotNil(t, succeededCond, "Succeeded condition should be set")
		assert.Equal(t, metav1.ConditionTrue, succeededCond.Status, "Succeeded condition should be True")
		assert.Equal(t, ocv1.ReasonSucceeded, succeededCond.Reason, "Reason should be Succeeded")
		assert.Equal(t, "Revision succeeded - migrated from Helm release", succeededCond.Message, "Message should indicate Helm migration")
		assert.Equal(t, int64(1), succeededCond.ObservedGeneration, "ObservedGeneration should match revision generation")
	})

	t.Run("does not create revision when revisions exist", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))

		brb := &mockBundleRevisionBuilder{}
		mag := &mockActionGetter{}
		client := &clientMock{}
		sm := &applier.BoxcutterStorageMigrator{
			RevisionGenerator:  brb,
			ActionClientGetter: mag,
			Client:             client,
			Scheme:             testScheme,
		}

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "test123"},
		}

		existingRev := ocv1.ClusterExtensionRevision{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-revision",
				Generation: 2,
				Labels: map[string]string{
					labels.MigratedFromHelmKey: "true",
				},
			},
			Spec: ocv1.ClusterExtensionRevisionSpec{
				Revision: 1, // Migration creates revision 1
			},
			Status: ocv1.ClusterExtensionRevisionStatus{
				Conditions: []metav1.Condition{
					{
						Type:   ocv1.ClusterExtensionRevisionTypeSucceeded,
						Status: metav1.ConditionTrue,
						Reason: ocv1.ReasonSucceeded,
					},
				},
			},
		}

		client.
			On("List", mock.Anything, mock.AnythingOfType("*v1.ClusterExtensionRevisionList"), mock.Anything).
			Run(func(args mock.Arguments) {
				list := args.Get(1).(*ocv1.ClusterExtensionRevisionList)
				list.Items = []ocv1.ClusterExtensionRevision{existingRev}
			}).
			Return(nil)

		err := sm.Migrate(t.Context(), ext, map[string]string{"my-label": "my-value"})
		require.NoError(t, err)

		client.AssertExpectations(t)
	})

	t.Run("sets status when revision exists but status is missing", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))

		brb := &mockBundleRevisionBuilder{}
		mag := &mockActionGetter{}
		client := &clientMock{}
		sm := &applier.BoxcutterStorageMigrator{
			RevisionGenerator:  brb,
			ActionClientGetter: mag,
			Client:             client,
			Scheme:             testScheme,
		}

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "test123"},
		}

		existingRev := ocv1.ClusterExtensionRevision{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-revision",
				Generation: 2,
				Labels: map[string]string{
					labels.MigratedFromHelmKey: "true",
				},
			},
			Spec: ocv1.ClusterExtensionRevisionSpec{
				Revision: 1, // Migration creates revision 1
			},
			// Status is empty - simulating the case where creation succeeded but status update failed
		}

		client.
			On("List", mock.Anything, mock.AnythingOfType("*v1.ClusterExtensionRevisionList"), mock.Anything).
			Run(func(args mock.Arguments) {
				list := args.Get(1).(*ocv1.ClusterExtensionRevisionList)
				list.Items = []ocv1.ClusterExtensionRevision{existingRev}
			}).
			Return(nil)

		client.
			On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1.ClusterExtensionRevision"), mock.Anything).
			Run(func(args mock.Arguments) {
				rev := args.Get(2).(*ocv1.ClusterExtensionRevision)
				*rev = existingRev
			}).
			Return(nil)

		err := sm.Migrate(t.Context(), ext, map[string]string{"my-label": "my-value"})
		require.NoError(t, err)

		client.AssertExpectations(t)

		// Verify the status was set
		statusWriter := client.Status().(*statusWriterMock)
		require.True(t, statusWriter.updateCalled, "Status().Update() should be called to set missing status")
		require.NotNil(t, statusWriter.updatedObj, "Updated object should not be nil")

		rev, ok := statusWriter.updatedObj.(*ocv1.ClusterExtensionRevision)
		require.True(t, ok, "Updated object should be a ClusterExtensionRevision")

		succeededCond := apimeta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeSucceeded)
		require.NotNil(t, succeededCond, "Succeeded condition should be set")
		assert.Equal(t, metav1.ConditionTrue, succeededCond.Status, "Succeeded condition should be True")
		assert.Equal(t, ocv1.ReasonSucceeded, succeededCond.Reason, "Reason should be Succeeded")
	})

	t.Run("updates status from False to True for migrated revision", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))

		brb := &mockBundleRevisionBuilder{}
		mag := &mockActionGetter{}
		client := &clientMock{}
		sm := &applier.BoxcutterStorageMigrator{
			RevisionGenerator:  brb,
			ActionClientGetter: mag,
			Client:             client,
			Scheme:             testScheme,
		}

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "test123"},
		}

		// Migrated revision with Succeeded=False (e.g., from a previous failed status update attempt)
		// This simulates a revision whose Succeeded condition should be corrected from False to True during migration.
		existingRev := ocv1.ClusterExtensionRevision{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-revision",
				Generation: 2,
				Labels: map[string]string{
					labels.MigratedFromHelmKey: "true",
				},
			},
			Spec: ocv1.ClusterExtensionRevisionSpec{
				Revision: 1,
			},
			Status: ocv1.ClusterExtensionRevisionStatus{
				Conditions: []metav1.Condition{
					{
						Type:   ocv1.ClusterExtensionRevisionTypeSucceeded,
						Status: metav1.ConditionFalse, // Important: False, not missing
						Reason: "InProgress",
					},
				},
			},
		}

		client.
			On("List", mock.Anything, mock.AnythingOfType("*v1.ClusterExtensionRevisionList"), mock.Anything).
			Run(func(args mock.Arguments) {
				list := args.Get(1).(*ocv1.ClusterExtensionRevisionList)
				list.Items = []ocv1.ClusterExtensionRevision{existingRev}
			}).
			Return(nil)

		client.
			On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1.ClusterExtensionRevision"), mock.Anything).
			Run(func(args mock.Arguments) {
				rev := args.Get(2).(*ocv1.ClusterExtensionRevision)
				*rev = existingRev
			}).
			Return(nil)

		err := sm.Migrate(t.Context(), ext, map[string]string{"my-label": "my-value"})
		require.NoError(t, err)

		client.AssertExpectations(t)

		// Verify the status was updated from False to True
		statusWriter := client.Status().(*statusWriterMock)
		require.True(t, statusWriter.updateCalled, "Status().Update() should be called to update False to True")
		require.NotNil(t, statusWriter.updatedObj, "Updated object should not be nil")

		rev, ok := statusWriter.updatedObj.(*ocv1.ClusterExtensionRevision)
		require.True(t, ok, "Updated object should be a ClusterExtensionRevision")

		succeededCond := apimeta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeSucceeded)
		require.NotNil(t, succeededCond, "Succeeded condition should be set")
		assert.Equal(t, metav1.ConditionTrue, succeededCond.Status, "Succeeded condition should be updated to True")
		assert.Equal(t, ocv1.ReasonSucceeded, succeededCond.Reason, "Reason should be Succeeded")
	})

	t.Run("does not set status on non-migrated revision 1", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))

		brb := &mockBundleRevisionBuilder{}
		mag := &mockActionGetter{}
		client := &clientMock{}
		sm := &applier.BoxcutterStorageMigrator{
			RevisionGenerator:  brb,
			ActionClientGetter: mag,
			Client:             client,
			Scheme:             testScheme,
		}

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "test123"},
		}

		// Revision 1 created by normal Boxcutter operation (no migration label)
		// This simulates the first rollout - status should NOT be set as it may still be in progress
		existingRev := ocv1.ClusterExtensionRevision{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-revision",
				Generation: 2,
				// No migration label - this is a normal Boxcutter revision
			},
			Spec: ocv1.ClusterExtensionRevisionSpec{
				Revision: 1,
			},
		}

		client.
			On("List", mock.Anything, mock.AnythingOfType("*v1.ClusterExtensionRevisionList"), mock.Anything).
			Run(func(args mock.Arguments) {
				list := args.Get(1).(*ocv1.ClusterExtensionRevisionList)
				list.Items = []ocv1.ClusterExtensionRevision{existingRev}
			}).
			Return(nil)

		// The migration flow calls Get() to re-fetch the revision before checking its status.
		// Even for non-migrated revisions, Get() is called to determine if status needs to be set.
		client.
			On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1.ClusterExtensionRevision"), mock.Anything).
			Run(func(args mock.Arguments) {
				rev := args.Get(2).(*ocv1.ClusterExtensionRevision)
				*rev = existingRev
			}).
			Return(nil)

		err := sm.Migrate(t.Context(), ext, map[string]string{"my-label": "my-value"})
		require.NoError(t, err)

		client.AssertExpectations(t)

		// Verify the status was NOT set for non-migrated revision
		statusWriter := client.Status().(*statusWriterMock)
		require.False(t, statusWriter.updateCalled, "Status().Update() should NOT be called for non-migrated revisions")
	})

	t.Run("migrates from most recent deployed release when latest is failed", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))

		brb := &mockBundleRevisionBuilder{}
		mag := &mockActionGetter{
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
				{
					Name:    "test123",
					Version: 2,
					Info:    &release.Info{Status: release.StatusDeployed},
				},
				{
					Name:    "test123",
					Version: 1,
					Info:    &release.Info{Status: release.StatusSuperseded},
				},
			},
		}
		client := &clientMock{}
		sm := &applier.BoxcutterStorageMigrator{
			RevisionGenerator:  brb,
			ActionClientGetter: mag,
			Client:             client,
			Scheme:             testScheme,
		}

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "test123"},
		}

		client.
			On("List", mock.Anything, mock.AnythingOfType("*v1.ClusterExtensionRevisionList"), mock.Anything).
			Return(nil)

		client.
			On("Create", mock.Anything, mock.AnythingOfType("*v1.ClusterExtensionRevision"), mock.Anything).
			Once().
			Run(func(args mock.Arguments) {
				// Verify the migration marker label is set before creation
				rev := args.Get(1).(*ocv1.ClusterExtensionRevision)
				require.Equal(t, "true", rev.Labels[labels.MigratedFromHelmKey], "Migration marker label should be set")

				// Simulate real Kubernetes behavior: Create() populates server-managed fields
				rev.Generation = 1
				rev.ResourceVersion = "1"
			}).
			Return(nil)

		client.
			On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1.ClusterExtensionRevision"), mock.Anything).
			Run(func(args mock.Arguments) {
				rev := args.Get(2).(*ocv1.ClusterExtensionRevision)
				rev.ObjectMeta.Name = "test-revision"
				rev.ObjectMeta.Generation = 1
				rev.ObjectMeta.ResourceVersion = "1"
				rev.Labels = map[string]string{
					labels.MigratedFromHelmKey: "true",
				}
			}).
			Return(nil)

		err := sm.Migrate(t.Context(), ext, map[string]string{"my-label": "my-value"})
		require.NoError(t, err)

		client.AssertExpectations(t)

		// Verify the correct release (version 2, deployed) was used instead of version 3 (failed)
		require.NotNil(t, brb.helmReleaseUsed, "GenerateRevisionFromHelmRelease should have been called")
		assert.Equal(t, 2, brb.helmReleaseUsed.Version, "Should use version 2 (deployed), not version 3 (failed)")
		assert.Equal(t, release.StatusDeployed, brb.helmReleaseUsed.Info.Status, "Should use deployed release")

		// Verify the migrated revision has Succeeded=True status
		statusWriter := client.Status().(*statusWriterMock)
		require.True(t, statusWriter.updateCalled, "Status().Update() should be called during migration")
		require.NotNil(t, statusWriter.updatedObj, "Updated object should not be nil")

		rev, ok := statusWriter.updatedObj.(*ocv1.ClusterExtensionRevision)
		require.True(t, ok, "Updated object should be a ClusterExtensionRevision")

		succeededCond := apimeta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeSucceeded)
		require.NotNil(t, succeededCond, "Succeeded condition should be set")
		assert.Equal(t, metav1.ConditionTrue, succeededCond.Status, "Succeeded condition should be True")
	})

	t.Run("does not create revision when helm release is not deployed and no deployed history", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))

		brb := &mockBundleRevisionBuilder{}
		mag := &mockActionGetter{
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
		}
		client := &clientMock{}
		sm := &applier.BoxcutterStorageMigrator{
			RevisionGenerator:  brb,
			ActionClientGetter: mag,
			Client:             client,
			Scheme:             testScheme,
		}

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "test123"},
		}

		client.
			On("List", mock.Anything, mock.AnythingOfType("*v1.ClusterExtensionRevisionList"), mock.Anything).
			Return(nil)

		err := sm.Migrate(t.Context(), ext, map[string]string{"my-label": "my-value"})
		require.NoError(t, err)

		client.AssertExpectations(t)
		// brb.GenerateRevisionFromHelmRelease should NOT have been called
		require.False(t, brb.generateRevisionFromHelmReleaseCalled, "GenerateRevisionFromHelmRelease should NOT be called when no deployed release exists")
	})

	t.Run("does not create revision when no helm release", func(t *testing.T) {
		testScheme := runtime.NewScheme()
		require.NoError(t, ocv1.AddToScheme(testScheme))

		brb := &mockBundleRevisionBuilder{}
		mag := &mockActionGetter{
			getClientErr: driver.ErrReleaseNotFound,
		}
		client := &clientMock{}
		sm := &applier.BoxcutterStorageMigrator{
			RevisionGenerator:  brb,
			ActionClientGetter: mag,
			Client:             client,
			Scheme:             testScheme,
		}

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: "test123"},
		}

		client.
			On("List", mock.Anything, mock.AnythingOfType("*v1.ClusterExtensionRevisionList"), mock.Anything).
			Return(nil)

		err := sm.Migrate(t.Context(), ext, map[string]string{"my-label": "my-value"})
		require.NoError(t, err)

		client.AssertExpectations(t)
	})
}

// mockBundleRevisionBuilder is a mock implementation of the ClusterExtensionRevisionGenerator for testing.
type mockBundleRevisionBuilder struct {
	makeRevisionFunc                      func(ctx context.Context, bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotation map[string]string) (*ocv1.ClusterExtensionRevision, error)
	generateRevisionFromHelmReleaseCalled bool
	helmReleaseUsed                       *release.Release
}

func (m *mockBundleRevisionBuilder) GenerateRevision(ctx context.Context, bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (*ocv1.ClusterExtensionRevision, error) {
	return m.makeRevisionFunc(ctx, bundleFS, ext, objectLabels, revisionAnnotations)
}

func (m *mockBundleRevisionBuilder) GenerateRevisionFromHelmRelease(
	ctx context.Context,
	helmRelease *release.Release, ext *ocv1.ClusterExtension,
	objectLabels map[string]string,
) (*ocv1.ClusterExtensionRevision, error) {
	m.generateRevisionFromHelmReleaseCalled = true
	m.helmReleaseUsed = helmRelease
	return &ocv1.ClusterExtensionRevision{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-revision",
			Labels: map[string]string{
				labels.OwnerNameKey: ext.Name,
			},
		},
		Spec: ocv1.ClusterExtensionRevisionSpec{},
	}, nil
}

type clientMock struct {
	mock.Mock
	statusWriter *statusWriterMock
}

func (m *clientMock) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	args := m.Called(ctx, list, opts)
	return args.Error(0)
}

func (m *clientMock) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	args := m.Called(ctx, key, obj, opts)
	return args.Error(0)
}

func (m *clientMock) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *clientMock) Status() client.StatusWriter {
	if m.statusWriter == nil {
		m.statusWriter = &statusWriterMock{mock: &m.Mock}
	}
	return m.statusWriter
}

type statusWriterMock struct {
	mock         *mock.Mock
	updatedObj   client.Object
	updateCalled bool
}

func (s *statusWriterMock) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	// Capture the status update for test verification
	s.updatedObj = obj
	s.updateCalled = true
	return nil
}

func (s *statusWriterMock) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	return nil
}

func (s *statusWriterMock) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	return nil
}
