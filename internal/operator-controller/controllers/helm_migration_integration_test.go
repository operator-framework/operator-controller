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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
	"pkg.package-operator.run/boxcutter/managedcache"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-registry/v2/alpha/declcfg"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	ocv1ac "github.com/operator-framework/operator-controller/applyconfigurations/api/v1"
	coscontrollers "github.com/operator-framework/operator-controller/internal/object-controller/controllers"
	ac "github.com/operator-framework/operator-controller/internal/operator-controller/action"
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
)

func TestHelmMigrationAPIAdoptsLiveResource(t *testing.T) {
	// Given a live Helm-managed ConfigMap and a stored Adopting deployment.
	e := newHelmMigrationEnv(t)
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
		fieldOperation             metav1.ManagedFieldsOperationType
	}{
		deploymentOwnedByExtension: true,
		revisionOwnedByDeployment:  true,
		resourceOwnedByRevision:    true,
		resourceUID:                input.resource.UID,
		fieldManager:               "cos-group/" + input.extension.Name,
		fieldOperation:             metav1.ManagedFieldsOperationApply,
	}

	// When the production controllers complete the adopting revision; migration writes are forbidden.
	cos := e.rollout(t, input.revisionName)

	// Then the original resource survives under the CE-owned COD and COS and SSA management.
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
		if mf.Operation == want.fieldOperation {
			managers = append(managers, mf.Manager)
		}
	}
	require.Contains(t, managers, want.fieldManager)
}

func TestHelmMigrationAPIProtectsSiblingHandoff(t *testing.T) {
	// Given the initial adopting deployment and its stored template.
	e := newHelmMigrationEnv(t)
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
		deploymentOwnedByExtension       bool
	}{
		adoptingProtection:               ocv1.CollisionProtectionNone,
		protectingProtection:             ocv1.CollisionProtectionPrevent,
		template:                         input.storedTemplate,
		resourceUID:                      input.resource.UID,
		resourceOwnedByProtectedRevision: true,
		deploymentOwnedByExtension:       true,
	}

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
	require.Equal(t, want.deploymentOwnedByExtension, metav1.IsControlledBy(cod, e.ext))
}

func TestHelmMigrationProtectionApplyConflictsWithConcurrentUpdate(t *testing.T) {
	e := newHelmMigrationEnv(t)
	input := struct {
		revision        string
		key             client.ObjectKey
		fieldOwner      string
		annotationKey   string
		annotationValue string
	}{
		revision:        e.ext.Name + "-1",
		key:             client.ObjectKeyFromObject(e.ext),
		fieldOwner:      "test-ce-applier",
		annotationKey:   "test.example.org/concurrent",
		annotationValue: "updated",
	}
	want := struct {
		conflict       bool
		protection     ocv1.CollisionProtection
		annotation     string
		historyReadErr error
	}{
		conflict:       true,
		protection:     ocv1.CollisionProtectionNone,
		annotation:     input.annotationValue,
		historyReadErr: nil,
	}
	e.rollout(t, input.revision)
	writer := mockctrlclient.NewMockClient(gomock.NewController(t))
	writer.EXPECT().Apply(gomock.Any(), gomock.AssignableToTypeOf(&ocv1ac.ClusterObjectDeploymentApplyConfiguration{}),
		client.FieldOwner(input.fieldOwner), client.ForceOwnership).
		DoAndReturn(func(ctx context.Context, config runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
			concurrent := &ocv1.ClusterObjectDeployment{}
			require.NoError(t, e.live.Get(ctx, input.key, concurrent))
			concurrent.Annotations = map[string]string{input.annotationKey: input.annotationValue}
			require.NoError(t, e.live.Update(ctx, concurrent))
			return e.live.Apply(ctx, config, opts...)
		})
	m := &applier.BoxcutterStorageMigrator{
		Client: writer, APIReader: e.live, SystemNamespace: e.helmIndex.Namespace, FieldOwner: input.fieldOwner,
	}

	_, err := m.Migrate(e.ctx, e.ext, nil)

	require.Equal(t, want.conflict, apierrors.IsConflict(err), "got %v", err)
	cod := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, e.live.Get(e.ctx, input.key, cod))
	require.Equal(t, want.protection, cod.Spec.Template.Spec.CollisionProtection)
	require.Equal(t, want.annotation, cod.Annotations[input.annotationKey])
	require.ErrorIs(t, e.live.Get(e.ctx, client.ObjectKeyFromObject(e.helmIndex), &corev1.Secret{}), want.historyReadErr)
}

func TestHelmMigrationAPIArchivalPreservesProtectedResource(t *testing.T) {
	// Given completed migration with the resource owned by the protected sibling.
	e := newHelmMigrationEnv(t)
	want := struct {
		lifecycleState                   ocv1.ClusterObjectSetLifecycleState
		readyReason                      string
		resourceUID                      types.UID
		resourceOwnedByProtectedRevision bool
	}{
		lifecycleState:                   ocv1.ClusterObjectSetLifecycleStateArchived,
		readyReason:                      ocv1.ClusterObjectSetReasonArchived,
		resourceUID:                      e.resource.UID,
		resourceOwnedByProtectedRevision: true,
	}
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
	e := newHelmMigrationEnv(t)
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
		afterCleanup     bool
		requestedVersion string
		installed        *ocv1.BundleMetadata
	}
	wantResolutions := []resolution{{
		afterCleanup:     true,
		requestedVersion: requestedVersion,
		installed:        &ocv1.BundleMetadata{Name: "test.v1.0.0", Version: "1.0.0", Release: ptr.To("")},
	}}
	wantRequeues := []time.Duration{0, 0, 10 * time.Second}
	wantDeploymentUID := staged.UID
	wantBundleName := target.Name
	wantResourceUID := e.resource.UID
	wantResourceData := maps.Clone(manifest.Data)
	wantHistoryGone := true
	wantCleanupMessage := "Helm migration: waiting for Helm release history cleanup to finish."

	scheme := e.live.Scheme()
	provider := mockapplier.NewMockManifestProvider(mockCtrl)
	provider.EXPECT().Get(bundle, gomock.Any()).Return([]client.Object{manifest}, nil)
	appl := &applier.Boxcutter{Client: e.cached, Scheme: scheme, FieldOwner: "test-ce-applier",
		RevisionGenerator: &applier.SimpleRevisionGenerator{Scheme: scheme, SystemNamespace: e.resource.Namespace, ManifestProvider: provider}}
	var resolutions []resolution
	cleanupObserved := false
	resolver := resolve.Func(func(_ context.Context, ext *ocv1.ClusterExtension, installed *ocv1.BundleMetadata) (*declcfg.Bundle, *declcfg.VersionRelease, *declcfg.Deprecation, error) {
		resolutions = append(resolutions, resolution{
			afterCleanup:     cleanupObserved,
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

	// Restart with fresh dependencies after the protected rollout. Cleanup
	// can proceed even though the release decoder and generator are absent.
	m := &applier.BoxcutterStorageMigrator{
		Client: e.cached, APIReader: e.live, SystemNamespace: e.resource.Namespace,
	}
	e.ce = &controllers.ClusterExtensionReconciler{Client: e.live, ReconcileSteps: append(controllers.ReconcileSteps{controllers.MigrateStorage(m)}, ordinarySteps...)}
	reconcilePending()
	require.NoError(t, e.live.Get(e.ctx, request.NamespacedName, &ext))
	progress := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.ClusterExtensionTypeProgressing)
	require.NotNil(t, progress)
	require.Equal(t, wantCleanupMessage, progress.Message)
	var protected ocv1.ClusterObjectDeployment
	require.NoError(t, e.live.Get(e.ctx, request.NamespacedName, &protected))
	e.checkHelmHistoryGone(t, wantHistoryGone)
	cleanupObserved = true
	_, err := e.ce.Reconcile(e.ctx, request)
	require.NoError(t, err)
	upgraded := e.rollout(t, ext.Name+"-3")
	var liveResource corev1.ConfigMap
	require.NoError(t, e.live.Get(e.ctx, client.ObjectKeyFromObject(e.resource), &liveResource))

	require.Equal(t, wantRequeues, requeues)
	require.Equal(t, wantDeploymentUID, protected.UID)
	require.Equal(t, wantResolutions, resolutions)
	require.Equal(t, wantBundleName, upgraded.Annotations[labels.BundleNameKey])
	require.Equal(t, wantResourceUID, liveResource.UID)
	require.Equal(t, wantResourceData, liveResource.Data)
}

type helmMigrationEnv struct {
	ctx       context.Context
	live      client.Client
	cached    client.Client
	ext       *ocv1.ClusterExtension
	helmIndex *corev1.Secret
	resource  *corev1.ConfigMap
	ce        *controllers.ClusterExtensionReconciler
	cod       *coscontrollers.ClusterObjectDeploymentReconciler
	cos       *coscontrollers.ClusterObjectSetReconciler
}

// newHelmMigrationEnv creates a legacy CE and a live Helm-managed ConfigMap on
// envtest's real API server. It starts indexed/tracking caches and runs the first
// CE migration reconcile, storing the Adopting COD and content Secrets. It does
// not roll out a COS. COD/COS reconcilers use the production revision engine.
func newHelmMigrationEnv(t *testing.T) *helmMigrationEnv {
	t.Helper()
	wantInitialRequeue := time.Duration(0)
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
	rel := &release.Release{Name: ext.Name, Version: 1, Info: &release.Info{Status: release.StatusDeployed}, Manifest: string(manifest) + "\n",
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
	core, err := kubernetes.NewForConfig(config)
	require.NoError(t, err)
	releaseDriver := ac.ChunkedStorageDriverMapper(core.CoreV1(), live, ns.Name)
	legacyOwner := ext.DeepCopy()
	legacyOwner.TypeMeta = metav1.TypeMeta{APIVersion: ocv1.GroupVersion.String(), Kind: ocv1.ClusterExtensionKind}
	legacyDriver, err := releaseDriver(ctx, legacyOwner, nil)
	require.NoError(t, err)
	indexName := "sh.helm.release.v1." + ext.Name + ".v1"
	require.NoError(t, legacyDriver.Create(indexName, rel))
	index := &corev1.Secret{}
	require.NoError(t, live.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: indexName}, index))
	m := &applier.BoxcutterStorageMigrator{
		Client: cached, APIReader: live, Scheme: scheme, SystemNamespace: ns.Name, FieldOwner: "test-ce-applier",
		ReleaseDriver:     releaseDriver,
		RevisionGenerator: &applier.SimpleRevisionGenerator{Scheme: scheme, SystemNamespace: ns.Name},
	}
	env := &helmMigrationEnv{ctx: ctx, live: live, cached: cached, ext: ext, resource: resource, helmIndex: index,
		ce:  &controllers.ClusterExtensionReconciler{Client: live, ReconcileSteps: controllers.ReconcileSteps{controllers.MigrateStorage(m)}},
		cod: &coscontrollers.ClusterObjectDeploymentReconciler{Client: cached, Scheme: scheme},
		cos: &coscontrollers.ClusterObjectSetReconciler{Client: cached, RevisionEngineFactory: factory, TrackingCache: tracking},
	}
	result, err := env.ce.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(ext)})
	require.NoError(t, err)
	require.Equal(t, wantInitialRequeue, result.RequeueAfter)
	// Do not let cache latency or subsequent reconciles trigger another Helm read.
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		cod := &ocv1.ClusterObjectDeployment{}
		assert.NoError(collect, cached.Get(ctx, client.ObjectKeyFromObject(ext), cod))
	}, 5*time.Second, 10*time.Millisecond)
	return env
}

// rollout repeatedly runs the production COD/COS reconcilers until the named
// revision records real completion and the COD observes current-generation
// success. It relies on API-assigned UIDs/generations and server-side apply;
// it never manufactures readiness status.
func (e *helmMigrationEnv) rollout(t *testing.T, revision string) *ocv1.ClusterObjectSet {
	t.Helper()
	wantComplete := true
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
		assert.Equal(collect, wantComplete, !cos.Status.CompletedAt.IsZero(), "real readiness must record completion")
	}, 10*time.Second, 25*time.Millisecond)
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		_, err := e.cod.Reconcile(e.ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(e.ext)})
		assert.NoError(collect, err)
		cod := &ocv1.ClusterObjectDeployment{}
		if assert.NoError(collect, e.cached.Get(e.ctx, client.ObjectKeyFromObject(e.ext), cod)) {
			assert.Equal(collect, wantComplete, deploymentutil.RolloutComplete(cod))
		}
	}, 5*time.Second, 25*time.Millisecond)
	return &cos
}

// protect completes adoption, reconciles the CE to enable protection, completes
// the protected revision, then reconciles the CE to delete Helm history.
func (e *helmMigrationEnv) protect(t *testing.T) (*ocv1.ClusterObjectSet, *ocv1.ClusterObjectSet) {
	t.Helper()
	adoptingName := e.ext.Name + "-1"
	protectingName := e.ext.Name + "-2"
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(e.ext)}
	want := struct {
		rolloutRequeue        time.Duration
		cleanupRequeue        time.Duration
		afterCleanupRequeue   time.Duration
		protection            ocv1.CollisionProtection
		historyBeforeComplete error
		historyGone           bool
	}{
		rolloutRequeue:        0,
		cleanupRequeue:        10 * time.Second,
		afterCleanupRequeue:   0,
		protection:            ocv1.CollisionProtectionPrevent,
		historyBeforeComplete: nil,
		historyGone:           true,
	}

	adopting := e.rollout(t, adoptingName)
	result, err := e.ce.Reconcile(e.ctx, request)
	require.NoError(t, err)
	require.Equal(t, want.rolloutRequeue, result.RequeueAfter)
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		cod := &ocv1.ClusterObjectDeployment{}
		if assert.NoError(collect, e.cached.Get(e.ctx, client.ObjectKeyFromObject(e.ext), cod)) {
			assert.Equal(collect, want.protection, cod.Spec.Template.Spec.CollisionProtection)
		}
	}, 5*time.Second, 10*time.Millisecond)
	require.ErrorIs(t, e.live.Get(e.ctx, client.ObjectKeyFromObject(e.helmIndex), &corev1.Secret{}), want.historyBeforeComplete)
	protecting := e.rollout(t, protectingName)
	result, err = e.ce.Reconcile(e.ctx, request)
	require.NoError(t, err)
	require.Equal(t, want.cleanupRequeue, result.RequeueAfter)
	e.checkHelmHistoryGone(t, want.historyGone)
	result, err = e.ce.Reconcile(e.ctx, request)
	require.NoError(t, err)
	require.Equal(t, want.afterCleanupRequeue, result.RequeueAfter)
	return adopting, protecting
}

func (e *helmMigrationEnv) checkHelmHistoryGone(t *testing.T, wantGone bool) {
	t.Helper()
	err := e.live.Get(e.ctx, client.ObjectKeyFromObject(e.helmIndex), &corev1.Secret{})
	require.Equal(t, wantGone, apierrors.IsNotFound(err), "reading Helm history: %v", err)
}
