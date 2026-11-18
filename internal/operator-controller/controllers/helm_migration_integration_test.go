//go:build !standard

package controllers_test

import (
	"context"
	"encoding/json"
	"maps"
	"testing"
	"time"

	bsemver "github.com/blang/semver/v4"
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
	"k8s.io/utils/ptr"
	"pkg.package-operator.run/boxcutter/managedcache"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-registry/v2/alpha/declcfg"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	coscontrollers "github.com/operator-framework/operator-controller/internal/object-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	deploymentutil "github.com/operator-framework/operator-controller/internal/operator-controller/clusterobjectdeployment"
	"github.com/operator-framework/operator-controller/internal/operator-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/resolve"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
	imageutil "github.com/operator-framework/operator-controller/internal/shared/util/image"
	bundlecsv "github.com/operator-framework/operator-controller/internal/testing/bundle/csv"
	bundlefs "github.com/operator-framework/operator-controller/internal/testing/bundle/fs"
	mockapplier "github.com/operator-framework/operator-controller/internal/testutil/mock/applier"
	mockctrlclient "github.com/operator-framework/operator-controller/internal/testutil/mock/ctrlclient"
	mockhelmclient "github.com/operator-framework/operator-controller/internal/testutil/mock/helmclient"
)

func TestHelmMigrationAPIAdoptsLiveResource(t *testing.T) {
	// Given a live Helm-managed ConfigMap and a stored Adopting deployment.
	api := mockctrlclient.NewMockClient(gomock.NewController(t))
	e := newHelmMigrationEnv(t, api)
	input := struct {
		extension    *ocv1.ClusterExtension
		resource     *corev1.ConfigMap
		revisionName string
	}{
		extension:    e.ext,
		resource:     e.resource,
		revisionName: e.ext.Name + "-1",
	}
	want := struct {
		deploymentOwnedByExtension bool
		revisionOwnedByDeployment  bool
		resourceOwnedByRevision    bool
		resourceUID                types.UID
		fieldManager               string
	}{
		deploymentOwnedByExtension: false,
		revisionOwnedByDeployment:  true,
		resourceOwnedByRevision:    true,
		resourceUID:                input.resource.UID,
		fieldManager:               "cos-group/" + input.extension.Name,
	}

	// When the production controllers complete the adopting revision; migration writes are forbidden.
	cos := e.rollout(t, input.revisionName)

	// Then the original resource survives under the staged COD and COS and SSA management.
	cod := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, e.live.Get(e.ctx, client.ObjectKeyFromObject(input.extension), cod))
	require.Equal(t, want.deploymentOwnedByExtension, metav1.IsControlledBy(cod, input.extension))
	require.Equal(t, want.revisionOwnedByDeployment, metav1.IsControlledBy(cos, cod))
	resource := &corev1.ConfigMap{}
	require.NoError(t, e.live.Get(e.ctx, client.ObjectKeyFromObject(input.resource), resource))
	require.Equal(t, want.resourceUID, resource.UID)
	require.Equal(t, want.resourceOwnedByRevision, metav1.IsControlledBy(resource, cos))
	var managers []string
	for _, mf := range resource.ManagedFields {
		if mf.Operation == metav1.ManagedFieldsOperationApply {
			managers = append(managers, mf.Manager)
		}
	}
	require.Contains(t, managers, want.fieldManager)
}

func TestHelmMigrationAPIProtectsSiblingHandoff(t *testing.T) {
	// Given the initial adopting deployment, allow one patch for protection and one for ownership.
	api := mockctrlclient.NewMockClient(gomock.NewController(t))
	e := newHelmMigrationEnv(t, api)
	deployment := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, e.live.Get(e.ctx, client.ObjectKeyFromObject(e.ext), deployment))
	input := struct {
		extension      *ocv1.ClusterExtension
		resource       *corev1.ConfigMap
		storedTemplate *ocv1.ClusterObjectDeploymentTemplate
	}{
		extension:      e.ext,
		resource:       e.resource,
		storedTemplate: deployment.Spec.Template.DeepCopy(),
	}
	want := struct {
		adoptingProtection               ocv1.CollisionProtection
		protectingProtection             ocv1.CollisionProtection
		template                         *ocv1.ClusterObjectDeploymentTemplate
		resourceUID                      types.UID
		resourceOwnedByProtectedRevision bool
		deploymentClaimed                bool
		transitionPatches                int
	}{
		adoptingProtection:               ocv1.CollisionProtectionNone,
		protectingProtection:             ocv1.CollisionProtectionPrevent,
		template:                         input.storedTemplate,
		resourceUID:                      input.resource.UID,
		resourceOwnedByProtectedRevision: true,
		deploymentClaimed:                true,
		transitionPatches:                2,
	}
	api.EXPECT().Patch(gomock.Any(), gomock.AssignableToTypeOf(&ocv1.ClusterObjectDeployment{}), gomock.Any()).
		DoAndReturn(e.cached.Patch).Times(want.transitionPatches)

	// When adoption hands off to the protected sibling and migration finishes.
	adopting, protecting := e.protect(t)

	// Then the content is identical and the same resource belongs to the protected revision.
	require.Equal(t, want.adoptingProtection, adopting.Spec.CollisionProtection)
	require.Equal(t, want.protectingProtection, protecting.Spec.CollisionProtection)
	require.Equal(t, want.template.Spec.Phases, adopting.Spec.Phases)
	require.Equal(t, want.template.Spec.Phases, protecting.Spec.Phases)
	require.Equal(t, want.template.Metadata.Annotations, adopting.Annotations)
	require.Equal(t, want.template.Metadata.Annotations, protecting.Annotations)
	require.Equal(t, want.template.Spec.ProgressionProbes, adopting.Spec.ProgressionProbes)
	require.Equal(t, want.template.Spec.ProgressionProbes, protecting.Spec.ProgressionProbes)
	resource := &corev1.ConfigMap{}
	require.NoError(t, e.live.Get(e.ctx, client.ObjectKeyFromObject(input.resource), resource))
	require.Equal(t, want.resourceUID, resource.UID)
	require.Equal(t, want.resourceOwnedByProtectedRevision, metav1.IsControlledBy(resource, protecting))
	cod := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, e.live.Get(e.ctx, client.ObjectKeyFromObject(input.extension), cod))
	require.Equal(t, want.deploymentClaimed, metav1.IsControlledBy(cod, e.ext))
}

func TestHelmMigrationAPIArchivalPreservesProtectedResource(t *testing.T) {
	// Given completed migration with the resource owned by the protected sibling.
	api := mockctrlclient.NewMockClient(gomock.NewController(t))
	e := newHelmMigrationEnv(t, api)
	want := struct {
		transitionPatches                int
		lifecycleState                   ocv1.ClusterObjectSetLifecycleState
		readyReason                      string
		resourceUID                      types.UID
		resourceOwnedByProtectedRevision bool
	}{
		transitionPatches:                2,
		lifecycleState:                   ocv1.ClusterObjectSetLifecycleStateArchived,
		readyReason:                      ocv1.ClusterObjectSetReasonArchived,
		resourceUID:                      e.resource.UID,
		resourceOwnedByProtectedRevision: true,
	}
	api.EXPECT().Patch(gomock.Any(), gomock.AssignableToTypeOf(&ocv1.ClusterObjectDeployment{}), gomock.Any()).
		DoAndReturn(e.cached.Patch).Times(want.transitionPatches)
	adopting, protecting := e.protect(t)
	input := struct {
		archivedRevision  *ocv1.ClusterObjectSet
		protectedRevision *ocv1.ClusterObjectSet
		resource          *corev1.ConfigMap
		timeout           time.Duration
		pollInterval      time.Duration
	}{
		archivedRevision:  adopting,
		protectedRevision: protecting,
		resource:          e.resource,
		timeout:           5 * time.Second,
		pollInterval:      25 * time.Millisecond,
	}

	// When the production COS controller reconciles the old adopting revision.
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		_, err := e.cos.Reconcile(e.ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: input.archivedRevision.Name}})
		assert.NoError(collect, err)
		actual := &ocv1.ClusterObjectSet{}
		if !assert.NoError(collect, e.live.Get(e.ctx, client.ObjectKeyFromObject(input.archivedRevision), actual)) {
			return
		}
		assert.Equal(collect, want.lifecycleState, actual.Spec.LifecycleState)
		ready := apimeta.FindStatusCondition(actual.Status.Conditions, ocv1.ClusterObjectSetTypeReady)
		if assert.NotNil(collect, ready) {
			assert.Equal(collect, want.readyReason, ready.Reason)
		}
	}, input.timeout, input.pollInterval)

	// Then archival has completed without deleting or taking ownership from the protected resource.
	resource := &corev1.ConfigMap{}
	require.NoError(t, e.live.Get(e.ctx, client.ObjectKeyFromObject(input.resource), resource))
	require.Equal(t, want.resourceUID, resource.UID)
	require.Equal(t, want.resourceOwnedByProtectedRevision, metav1.IsControlledBy(resource, input.protectedRevision))
}

// Stage a CE edit between controller reconciles so a fast cluster cannot miss
// the timing window between the two migration rollouts.
func TestHelmMigrationRollsOutPendingUpgradeAfterRestart(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	api := mockctrlclient.NewMockClient(mockCtrl)
	e := newHelmMigrationEnv(t, api)
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(e.ext)}
	var staged ocv1.ClusterObjectDeployment
	require.NoError(t, e.live.Get(e.ctx, request.NamespacedName, &staged))

	requestedVersion := "2.0.0"
	target := &declcfg.Bundle{Name: "test.v2.0.0", Package: "test", Image: "example.com/bundle:v2"}
	version := &declcfg.VersionRelease{Version: bsemver.MustParse(requestedVersion)}
	bundle := bundlefs.Builder().WithPackageName(target.Package).WithCSV(bundlecsv.Builder().WithName(target.Name).Build()).Build()
	manifest := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: e.resource.Name, Namespace: e.resource.Namespace},
		Data: map[string]string{"value": "updated"}}

	type resolution struct {
		afterClaim       bool
		requestedVersion string
		installed        *ocv1.BundleMetadata
	}
	wantResolutions := []resolution{{
		afterClaim:       true,
		requestedVersion: requestedVersion,
		installed:        &ocv1.BundleMetadata{Name: "test.v1.0.0", Version: "1.0.0", Release: ptr.To("")},
	}}
	wantRequeues := []time.Duration{10 * time.Second, 10 * time.Second, 10 * time.Second}
	wantDeploymentUID := staged.UID
	wantBundleName := target.Name
	wantResourceUID := e.resource.UID
	wantResourceData := maps.Clone(manifest.Data)

	scheme := e.live.Scheme()
	provider := mockapplier.NewMockManifestProvider(mockCtrl)
	provider.EXPECT().Get(bundle, gomock.Any()).Return([]client.Object{manifest}, nil)
	appl := &applier.Boxcutter{Client: e.cached, Scheme: scheme, FieldOwner: "test-ce-applier",
		RevisionGenerator: &applier.SimpleRevisionGenerator{Scheme: scheme, SystemNamespace: e.resource.Namespace, ManifestProvider: provider}}
	var resolutions []resolution
	claimObserved := false
	resolver := resolve.Func(func(_ context.Context, ext *ocv1.ClusterExtension, installed *ocv1.BundleMetadata) (*declcfg.Bundle, *declcfg.VersionRelease, *declcfg.Deprecation, error) {
		resolutions = append(resolutions, resolution{
			afterClaim:       claimObserved,
			requestedVersion: ext.Spec.Source.Catalog.Version,
			installed:        installed.DeepCopy(),
		})
		return target, version, nil, nil
	})
	ordinarySteps := controllers.ReconcileSteps{
		controllers.RetrieveRevisionStates(&controllers.BoxcutterRevisionStatesGetter{Reader: e.cached}),
		controllers.ResolveBundle(resolver, e.cached),
		controllers.UnpackBundle(&imageutil.FakePuller{ImageFS: bundle}, nil),
		controllers.ApplyBundleWithBoxcutter(appl.Apply),
	}
	e.ce.ReconcileSteps = append(e.ce.ReconcileSteps, ordinarySteps...)
	api.EXPECT().Patch(gomock.Any(), gomock.AssignableToTypeOf(&staged), gomock.Any()).DoAndReturn(e.cached.Patch)
	var ext ocv1.ClusterExtension
	require.NoError(t, e.live.Get(e.ctx, request.NamespacedName, &ext))
	ext.Spec.Source.Catalog.Version = requestedVersion
	require.NoError(t, e.live.Update(e.ctx, &ext))

	var requeues []time.Duration
	reconcilePending := func() {
		t.Helper()
		result, err := e.ce.Reconcile(e.ctx, request)
		require.NoError(t, err)
		requeues = append(requeues, result.RequeueAfter)
	}
	reconcilePending()
	e.rollout(t, ext.Name+"-1")
	reconcilePending()
	e.rollout(t, ext.Name+"-2")

	// Restart after the protected rollout. The fresh reconciler claims the
	// deployment before resolving the pending upgrade against installed v1.
	m := &applier.BoxcutterStorageMigrator{Client: e.cached, Scheme: scheme}
	e.ce = &controllers.ClusterExtensionReconciler{Client: e.live, ReconcileSteps: append(controllers.ReconcileSteps{controllers.MigrateStorage(m)}, ordinarySteps...)}
	reconcilePending()
	var claimed ocv1.ClusterObjectDeployment
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		if assert.NoError(collect, e.cached.Get(e.ctx, request.NamespacedName, &claimed)) {
			assert.True(collect, metav1.IsControlledBy(&claimed, &ext))
		}
	}, 5*time.Second, 10*time.Millisecond)
	claimObserved = true
	_, err := e.ce.Reconcile(e.ctx, request)
	require.NoError(t, err)
	upgraded := e.rollout(t, ext.Name+"-3")
	var liveResource corev1.ConfigMap
	require.NoError(t, e.live.Get(e.ctx, client.ObjectKeyFromObject(e.resource), &liveResource))

	require.Equal(t, wantRequeues, requeues)
	require.Equal(t, wantDeploymentUID, claimed.UID)
	require.Equal(t, wantResolutions, resolutions)
	require.Equal(t, wantBundleName, upgraded.Annotations[labels.BundleNameKey])
	require.Equal(t, wantResourceUID, liveResource.UID)
	require.Equal(t, wantResourceData, liveResource.Data)
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
	ext.TypeMeta = metav1.TypeMeta{} // Match the typed API reader used by the CE reconciler.
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
	ceAPI.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(ext), gomock.AssignableToTypeOf(ext)).DoAndReturn(live.Get).AnyTimes()
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
// the protected revision, then reconciles the CE to authorize and claim the deployment.
// Transition patches must already be permitted by the calling test.
func (e *helmMigrationEnv) protect(t *testing.T) (*ocv1.ClusterObjectSet, *ocv1.ClusterObjectSet) {
	t.Helper()
	adopting := e.rollout(t, e.ext.Name+"-1")
	result, err := e.ce.Reconcile(e.ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(e.ext)})
	require.NoError(t, err)
	require.Equal(t, 10*time.Second, result.RequeueAfter)
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		cod := &ocv1.ClusterObjectDeployment{}
		if assert.NoError(collect, e.cached.Get(e.ctx, client.ObjectKeyFromObject(e.ext), cod)) {
			assert.Equal(collect, ocv1.CollisionProtectionPrevent, cod.Spec.Template.Spec.CollisionProtection)
		}
	}, 5*time.Second, 10*time.Millisecond)
	protecting := e.rollout(t, e.ext.Name+"-2")
	result, err = e.ce.Reconcile(e.ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(e.ext)})
	require.NoError(t, err)
	require.Equal(t, 10*time.Second, result.RequeueAfter)
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		observed := &ocv1.ClusterObjectDeployment{}
		if assert.NoError(collect, e.cached.Get(e.ctx, client.ObjectKeyFromObject(e.ext), observed)) {
			assert.True(collect, metav1.IsControlledBy(observed, e.ext))
		}
	}, 5*time.Second, 10*time.Millisecond)
	result, err = e.ce.Reconcile(e.ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(e.ext)})
	require.NoError(t, err)
	require.Zero(t, result.RequeueAfter)
	return adopting, protecting
}
