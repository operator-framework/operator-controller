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
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/conditionsets"
	"github.com/operator-framework/operator-controller/internal/controllers"
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
	require.Empty(t, clusterExtension.Status.Resolution)
	require.Empty(t, clusterExtension.Status.Install)

	t.Log("By checking the expected conditions")
	cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, ocv1alpha1.ReasonResolutionFailed, cond.Reason)
	require.Equal(t, fmt.Sprintf("no package %q found", pkgName), cond.Message)

	verifyInvariants(ctx, t, reconciler.Client, clusterExtension)
	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
}

func TestClusterExtensionResolutionSucceeds(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	reconciler.Unpacker = &MockUnpacker{
		result: &source.Result{
			State: source.StatePending,
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
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("By checking the status fields")
	require.Equal(t, &ocv1alpha1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"}, clusterExtension.Status.Resolution.Bundle)
	require.Empty(t, clusterExtension.Status.Install)

	t.Log("By checking the expected conditions")
	resolvedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, resolvedCond)
	require.Equal(t, metav1.ConditionTrue, resolvedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, resolvedCond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake1.0.0\"", resolvedCond.Message)

	t.Log("By checking the expected unpacked conditions")
	unpackedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeUnpacked)
	require.NotNil(t, unpackedCond)
	require.Equal(t, metav1.ConditionFalse, unpackedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonUnpackFailed, unpackedCond.Reason)

	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
}

func TestClusterExtensionUnpackFails(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)
	reconciler.Unpacker = &MockUnpacker{
		err: errors.New("unpack failure"),
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

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("By checking the status fields")
	require.Equal(t, &ocv1alpha1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"}, clusterExtension.Status.Resolution.Bundle)
	require.Empty(t, clusterExtension.Status.Install)

	t.Log("By checking the expected conditions")
	resolvedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, resolvedCond)
	require.Equal(t, metav1.ConditionTrue, resolvedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, resolvedCond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake1.0.0\"", resolvedCond.Message)

	t.Log("By checking the expected unpacked conditions")
	unpackedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeUnpacked)
	require.NotNil(t, unpackedCond)
	require.Equal(t, metav1.ConditionFalse, unpackedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonUnpackFailed, unpackedCond.Reason)

	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
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
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: extKey})
	require.Equal(t, ctrl.Result{}, res)
	require.Error(t, err)

	t.Log("By fetching updated cluster extension after reconcile")
	require.NoError(t, cl.Get(ctx, extKey, clusterExtension))

	t.Log("By checking the status fields")
	require.Equal(t, &ocv1alpha1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"}, clusterExtension.Status.Resolution.Bundle)
	require.Empty(t, clusterExtension.Status.Install)

	t.Log("By checking the expected conditions")
	resolvedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, resolvedCond)
	require.Equal(t, metav1.ConditionTrue, resolvedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, resolvedCond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake1.0.0\"", resolvedCond.Message)

	t.Log("By checking the expected unpacked conditions")
	unpackedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeUnpacked)
	require.NotNil(t, unpackedCond)
	require.Equal(t, metav1.ConditionFalse, unpackedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonUnpackFailed, unpackedCond.Reason)

	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
}

func TestClusterExtensionUnpackSucceeds(t *testing.T) {
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
	require.Equal(t, &ocv1alpha1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"}, clusterExtension.Status.Resolution.Bundle)
	require.Empty(t, clusterExtension.Status.Install)

	t.Log("By checking the expected resolution conditions")
	resolvedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, resolvedCond)
	require.Equal(t, metav1.ConditionTrue, resolvedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, resolvedCond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake1.0.0\"", resolvedCond.Message)

	t.Log("By checking the expected unpacked conditions")
	unpackedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeUnpacked)
	require.NotNil(t, unpackedCond)
	require.Equal(t, metav1.ConditionTrue, unpackedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonUnpackSuccess, unpackedCond.Reason)

	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
}

func TestClusterExtensionInstallationFailedApplierFails(t *testing.T) {
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
	require.Equal(t, &ocv1alpha1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"}, clusterExtension.Status.Resolution.Bundle)
	require.Empty(t, clusterExtension.Status.Install)

	t.Log("By checking the expected resolution conditions")
	resolvedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, resolvedCond)
	require.Equal(t, metav1.ConditionTrue, resolvedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, resolvedCond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake1.0.0\"", resolvedCond.Message)

	t.Log("By checking the expected unpacked conditions")
	unpackedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeUnpacked)
	require.NotNil(t, unpackedCond)
	require.Equal(t, metav1.ConditionTrue, unpackedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonUnpackSuccess, unpackedCond.Reason)

	t.Log("By checking the expected installed conditions")
	installedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, installedCond)
	require.Equal(t, metav1.ConditionFalse, installedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonInstallationFailed, installedCond.Reason)

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
	require.Equal(t, &ocv1alpha1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"}, clusterExtension.Status.Resolution.Bundle)
	require.Equal(t, &ocv1alpha1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"}, clusterExtension.Status.Install.Bundle)

	t.Log("By checking the expected resolution conditions")
	resolvedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, resolvedCond)
	require.Equal(t, metav1.ConditionTrue, resolvedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, resolvedCond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake1.0.0\"", resolvedCond.Message)

	t.Log("By checking the expected unpacked conditions")
	unpackedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeUnpacked)
	require.NotNil(t, unpackedCond)
	require.Equal(t, metav1.ConditionTrue, unpackedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonUnpackSuccess, unpackedCond.Reason)

	t.Log("By checking the expected installed conditions")
	installedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, installedCond)
	require.Equal(t, metav1.ConditionTrue, installedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, installedCond.Reason)

	t.Log("By checking the expected healthy conditions")
	managedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeHealthy)
	require.NotNil(t, managedCond)
	require.Equal(t, metav1.ConditionUnknown, managedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonUnverifiable, managedCond.Reason)

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
	require.Equal(t, &ocv1alpha1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"}, clusterExtension.Status.Resolution.Bundle)
	require.Equal(t, &ocv1alpha1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"}, clusterExtension.Status.Install.Bundle)

	t.Log("By checking the expected resolution conditions")
	resolvedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, resolvedCond)
	require.Equal(t, metav1.ConditionTrue, resolvedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, resolvedCond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake1.0.0\"", resolvedCond.Message)

	t.Log("By checking the expected unpacked conditions")
	unpackedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeUnpacked)
	require.NotNil(t, unpackedCond)
	require.Equal(t, metav1.ConditionTrue, unpackedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonUnpackSuccess, unpackedCond.Reason)

	t.Log("By checking the expected installed conditions")
	installedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, installedCond)
	require.Equal(t, metav1.ConditionTrue, installedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, installedCond.Reason)

	t.Log("By checking the expected healthy conditions")
	managedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeHealthy)
	require.NotNil(t, managedCond)
	require.Equal(t, metav1.ConditionUnknown, managedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonUnverifiable, managedCond.Reason)

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
	require.Equal(t, &ocv1alpha1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"}, clusterExtension.Status.Resolution.Bundle)
	require.Equal(t, &ocv1alpha1.BundleMetadata{Name: "prometheus.v1.0.0", Version: "1.0.0"}, clusterExtension.Status.Install.Bundle)

	t.Log("By checking the expected resolution conditions")
	resolvedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeResolved)
	require.NotNil(t, resolvedCond)
	require.Equal(t, metav1.ConditionTrue, resolvedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, resolvedCond.Reason)
	require.Equal(t, "resolved to \"quay.io/operatorhubio/prometheus@fake1.0.0\"", resolvedCond.Message)

	t.Log("By checking the expected unpacked conditions")
	unpackedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeUnpacked)
	require.NotNil(t, unpackedCond)
	require.Equal(t, metav1.ConditionTrue, unpackedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonUnpackSuccess, unpackedCond.Reason)

	t.Log("By checking the expected installed conditions")
	installedCond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1alpha1.TypeInstalled)
	require.NotNil(t, installedCond)
	require.Equal(t, metav1.ConditionTrue, installedCond.Status)
	require.Equal(t, ocv1alpha1.ReasonSuccess, installedCond.Reason)

	require.NoError(t, cl.DeleteAllOf(ctx, &ocv1alpha1.ClusterExtension{}))
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
