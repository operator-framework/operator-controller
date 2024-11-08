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

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/conditionsets"
	"github.com/operator-framework/operator-controller/internal/controllers"
	"github.com/operator-framework/operator-controller/internal/finalizers"
	"github.com/operator-framework/operator-controller/internal/labels"
	"github.com/operator-framework/operator-controller/internal/resolve"
	"github.com/operator-framework/operator-controller/internal/rukpak/source"
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

func TestClusterExtensionResolutionFails(t *testing.T) {
	pkgName := fmt.Sprintf("non-existent-%s", rand.String(6))
	cl, reconciler := newClientAndReconciler(t)
	reconciler.Resolver = resolve.Func(func(_ context.Context, _ *ocv1alpha1.ClusterExtension, _ *ocv1alpha1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
		return nil, nil, nil, fmt.Errorf("no package %q found", pkgName)
	})
	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

	t.Log("When the cluster extension specifies a non-existent package")
	t.Log("By initializing cluster state")
	clusterExtension := &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			Source: ocv1alpha1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &ocv1alpha1.CatalogSource{
					PackageName: pkgName,
				},
			},
			Install: ocv1alpha1.ClusterExtensionInstallConfig{
				Namespace: "default",
				ServiceAccount: ocv1alpha1.ServiceAccountReference{
					Name: "default",
				},
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
	cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeProgressing)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonRetrying, cond.Reason)
	require.Equal(t, fmt.Sprintf("no package %q found", pkgName), cond.Message)

	verifyInvariants(ctx, t, reconciler.Client, clusterExtension)
	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
}

func TestClusterExtensionResolutionSuccessfulUnpackFails(t *testing.T) {
	type testCase struct {
		name           string
		unpackErr      error
		expectTerminal bool
	}
	for _, tc := range []testCase{
		{
			name:      "non-terminal unpack failure",
			unpackErr: errors.New("unpack failure"),
		},
		{
			name:           "terminal unpack failure",
			unpackErr:      reconcile.TerminalError(errors.New("terminal unpack failure")),
			expectTerminal: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cl, reconciler := newClientAndReconciler(t)
			reconciler.Unpacker = &MockUnpacker{
				err: tc.unpackErr,
			}

			ctx := context.Background()
			extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

			t.Log("When the cluster extension specifies a channel with version that exist")
			t.Log("By initializing cluster state")
			pkgName := "prometheus"
			pkgVer := "1.0.0"
			pkgChan := "beta"
			namespace := fmt.Sprintf("test-ns-%s", rand.String(8))
			serviceAccount := fmt.Sprintf("test-sa-%s", rand.String(8))

			clusterExtension := &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
				Spec: ocv1alpha1.ClusterExtensionSpec{
					Source: ocv1alpha1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1alpha1.CatalogSource{
							PackageName: pkgName,
							Version:     pkgVer,
							Channels:    []string{pkgChan},
						},
					},
					Install: ocv1alpha1.ClusterExtensionInstallConfig{
						Namespace: namespace,
						ServiceAccount: ocv1alpha1.ServiceAccountReference{
							Name: serviceAccount,
						},
					},
				},
			}
			err := cl.Create(ctx, clusterExtension)
			require.NoError(t, err)

			t.Log("It sets resolution success status")
			t.Log("By running reconcile")
			reconciler.Resolver = resolve.Func(func(_ context.Context, _ *ocv1alpha1.ClusterExtension, _ *ocv1alpha1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
				v := bsemver.MustParse("1.0.0")
				return &declcfg.Bundle{
					Name:    "prometheus.v1.0.0",
					Package: "prometheus",
					Image:   "quay.io/operatorhubio/prometheus@fake1.0.0",
				}, &v, nil, nil
			})
			res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
			require.Equal(t, ctrl.Result{}, res)
			require.Error(t, err)

			isTerminal := errors.Is(err, reconcile.TerminalError(nil))
			assert.Equal(t, tc.expectTerminal, isTerminal, "expected terminal error: %v, got: %v", tc.expectTerminal, isTerminal)
			require.ErrorContains(t, err, tc.unpackErr.Error())

			t.Log("By fetching updated cluster extension after reconcile")
			require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

			t.Log("By checking the status fields")
			expectedBundleMetadata := ocv1alpha1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"}
			require.Empty(t, clusterExtension.Status.Install)

			t.Log("By checking the expected conditions")
			expectStatus := metav1.ConditionTrue
			expectReason := ocv1alpha1.ReasonRetrying
			if tc.expectTerminal {
				expectStatus = metav1.ConditionFalse
				expectReason = ocv1alpha1.ReasonBlocked
			}
			progressingCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeProgressing)
			require.NotNil(t, progressingCond)
			require.Equal(t, expectStatus, progressingCond.Status)
			require.Equal(t, expectReason, progressingCond.Reason)
			require.Contains(t, progressingCond.Message, fmt.Sprintf("for resolved bundle %q with version %q", expectedBundleMetadata.Name, expectedBundleMetadata.Version))

			require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
		})
	}
}

func TestClusterExtensionUnpackUnexpectedState(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	reconciler.Unpacker = &MockUnpacker{
		result: &source.Result{
			State: "unexpected",
		},
	}

	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

	t.Log("When the cluster extension specifies a channel with version that exist")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgVer := "1.0.0"
	pkgChan := "beta"
	namespace := fmt.Sprintf("test-ns-%s", rand.String(8))
	serviceAccount := fmt.Sprintf("test-sa-%s", rand.String(8))

	clusterExtension := &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			Source: ocv1alpha1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &ocv1alpha1.CatalogSource{
					PackageName: pkgName,
					Version:     pkgVer,
					Channels:    []string{pkgChan},
				},
			},
			Install: ocv1alpha1.ClusterExtensionInstallConfig{
				Namespace: namespace,
				ServiceAccount: ocv1alpha1.ServiceAccountReference{
					Name: serviceAccount,
				},
			},
		},
	}
	err := cl.Create(ctx, clusterExtension)
	require.NoError(t, err)

	t.Log("It sets resolution success status")
	t.Log("By running reconcile")
	reconciler.Resolver = resolve.Func(func(_ context.Context, _ *ocv1alpha1.ClusterExtension, _ *ocv1alpha1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
		v := bsemver.MustParse("1.0.0")
		return &declcfg.Bundle{
			Name:    "prometheus.v1.0.0",
			Package: "prometheus",
			Image:   "quay.io/operatorhubio/prometheus@fake1.0.0",
		}, &v, nil, nil
	})

	require.Panics(t, func() {
		_, _ = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	}, "reconciliation should panic on unknown unpack state")

	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
}

func TestClusterExtensionResolutionAndUnpackSuccessfulApplierFails(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	reconciler.Unpacker = &MockUnpacker{
		result: &source.Result{
			State:  source.StateUnpacked,
			Bundle: fstest.MapFS{},
		},
	}

	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

	t.Log("When the cluster extension specifies a channel with version that exist")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgVer := "1.0.0"
	pkgChan := "beta"
	namespace := fmt.Sprintf("test-ns-%s", rand.String(8))
	serviceAccount := fmt.Sprintf("test-sa-%s", rand.String(8))

	clusterExtension := &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			Source: ocv1alpha1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &ocv1alpha1.CatalogSource{
					PackageName: pkgName,
					Version:     pkgVer,
					Channels:    []string{pkgChan},
				},
			},
			Install: ocv1alpha1.ClusterExtensionInstallConfig{
				Namespace: namespace,
				ServiceAccount: ocv1alpha1.ServiceAccountReference{
					Name: serviceAccount,
				},
			},
		},
	}
	err := cl.Create(ctx, clusterExtension)
	require.NoError(t, err)

	t.Log("It sets resolution success status")
	t.Log("By running reconcile")
	reconciler.Resolver = resolve.Func(func(_ context.Context, _ *ocv1alpha1.ClusterExtension, _ *ocv1alpha1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
		v := bsemver.MustParse("1.0.0")
		return &declcfg.Bundle{
			Name:    "prometheus.v1.0.0",
			Package: "prometheus",
			Image:   "quay.io/operatorhubio/prometheus@fake1.0.0",
		}, &v, nil, nil
	})
	reconciler.Applier = &MockApplier{
		err: errors.New("apply failure"),
	}
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.Error(t, err)

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("By checking the status fields")
	expectedBundleMetadata := ocv1alpha1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"}
	require.Empty(t, clusterExtension.Status.Install)

	t.Log("By checking the expected installed conditions")
	installedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, installedCond)
	require.Equal(t, metav1.ConditionFalse, installedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonFailed, installedCond.Reason)

	t.Log("By checking the expected progressing conditions")
	progressingCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeProgressing)
	require.NotNil(t, progressingCond)
	require.Equal(t, metav1.ConditionTrue, progressingCond.Status)
	require.Equal(t, ocv1alpha1.ReasonRetrying, progressingCond.Reason)
	require.Contains(t, progressingCond.Message, fmt.Sprintf("for resolved bundle %q with version %q", expectedBundleMetadata.Name, expectedBundleMetadata.Version))

	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
}

func TestClusterExtensionApplierFailsWithBundleInstalled(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	reconciler.Unpacker = &MockUnpacker{
		result: &source.Result{
			State:  source.StateUnpacked,
			Bundle: fstest.MapFS{},
		},
	}

	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

	t.Log("When the cluster extension specifies a channel with version that exist")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgVer := "1.0.0"
	pkgChan := "beta"
	namespace := fmt.Sprintf("test-ns-%s", rand.String(8))
	serviceAccount := fmt.Sprintf("test-sa-%s", rand.String(8))

	clusterExtension := &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			Source: ocv1alpha1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &ocv1alpha1.CatalogSource{
					PackageName: pkgName,
					Version:     pkgVer,
					Channels:    []string{pkgChan},
				},
			},
			Install: ocv1alpha1.ClusterExtensionInstallConfig{
				Namespace: namespace,
				ServiceAccount: ocv1alpha1.ServiceAccountReference{
					Name: serviceAccount,
				},
			},
		},
	}
	err := cl.Create(ctx, clusterExtension)
	require.NoError(t, err)

	t.Log("It sets resolution success status")
	t.Log("By running reconcile")
	reconciler.Resolver = resolve.Func(func(_ context.Context, _ *ocv1alpha1.ClusterExtension, _ *ocv1alpha1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
		v := bsemver.MustParse("1.0.0")
		return &declcfg.Bundle{
			Name:    "prometheus.v1.0.0",
			Package: "prometheus",
			Image:   "quay.io/operatorhubio/prometheus@fake1.0.0",
		}, &v, nil, nil
	})

	reconciler.Manager = &MockManagedContentCacheManager{
		cache: &MockManagedContentCache{},
	}
	reconciler.InstalledBundleGetter = &MockInstalledBundleGetter{
		bundle: &controllers.InstalledBundle{
			BundleMetadata: ocv1alpha1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"},
			Image:          "quay.io/operatorhubio/prometheus@fake1.0.0",
		},
	}
	reconciler.Applier = &MockApplier{
		objs: []client.Object{},
	}

	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	reconciler.Applier = &MockApplier{
		err: errors.New("apply failure"),
	}

	res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.Error(t, err)

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("By checking the status fields")
	expectedBundleMetadata := ocv1alpha1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"}
	require.Equal(t, expectedBundleMetadata, clusterExtension.Status.Install.Bundle)

	t.Log("By checking the expected installed conditions")
	installedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, installedCond)
	require.Equal(t, metav1.ConditionTrue, installedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonSucceeded, installedCond.Reason)

	t.Log("By checking the expected progressing conditions")
	progressingCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeProgressing)
	require.NotNil(t, progressingCond)
	require.Equal(t, metav1.ConditionTrue, progressingCond.Status)
	require.Equal(t, ocv1alpha1.ReasonRetrying, progressingCond.Reason)
	require.Contains(t, progressingCond.Message, fmt.Sprintf("for resolved bundle %q with version %q", expectedBundleMetadata.Name, expectedBundleMetadata.Version))

	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
}

func TestClusterExtensionManagerFailed(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	reconciler.Unpacker = &MockUnpacker{
		result: &source.Result{
			State:  source.StateUnpacked,
			Bundle: fstest.MapFS{},
		},
	}

	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

	t.Log("When the cluster extension specifies a channel with version that exist")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgVer := "1.0.0"
	pkgChan := "beta"
	namespace := fmt.Sprintf("test-ns-%s", rand.String(8))
	serviceAccount := fmt.Sprintf("test-sa-%s", rand.String(8))

	clusterExtension := &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			Source: ocv1alpha1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &ocv1alpha1.CatalogSource{
					PackageName: pkgName,
					Version:     pkgVer,
					Channels:    []string{pkgChan},
				},
			},
			Install: ocv1alpha1.ClusterExtensionInstallConfig{
				Namespace: namespace,
				ServiceAccount: ocv1alpha1.ServiceAccountReference{
					Name: serviceAccount,
				},
			},
		},
	}
	err := cl.Create(ctx, clusterExtension)
	require.NoError(t, err)

	t.Log("It sets resolution success status")
	t.Log("By running reconcile")
	reconciler.Resolver = resolve.Func(func(_ context.Context, _ *ocv1alpha1.ClusterExtension, _ *ocv1alpha1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
		v := bsemver.MustParse("1.0.0")
		return &declcfg.Bundle{
			Name:    "prometheus.v1.0.0",
			Package: "prometheus",
			Image:   "quay.io/operatorhubio/prometheus@fake1.0.0",
		}, &v, nil, nil
	})
	reconciler.Applier = &MockApplier{
		objs: []client.Object{},
	}
	reconciler.Manager = &MockManagedContentCacheManager{
		err: errors.New("manager fail"),
	}
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.Error(t, err)

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("By checking the status fields")
	require.Equal(t, ocv1alpha1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"}, clusterExtension.Status.Install.Bundle)

	t.Log("By checking the expected installed conditions")
	installedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, installedCond)
	require.Equal(t, metav1.ConditionTrue, installedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonSucceeded, installedCond.Reason)

	t.Log("By checking the expected progressing conditions")
	progressingCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeProgressing)
	require.NotNil(t, progressingCond)
	require.Equal(t, metav1.ConditionTrue, progressingCond.Status)
	require.Equal(t, ocv1alpha1.ReasonRetrying, progressingCond.Reason)

	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
}

func TestClusterExtensionManagedContentCacheWatchFail(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	reconciler.Unpacker = &MockUnpacker{
		result: &source.Result{
			State:  source.StateUnpacked,
			Bundle: fstest.MapFS{},
		},
	}

	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

	t.Log("When the cluster extension specifies a channel with version that exist")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgVer := "1.0.0"
	pkgChan := "beta"
	installNamespace := fmt.Sprintf("test-ns-%s", rand.String(8))
	serviceAccount := fmt.Sprintf("test-sa-%s", rand.String(8))

	clusterExtension := &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			Source: ocv1alpha1.SourceConfig{
				SourceType: ocv1alpha1.SourceTypeCatalog,

				Catalog: &ocv1alpha1.CatalogSource{
					PackageName: pkgName,
					Version:     pkgVer,
					Channels:    []string{pkgChan},
				},
			},
			Install: ocv1alpha1.ClusterExtensionInstallConfig{
				Namespace: installNamespace,
				ServiceAccount: ocv1alpha1.ServiceAccountReference{
					Name: serviceAccount,
				},
			},
		},
	}
	err := cl.Create(ctx, clusterExtension)
	require.NoError(t, err)

	t.Log("It sets resolution success status")
	t.Log("By running reconcile")
	reconciler.Resolver = resolve.Func(func(_ context.Context, _ *ocv1alpha1.ClusterExtension, _ *ocv1alpha1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
		v := bsemver.MustParse("1.0.0")
		return &declcfg.Bundle{
			Name:    "prometheus.v1.0.0",
			Package: "prometheus",
			Image:   "quay.io/operatorhubio/prometheus@fake1.0.0",
		}, &v, nil, nil
	})
	reconciler.Applier = &MockApplier{
		objs: []client.Object{},
	}
	reconciler.Manager = &MockManagedContentCacheManager{
		cache: &MockManagedContentCache{
			err: errors.New("watch error"),
		},
	}
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.Error(t, err)

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("By checking the status fields")
	require.Equal(t, ocv1alpha1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"}, clusterExtension.Status.Install.Bundle)

	t.Log("By checking the expected installed conditions")
	installedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, installedCond)
	require.Equal(t, metav1.ConditionTrue, installedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonSucceeded, installedCond.Reason)

	t.Log("By checking the expected progressing conditions")
	progressingCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeProgressing)
	require.NotNil(t, progressingCond)
	require.Equal(t, metav1.ConditionTrue, progressingCond.Status)
	require.Equal(t, ocv1alpha1.ReasonRetrying, progressingCond.Reason)

	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
}

func TestClusterExtensionInstallationSucceeds(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	reconciler.Unpacker = &MockUnpacker{
		result: &source.Result{
			State:  source.StateUnpacked,
			Bundle: fstest.MapFS{},
		},
	}

	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

	t.Log("When the cluster extension specifies a channel with version that exist")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgVer := "1.0.0"
	pkgChan := "beta"
	namespace := fmt.Sprintf("test-ns-%s", rand.String(8))
	serviceAccount := fmt.Sprintf("test-sa-%s", rand.String(8))

	clusterExtension := &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			Source: ocv1alpha1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &ocv1alpha1.CatalogSource{
					PackageName: pkgName,
					Version:     pkgVer,
					Channels:    []string{pkgChan},
				},
			},
			Install: ocv1alpha1.ClusterExtensionInstallConfig{
				Namespace: namespace,
				ServiceAccount: ocv1alpha1.ServiceAccountReference{
					Name: serviceAccount,
				},
			},
		},
	}
	err := cl.Create(ctx, clusterExtension)
	require.NoError(t, err)

	t.Log("It sets resolution success status")
	t.Log("By running reconcile")
	reconciler.Resolver = resolve.Func(func(_ context.Context, _ *ocv1alpha1.ClusterExtension, _ *ocv1alpha1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
		v := bsemver.MustParse("1.0.0")
		return &declcfg.Bundle{
			Name:    "prometheus.v1.0.0",
			Package: "prometheus",
			Image:   "quay.io/operatorhubio/prometheus@fake1.0.0",
		}, &v, nil, nil
	})
	reconciler.Applier = &MockApplier{
		objs: []client.Object{},
	}
	reconciler.Manager = &MockManagedContentCacheManager{
		cache: &MockManagedContentCache{},
	}
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("By checking the status fields")
	require.Equal(t, ocv1alpha1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"}, clusterExtension.Status.Install.Bundle)

	t.Log("By checking the expected installed conditions")
	installedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, installedCond)
	require.Equal(t, metav1.ConditionTrue, installedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonSucceeded, installedCond.Reason)

	t.Log("By checking the expected progressing conditions")
	progressingCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeProgressing)
	require.NotNil(t, progressingCond)
	require.Equal(t, metav1.ConditionTrue, progressingCond.Status)
	require.Equal(t, ocv1alpha1.ReasonSucceeded, progressingCond.Reason)

	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
}

func TestClusterExtensionDeleteFinalizerFails(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	reconciler.Unpacker = &MockUnpacker{
		result: &source.Result{
			State:  source.StateUnpacked,
			Bundle: fstest.MapFS{},
		},
	}

	ctx := context.Background()
	extKey := types.NamespacedName{Name: fmt.Sprintf("cluster-extension-test-%s", rand.String(8))}

	t.Log("When the cluster extension specifies a channel with version that exist")
	t.Log("By initializing cluster state")
	pkgName := "prometheus"
	pkgVer := "1.0.0"
	pkgChan := "beta"
	namespace := fmt.Sprintf("test-ns-%s", rand.String(8))
	serviceAccount := fmt.Sprintf("test-sa-%s", rand.String(8))

	clusterExtension := &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: extKey.Name},
		Spec: ocv1alpha1.ClusterExtensionSpec{
			Source: ocv1alpha1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &ocv1alpha1.CatalogSource{
					PackageName: pkgName,
					Version:     pkgVer,
					Channels:    []string{pkgChan},
				},
			},
			Install: ocv1alpha1.ClusterExtensionInstallConfig{
				Namespace: namespace,
				ServiceAccount: ocv1alpha1.ServiceAccountReference{
					Name: serviceAccount,
				},
			},
		},
	}
	err := cl.Create(ctx, clusterExtension)
	require.NoError(t, err)
	t.Log("It sets resolution success status")
	t.Log("By running reconcile")
	reconciler.Resolver = resolve.Func(func(_ context.Context, _ *ocv1alpha1.ClusterExtension, _ *ocv1alpha1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
		v := bsemver.MustParse("1.0.0")
		return &declcfg.Bundle{
			Name:    "prometheus.v1.0.0",
			Package: "prometheus",
			Image:   "quay.io/operatorhubio/prometheus@fake1.0.0",
		}, &v, nil, nil
	})
	fakeFinalizer := "fake.testfinalizer.io"
	finalizersMessage := "still have finalizers"
	reconciler.Applier = &MockApplier{
		objs: []client.Object{},
	}
	reconciler.Manager = &MockManagedContentCacheManager{
		cache: &MockManagedContentCache{},
	}
	reconciler.InstalledBundleGetter = &MockInstalledBundleGetter{
		bundle: &controllers.InstalledBundle{
			BundleMetadata: ocv1alpha1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"},
			Image:          "quay.io/operatorhubio/prometheus@fake1.0.0",
		},
	}
	err = reconciler.Finalizers.Register(fakeFinalizer, finalizers.FinalizerFunc(func(ctx context.Context, obj client.Object) (crfinalizer.Result, error) {
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
	cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
	expectedBundleMetadata := ocv1alpha1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"}
	require.Equal(t, expectedBundleMetadata, clusterExtension.Status.Install.Bundle)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)

	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
	res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Error(t, err, res)

	t.Log("By fetching updated cluster extension after second reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))
	cond = apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.Equal(t, expectedBundleMetadata, clusterExtension.Status.Install.Bundle)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, fakeFinalizer, clusterExtension.Finalizers[0])
	cond = apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeProgressing)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Contains(t, cond.Message, finalizersMessage)
}

func verifyInvariants(ctx context.Context, t *testing.T, c client.Client, ext *ocv1alpha1.ClusterExtension) {
	key := client.ObjectKeyFromObject(ext)
	require.NoError(t, c.Get(ctx, key, ext))

	verifyConditionsInvariants(t, ext)
}

func verifyConditionsInvariants(t *testing.T, ext *ocv1alpha1.ClusterExtension) {
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
		clusterExtension         *ocv1alpha1.ClusterExtension
		expectedClusterExtension *ocv1alpha1.ClusterExtension
		bundle                   *declcfg.Bundle
		deprecation              *declcfg.Deprecation
	}{
		{
			name: "no deprecations, all deprecation statuses set to False",
			clusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectedClusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:               ocv1alpha1.TypeDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypePackageDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeChannelDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeBundleDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
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
			clusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1alpha1.ClusterExtensionSpec{
					Source: ocv1alpha1.SourceConfig{
						SourceType: "Catalog",
						Catalog:    &ocv1alpha1.CatalogSource{},
					},
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectedClusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1alpha1.ClusterExtensionSpec{
					Source: ocv1alpha1.SourceConfig{
						SourceType: "Catalog",
						Catalog:    &ocv1alpha1.CatalogSource{},
					},
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:               ocv1alpha1.TypeDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypePackageDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeChannelDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeBundleDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
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
			clusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1alpha1.ClusterExtensionSpec{
					Source: ocv1alpha1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1alpha1.CatalogSource{
							Channels: []string{"nondeprecated"},
						},
					},
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectedClusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1alpha1.ClusterExtensionSpec{
					Source: ocv1alpha1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1alpha1.CatalogSource{
							Channels: []string{"nondeprecated"},
						},
					},
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:               ocv1alpha1.TypeDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypePackageDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeChannelDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeBundleDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
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
			clusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1alpha1.ClusterExtensionSpec{
					Source: ocv1alpha1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1alpha1.CatalogSource{
							Channels: []string{"badchannel"},
						},
					},
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectedClusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1alpha1.ClusterExtensionSpec{
					Source: ocv1alpha1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1alpha1.CatalogSource{
							Channels: []string{"badchannel"},
						},
					},
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:               ocv1alpha1.TypeDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypePackageDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeChannelDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeBundleDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
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
			clusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1alpha1.ClusterExtensionSpec{
					Source: ocv1alpha1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1alpha1.CatalogSource{
							Channels: []string{"badchannel"},
						},
					},
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectedClusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1alpha1.ClusterExtensionSpec{
					Source: ocv1alpha1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1alpha1.CatalogSource{
							Channels: []string{"badchannel"},
						},
					},
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:               ocv1alpha1.TypeDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypePackageDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeChannelDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeBundleDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
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
			clusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1alpha1.ClusterExtensionSpec{
					Source: ocv1alpha1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1alpha1.CatalogSource{
							Channels: []string{"badchannel"},
						},
					},
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectedClusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1alpha1.ClusterExtensionSpec{
					Source: ocv1alpha1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1alpha1.CatalogSource{
							Channels: []string{"badchannel"},
						},
					},
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:               ocv1alpha1.TypeDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypePackageDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeChannelDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeBundleDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
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
			clusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1alpha1.ClusterExtensionSpec{
					Source: ocv1alpha1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1alpha1.CatalogSource{
							Channels: []string{"badchannel"},
						},
					},
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectedClusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1alpha1.ClusterExtensionSpec{
					Source: ocv1alpha1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1alpha1.CatalogSource{
							Channels: []string{"badchannel"},
						},
					},
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:               ocv1alpha1.TypeDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypePackageDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeChannelDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeBundleDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
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
			clusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1alpha1.ClusterExtensionSpec{
					Source: ocv1alpha1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1alpha1.CatalogSource{
							Channels: []string{"badchannel", "anotherbadchannel"},
						},
					},
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectedClusterExtension: &ocv1alpha1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Spec: ocv1alpha1.ClusterExtensionSpec{
					Source: ocv1alpha1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &ocv1alpha1.CatalogSource{
							Channels: []string{"badchannel", "anotherbadchannel"},
						},
					},
				},
				Status: ocv1alpha1.ClusterExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:               ocv1alpha1.TypeDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypePackageDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeChannelDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
						{
							Type:               ocv1alpha1.TypeBundleDeprecated,
							Reason:             ocv1alpha1.ReasonDeprecated,
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
			assert.Equal(t, "", cmp.Diff(tc.expectedClusterExtension, tc.clusterExtension, cmpopts.IgnoreFields(metav1.Condition{}, "Message", "LastTransitionTime")))
		})
	}
}

type MockActionGetter struct {
	description    string
	rels           []*release.Release
	err            error
	expectedBundle *controllers.InstalledBundle
	expectedError  error
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
	getter := controllers.DefaultInstalledBundleGetter{}

	ext := ocv1alpha1.ClusterExtension{
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
			}, nil,
			&controllers.InstalledBundle{
				BundleMetadata: ocv1alpha1.BundleMetadata{
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
			}, nil,
			&controllers.InstalledBundle{
				BundleMetadata: ocv1alpha1.BundleMetadata{
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
		md, err := getter.GetInstalledBundle(context.Background(), &ext)
		require.Equal(t, tst.expectedError, err)
		require.Equal(t, tst.expectedBundle, md)
	}
}
