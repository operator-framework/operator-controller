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
	"helm.sh/helm/v3/pkg/release"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crfinalizer "sigs.k8s.io/controller-runtime/pkg/finalizer"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/conditionsets"
	"github.com/operator-framework/operator-controller/internal/operator-controller/controllers"
	finalizers "github.com/operator-framework/operator-controller/internal/operator-controller/finalizers"
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
		d.Resolver = resolve.Func(func(_ context.Context, _ *ocv1.ClusterExtension, _ *ocv1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
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
					d.Resolver = resolve.Func(func(_ context.Context, _ *ocv1.ClusterExtension, _ *ocv1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
						v := bsemver.MustParse("1.0.0")
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
			d.Resolver = resolve.Func(func(_ context.Context, _ *ocv1.ClusterExtension, _ *ocv1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
				v := bsemver.MustParse("1.0.0")
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

func TestClusterExtensionApplierFailsWithBundleInstalled(t *testing.T) {
	mockApplier := &MockApplier{
		installCompleted: true,
	}
	cl, reconciler := newClientAndReconciler(t, func(d *deps) {
		d.ImagePuller = &imageutil.MockPuller{
			ImageFS: fstest.MapFS{},
		}
		d.Resolver = resolve.Func(func(_ context.Context, _ *ocv1.ClusterExtension, _ *ocv1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
			v := bsemver.MustParse("1.0.0")
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
		d.Resolver = resolve.Func(func(_ context.Context, _ *ocv1.ClusterExtension, _ *ocv1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
			v := bsemver.MustParse("1.0.0")
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
		d.Resolver = resolve.Func(func(_ context.Context, _ *ocv1.ClusterExtension, _ *ocv1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
			v := bsemver.MustParse("1.0.0")
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
		d.Resolver = resolve.Func(func(_ context.Context, _ *ocv1.ClusterExtension, _ *ocv1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
			v := bsemver.MustParse("1.0.0")
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
		d.Resolver = resolve.Func(func(_ context.Context, _ *ocv1.ClusterExtension, _ *ocv1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
			v := bsemver.MustParse("1.0.0")
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
	// The catalogDataProvided/hasCatalogData pair lets each test express whether the catalog
	// answered during reconciliation and, if it did, whether it marked anything as deprecated.
	// This helps us cover three distinct user-facing states: "no catalog response" (everything
	// stays Unknown), "catalog answered with no deprecations" (conditions explicitly set to
	// False with reason NotDeprecated, with BundleDeprecated remaining Unknown when no bundle
	// is installed), and "catalog answered with explicit deprecations" (conditions go True).
	//
	// Key scenarios tested:
	//   1. No catalog data + no bundle -> all Unknown, BundleDeprecated uses reason Absent
	//   2. No catalog data + bundle installed -> all Unknown, BundleDeprecated uses reason DeprecationStatusUnknown
	//   3. Catalog data provided + no deprecations -> deprecation conditions explicitly set to False
	//      with reason NotDeprecated (BundleDeprecated remains Unknown when no bundle is installed)
	//   4. Catalog data provided + explicit deprecations -> relevant conditions True
	for _, tc := range []struct {
		name                     string
		clusterExtension         *ocv1.ClusterExtension
		expectedClusterExtension *ocv1.ClusterExtension
		bundle                   *declcfg.Bundle
		deprecation              *declcfg.Deprecation
		catalogDataProvided      bool
		hasCatalogData           bool
	}{
		{
			name: "no catalog data, all deprecation statuses set to Unknown",
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
							Reason:             ocv1.ReasonDeprecationStatusUnknown,
							Status:             metav1.ConditionUnknown,
							Message:            "deprecation status unknown: catalog data unavailable",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypePackageDeprecated,
							Reason:             ocv1.ReasonDeprecationStatusUnknown,
							Status:             metav1.ConditionUnknown,
							Message:            "deprecation status unknown: catalog data unavailable",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeChannelDeprecated,
							Reason:             ocv1.ReasonDeprecationStatusUnknown,
							Status:             metav1.ConditionUnknown,
							Message:            "deprecation status unknown: catalog data unavailable",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeBundleDeprecated,
							Reason:             ocv1.ReasonAbsent,
							Status:             metav1.ConditionUnknown,
							Message:            "no bundle installed yet",
							ObservedGeneration: 1,
						},
					},
				},
			},
			bundle:              &declcfg.Bundle{},
			deprecation:         nil,
			catalogDataProvided: false,
			hasCatalogData:      false,
		},
		{
			// Scenario:
			//   - A bundle is installed (v1.0.0)
			//   - Catalog becomes unavailable (removed or network failure)
			//   - No catalog data can be retrieved
			//   - BundleDeprecated must show Unknown/DeprecationStatusUnknown (not Absent)
			//   - Reason is DeprecationStatusUnknown because catalog data is unavailable; Absent is only for no bundle
			name: "no catalog data with installed bundle keeps bundle condition Unknown",
			clusterExtension: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: ocv1.ClusterExtensionStatus{Conditions: []metav1.Condition{}},
			},
			expectedClusterExtension: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: ocv1.ClusterExtensionStatus{Conditions: []metav1.Condition{
					{Type: ocv1.TypeDeprecated, Reason: ocv1.ReasonDeprecationStatusUnknown, Status: metav1.ConditionUnknown, Message: "deprecation status unknown: catalog data unavailable", ObservedGeneration: 1},
					{Type: ocv1.TypePackageDeprecated, Reason: ocv1.ReasonDeprecationStatusUnknown, Status: metav1.ConditionUnknown, Message: "deprecation status unknown: catalog data unavailable", ObservedGeneration: 1},
					{Type: ocv1.TypeChannelDeprecated, Reason: ocv1.ReasonDeprecationStatusUnknown, Status: metav1.ConditionUnknown, Message: "deprecation status unknown: catalog data unavailable", ObservedGeneration: 1},
					{Type: ocv1.TypeBundleDeprecated, Reason: ocv1.ReasonDeprecationStatusUnknown, Status: metav1.ConditionUnknown, Message: "deprecation status unknown: catalog data unavailable", ObservedGeneration: 1},
				}},
			},
			bundle:              &declcfg.Bundle{Name: "installed.v1.0.0"},
			deprecation:         nil,
			catalogDataProvided: false,
			hasCatalogData:      false,
		},
		{
			// Scenario:
			//   - A bundle is installed
			//   - Catalog returns deprecation entries but catalogDataProvided=false
			//   - This tests that deprecation data is ignored when hasCatalogData is false
			//   - All conditions go to Unknown regardless of deprecation entries present
			//   - BundleDeprecated uses DeprecationStatusUnknown (not Absent) because bundle exists
			name: "deprecation entries ignored when catalog data flag is false",
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
							Reason:             ocv1.ReasonDeprecationStatusUnknown,
							Status:             metav1.ConditionUnknown,
							Message:            "deprecation status unknown: catalog data unavailable",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypePackageDeprecated,
							Reason:             ocv1.ReasonDeprecationStatusUnknown,
							Status:             metav1.ConditionUnknown,
							Message:            "deprecation status unknown: catalog data unavailable",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeChannelDeprecated,
							Reason:             ocv1.ReasonDeprecationStatusUnknown,
							Status:             metav1.ConditionUnknown,
							Message:            "deprecation status unknown: catalog data unavailable",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeBundleDeprecated,
							Reason:             ocv1.ReasonDeprecationStatusUnknown,
							Status:             metav1.ConditionUnknown,
							Message:            "deprecation status unknown: catalog data unavailable",
							ObservedGeneration: 1,
						},
					},
				},
			},
			bundle: &declcfg.Bundle{Name: "ignored"},
			deprecation: &declcfg.Deprecation{Entries: []declcfg.DeprecationEntry{{
				Reference: declcfg.PackageScopedReference{Schema: declcfg.SchemaPackage},
				Message:   "should not surface",
			}}},
			catalogDataProvided: true,
			hasCatalogData:      false,
		},
		{
			name: "catalog consulted but no deprecations, conditions False except BundleDeprecated Unknown when no bundle",
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
							Reason:             ocv1.ReasonNotDeprecated,
							Status:             metav1.ConditionFalse,
							Message:            "not deprecated",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypePackageDeprecated,
							Reason:             ocv1.ReasonNotDeprecated,
							Status:             metav1.ConditionFalse,
							Message:            "package not deprecated",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeChannelDeprecated,
							Reason:             ocv1.ReasonNotDeprecated,
							Status:             metav1.ConditionFalse,
							Message:            "channel not deprecated",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeBundleDeprecated,
							Reason:             ocv1.ReasonAbsent,
							Status:             metav1.ConditionUnknown,
							Message:            "no bundle installed yet",
							ObservedGeneration: 1,
						},
					},
				},
			},
			bundle:              &declcfg.Bundle{},
			deprecation:         nil,
			catalogDataProvided: true,
			hasCatalogData:      true,
		},
		{
			name: "deprecated channel exists, no channels specified (auto-select), channel deprecation shown",
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
							Status:             metav1.ConditionTrue,
							Message:            "bad channel!",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypePackageDeprecated,
							Reason:             ocv1.ReasonNotDeprecated,
							Status:             metav1.ConditionFalse,
							Message:            "package not deprecated",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeChannelDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							Message:            "bad channel!",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeBundleDeprecated,
							Reason:             ocv1.ReasonAbsent,
							Status:             metav1.ConditionUnknown,
							Message:            "no bundle installed yet",
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
					Message: "bad channel!",
				}},
			},
			catalogDataProvided: true,
			hasCatalogData:      true,
		},
		{
			name: "deprecated channel exists but non-deprecated channel specified; conditions False except BundleDeprecated Unknown",
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
							Reason:             ocv1.ReasonNotDeprecated,
							Status:             metav1.ConditionFalse,
							Message:            "not deprecated",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypePackageDeprecated,
							Reason:             ocv1.ReasonNotDeprecated,
							Status:             metav1.ConditionFalse,
							Message:            "package not deprecated",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeChannelDeprecated,
							Reason:             ocv1.ReasonNotDeprecated,
							Status:             metav1.ConditionFalse,
							Message:            "channel not deprecated",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeBundleDeprecated,
							Reason:             ocv1.ReasonAbsent,
							Status:             metav1.ConditionUnknown,
							Message:            "no bundle installed yet",
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
			catalogDataProvided: true,
			hasCatalogData:      true,
		},
		{
			name: "deprecated channel specified, ChannelDeprecated and Deprecated set to true, PackageDeprecated False, BundleDeprecated Unknown",
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
							Message:            "bad channel!",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypePackageDeprecated,
							Reason:             ocv1.ReasonNotDeprecated,
							Status:             metav1.ConditionFalse,
							Message:            "package not deprecated",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeChannelDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							Message:            "bad channel!",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeBundleDeprecated,
							Reason:             ocv1.ReasonAbsent,
							Status:             metav1.ConditionUnknown,
							Message:            "no bundle installed yet",
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
			catalogDataProvided: true,
			hasCatalogData:      true,
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
							Message:            "bad package!\nbad channel!\nbad bundle!",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypePackageDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							Message:            "bad package!",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeChannelDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							Message:            "bad channel!",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeBundleDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							Message:            "bad bundle!",
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
			catalogDataProvided: true,
			hasCatalogData:      true,
		},
		{
			name: "deprecated channel and bundle specified, Deprecated/ChannelDeprecated/BundleDeprecated set to true, PackageDeprecated False",
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
							Message:            "bad channel!\nbad bundle!",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypePackageDeprecated,
							Reason:             ocv1.ReasonNotDeprecated,
							Status:             metav1.ConditionFalse,
							Message:            "package not deprecated",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeChannelDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							Message:            "bad channel!",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeBundleDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							Message:            "bad bundle!",
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
			catalogDataProvided: true,
			hasCatalogData:      true,
		},
		{
			name: "deprecated package and channel specified, Deprecated/PackageDeprecated/ChannelDeprecated set to true, BundleDeprecated Unknown/Absent (no bundle installed)",
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
							Message:            "bad package!\nbad channel!",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypePackageDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							Message:            "bad package!",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeChannelDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							Message:            "bad channel!",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeBundleDeprecated,
							Reason:             ocv1.ReasonAbsent,
							Status:             metav1.ConditionUnknown,
							Message:            "no bundle installed yet",
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
			catalogDataProvided: true,
			hasCatalogData:      true,
		},
		{
			name: "deprecated channels specified, ChannelDeprecated and Deprecated set to true, PackageDeprecated False, BundleDeprecated Unknown/Absent",
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
							Message:            "bad channel!\nanother bad channel!",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypePackageDeprecated,
							Reason:             ocv1.ReasonNotDeprecated,
							Status:             metav1.ConditionFalse,
							Message:            "package not deprecated",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeChannelDeprecated,
							Reason:             ocv1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							Message:            "bad channel!\nanother bad channel!",
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1.TypeBundleDeprecated,
							Reason:             ocv1.ReasonAbsent,
							Status:             metav1.ConditionUnknown,
							Message:            "no bundle installed yet",
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
						Message: "another bad channel!",
					},
				},
			},
			catalogDataProvided: true,
			hasCatalogData:      true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// When a test provides deprecation data it must also explicitly state that the catalog responded.
			// This guard keeps future cases from silently falling back to the "catalog absent" branch.
			if tc.deprecation != nil && !tc.catalogDataProvided {
				require.Failf(t, "test case must set catalogDataProvided when deprecation is supplied", "test case %q", tc.name)

type MockActionGetter struct {
	description       string
	rels              []*release.Release
	err               error
	expectedInstalled *controllers.RevisionMetadata
	expectedError     error
}

			}
			hasCatalogData := tc.catalogDataProvided && tc.hasCatalogData
			controllers.SetDeprecationStatus(tc.clusterExtension, tc.bundle.Name, tc.deprecation, hasCatalogData)
			// TODO: we should test for unexpected changes to lastTransitionTime. We only expect
			//  lastTransitionTime to change when the status of the condition changes.
			assert.Empty(t, cmp.Diff(tc.expectedClusterExtension, tc.clusterExtension, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")))
		})
	}
}

// TestSetDeprecationStatus_NoInfiniteReconcileLoop verifies that calling SetDeprecationStatus
// multiple times with the same inputs does not cause infinite reconciliation loops.
//
// The issue: If we always remove and re-add conditions, lastTransitionTime updates every time,
// which causes DeepEqual to fail, triggering another reconcile indefinitely.
//
// The fix: Only remove conditions when we're NOT re-adding them. When setting a condition,
// call SetStatusCondition directly - it preserves lastTransitionTime when status/reason/message
// haven't changed.
func TestSetDeprecationStatus_NoInfiniteReconcileLoop(t *testing.T) {
	tests := []struct {
		name                  string
		installedBundleName   string
		deprecation           *declcfg.Deprecation
		hasCatalogData        bool
		setupConditions       func(*ocv1.ClusterExtension)
		expectConditionsCount int
		description           string
	}{
		{
			name:                "deprecated package - should stabilize after first reconcile",
			installedBundleName: "test.v1.0.0",
			deprecation: &declcfg.Deprecation{
				Entries: []declcfg.DeprecationEntry{
					{
						Reference: declcfg.PackageScopedReference{
							Schema: declcfg.SchemaPackage,
						},
						Message: "package is deprecated",
					},
				},
			},
			hasCatalogData: true,
			setupConditions: func(ext *ocv1.ClusterExtension) {
				// No conditions initially
			},
			expectConditionsCount: 4, // All 4 conditions: Deprecated/PackageDeprecated=True, ChannelDeprecated/BundleDeprecated=False
			description:           "First call adds conditions, second call preserves lastTransitionTime",
		},
		{
			name:                "not deprecated - conditions always present as False",
			installedBundleName: "", // No bundle installed
			deprecation:         nil,
			hasCatalogData:      true,
			setupConditions: func(ext *ocv1.ClusterExtension) {
				// Simulate old behavior: False conditions present with old reason
				apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
					Type:               ocv1.TypeDeprecated,
					Status:             metav1.ConditionFalse,
					Reason:             ocv1.ReasonDeprecated,
					Message:            "",
					ObservedGeneration: 1,
				})
				apimeta.SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
					Type:               ocv1.TypePackageDeprecated,
					Status:             metav1.ConditionFalse,
					Reason:             ocv1.ReasonDeprecated,
					Message:            "",
					ObservedGeneration: 1,
				})
			},
			expectConditionsCount: 4, // All 4 conditions as False (except BundleDeprecated Unknown when no bundle)
			description:           "Sets all conditions to False with NotDeprecated reason, then stabilizes",
		},
		{
			name:                "catalog unavailable - should stabilize with Unknown conditions",
			installedBundleName: "test.v1.0.0",
			deprecation:         nil,
			hasCatalogData:      false,
			setupConditions: func(ext *ocv1.ClusterExtension) {
				// No conditions initially
			},
			expectConditionsCount: 4, // All four Unknown conditions
			description:           "Sets Unknown conditions, then preserves them",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ext := &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: ocv1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{},
				},
			}

			// Setup initial conditions if specified
			if tt.setupConditions != nil {
				tt.setupConditions(ext)
			}

			// First reconcile: should add/update conditions
			controllers.SetDeprecationStatus(ext, tt.installedBundleName, tt.deprecation, tt.hasCatalogData)

			firstReconcileConditions := make([]metav1.Condition, len(ext.Status.Conditions))
			copy(firstReconcileConditions, ext.Status.Conditions)

			// Verify expected number of conditions
			deprecationConditions := filterDeprecationConditions(ext.Status.Conditions)
			require.Len(t, deprecationConditions, tt.expectConditionsCount,
				"First reconcile should have %d deprecation conditions", tt.expectConditionsCount)

			// Second reconcile: should preserve lastTransitionTime (no changes)
			controllers.SetDeprecationStatus(ext, tt.installedBundleName, tt.deprecation, tt.hasCatalogData)

			secondReconcileConditions := ext.Status.Conditions

			// Verify conditions are identical (including lastTransitionTime)
			require.Len(t, secondReconcileConditions, len(firstReconcileConditions),
				"Number of conditions should remain the same")

			for i, firstCond := range firstReconcileConditions {
				secondCond := secondReconcileConditions[i]
				require.Equal(t, firstCond.Type, secondCond.Type, "Condition type should match")
				require.Equal(t, firstCond.Status, secondCond.Status, "Condition status should match")
				require.Equal(t, firstCond.Reason, secondCond.Reason, "Condition reason should match")
				require.Equal(t, firstCond.Message, secondCond.Message, "Condition message should match")

				// This is the critical check: lastTransitionTime should NOT change
				require.Equal(t, firstCond.LastTransitionTime, secondCond.LastTransitionTime,
					"lastTransitionTime should be preserved (prevents infinite reconcile loop)")
			}

			// Third reconcile: verify it remains stable
			controllers.SetDeprecationStatus(ext, tt.installedBundleName, tt.deprecation, tt.hasCatalogData)

			thirdReconcileConditions := ext.Status.Conditions
			require.Len(t, thirdReconcileConditions, len(secondReconcileConditions),
				"Conditions should remain stable after multiple reconciles")

			for i, secondCond := range secondReconcileConditions {
				thirdCond := thirdReconcileConditions[i]
				require.Equal(t, secondCond.LastTransitionTime, thirdCond.LastTransitionTime,
					"lastTransitionTime should remain stable across reconciles")
			}
		})
	}
}

// TestSetDeprecationStatus_StatusChangesOnlyWhenNeeded verifies that calling SetDeprecationStatus
// only modifies the status when actual deprecation state changes, not on every reconcile.
func TestSetDeprecationStatus_StatusChangesOnlyWhenNeeded(t *testing.T) {
	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Generation: 1,
		},
		Status: ocv1.ClusterExtensionStatus{
			Conditions: []metav1.Condition{},
		},
	}

	// Scenario 1: Package becomes deprecated
	deprecation := &declcfg.Deprecation{
		Entries: []declcfg.DeprecationEntry{
			{
				Reference: declcfg.PackageScopedReference{Schema: declcfg.SchemaPackage},
				Message:   "package is deprecated",
			},
		},
	}

	// First reconcile: add deprecation condition
	controllers.SetDeprecationStatus(ext, "test.v1.0.0", deprecation, true)
	statusAfterFirstReconcile := ext.Status.DeepCopy()

	// Second reconcile: same deprecation state
	controllers.SetDeprecationStatus(ext, "test.v1.0.0", deprecation, true)
	statusAfterSecondReconcile := ext.Status.DeepCopy()

	// Status should be semantically equal (DeepEqual would return true)
	require.True(t, equality.Semantic.DeepEqual(statusAfterFirstReconcile, statusAfterSecondReconcile),
		"Status should not change when deprecation state is unchanged")

	// Scenario 2: Deprecation is resolved (package no longer deprecated)
	controllers.SetDeprecationStatus(ext, "test.v1.0.0", nil, true)
	statusAfterResolution := ext.Status.DeepCopy()

	// Status should have changed (conditions removed)
	require.False(t, equality.Semantic.DeepEqual(statusAfterSecondReconcile, statusAfterResolution),
		"Status should change when deprecation is resolved")

	// Scenario 3: Verify resolution is stable
	controllers.SetDeprecationStatus(ext, "test.v1.0.0", nil, true)
	statusAfterFourthReconcile := ext.Status.DeepCopy()

	require.True(t, equality.Semantic.DeepEqual(statusAfterResolution, statusAfterFourthReconcile),
		"Status should remain stable after deprecation is resolved")
}

// filterDeprecationConditions returns only the deprecation-related conditions
func filterDeprecationConditions(conditions []metav1.Condition) []metav1.Condition {
	var result []metav1.Condition
	for _, cond := range conditions {
		switch cond.Type {
		case ocv1.TypeDeprecated, ocv1.TypePackageDeprecated, ocv1.TypeChannelDeprecated, ocv1.TypeBundleDeprecated:
			result = append(result, cond)
		}
	}
	return result
}

