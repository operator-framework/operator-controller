//go:build !standard

package controllers_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"helm.sh/helm/v3/pkg/release"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"pkg.package-operator.run/boxcutter/managedcache"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	coscontrollers "github.com/operator-framework/operator-controller/internal/object-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	deploymentutil "github.com/operator-framework/operator-controller/internal/operator-controller/clusterobjectdeployment"
	"github.com/operator-framework/operator-controller/internal/operator-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
	mockapplier "github.com/operator-framework/operator-controller/internal/testutil/mock/applier"
	mockctrlclient "github.com/operator-framework/operator-controller/internal/testutil/mock/ctrlclient"
	mockhelmclient "github.com/operator-framework/operator-controller/internal/testutil/mock/helmclient"
)

func TestHelmMigrationAPIAdoptsLiveResource(t *testing.T) {
	// Given a live Helm-managed ConfigMap and a stored Adopting deployment.
	api := mockctrlclient.NewMockClient(gomock.NewController(t))
	e := newHelmMigrationEnv(t, api)

	// When the production controllers complete the adopting revision; migration writes are forbidden.
	cos := e.rollout(t, e.ext.Name+"-1")

	// Then the original resource survives under CE -> COD -> COS ownership and SSA management.
	cod := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, e.live.Get(e.ctx, client.ObjectKeyFromObject(e.ext), cod))
	require.True(t, metav1.IsControlledBy(cod, e.ext))
	require.True(t, metav1.IsControlledBy(cos, cod))
	resource := &corev1.ConfigMap{}
	require.NoError(t, e.live.Get(e.ctx, client.ObjectKeyFromObject(e.resource), resource))
	require.Equal(t, e.resource.UID, resource.UID)
	require.True(t, metav1.IsControlledBy(resource, cos))
	var managers []string
	for _, mf := range resource.ManagedFields {
		if mf.Operation == metav1.ManagedFieldsOperationApply {
			managers = append(managers, mf.Manager)
		}
	}
	require.Contains(t, managers, "cos-group/"+e.ext.Name)
}

func TestHelmMigrationAPIProtectsSiblingHandoff(t *testing.T) {
	// Given the initial adopting deployment; allow only the two migration transition patches.
	api := mockctrlclient.NewMockClient(gomock.NewController(t))
	e := newHelmMigrationEnv(t, api)
	api.EXPECT().Patch(gomock.Any(), gomock.AssignableToTypeOf(&ocv1.ClusterObjectDeployment{}), gomock.Any()).
		DoAndReturn(e.cached.Patch).Times(2)

	// When adoption hands off to the protected sibling and migration finishes.
	adopting, protecting := e.protect(t)

	// Then the content is identical and the same resource belongs to the protected revision.
	require.Equal(t, ocv1.CollisionProtectionNone, adopting.Spec.CollisionProtection)
	require.Equal(t, ocv1.CollisionProtectionPrevent, protecting.Spec.CollisionProtection)
	require.Equal(t, adopting.Spec.Phases, protecting.Spec.Phases)
	require.Equal(t, adopting.Annotations, protecting.Annotations)
	require.Equal(t, adopting.Spec.ProgressionProbes, protecting.Spec.ProgressionProbes)
	resource := &corev1.ConfigMap{}
	require.NoError(t, e.live.Get(e.ctx, client.ObjectKeyFromObject(e.resource), resource))
	require.Equal(t, e.resource.UID, resource.UID)
	require.True(t, metav1.IsControlledBy(resource, protecting))
	cod := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, e.live.Get(e.ctx, client.ObjectKeyFromObject(e.ext), cod))
	require.NotContains(t, cod.Annotations, labels.HelmMigrationKey)
}

func TestHelmMigrationAPIArchivalPreservesProtectedResource(t *testing.T) {
	// Given completed migration with the resource owned by the protected sibling.
	api := mockctrlclient.NewMockClient(gomock.NewController(t))
	e := newHelmMigrationEnv(t, api)
	api.EXPECT().Patch(gomock.Any(), gomock.AssignableToTypeOf(&ocv1.ClusterObjectDeployment{}), gomock.Any()).
		DoAndReturn(e.cached.Patch).Times(2)
	adopting, protecting := e.protect(t)

	// When the production COS controller reconciles the old adopting revision.
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		_, err := e.cos.Reconcile(e.ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: adopting.Name}})
		assert.NoError(collect, err)
		actual := &ocv1.ClusterObjectSet{}
		if !assert.NoError(collect, e.live.Get(e.ctx, client.ObjectKeyFromObject(adopting), actual)) {
			return
		}
		assert.Equal(collect, ocv1.ClusterObjectSetLifecycleStateArchived, actual.Spec.LifecycleState)
		ready := apimeta.FindStatusCondition(actual.Status.Conditions, ocv1.ClusterObjectSetTypeReady)
		if assert.NotNil(collect, ready) {
			assert.Equal(collect, ocv1.ClusterObjectSetReasonArchived, ready.Reason)
		}
	}, 5*time.Second, 25*time.Millisecond)

	// Then archival has completed without deleting or taking ownership from the protected resource.
	resource := &corev1.ConfigMap{}
	require.NoError(t, e.live.Get(e.ctx, client.ObjectKeyFromObject(e.resource), resource))
	require.Equal(t, e.resource.UID, resource.UID)
	require.True(t, metav1.IsControlledBy(resource, protecting))
}

type helmMigrationEnv struct {
	ctx      context.Context
	live     client.Client
	cached   client.Client
	ext      *ocv1.ClusterExtension
	resource *corev1.ConfigMap
	ce       *controllers.ClusterExtensionReconciler
	cod      *coscontrollers.ClusterObjectDeploymentReconciler
	cos      *coscontrollers.ClusterObjectSetReconciler
}

// newHelmMigrationEnv creates a legacy CE and a live Helm-managed ConfigMap on
// envtest's real API server. It starts indexed/tracking caches and runs the first
// CE migration reconcile, storing the Adopting COD and content Secrets. It does
// not roll out a COS. Helm and generation are each allowed once; subsequent
// migration writes must be explicitly permitted by the calling test via api.
// COD/COS reconcilers use real clients and the production revision engine.
func newHelmMigrationEnv(t *testing.T, api *mockctrlclient.MockClient) *helmMigrationEnv {
	t.Helper()
	scheme := newScheme(t)
	require.NoError(t, corev1.AddToScheme(scheme))
	live, err := client.New(config, client.Options{Scheme: scheme})
	require.NoError(t, err)
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "helm-migration-"}}
	require.NoError(t, live.Create(t.Context(), ns))
	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "helm-migration-"},
		Spec: ocv1.ClusterExtensionSpec{Namespace: ns.Name, ProgressDeadlineMinutes: 15,
			Source: ocv1.SourceConfig{SourceType: ocv1.SourceTypeCatalog, Catalog: &ocv1.CatalogFilter{PackageName: "test", Version: "1.0.0"}},
		},
	}
	require.NoError(t, live.Create(t.Context(), ext))
	// Cached reads include type metadata; keep the expected Helm/generator input equivalent.
	ext.SetGroupVersionKind(ocv1.GroupVersion.WithKind(ocv1.ClusterExtensionKind))
	resource := &corev1.ConfigMap{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{Name: "stored", Namespace: ns.Name, Labels: map[string]string{"app.kubernetes.io/managed-by": "Helm"}},
		Data:       map[string]string{"value": "from-helm"},
	}
	require.NoError(t, live.Create(t.Context(), resource, client.FieldOwner("helm")))
	resource.TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"}
	manifest, err := json.Marshal(resource)
	require.NoError(t, err)
	rel := &release.Release{Name: ext.Name, Info: &release.Info{Status: release.StatusDeployed}, Manifest: string(manifest) + "\n",
		Labels: map[string]string{labels.BundleNameKey: "test.v1.0.0", labels.BundleVersionKey: "1.0.0", labels.BundleReferenceKey: "image:stored", labels.PackageNameKey: "test", labels.BundleReleaseKey: ""},
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	reader, err := cache.New(config, cache.Options{Scheme: scheme})
	require.NoError(t, err)
	require.NoError(t, coscontrollers.SetupIndexes(ctx, reader))
	tracking, err := managedcache.NewTrackingCache(logr.Discard(), config, cache.Options{Scheme: scheme, Mapper: live.RESTMapper()})
	require.NoError(t, err)
	readerDone, trackingDone := make(chan error, 1), make(chan error, 1)
	go func() { readerDone <- reader.Start(ctx) }()
	go func() { trackingDone <- tracking.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		for _, done := range []chan error{readerDone, trackingDone} {
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Error("cache did not stop")
			}
		}
		// Envtest has no garbage collector; explicitly clean up owned objects.
		cleanup := context.Background()
		sets := &ocv1.ClusterObjectSetList{}
		require.NoError(t, live.List(cleanup, sets))
		for i := range sets.Items {
			if sets.Items[i].Spec.Group != ext.Name {
				continue
			}
			sets.Items[i].Finalizers = nil
			require.NoError(t, live.Update(cleanup, &sets.Items[i]))
			require.NoError(t, live.Delete(cleanup, &sets.Items[i]))
		}
		require.NoError(t, client.IgnoreNotFound(live.Delete(cleanup, &ocv1.ClusterObjectDeployment{ObjectMeta: metav1.ObjectMeta{Name: ext.Name}})))
		require.NoError(t, live.DeleteAllOf(cleanup, &corev1.Secret{}, client.InNamespace(ns.Name)))
		require.NoError(t, live.Delete(cleanup, resource))
		require.NoError(t, live.Delete(cleanup, ext))
		require.NoError(t, live.Delete(cleanup, ns))
	})
	cached, err := client.New(config, client.Options{Scheme: scheme, Cache: &client.CacheOptions{Reader: reader}})
	require.NoError(t, err)
	dc, err := discovery.NewDiscoveryClientForConfig(config)
	require.NoError(t, err)
	factory, err := coscontrollers.NewDefaultRevisionEngineFactory(scheme, tracking, memory.NewMemCacheClient(dc), live.RESTMapper(), config)
	require.NoError(t, err)
	// Only the initial migration may consult Helm or generate content.
	mockCtrl := gomock.NewController(t)
	helm := mockhelmclient.NewMockActionClientGetterAndInterface(mockCtrl)
	helm.EXPECT().ActionClientFor(gomock.Any(), ext).Return(helm, nil)
	helm.EXPECT().Get(ext.Name).Return(rel, nil)
	realGenerator := &applier.SimpleRevisionGenerator{Scheme: scheme, SystemNamespace: ns.Name}
	generator := mockapplier.NewMockRevisionGenerator(mockCtrl)
	generator.EXPECT().GenerateClusterObjectDeploymentFromHelmRelease(gomock.Any(), rel, ext, gomock.Any()).
		DoAndReturn(realGenerator.GenerateClusterObjectDeploymentFromHelmRelease)
	api.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(ext), gomock.AssignableToTypeOf(&ocv1.ClusterObjectDeployment{})).
		DoAndReturn(cached.Get).AnyTimes()
	api.EXPECT().List(gomock.Any(), gomock.AssignableToTypeOf(&ocv1.ClusterObjectSetList{}), gomock.Any()).DoAndReturn(cached.List)
	gomock.InOrder(
		api.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&corev1.Secret{})).DoAndReturn(cached.Create),
		api.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&ocv1.ClusterObjectDeployment{})).DoAndReturn(cached.Create),
	)
	m := &applier.BoxcutterStorageMigrator{Client: api, Scheme: scheme, ActionClientGetter: helm, RevisionGenerator: generator}
	// The CE controller may read the CE and write its status, never its spec or COD.
	ceAPI := mockctrlclient.NewMockClient(mockCtrl)
	ceAPI.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(ext), gomock.AssignableToTypeOf(ext)).DoAndReturn(cached.Get).AnyTimes()
	status := mockctrlclient.NewMockStatusWriter(mockCtrl)
	ceAPI.EXPECT().Status().Return(status).AnyTimes()
	status.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(ext)).DoAndReturn(cached.Status().Update).AnyTimes()
	env := &helmMigrationEnv{ctx: ctx, live: live, cached: cached, ext: ext, resource: resource,
		ce:  &controllers.ClusterExtensionReconciler{Client: ceAPI, ReconcileSteps: controllers.ReconcileSteps{controllers.MigrateStorage(m)}},
		cod: &coscontrollers.ClusterObjectDeploymentReconciler{Client: cached, Scheme: scheme},
		cos: &coscontrollers.ClusterObjectSetReconciler{Client: cached, RevisionEngineFactory: factory, TrackingCache: tracking},
	}
	result, err := env.ce.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(ext)})
	require.NoError(t, err)
	require.Equal(t, 10*time.Second, result.RequeueAfter)
	// Do not let cache latency or subsequent reconciles trigger another Helm read.
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		cod := &ocv1.ClusterObjectDeployment{}
		assert.NoError(collect, cached.Get(ctx, client.ObjectKeyFromObject(ext), cod))
	}, 5*time.Second, 10*time.Millisecond)
	// Exhausted mock expectations reject every later Helm read or generation call.
	return env
}

// rollout repeatedly runs the production COD/COS reconcilers until the named
// revision records real completion and the COD observes current-generation
// success. It relies on API-assigned UIDs/generations and server-side apply;
// it never manufactures readiness status.
func (e *helmMigrationEnv) rollout(t *testing.T, revision string) *ocv1.ClusterObjectSet {
	t.Helper()
	// Run both production reconcilers with the real engine until the revision is
	// ready. The API server supplies UIDs, generations, defaulting and SSA behavior.
	var cos ocv1.ClusterObjectSet
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		_, err := e.cod.Reconcile(e.ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(e.ext)})
		if !assert.NoError(collect, err) {
			return
		}
		if !assert.NoError(collect, e.live.Get(e.ctx, client.ObjectKey{Name: revision}, &cos)) {
			return
		}
		_, err = e.cos.Reconcile(e.ctx, ctrl.Request{NamespacedName: client.ObjectKey{Name: revision}})
		if !assert.NoError(collect, err) {
			return
		}
		if !assert.NoError(collect, e.live.Get(e.ctx, client.ObjectKey{Name: revision}, &cos)) {
			return
		}
		assert.False(collect, cos.Status.CompletedAt.IsZero(), "real readiness must record completion")
	}, 10*time.Second, 25*time.Millisecond)
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		_, err := e.cod.Reconcile(e.ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(e.ext)})
		assert.NoError(collect, err)
		cod := &ocv1.ClusterObjectDeployment{}
		if assert.NoError(collect, e.cached.Get(e.ctx, client.ObjectKeyFromObject(e.ext), cod)) {
			assert.True(collect, deploymentutil.RolloutComplete(cod))
		}
	}, 5*time.Second, 25*time.Millisecond)
	return &cos
}

// protect completes adoption, reconciles the CE to enable protection, completes
// the protected revision, then reconciles the CE to remove the migration marker.
// Both transition patches must already be permitted by the calling test.
func (e *helmMigrationEnv) protect(t *testing.T) (*ocv1.ClusterObjectSet, *ocv1.ClusterObjectSet) {
	t.Helper()
	adopting := e.rollout(t, e.ext.Name+"-1")
	result, err := e.ce.Reconcile(e.ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(e.ext)})
	require.NoError(t, err)
	require.Equal(t, 10*time.Second, result.RequeueAfter)
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		cod := &ocv1.ClusterObjectDeployment{}
		if assert.NoError(collect, e.cached.Get(e.ctx, client.ObjectKeyFromObject(e.ext), cod)) {
			assert.Equal(collect, labels.HelmMigrationProtecting, cod.Annotations[labels.HelmMigrationKey])
		}
	}, 5*time.Second, 10*time.Millisecond)
	protecting := e.rollout(t, e.ext.Name+"-2")
	result, err = e.ce.Reconcile(e.ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(e.ext)})
	require.NoError(t, err)
	require.Zero(t, result.RequeueAfter)
	return adopting, protecting
}
