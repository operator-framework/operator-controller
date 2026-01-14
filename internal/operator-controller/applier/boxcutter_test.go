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
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	k8scheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
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

	rev, err := b.GenerateRevision(t.Context(), fstest.MapFS{}, ext, map[string]string{}, map[string]string{})
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

func Test_SimpleRevisionGenerator_Renderer_Integration(t *testing.T) {
	bundleFS := fstest.MapFS{}
	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-extension",
		},
	}
	r := &FakeManifestProvider{
		GetFn: func(b fs.FS, e *ocv1.ClusterExtension) ([]client.Object, error) {
			t.Log("by checking renderer was called with the correct parameters")
			require.Equal(t, bundleFS, b)
			require.Equal(t, ext, e)
			return nil, nil
		},
	}
	b := applier.SimpleRevisionGenerator{
		Scheme:           k8scheme.Scheme,
		ManifestProvider: r,
	}

	_, err := b.GenerateRevision(t.Context(), bundleFS, ext, map[string]string{}, map[string]string{})
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

	rev, err := b.GenerateRevision(t.Context(), fstest.MapFS{}, &ocv1.ClusterExtension{
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
			err := boxcutter.Apply(t.Context(), testFS, ext, nil, revisionAnnotations)

			// Assert
			if tc.expectedErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedErr)
			} else {
				require.NoError(t, err)
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
	t.Run("creates revision", func(t *testing.T) {
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

		client.
			On("List", mock.Anything, mock.AnythingOfType("*v1.ClusterExtensionRevisionList"), mock.Anything).
			Return(nil)
		client.
			On("Create", mock.Anything, mock.AnythingOfType("*v1.ClusterExtensionRevision"), mock.Anything).
			Once().
			Run(func(args mock.Arguments) {
				// Simulate real Kubernetes behavior: Create() populates server-managed fields
				rev := args.Get(1).(*ocv1.ClusterExtensionRevision)
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

		client.
			On("List", mock.Anything, mock.AnythingOfType("*v1.ClusterExtensionRevisionList"), mock.Anything).
			Run(func(args mock.Arguments) {
				list := args.Get(1).(*ocv1.ClusterExtensionRevisionList)
				list.Items = []ocv1.ClusterExtensionRevision{
					{}, {}, // Existing revisions.
				}
			}).
			Return(nil)

		err := sm.Migrate(t.Context(), ext, map[string]string{"my-label": "my-value"})
		require.NoError(t, err)

		client.AssertExpectations(t)
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
	makeRevisionFunc func(ctx context.Context, bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotation map[string]string) (*ocv1.ClusterExtensionRevision, error)
}

func (m *mockBundleRevisionBuilder) GenerateRevision(ctx context.Context, bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (*ocv1.ClusterExtensionRevision, error) {
	return m.makeRevisionFunc(ctx, bundleFS, ext, objectLabels, revisionAnnotations)
}

func (m *mockBundleRevisionBuilder) GenerateRevisionFromHelmRelease(
	ctx context.Context,
	helmRelease *release.Release, ext *ocv1.ClusterExtension,
	objectLabels map[string]string,
) (*ocv1.ClusterExtensionRevision, error) {
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
	return &statusWriterMock{mock: &m.Mock}
}

type statusWriterMock struct {
	mock *mock.Mock
}

func (s *statusWriterMock) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	// Status updates are expected during migration - return success by default
	return nil
}

func (s *statusWriterMock) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	return nil
}

func (s *statusWriterMock) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	return nil
}
