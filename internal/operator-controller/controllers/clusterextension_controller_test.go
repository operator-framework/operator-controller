package controllers_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/fstest"

	bsemver "github.com/blang/semver/v4"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crfinalizer "sigs.k8s.io/controller-runtime/pkg/finalizer"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"
	"github.com/operator-framework/operator-registry/alpha/declcfg"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/authentication"
	"github.com/operator-framework/operator-controller/internal/operator-controller/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/conditionsets"
	"github.com/operator-framework/operator-controller/internal/operator-controller/controllers"
	finalizers "github.com/operator-framework/operator-controller/internal/operator-controller/finalizers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
	"github.com/operator-framework/operator-controller/internal/operator-controller/resolve"
	imageutil "github.com/operator-framework/operator-controller/internal/shared/util/image"
)

// Describe: ClusterExtension Controller Test
func TestClusterExtensionDoesNotExist(t *testing.T) {
	_, reconciler := newClientAndReconciler(t)

	t.Log("When the cluster extension does not exist")
	t.Log("It returns no error")
	res, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "non-existent"}})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)
}

func TestClusterExtensionShortCircuitsReconcileDuringDeletion(t *testing.T) {
	installedBundleGetterCalledErr := errors.New("revision states getter called")

	cl, reconciler := newClientAndReconciler(t, func(d *deps) {
		d.RevisionStatesGetter = &MockRevisionStatesGetter{
			Err: installedBundleGetterCalledErr,
		}
	})

	checkInstalledBundleGetterCalled := func(t require.TestingT, err error, args ...interface{}) {
		require.Equal(t, installedBundleGetterCalledErr, err)
	}

	type testCase struct {
		name         string
		finalizers   []string
		shouldDelete bool
		expectErr    require.ErrorAssertionFunc
	}
	for _, tc := range []testCase{
		{
			name:      "no finalizers, not deleted",
			expectErr: checkInstalledBundleGetterCalled,
		},
		{
			name:       "has finalizers, not deleted",
			finalizers: []string{"finalizer"},
			expectErr:  checkInstalledBundleGetterCalled,
		},
		{
			name:         "has finalizers, deleted",
			finalizers:   []string{"finalizer"},
			shouldDelete: true,
			expectErr:    require.NoError,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkgName := fmt.Sprintf("test-pkg-%s", rand.String(6))

			ctx := context.Background()
			extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

			t.Log("When the cluster extension specifies a non-existent package")
			t.Log("By initializing cluster state")
			clusterExtension := &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name:       extKey.Name,
					Finalizers: tc.finalizers,
				},
				Spec: ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1.CatalogFilter{
							PackageName: pkgName,
						},
					},
					Namespace: "default",
					ServiceAccount: ocv1.ServiceAccountReference{
						Name: "default",
					},
				},
			}
			require.NoError(t, cl.Create(ctx, clusterExtension))
			if tc.shouldDelete {
				require.NoError(t, cl.Delete(ctx, clusterExtension))
			}

			t.Log("By running reconcile")
			res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
			require.Equal(t, ctrl.Result{}, res)
			tc.expectErr(t, err)
		})
	}
}

func TestClusterExtensionResolutionFails(t *testing.T) {
	pkgName := fmt.Sprintf("non-existent-%s", rand.String(6))
	cl, reconciler := newClientAndReconciler(t, func(d *deps) {
		d.Resolver = resolve.Func(func(_ context.Context, _ *ocv1.ClusterExtension, _ *ocv1.BundleMetadata) (*declcfg.Bundle, *bundle.VersionRelease, *declcfg.Deprecation, error) {
			return nil, nil, nil, fmt.Errorf("no package %q found", pkgName)
		})
	})

	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

	t.Log("When the cluster extension specifies a non-existent package")
	t.Log("By initializing cluster state")
	clusterExtension := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1.ClusterExtensionSpec{
			Source: ocv1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &ocv1.CatalogFilter{
					PackageName: pkgName,
				},
			},
			Namespace: "default",
			ServiceAccount: ocv1.ServiceAccountReference{
				Name: "default",
			},
		},
	}
	require.NoError(t, cl.Create(ctx, clusterExtension))

	t.Log("It sets resolution failure status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.EqualError(t, err, fmt.Sprintf("no package %q found", pkgName))

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("By checking the status fields")
	require.Empty(t, clusterExtension.Status.Install)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ocv1.ReasonRetrying, cond.Reason)
	require.Equal(t, fmt.Sprintf("no package %q found", pkgName), cond.Message)

	verifyInvariants(ctx, t, reconciler.Client, clusterExtension)
	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1.ClusterExtension{}))
}

func TestClusterExtensionResolutionSuccessfulUnpackFails(t *testing.T) {
	type testCase struct {
		name           string
		pullErr        error
		expectTerminal bool
	}
	for _, tc := range []testCase{
		{
			name:    "non-terminal pull failure",
			pullErr: errors.New("pull failure"),
		},
		{
			name:           "terminal pull failure",
			pullErr:        reconcile.TerminalError(errors.New("terminal pull failure")),
			expectTerminal: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

			t.Log("When the cluster extension specifies a channel with version that exist")
			t.Log("By initializing cluster state")
			pkgName := "prometheus"
			pkgVer := "1.0.0"
			pkgChan := "beta"
			namespace := fmt.Sprintf("test-ns-%s", rand.String(8))
			serviceAccount := fmt.Sprintf("test-sa-%s", rand.String(8))

			clusterExtension := &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
				Spec: ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1.CatalogFilter{
							PackageName: pkgName,
							Version:     pkgVer,
							Channels:    []string{pkgChan},
						},
					},
					Namespace: namespace,
					ServiceAccount: ocv1.ServiceAccountReference{
						Name: serviceAccount,
					},
				},
			}
			cl, reconciler := newClientAndReconciler(t,
				func(d *deps) {
					d.ImagePuller = &imageutil.MockPuller{
						Error: tc.pullErr,
					}
				},
				func(d *deps) {
					d.Resolver = resolve.Func(func(_ context.Context, _ *ocv1.ClusterExtension, _ *ocv1.BundleMetadata) (*declcfg.Bundle, *bundle.VersionRelease, *declcfg.Deprecation, error) {
						v := bundle.VersionRelease{
							Version: bsemver.MustParse("1.0.0"),
						}
						return &declcfg.Bundle{
							Name:    "prometheus.v1.0.0",
							Package: "prometheus",
							Image:   "quay.io/operatorhubio/prometheus@fake1.0.0",
						}, &v, nil, nil
					})
				},
			)

			err := cl.Create(ctx, clusterExtension)
			require.NoError(t, err)

			t.Log("It sets resolution success status")
			t.Log("By running reconcile")

			res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
			require.Equal(t, ctrl.Result{}, res)
			require.Error(t, err)

			isTerminal := errors.Is(err, reconcile.TerminalError(nil))
			assert.Equal(t, tc.expectTerminal, isTerminal, "expected terminal error: %v, got: %v", tc.expectTerminal, isTerminal)
			require.ErrorContains(t, err, tc.pullErr.Error())

			t.Log("By fetching updated cluster extension after reconcile")
			require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

			t.Log("By checking the status fields")
			expectedBundleMetadata := ocv1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"}
			require.Empty(t, clusterExtension.Status.Install)

			t.Log("By checking the expected conditions")
			expectStatus := metav1.ConditionTrue
			expectReason := ocv1.ReasonRetrying
			if tc.expectTerminal {
				expectStatus = metav1.ConditionFalse
				expectReason = ocv1.ReasonBlocked
			}
			progressingCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
			require.NotNil(t, progressingCond)
			require.Equal(t, expectStatus, progressingCond.Status)
			require.Equal(t, expectReason, progressingCond.Reason)
			require.Contains(t, progressingCond.Message, fmt.Sprintf("for resolved bundle %q with version %q", expectedBundleMetadata.Name, expectedBundleMetadata.Version))

			require.NoError(t, cl.DeleteAllOf(ctx, &ocv1.ClusterExtension{}))
		})
	}
}

func TestClusterExtensionResolutionAndUnpackSuccessfulApplierFails(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t,
		func(d *deps) {
			d.ImagePuller = &imageutil.MockPuller{
				ImageFS: fstest.MapFS{},
			}
			d.Resolver = resolve.Func(func(_ context.Context, _ *ocv1.ClusterExtension, _ *ocv1.BundleMetadata) (*declcfg.Bundle, *bundle.VersionRelease, *declcfg.Deprecation, error) {
				v := bundle.VersionRelease{
					Version: bsemver.MustParse("1.0.0"),
				}
				return &declcfg.Bundle{
					Name:    "prometheus.v1.0.0",
					Package: "prometheus",
					Image:   "quay.io/operatorhubio/prometheus@fake1.0.0",
				}, &v, nil, nil
			})
			d.Applier = &MockApplier{
				err: errors.New("apply failure"),
			}
		})

	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

	t.Log("When the cluster extension specifies a channel with version that exist")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgVer := "1.0.0"
	pkgChan := "beta"
	namespace := fmt.Sprintf("test-ns-%s", rand.String(8))
	serviceAccount := fmt.Sprintf("test-sa-%s", rand.String(8))

	clusterExtension := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1.ClusterExtensionSpec{
			Source: ocv1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &ocv1.CatalogFilter{
					PackageName: pkgName,
					Version:     pkgVer,
					Channels:    []string{pkgChan},
				},
			},
			Namespace: namespace,
			ServiceAccount: ocv1.ServiceAccountReference{
				Name: serviceAccount,
			},
		},
	}
	err := cl.Create(ctx, clusterExtension)
	require.NoError(t, err)

	t.Log("It sets resolution success status")
	t.Log("By running reconcile")

	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.Error(t, err)

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("By checking the status fields")
	expectedBundleMetadata := ocv1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"}
	require.Empty(t, clusterExtension.Status.Install)

	t.Log("By checking the expected installed conditions")
	installedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
	require.NotNil(t, installedCond)
	require.Equal(t, metav1.ConditionFalse, installedCond.Status)
	require.Equal(t, ocv1.ReasonFailed, installedCond.Reason)

	t.Log("By checking the expected progressing conditions")
	progressingCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
	require.NotNil(t, progressingCond)
	require.Equal(t, metav1.ConditionTrue, progressingCond.Status)
	require.Equal(t, ocv1.ReasonRetrying, progressingCond.Reason)
	require.Contains(t, progressingCond.Message, fmt.Sprintf("for resolved bundle %q with version %q", expectedBundleMetadata.Name, expectedBundleMetadata.Version))

	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1.ClusterExtension{}))
}

func TestClusterExtensionServiceAccountNotFound(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t, func(d *deps) {
		d.RevisionStatesGetter = &MockRevisionStatesGetter{
			Err: &authentication.ServiceAccountNotFoundError{
				ServiceAccountName:      "missing-sa",
				ServiceAccountNamespace: "default",
			}}
	})

	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

	t.Log("Given a cluster extension with a missing service account")
	clusterExtension := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1.ClusterExtensionSpec{
			Source: ocv1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &ocv1.CatalogFilter{
					PackageName: "test-package",
				},
			},
			Namespace: "default",
			ServiceAccount: ocv1.ServiceAccountReference{
				Name: "missing-sa",
			},
		},
	}

	require.NoError(t, cl.Create(ctx, clusterExtension))

	t.Log("When reconciling the cluster extension")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})

	require.Equal(t, ctrl.Result{}, res)
	require.Error(t, err)
	var saErr *authentication.ServiceAccountNotFoundError
	require.ErrorAs(t, err, &saErr)
	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("By checking the status conditions")
	installedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
	require.NotNil(t, installedCond)
	require.Equal(t, metav1.ConditionUnknown, installedCond.Status)
	require.Contains(t, installedCond.Message, fmt.Sprintf("service account %q not found in namespace %q: unable to authenticate with the Kubernetes cluster.",
		"missing-sa", "default"))

	progressingCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
	require.NotNil(t, progressingCond)
	require.Equal(t, metav1.ConditionTrue, progressingCond.Status)
	require.Equal(t, ocv1.ReasonRetrying, progressingCond.Reason)
	require.Contains(t, progressingCond.Message, "installation cannot proceed due to missing ServiceAccount")
	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1.ClusterExtension{}))
}

func TestClusterExtensionApplierFailsWithBundleInstalled(t *testing.T) {
	mockApplier := &MockApplier{
		installCompleted: true,
	}
	cl, reconciler := newClientAndReconciler(t, func(d *deps) {
		d.ImagePuller = &imageutil.MockPuller{
			ImageFS: fstest.MapFS{},
		}
		d.Resolver = resolve.Func(func(_ context.Context, _ *ocv1.ClusterExtension, _ *ocv1.BundleMetadata) (*declcfg.Bundle, *bundle.VersionRelease, *declcfg.Deprecation, error) {
			v := bundle.VersionRelease{
				Version: bsemver.MustParse("1.0.0"),
			}
			return &declcfg.Bundle{
				Name:    "prometheus.v1.0.0",
				Package: "prometheus",
				Image:   "quay.io/operatorhubio/prometheus@fake1.0.0",
			}, &v, nil, nil
		})

		d.RevisionStatesGetter = &MockRevisionStatesGetter{
			RevisionStates: &controllers.RevisionStates{
				Installed: &controllers.RevisionMetadata{
					BundleMetadata: ocv1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"},
					Image:          "quay.io/operatorhubio/prometheus@fake1.0.0",
				},
			},
		}
		d.Applier = mockApplier
	})

	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

	t.Log("When the cluster extension specifies a channel with version that exist")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgVer := "1.0.0"
	pkgChan := "beta"
	namespace := fmt.Sprintf("test-ns-%s", rand.String(8))
	serviceAccount := fmt.Sprintf("test-sa-%s", rand.String(8))

	clusterExtension := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1.ClusterExtensionSpec{
			Source: ocv1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &ocv1.CatalogFilter{
					PackageName: pkgName,
					Version:     pkgVer,
					Channels:    []string{pkgChan},
				},
			},
			Namespace: namespace,
			ServiceAccount: ocv1.ServiceAccountReference{
				Name: serviceAccount,
			},
		},
	}
	err := cl.Create(ctx, clusterExtension)
	require.NoError(t, err)

	t.Log("It sets resolution success status")
	t.Log("By running reconcile")

	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	mockApplier.installCompleted = false
	mockApplier.err = errors.New("apply failure")

	res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.Error(t, err)

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("By checking the status fields")
	expectedBundleMetadata := ocv1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"}
	require.Equal(t, expectedBundleMetadata, clusterExtension.Status.Install.Bundle)

	t.Log("By checking the expected installed conditions")
	installedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
	require.NotNil(t, installedCond)
	require.Equal(t, metav1.ConditionTrue, installedCond.Status)
	require.Equal(t, ocv1.ReasonSucceeded, installedCond.Reason)

	t.Log("By checking the expected progressing conditions")
	progressingCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
	require.NotNil(t, progressingCond)
	require.Equal(t, metav1.ConditionTrue, progressingCond.Status)
	require.Equal(t, ocv1.ReasonRetrying, progressingCond.Reason)
	require.Contains(t, progressingCond.Message, fmt.Sprintf("for resolved bundle %q with version %q", expectedBundleMetadata.Name, expectedBundleMetadata.Version))

	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1.ClusterExtension{}))
}

func TestClusterExtensionManagerFailed(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t, func(d *deps) {
		d.ImagePuller = &imageutil.MockPuller{
			ImageFS: fstest.MapFS{},
		}
		d.Resolver = resolve.Func(func(_ context.Context, _ *ocv1.ClusterExtension, _ *ocv1.BundleMetadata) (*declcfg.Bundle, *bundle.VersionRelease, *declcfg.Deprecation, error) {
			v := bundle.VersionRelease{
				Version: bsemver.MustParse("1.0.0"),
			}
			return &declcfg.Bundle{
				Name:    "prometheus.v1.0.0",
				Package: "prometheus",
				Image:   "quay.io/operatorhubio/prometheus@fake1.0.0",
			}, &v, nil, nil
		})
		d.Applier = &MockApplier{
			installCompleted: true,
			err:              errors.New("manager fail"),
		}
	})

	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

	t.Log("When the cluster extension specifies a channel with version that exist")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgVer := "1.0.0"
	pkgChan := "beta"
	namespace := fmt.Sprintf("test-ns-%s", rand.String(8))
	serviceAccount := fmt.Sprintf("test-sa-%s", rand.String(8))

	clusterExtension := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1.ClusterExtensionSpec{
			Source: ocv1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &ocv1.CatalogFilter{
					PackageName: pkgName,
					Version:     pkgVer,
					Channels:    []string{pkgChan},
				},
			},
			Namespace: namespace,
			ServiceAccount: ocv1.ServiceAccountReference{
				Name: serviceAccount,
			},
		},
	}
	err := cl.Create(ctx, clusterExtension)
	require.NoError(t, err)

	t.Log("It sets resolution success status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.Error(t, err)

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("By checking the status fields")
	require.Equal(t, ocv1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"}, clusterExtension.Status.Install.Bundle)

	t.Log("By checking the expected installed conditions")
	installedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
	require.NotNil(t, installedCond)
	require.Equal(t, metav1.ConditionTrue, installedCond.Status)
	require.Equal(t, ocv1.ReasonSucceeded, installedCond.Reason)

	t.Log("By checking the expected progressing conditions")
	progressingCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
	require.NotNil(t, progressingCond)
	require.Equal(t, metav1.ConditionTrue, progressingCond.Status)
	require.Equal(t, ocv1.ReasonRetrying, progressingCond.Reason)

	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1.ClusterExtension{}))
}

func TestClusterExtensionManagedContentCacheWatchFail(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t, func(d *deps) {
		d.ImagePuller = &imageutil.MockPuller{
			ImageFS: fstest.MapFS{},
		}
		d.Resolver = resolve.Func(func(_ context.Context, _ *ocv1.ClusterExtension, _ *ocv1.BundleMetadata) (*declcfg.Bundle, *bundle.VersionRelease, *declcfg.Deprecation, error) {
			v := bundle.VersionRelease{
				Version: bsemver.MustParse("1.0.0"),
			}
			return &declcfg.Bundle{
				Name:    "prometheus.v1.0.0",
				Package: "prometheus",
				Image:   "quay.io/operatorhubio/prometheus@fake1.0.0",
			}, &v, nil, nil
		})
		d.Applier = &MockApplier{
			installCompleted: true,
			err:              errors.New("watch error"),
		}
	})

	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

	t.Log("When the cluster extension specifies a channel with version that exist")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgVer := "1.0.0"
	pkgChan := "beta"
	installNamespace := fmt.Sprintf("test-ns-%s", rand.String(8))
	serviceAccount := fmt.Sprintf("test-sa-%s", rand.String(8))

	clusterExtension := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1.ClusterExtensionSpec{
			Source: ocv1.SourceConfig{
				SourceType: ocv1.SourceTypeCatalog,

				Catalog: &ocv1.CatalogFilter{
					PackageName: pkgName,
					Version:     pkgVer,
					Channels:    []string{pkgChan},
				},
			},
			Namespace: installNamespace,
			ServiceAccount: ocv1.ServiceAccountReference{
				Name: serviceAccount,
			},
		},
	}
	err := cl.Create(ctx, clusterExtension)
	require.NoError(t, err)

	t.Log("It sets resolution success status")
	t.Log("By running reconcile")

	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.Error(t, err)

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("By checking the status fields")
	require.Equal(t, ocv1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"}, clusterExtension.Status.Install.Bundle)

	t.Log("By checking the expected installed conditions")
	installedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
	require.NotNil(t, installedCond)
	require.Equal(t, metav1.ConditionTrue, installedCond.Status)
	require.Equal(t, ocv1.ReasonSucceeded, installedCond.Reason)

	t.Log("By checking the expected progressing conditions")
	progressingCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
	require.NotNil(t, progressingCond)
	require.Equal(t, metav1.ConditionTrue, progressingCond.Status)
	require.Equal(t, ocv1.ReasonRetrying, progressingCond.Reason)

	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1.ClusterExtension{}))
}

func TestClusterExtensionInstallationSucceeds(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t, func(d *deps) {
		d.ImagePuller = &imageutil.MockPuller{
			ImageFS: fstest.MapFS{},
		}
		d.Resolver = resolve.Func(func(_ context.Context, _ *ocv1.ClusterExtension, _ *ocv1.BundleMetadata) (*declcfg.Bundle, *bundle.VersionRelease, *declcfg.Deprecation, error) {
			v := bundle.VersionRelease{
				Version: bsemver.MustParse("1.0.0"),
			}
			return &declcfg.Bundle{
				Name:    "prometheus.v1.0.0",
				Package: "prometheus",
				Image:   "quay.io/operatorhubio/prometheus@fake1.0.0",
			}, &v, nil, nil
		})
		d.Applier = &MockApplier{
			installCompleted: true,
		}
	})

	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

	t.Log("When the cluster extension specifies a channel with version that exist")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgVer := "1.0.0"
	pkgChan := "beta"
	namespace := fmt.Sprintf("test-ns-%s", rand.String(8))
	serviceAccount := fmt.Sprintf("test-sa-%s", rand.String(8))

	clusterExtension := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1.ClusterExtensionSpec{
			Source: ocv1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &ocv1.CatalogFilter{
					PackageName: pkgName,
					Version:     pkgVer,
					Channels:    []string{pkgChan},
				},
			},
			Namespace: namespace,
			ServiceAccount: ocv1.ServiceAccountReference{
				Name: serviceAccount,
			},
		},
	}
	err := cl.Create(ctx, clusterExtension)
	require.NoError(t, err)

	t.Log("It sets resolution success status")
	t.Log("By running reconcile")
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("By checking the status fields")
	require.Equal(t, ocv1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"}, clusterExtension.Status.Install.Bundle)

	t.Log("By checking the expected installed conditions")
	installedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
	require.NotNil(t, installedCond)
	require.Equal(t, metav1.ConditionTrue, installedCond.Status)
	require.Equal(t, ocv1.ReasonSucceeded, installedCond.Reason)

	t.Log("By checking the expected progressing conditions")
	progressingCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
	require.NotNil(t, progressingCond)
	require.Equal(t, metav1.ConditionTrue, progressingCond.Status)
	require.Equal(t, ocv1.ReasonSucceeded, progressingCond.Reason)

	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1.ClusterExtension{}))
}

func TestClusterExtensionDeleteFinalizerFails(t *testing.T) {
	fakeFinalizer := "fake.testfinalizer.io"
	finalizersMessage := "still have finalizers"
	var rfinalizers crfinalizer.Finalizers
	cl, reconciler := newClientAndReconciler(t, func(d *deps) {
		d.ImagePuller = &imageutil.MockPuller{
			ImageFS: fstest.MapFS{},
		}
		d.Resolver = resolve.Func(func(_ context.Context, _ *ocv1.ClusterExtension, _ *ocv1.BundleMetadata) (*declcfg.Bundle, *bundle.VersionRelease, *declcfg.Deprecation, error) {
			v := bundle.VersionRelease{
				Version: bsemver.MustParse("1.0.0"),
			}
			return &declcfg.Bundle{
				Name:    "prometheus.v1.0.0",
				Package: "prometheus",
				Image:   "quay.io/operatorhubio/prometheus@fake1.0.0",
			}, &v, nil, nil
		})
		d.Applier = &MockApplier{
			installCompleted: true,
		}
		d.RevisionStatesGetter = &MockRevisionStatesGetter{
			RevisionStates: &controllers.RevisionStates{
				Installed: &controllers.RevisionMetadata{
					BundleMetadata: ocv1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"},
					Image:          "quay.io/operatorhubio/prometheus@fake1.0.0",
				},
			},
		}
		rfinalizers = d.Finalizers
	})

	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

	t.Log("When the cluster extension specifies a channel with version that exist")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgVer := "1.0.0"
	pkgChan := "beta"
	namespace := fmt.Sprintf("test-ns-%s", rand.String(8))
	serviceAccount := fmt.Sprintf("test-sa-%s", rand.String(8))

	clusterExtension := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1.ClusterExtensionSpec{
			Source: ocv1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &ocv1.CatalogFilter{
					PackageName: pkgName,
					Version:     pkgVer,
					Channels:    []string{pkgChan},
				},
			},
			Namespace: namespace,
			ServiceAccount: ocv1.ServiceAccountReference{
				Name: serviceAccount,
			},
		},
	}
	err := cl.Create(ctx, clusterExtension)
	require.NoError(t, err)
	t.Log("It sets resolution success status")
	t.Log("By running reconcile")
	err = rfinalizers.Register(fakeFinalizer, finalizers.FinalizerFunc(func(ctx context.Context, obj client.Object) (crfinalizer.Result, error) {
		return crfinalizer.Result{}, errors.New(finalizersMessage)
	}))

	require.NoError(t, err)

	// Reconcile twice to simulate installing the ClusterExtension and loading in the finalizers
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)
	res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching updated cluster extension after first reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))
	cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
	expectedBundleMetadata := ocv1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"}
	require.Equal(t, expectedBundleMetadata, clusterExtension.Status.Install.Bundle)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)

	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1.ClusterExtension{}))
	res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Error(t, err, res)

	t.Log("By fetching updated cluster extension after second reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))
	cond = apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
	require.Equal(t, expectedBundleMetadata, clusterExtension.Status.Install.Bundle)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, fakeFinalizer, clusterExtension.Finalizers[0])
	cond = apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeProgressing)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Contains(t, cond.Message, finalizersMessage)
}

func verifyInvariants(ctx context.Context, t *testing.T, c client.Client, ext *ocv1.ClusterExtension) {
	key := client.ObjectKeyFromObject(ext)
	require.NoError(t, c.Get(ctx, key, ext))

	verifyConditionsInvariants(t, ext)
}

func verifyConditionsInvariants(t *testing.T, ext *ocv1.ClusterExtension) {
	// Expect that the cluster extension's set of conditions contains all defined
	// condition types for the ClusterExtension API. Every reconcile should always
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

func TestSetDeprecationStatus(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		clusterExtension         *ocv1.ClusterExtension
		expectedClusterExtension *ocv1.ClusterExtension
		bundle                   *declcfg.Bundle
		deprecation              *declcfg.Deprecation
	}{
		{
			name: "no deprecations, all deprecation statuses set to False",
			clusterExtension: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: ocv1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectedClusterExtension: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: ocv1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:               ocv1.TypeDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypePackageDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeChannelDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeBundleDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
					},
				},
			},
			bundle:      &declcfg.Bundle{},
			deprecation: nil,
		},
		{
			name: "deprecated channel, but no channel specified, all deprecation statuses set to False",
			clusterExtension: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: "Catalog",
						Catalog:    &ocv1.CatalogFilter{},
					},
				},
				Status: ocv1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectedClusterExtension: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: "Catalog",
						Catalog:    &ocv1.CatalogFilter{},
					},
				},
				Status: ocv1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:               ocv1.TypeDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypePackageDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeChannelDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeBundleDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
					},
				},
			},
			bundle: &declcfg.Bundle{},
			deprecation: &declcfg.Deprecation{
				Entries: []declcfg.DeprecationEntry{{
					Reference: declcfg.PackageScopedReference{
						Schema: declcfg.SchemaChannel,
						Name:   "badchannel",
					},
				}},
			},
		},
		{
			name: "deprecated channel, but a non-deprecated channel specified, all deprecation statuses set to False",
			clusterExtension: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1.CatalogFilter{
							Channels: []string{"nondeprecated"},
						},
					},
				},
				Status: ocv1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectedClusterExtension: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1.CatalogFilter{
							Channels: []string{"nondeprecated"},
						},
					},
				},
				Status: ocv1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:               ocv1.TypeDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypePackageDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeChannelDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeBundleDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
					},
				},
			},
			bundle: &declcfg.Bundle{},
			deprecation: &declcfg.Deprecation{
				Entries: []declcfg.DeprecationEntry{
					{
						Reference: declcfg.PackageScopedReference{
							Schema: declcfg.SchemaChannel,
							Name:   "badchannel",
						},
					},
				},
			},
		},
		{
			name: "deprecated channel specified, ChannelDeprecated and Deprecated status set to true, others set to false",
			clusterExtension: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1.CatalogFilter{
							Channels: []string{"badchannel"},
						},
					},
				},
				Status: ocv1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectedClusterExtension: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1.CatalogFilter{
							Channels: []string{"badchannel"},
						},
					},
				},
				Status: ocv1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:               ocv1.TypeDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypePackageDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeChannelDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeBundleDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
					},
				},
			},
			bundle: &declcfg.Bundle{},
			deprecation: &declcfg.Deprecation{
				Entries: []declcfg.DeprecationEntry{
					{
						Reference: declcfg.PackageScopedReference{
							Schema: declcfg.SchemaChannel,
							Name:   "badchannel",
						},
						Message: "bad channel!",
					},
				},
			},
		},
		{
			name: "deprecated package and channel specified, deprecated bundle, all deprecation statuses set to true",
			clusterExtension: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1.CatalogFilter{
							Channels: []string{"badchannel"},
						},
					},
				},
				Status: ocv1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectedClusterExtension: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1.CatalogFilter{
							Channels: []string{"badchannel"},
						},
					},
				},
				Status: ocv1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:               ocv1.TypeDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypePackageDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeChannelDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeBundleDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
					},
				},
			},
			bundle: &declcfg.Bundle{Name: "badbundle"},
			deprecation: &declcfg.Deprecation{
				Entries: []declcfg.DeprecationEntry{
					{
						Reference: declcfg.PackageScopedReference{
							Schema: declcfg.SchemaChannel,
							Name:   "badchannel",
						},
						Message: "bad channel!",
					},
					{
						Reference: declcfg.PackageScopedReference{
							Schema: declcfg.SchemaPackage,
						},
						Message: "bad package!",
					},
					{
						Reference: declcfg.PackageScopedReference{
							Schema: declcfg.SchemaBundle,
							Name:   "badbundle",
						},
						Message: "bad bundle!",
					},
				},
			},
		},
		{
			name: "deprecated channel specified, deprecated bundle, all deprecation statuses set to true, all deprecation statuses set to true except PackageDeprecated",
			clusterExtension: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1.CatalogFilter{
							Channels: []string{"badchannel"},
						},
					},
				},
				Status: ocv1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectedClusterExtension: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1.CatalogFilter{
							Channels: []string{"badchannel"},
						},
					},
				},
				Status: ocv1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:               ocv1.TypeDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypePackageDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeChannelDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeBundleDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
					},
				},
			},
			bundle: &declcfg.Bundle{Name: "badbundle"},
			deprecation: &declcfg.Deprecation{
				Entries: []declcfg.DeprecationEntry{
					{
						Reference: declcfg.PackageScopedReference{
							Schema: declcfg.SchemaChannel,
							Name:   "badchannel",
						},
						Message: "bad channel!",
					},
					{
						Reference: declcfg.PackageScopedReference{
							Schema: declcfg.SchemaBundle,
							Name:   "badbundle",
						},
						Message: "bad bundle!",
					},
				},
			},
		},
		{
			name: "deprecated package and channel specified, all deprecation statuses set to true except BundleDeprecated",
			clusterExtension: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1.CatalogFilter{
							Channels: []string{"badchannel"},
						},
					},
				},
				Status: ocv1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectedClusterExtension: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1.CatalogFilter{
							Channels: []string{"badchannel"},
						},
					},
				},
				Status: ocv1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:               ocv1.TypeDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypePackageDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeChannelDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeBundleDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
					},
				},
			},
			bundle: &declcfg.Bundle{},
			deprecation: &declcfg.Deprecation{
				Entries: []declcfg.DeprecationEntry{
					{
						Reference: declcfg.PackageScopedReference{
							Schema: declcfg.SchemaChannel,
							Name:   "badchannel",
						},
						Message: "bad channel!",
					},
					{
						Reference: declcfg.PackageScopedReference{
							Schema: declcfg.SchemaPackage,
						},
						Message: "bad package!",
					},
				},
			},
		},
		{
			name: "deprecated channels specified, ChannelDeprecated and Deprecated status set to true, others set to false",
			clusterExtension: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1.CatalogFilter{
							Channels: []string{"badchannel", "anotherbadchannel"},
						},
					},
				},
				Status: ocv1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectedClusterExtension: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1.CatalogFilter{
							Channels: []string{"badchannel", "anotherbadchannel"},
						},
					},
				},
				Status: ocv1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:               ocv1.TypeDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypePackageDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeChannelDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeBundleDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
					},
				},
			},
			bundle: &declcfg.Bundle{},
			deprecation: &declcfg.Deprecation{
				Entries: []declcfg.DeprecationEntry{
					{
						Reference: declcfg.PackageScopedReference{
							Schema: declcfg.SchemaChannel,
							Name:   "badchannel",
						},
						Message: "bad channel!",
					},
					{
						Reference: declcfg.PackageScopedReference{
							Schema: declcfg.SchemaChannel,
							Name:   "anotherbadchannel",
						},
						Message: "another bad channedl!",
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			controllers.SetDeprecationStatus(tc.clusterExtension, tc.bundle.Name, tc.deprecation)
			// TODO: we should test for unexpected changes to lastTransitionTime. We only expect
			//  lastTransitionTime to change when the status of the condition changes.
			assert.Empty(t, cmp.Diff(tc.expectedClusterExtension, tc.clusterExtension, cmpopts.IgnoreFields(metav1.Condition{}, "Message", "LastTransitionTime")))
		})
	}
}

type MockActionGetter struct {
	description       string
	rels              []*release.Release
	err               error
	expectedInstalled *controllers.RevisionMetadata
	expectedError     error
}

func (mag *MockActionGetter) ActionClientFor(ctx context.Context, obj client.Object) (helmclient.ActionInterface, error) {
	return mag, nil
}

func (mag *MockActionGetter) Get(name string, opts ...helmclient.GetOption) (*release.Release, error) {
	return nil, nil
}

// This is the function we are really testing
func (mag *MockActionGetter) History(name string, opts ...helmclient.HistoryOption) ([]*release.Release, error) {
	return mag.rels, mag.err
}

func (mag *MockActionGetter) Install(name, namespace string, chrt *chart.Chart, vals map[string]interface{}, opts ...helmclient.InstallOption) (*release.Release, error) {
	return nil, nil
}

func (mag *MockActionGetter) Upgrade(name, namespace string, chrt *chart.Chart, vals map[string]interface{}, opts ...helmclient.UpgradeOption) (*release.Release, error) {
	return nil, nil
}

func (mag *MockActionGetter) Uninstall(name string, opts ...helmclient.UninstallOption) (*release.UninstallReleaseResponse, error) {
	return nil, nil
}

func (mag *MockActionGetter) Reconcile(rel *release.Release) error {
	return nil
}

func TestGetInstalledBundleHistory(t *testing.T) {
	getter := controllers.HelmRevisionStatesGetter{}

	ext := ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ext",
		},
	}

	mag := []MockActionGetter{
		{
			"No return",
			nil, nil,
			nil, nil,
		},
		{
			"ErrReleaseNotFound (special case)",
			nil, driver.ErrReleaseNotFound,
			nil, nil,
		},
		{
			"Error from History",
			nil, fmt.Errorf("generic error"),
			nil, fmt.Errorf("generic error"),
		},
		{
			"One item in history",
			[]*release.Release{
				{
					Name: "test-ext",
					Info: &release.Info{
						Status: release.StatusDeployed,
					},
					Labels: map[string]string{
						labels.BundleNameKey:      "test-ext",
						labels.BundleVersionKey:   "1.0",
						labels.BundleReferenceKey: "bundle-ref",
					},
				},
			},
			nil,
			&controllers.RevisionMetadata{
				BundleMetadata: ocv1.BundleMetadata{
					Name:    "test-ext",
					Version: "1.0",
				},
				Image: "bundle-ref",
			}, nil,
		},
		{
			"Two items in history",
			[]*release.Release{
				{
					Name: "test-ext",
					Info: &release.Info{
						Status: release.StatusFailed,
					},
					Labels: map[string]string{
						labels.BundleNameKey:      "test-ext",
						labels.BundleVersionKey:   "2.0",
						labels.BundleReferenceKey: "bundle-ref-2",
					},
				},
				{
					Name: "test-ext",
					Info: &release.Info{
						Status: release.StatusDeployed,
					},
					Labels: map[string]string{
						labels.BundleNameKey:      "test-ext",
						labels.BundleVersionKey:   "1.0",
						labels.BundleReferenceKey: "bundle-ref-1",
					},
				},
			},
			nil,
			&controllers.RevisionMetadata{
				BundleMetadata: ocv1.BundleMetadata{
					Name:    "test-ext",
					Version: "1.0",
				},
				Image: "bundle-ref-1",
			}, nil,
		},
	}

	for _, tst := range mag {
		t.Log(tst.description)
		getter.ActionClientGetter = &tst
		md, err := getter.GetRevisionStates(context.Background(), &ext)
		if tst.expectedError != nil {
			require.Equal(t, tst.expectedError, err)
			require.Nil(t, md)
		} else {
			require.NoError(t, err)
			require.Equal(t, tst.expectedInstalled, md.Installed)
			require.Nil(t, md.RollingOut)
		}
	}
}

// TestResolutionFallbackToInstalledBundle tests the catalog deletion resilience fallback logic
func TestResolutionFallbackToInstalledBundle(t *testing.T) {
	t.Run("falls back when catalog unavailable and no version change", func(t *testing.T) {
		resolveAttempt := 0
		cl, reconciler := newClientAndReconciler(t, func(d *deps) {
			// First reconcile: catalog available, second reconcile: catalog unavailable
			d.Resolver = resolve.Func(func(_ context.Context, _ *ocv1.ClusterExtension, _ *ocv1.BundleMetadata) (*declcfg.Bundle, *bundle.VersionRelease, *declcfg.Deprecation, error) {
				resolveAttempt++
				if resolveAttempt == 1 {
					// First reconcile: catalog available, resolve to version 1.0.0
					v := bundle.VersionRelease{Version: bsemver.MustParse("1.0.0")}
					return &declcfg.Bundle{
						Name:    "test.1.0.0",
						Package: "test-pkg",
						Image:   "test-image:1.0.0",
					}, &v, &declcfg.Deprecation{}, nil
				}
				// Second reconcile: catalog unavailable
				return nil, nil, nil, fmt.Errorf("catalog unavailable")
			})
			// Applier succeeds (resources maintained)
			d.Applier = &MockApplier{
				installCompleted: true,
				installStatus:    "",
				err:              nil,
			}
			d.ImagePuller = &imageutil.MockPuller{ImageFS: fstest.MapFS{}}
			d.RevisionStatesGetter = &MockRevisionStatesGetter{
				RevisionStates: &controllers.RevisionStates{
					Installed: &controllers.RevisionMetadata{
						Package:        "test-pkg",
						BundleMetadata: ocv1.BundleMetadata{Name: "test.1.0.0", Version: "1.0.0"},
						Image:          "test-image:1.0.0",
					},
				},
			}
		})

		ctx := context.Background()
		extKey := types.NamespacedName{Name: fmt.Sprintf("test-%s", rand.String(8))}

		// Create ClusterExtension with no version specified
		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
			Spec: ocv1.ClusterExtensionSpec{
				Source: ocv1.SourceConfig{
					SourceType: "Catalog",
					Catalog: &ocv1.CatalogFilter{
						PackageName: "test-pkg",
						// No version - should fall back
					},
				},
				Namespace:      "default",
				ServiceAccount: ocv1.ServiceAccountReference{Name: "default"},
			},
		}
		require.NoError(t, cl.Create(ctx, ext))

		// First reconcile: catalog available, install version 1.0.0
		res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
		require.NoError(t, err)
		require.Equal(t, ctrl.Result{}, res)

		require.NoError(t, cl.Get(ctx, extKey, ext))
		require.Equal(t, "1.0.0", ext.Status.Install.Bundle.Version)

		// Verify status after first install
		instCond := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeInstalled)
		require.NotNil(t, instCond)
		require.Equal(t, metav1.ConditionTrue, instCond.Status)
		require.Equal(t, ocv1.ReasonSucceeded, instCond.Reason)

		progCond := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(t, progCond)
		require.Equal(t, metav1.ConditionTrue, progCond.Status)
		require.Equal(t, ocv1.ReasonSucceeded, progCond.Reason)

		// Verify all conditions are present and valid after first reconcile
		verifyInvariants(ctx, t, cl, ext)

		// Second reconcile: catalog unavailable, should fallback to installed version
		// Catalog watch will trigger reconciliation when catalog becomes available again
		res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
		require.NoError(t, err)
		require.Equal(t, ctrl.Result{}, res)

		// Verify status shows successful reconciliation after fallback
		require.NoError(t, cl.Get(ctx, extKey, ext))

		// Version should remain 1.0.0 (maintained from fallback)
		require.Equal(t, "1.0.0", ext.Status.Install.Bundle.Version)

		// Progressing should be Succeeded (apply completed successfully)
		progCond = apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(t, progCond)
		require.Equal(t, metav1.ConditionTrue, progCond.Status)
		require.Equal(t, ocv1.ReasonSucceeded, progCond.Reason)

		// Installed should be True (maintaining current version)
		instCond = apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeInstalled)
		require.NotNil(t, instCond)
		require.Equal(t, metav1.ConditionTrue, instCond.Status)
		require.Equal(t, ocv1.ReasonSucceeded, instCond.Reason)

		// Verify all conditions remain valid after fallback
		verifyInvariants(ctx, t, cl, ext)
	})

	t.Run("fails when version upgrade requested without catalog", func(t *testing.T) {
		cl, reconciler := newClientAndReconciler(t, func(d *deps) {
			d.Resolver = resolve.Func(func(_ context.Context, _ *ocv1.ClusterExtension, _ *ocv1.BundleMetadata) (*declcfg.Bundle, *bundle.VersionRelease, *declcfg.Deprecation, error) {
				return nil, nil, nil, fmt.Errorf("catalog unavailable")
			})
			d.RevisionStatesGetter = &MockRevisionStatesGetter{
				RevisionStates: &controllers.RevisionStates{
					Installed: &controllers.RevisionMetadata{
						BundleMetadata: ocv1.BundleMetadata{Name: "test.1.0.0", Version: "1.0.0"},
					},
				},
			}
		})

		ctx := context.Background()
		extKey := types.NamespacedName{Name: fmt.Sprintf("test-%s", rand.String(8))}

		// Create ClusterExtension requesting version upgrade
		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
			Spec: ocv1.ClusterExtensionSpec{
				Source: ocv1.SourceConfig{
					SourceType: "Catalog",
					Catalog: &ocv1.CatalogFilter{
						PackageName: "test-pkg",
						Version:     "1.0.1", // Requesting upgrade
					},
				},
				Namespace:      "default",
				ServiceAccount: ocv1.ServiceAccountReference{Name: "default"},
			},
		}
		require.NoError(t, cl.Create(ctx, ext))

		// Reconcile should fail (can't upgrade without catalog)
		res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
		require.Error(t, err)
		require.Equal(t, ctrl.Result{}, res)

		// Verify status shows Retrying
		require.NoError(t, cl.Get(ctx, extKey, ext))

		// Progressing should be Retrying (can't resolve without catalog)
		progCond := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(t, progCond)
		require.Equal(t, metav1.ConditionTrue, progCond.Status)
		require.Equal(t, ocv1.ReasonRetrying, progCond.Reason)

		// Installed should be True (v1.0.0 is already installed per RevisionStatesGetter)
		// but we can't upgrade to v1.0.1 without catalog
		instCond := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeInstalled)
		require.NotNil(t, instCond)
		require.Equal(t, metav1.ConditionTrue, instCond.Status)

		// Verify all conditions are present and valid
		verifyInvariants(ctx, t, cl, ext)
	})

	t.Run("auto-updates when catalog becomes available after fallback", func(t *testing.T) {
		resolveAttempt := 0
		cl, reconciler := newClientAndReconciler(t, func(d *deps) {
			// First attempt: catalog unavailable, then becomes available
			d.Resolver = resolve.Func(func(_ context.Context, _ *ocv1.ClusterExtension, _ *ocv1.BundleMetadata) (*declcfg.Bundle, *bundle.VersionRelease, *declcfg.Deprecation, error) {
				resolveAttempt++
				if resolveAttempt == 1 {
					// First reconcile: catalog unavailable
					return nil, nil, nil, fmt.Errorf("catalog temporarily unavailable")
				}
				// Second reconcile (triggered by catalog watch): catalog available with new version
				v := bundle.VersionRelease{Version: bsemver.MustParse("2.0.0")}
				return &declcfg.Bundle{
					Name:    "test.2.0.0",
					Package: "test-pkg",
					Image:   "test-image:2.0.0",
				}, &v, &declcfg.Deprecation{}, nil
			})
			d.RevisionStatesGetter = &MockRevisionStatesGetter{
				RevisionStates: &controllers.RevisionStates{
					Installed: &controllers.RevisionMetadata{
						Package:        "test-pkg",
						BundleMetadata: ocv1.BundleMetadata{Name: "test.1.0.0", Version: "1.0.0"},
						Image:          "test-image:1.0.0",
					},
				},
			}
			d.ImagePuller = &imageutil.MockPuller{ImageFS: fstest.MapFS{}}
			d.Applier = &MockApplier{installCompleted: true}
		})

		ctx := context.Background()
		extKey := types.NamespacedName{Name: fmt.Sprintf("test-%s", rand.String(8))}

		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
			Spec: ocv1.ClusterExtensionSpec{
				Source: ocv1.SourceConfig{
					SourceType: "Catalog",
					Catalog: &ocv1.CatalogFilter{
						PackageName: "test-pkg",
						// No version - auto-update to latest
					},
				},
				Namespace:      "default",
				ServiceAccount: ocv1.ServiceAccountReference{Name: "default"},
			},
		}
		require.NoError(t, cl.Create(ctx, ext))

		// First reconcile: catalog unavailable, falls back to v1.0.0
		res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
		require.NoError(t, err)
		require.Equal(t, ctrl.Result{}, res)

		require.NoError(t, cl.Get(ctx, extKey, ext))
		require.Equal(t, "1.0.0", ext.Status.Install.Bundle.Version)

		// Verify core status after fallback to installed version
		instCond := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeInstalled)
		require.NotNil(t, instCond)
		require.Equal(t, metav1.ConditionTrue, instCond.Status)
		require.Equal(t, ocv1.ReasonSucceeded, instCond.Reason)

		progCond := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(t, progCond)
		require.Equal(t, metav1.ConditionTrue, progCond.Status)
		require.Equal(t, ocv1.ReasonSucceeded, progCond.Reason)

		// Note: When falling back without catalog access initially, deprecation conditions
		// may not be set yet. Full validation happens after catalog is available.

		// Second reconcile: simulating catalog watch trigger, catalog now available with v2.0.0
		res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
		require.NoError(t, err)
		require.Equal(t, ctrl.Result{}, res)

		// Should have upgraded to v2.0.0
		require.NoError(t, cl.Get(ctx, extKey, ext))
		require.Equal(t, "2.0.0", ext.Status.Install.Bundle.Version)

		// Verify status after upgrade
		instCond = apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeInstalled)
		require.NotNil(t, instCond)
		require.Equal(t, metav1.ConditionTrue, instCond.Status)
		require.Equal(t, ocv1.ReasonSucceeded, instCond.Reason)

		progCond = apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(t, progCond)
		require.Equal(t, metav1.ConditionTrue, progCond.Status)
		require.Equal(t, ocv1.ReasonSucceeded, progCond.Reason)

		// Verify all conditions remain valid after upgrade
		verifyInvariants(ctx, t, cl, ext)

		// Verify resolution was attempted twice (fallback, then success)
		require.Equal(t, 2, resolveAttempt)
	})

	t.Run("retries when catalogs exist but resolution fails", func(t *testing.T) {
		cl, reconciler := newClientAndReconciler(t, func(d *deps) {
			// Resolver fails (transient issue)
			d.Resolver = resolve.Func(func(_ context.Context, _ *ocv1.ClusterExtension, _ *ocv1.BundleMetadata) (*declcfg.Bundle, *bundle.VersionRelease, *declcfg.Deprecation, error) {
				return nil, nil, nil, fmt.Errorf("transient catalog issue: cache stale")
			})
			d.RevisionStatesGetter = &MockRevisionStatesGetter{
				RevisionStates: &controllers.RevisionStates{
					Installed: &controllers.RevisionMetadata{
						Package:        "test-pkg",
						BundleMetadata: ocv1.BundleMetadata{Name: "test.1.0.0", Version: "1.0.0"},
						Image:          "test-image:1.0.0",
					},
				},
			}
		})

		ctx := context.Background()
		extKey := types.NamespacedName{Name: fmt.Sprintf("test-%s", rand.String(8))}

		// Create a ClusterCatalog matching the extension's selector
		catalog := &ocv1.ClusterCatalog{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-catalog",
			},
			Spec: ocv1.ClusterCatalogSpec{
				Source: ocv1.CatalogSource{
					Type: ocv1.SourceTypeImage,
					Image: &ocv1.ImageSource{
						Ref: "test-registry/catalog:latest",
					},
				},
			},
		}
		require.NoError(t, cl.Create(ctx, catalog))

		// Create ClusterExtension with no version specified
		ext := &ocv1.ClusterExtension{
			ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
			Spec: ocv1.ClusterExtensionSpec{
				Source: ocv1.SourceConfig{
					SourceType: "Catalog",
					Catalog: &ocv1.CatalogFilter{
						PackageName: "test-pkg",
						// No version specified
					},
				},
				Namespace:      "default",
				ServiceAccount: ocv1.ServiceAccountReference{Name: "default"},
			},
		}
		require.NoError(t, cl.Create(ctx, ext))

		// Reconcile should fail and RETRY (not fallback)
		// Catalogs exist, so this is likely a transient issue (catalog updating, cache stale, etc.)
		res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
		require.Error(t, err)
		require.Equal(t, ctrl.Result{}, res)

		// Verify status shows Retrying (not falling back to installed bundle)
		require.NoError(t, cl.Get(ctx, extKey, ext))

		progCond := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(t, progCond)
		require.Equal(t, metav1.ConditionTrue, progCond.Status)
		require.Equal(t, ocv1.ReasonRetrying, progCond.Reason)
		require.Contains(t, progCond.Message, "transient catalog issue")

		// Installed should remain True (existing installation is maintained)
		instCond := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeInstalled)
		require.NotNil(t, instCond)
		require.Equal(t, metav1.ConditionTrue, instCond.Status)

		// Verify we did NOT fall back - status should show we're retrying
		verifyInvariants(ctx, t, cl, ext)

		// Clean up the catalog so it doesn't affect other tests
		require.NoError(t, cl.Delete(ctx, catalog))
	})
}

func TestCheckCatalogsExist(t *testing.T) {
	t.Run("returns false when no catalogs exist", func(t *testing.T) {
		cl := newClient(t)
		ctx := context.Background()

		ext := &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Source: ocv1.SourceConfig{
					SourceType: "Catalog",
					Catalog: &ocv1.CatalogFilter{
						PackageName: "test-pkg",
					},
				},
			},
		}

		exists, err := controllers.CheckCatalogsExist(ctx, cl, ext)
		require.NoError(t, err)
		require.False(t, exists, "should return false when no catalogs exist")
	})

	t.Run("returns false when no selector provided", func(t *testing.T) {
		cl := newClient(t)
		ctx := context.Background()

		ext := &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Source: ocv1.SourceConfig{
					SourceType: "Catalog",
					Catalog: &ocv1.CatalogFilter{
						PackageName: "test-pkg",
						Selector:    nil, // No selector
					},
				},
			},
		}

		exists, err := controllers.CheckCatalogsExist(ctx, cl, ext)
		require.NoError(t, err)
		require.False(t, exists, "should return false when no catalogs exist (no selector)")
	})

	t.Run("returns false when empty selector provided", func(t *testing.T) {
		cl := newClient(t)
		ctx := context.Background()

		ext := &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Source: ocv1.SourceConfig{
					SourceType: "Catalog",
					Catalog: &ocv1.CatalogFilter{
						PackageName: "test-pkg",
						Selector:    &metav1.LabelSelector{}, // Empty selector (matches everything)
					},
				},
			},
		}

		exists, err := controllers.CheckCatalogsExist(ctx, cl, ext)
		require.NoError(t, err, "empty selector should not cause error")
		require.False(t, exists, "should return false when no catalogs exist (empty selector)")
	})

	t.Run("returns error for invalid selector", func(t *testing.T) {
		cl := newClient(t)
		ctx := context.Background()

		ext := &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Source: ocv1.SourceConfig{
					SourceType: "Catalog",
					Catalog: &ocv1.CatalogFilter{
						PackageName: "test-pkg",
						Selector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "invalid",
									Operator: "InvalidOperator", // Invalid operator
									Values:   []string{"value"},
								},
							},
						},
					},
				},
			},
		}

		exists, err := controllers.CheckCatalogsExist(ctx, cl, ext)
		require.Error(t, err, "should return error for invalid selector")
		require.Contains(t, err.Error(), "invalid catalog selector")
		require.False(t, exists)
	})
}
