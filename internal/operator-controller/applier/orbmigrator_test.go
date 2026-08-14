package applier_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	orbv1alpha1 "github.com/joelanford/orb-operator/api/v1alpha1"
	orbac "github.com/joelanford/orb-operator/applyconfigurations/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	orb "github.com/operator-framework/operator-controller/internal/operator-controller/applier/orb"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
	mockhelmclient "github.com/operator-framework/operator-controller/internal/testutil/mock/helmclient"
)

func orbMigratorScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, orbv1alpha1.AddToScheme(scheme))
	require.NoError(t, ocv1.AddToScheme(scheme))
	return scheme
}

// orbActionGetterConfig configures the fake Helm action client used by the
// migrator tests.
type orbActionGetterConfig struct {
	getRel     *release.Release
	getErr     error
	history    []*release.Release
	historyErr error
	storage    *storage.Storage
}

func newOrbActionGetter(ctrl *gomock.Controller, cfg orbActionGetterConfig) *mockhelmclient.MockActionClientGetterAndInterface {
	m := mockhelmclient.NewMockActionClientGetterAndInterface(ctrl)
	m.EXPECT().ActionClientFor(gomock.Any(), gomock.Any()).Return(m, nil).AnyTimes()
	m.EXPECT().Get(gomock.Any(), gomock.Any()).Return(cfg.getRel, cfg.getErr).AnyTimes()
	m.EXPECT().History(gomock.Any(), gomock.Any()).Return(cfg.history, cfg.historyErr).AnyTimes()
	if cfg.storage != nil {
		m.EXPECT().Config().Return(&action.Configuration{Releases: cfg.storage}).AnyTimes()
	} else {
		m.EXPECT().Config().Return(nil).AnyTimes()
	}
	return m
}

const testExtName = "my-ext"

func deployedRelease(version int, manifest string) *release.Release {
	return &release.Release{
		Name:     testExtName,
		Version:  version,
		Manifest: manifest,
		Info:     &release.Info{Status: release.StatusDeployed},
		Labels: map[string]string{
			labels.BundleNameKey:      testExtName + ".v1.0.0",
			labels.PackageNameKey:     testExtName,
			labels.BundleVersionKey:   "1.0.0",
			labels.BundleReferenceKey: "example.com/" + testExtName + "@sha256:abc",
		},
	}
}

// Helm release manifests in this codebase are stored as one JSON object per
// line (see splitManifestDocuments), so tests use that format.
const cmManifest = `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"my-config","namespace":"my-ns"},"data":{"key":"value"}}`

// typedCOSFromAC converts a ClusterObjectSet apply configuration into a typed
// ClusterObjectSet, so a matching existing revision can be seeded into the fake
// client for reuse/idempotency tests.
func typedCOSFromAC(t *testing.T, ac *orbac.ClusterObjectSetApplyConfiguration) *orbv1alpha1.ClusterObjectSet {
	t.Helper()
	raw, err := json.Marshal(ac)
	require.NoError(t, err)
	cos := &orbv1alpha1.ClusterObjectSet{}
	require.NoError(t, json.Unmarshal(raw, cos))
	return cos
}

// seededRevision builds the adopting revision the migrator would create for the
// given release/labels at the given revision number, as a typed ClusterObjectSet
// suitable for seeding into the fake client.
func seededRevision(t *testing.T, ext *ocv1.ClusterExtension, rel *release.Release, objLbls map[string]string) *orbv1alpha1.ClusterObjectSet {
	t.Helper()
	gen := &applier.SimpleOrbRevisionGenerator{}
	ac, err := gen.GenerateRevisionFromHelmRelease(context.Background(), rel, ext, objLbls)
	require.NoError(t, err)
	ac.WithName(fmt.Sprintf("%s-1", ext.Name))
	ac.Spec.WithRevision(1)
	externalized, _, err := orb.ExternalizeCOS(ac)
	require.NoError(t, err)
	return typedCOSFromAC(t, externalized)
}

func objectLabelsFor(ext *ocv1.ClusterExtension) map[string]string {
	return map[string]string{
		labels.OwnerKindKey: ocv1.ClusterExtensionKind,
		labels.OwnerNameKey: ext.GetName(),
	}
}

// --- Generator tests ---

func TestSimpleOrbRevisionGenerator_SingleAssertionFreePhase(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "my-ext"}}
	rel := deployedRelease(1, cmManifest)

	gen := &applier.SimpleOrbRevisionGenerator{}
	cos, err := gen.GenerateRevisionFromHelmRelease(context.Background(), rel, ext, objectLabelsFor(ext))
	require.NoError(t, err)

	require.NotNil(t, cos.Spec)
	assert.Equal(t, ext.Name, *cos.Spec.Group)
	assert.Equal(t, orbv1alpha1.LifecycleStateActive, *cos.Spec.LifecycleState)
	assert.Equal(t, orbv1alpha1.CollisionProtectionNone, *cos.Spec.CollisionProtection)

	require.Len(t, cos.Spec.Phases, 1)
	assert.Len(t, cos.Spec.Phases[0].Objects, 1)
	for _, obj := range cos.Spec.Phases[0].Objects {
		assert.Empty(t, obj.Assertions, "adopting phase objects must have no assertions")
	}

	// Owner labels present, bundle annotations present, no template-hash label.
	assert.Equal(t, ocv1.ClusterExtensionKind, cos.Labels[labels.OwnerKindKey])
	assert.Equal(t, ext.Name, cos.Labels[labels.OwnerNameKey])
	assert.Empty(t, cos.Labels["orb.operatorframework.io/template-hash"])
	assert.Equal(t, "my-ext.v1.0.0", cos.Annotations[labels.BundleNameKey])
	assert.Equal(t, "1.0.0", cos.Annotations[labels.BundleVersionKey])
}

func TestSimpleOrbRevisionGenerator_SplitsAtObjectCap(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "my-ext"}}

	// Build a manifest with 120 objects: expect 3 assertion-free phases (50/50/20).
	var sb strings.Builder
	for i := range 120 {
		if i > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"cm-%d","namespace":"ns"}}`, i)
	}
	rel := deployedRelease(1, sb.String())

	gen := &applier.SimpleOrbRevisionGenerator{}
	cos, err := gen.GenerateRevisionFromHelmRelease(context.Background(), rel, ext, objectLabelsFor(ext))
	require.NoError(t, err)

	require.Len(t, cos.Spec.Phases, 3)
	total := 0
	for _, p := range cos.Spec.Phases {
		assert.LessOrEqual(t, len(p.Objects), 50)
		for _, obj := range p.Objects {
			assert.Empty(t, obj.Assertions, "no per-GVK assertions on migration phases")
		}
		total += len(p.Objects)
	}
	assert.Equal(t, 120, total)
}

// --- Migrator tests ---

func TestOrbStorageMigrator_NoDeployedRelease_NoOp(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "my-ext"}}
	ctrl := gomock.NewController(t)

	ag := newOrbActionGetter(ctrl, orbActionGetterConfig{getErr: driver.ErrReleaseNotFound})

	var applied int
	fakeClient := fake.NewClientBuilder().
		WithScheme(orbMigratorScheme(t)).
		WithInterceptorFuncs(interceptor.Funcs{
			Apply: func(context.Context, client.WithWatch, runtime.ApplyConfiguration, ...client.ApplyOption) error {
				applied++
				return nil
			},
		}).Build()

	m := &applier.OrbStorageMigrator{
		ActionClientGetter: ag,
		RevisionGenerator:  &applier.SimpleOrbRevisionGenerator{},
		Client:             fakeClient,
		Scheme:             orbMigratorScheme(t),
		FieldOwner:         "test-owner",
	}

	res, err := m.Migrate(context.Background(), ext, objectLabelsFor(ext))
	require.NoError(t, err)
	assert.Nil(t, res, "pipeline should proceed when nothing to migrate")
	assert.Zero(t, applied, "no revision should be created")
}

func TestOrbStorageMigrator_DeployedNoCOS_CreatesRevision1(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "my-ext", UID: "ext-uid"}}
	ctrl := gomock.NewController(t)

	ag := newOrbActionGetter(ctrl, orbActionGetterConfig{getRel: deployedRelease(1, cmManifest)})

	var appliedCOS *orbac.ClusterObjectSetApplyConfiguration
	fakeClient := fake.NewClientBuilder().
		WithScheme(orbMigratorScheme(t)).
		WithInterceptorFuncs(interceptor.Funcs{
			Apply: func(_ context.Context, _ client.WithWatch, obj runtime.ApplyConfiguration, _ ...client.ApplyOption) error {
				if cos, ok := obj.(*orbac.ClusterObjectSetApplyConfiguration); ok {
					appliedCOS = cos
				}
				return nil
			},
		}).Build()

	m := &applier.OrbStorageMigrator{
		ActionClientGetter: ag,
		RevisionGenerator:  &applier.SimpleOrbRevisionGenerator{},
		Client:             fakeClient,
		Scheme:             orbMigratorScheme(t),
		FieldOwner:         "test-owner",
	}

	res, err := m.Migrate(context.Background(), ext, objectLabelsFor(ext))
	require.NoError(t, err)
	require.NotNil(t, res, "pipeline should be gated while adoption is in progress")
	assert.Greater(t, res.RequeueAfter, time.Duration(0))

	require.NotNil(t, appliedCOS, "adopting revision should be created")
	assert.Equal(t, "my-ext-1", *appliedCOS.GetName())
	assert.Equal(t, uint32(1), *appliedCOS.Spec.Revision)
	assert.Equal(t, "my-ext", *appliedCOS.Spec.Group)
	assert.Equal(t, orbv1alpha1.CollisionProtectionNone, *appliedCOS.Spec.CollisionProtection)
	assert.Equal(t, "my-ext", appliedCOS.Labels[labels.OwnerNameKey])
	assert.Equal(t, "my-ext.v1.0.0", appliedCOS.Annotations[labels.BundleNameKey])

	// Non-controller CE owner reference, and no template-hash label.
	require.Len(t, appliedCOS.OwnerReferences, 1)
	ref := appliedCOS.OwnerReferences[0]
	assert.Equal(t, "ClusterExtension", *ref.Kind)
	assert.Equal(t, "my-ext", *ref.Name)
	assert.Equal(t, "ext-uid", string(*ref.UID))
	assert.Nil(t, ref.Controller, "CE owner reference must not be a controller ref")
	assert.Empty(t, appliedCOS.Labels["orb.operatorframework.io/template-hash"])
}

func TestOrbStorageMigrator_EquivalentDesired_NoNewRevision(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "my-ext", UID: "ext-uid"}}
	rel := deployedRelease(1, cmManifest)
	objLbls := objectLabelsFor(ext)
	ctrl := gomock.NewController(t)

	ag := newOrbActionGetter(ctrl, orbActionGetterConfig{getRel: rel})

	existing := seededRevision(t, ext, rel, objLbls)
	// completedAt nil -> pipeline should be gated, but no new revision created.

	var applied int
	fakeClient := fake.NewClientBuilder().
		WithScheme(orbMigratorScheme(t)).
		WithObjects(existing).
		WithInterceptorFuncs(interceptor.Funcs{
			Apply: func(context.Context, client.WithWatch, runtime.ApplyConfiguration, ...client.ApplyOption) error {
				applied++
				return nil
			},
		}).Build()

	m := &applier.OrbStorageMigrator{
		ActionClientGetter: ag,
		RevisionGenerator:  &applier.SimpleOrbRevisionGenerator{},
		Client:             fakeClient,
		Scheme:             orbMigratorScheme(t),
		FieldOwner:         "test-owner",
	}

	res, err := m.Migrate(context.Background(), ext, objLbls)
	require.NoError(t, err)
	require.NotNil(t, res, "gated because completedAt is nil")
	assert.Zero(t, applied, "no new revision should be created for an equivalent desired spec")
}

func TestOrbStorageMigrator_DifferingDesired_CreatesNextRevision(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "my-ext", UID: "ext-uid"}}
	rel := deployedRelease(1, cmManifest)
	objLbls := objectLabelsFor(ext)
	ctrl := gomock.NewController(t)

	ag := newOrbActionGetter(ctrl, orbActionGetterConfig{getRel: rel})

	// Seed an existing revision 1 built from a DIFFERENT release manifest so the
	// desired spec does not match.
	otherRel := deployedRelease(1, `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"other","namespace":"ns"}}`)
	existing := seededRevision(t, ext, otherRel, objLbls)

	var appliedCOS *orbac.ClusterObjectSetApplyConfiguration
	fakeClient := fake.NewClientBuilder().
		WithScheme(orbMigratorScheme(t)).
		WithObjects(existing).
		WithInterceptorFuncs(interceptor.Funcs{
			Apply: func(_ context.Context, _ client.WithWatch, obj runtime.ApplyConfiguration, _ ...client.ApplyOption) error {
				if cos, ok := obj.(*orbac.ClusterObjectSetApplyConfiguration); ok {
					appliedCOS = cos
				}
				return nil
			},
		}).Build()

	m := &applier.OrbStorageMigrator{
		ActionClientGetter: ag,
		RevisionGenerator:  &applier.SimpleOrbRevisionGenerator{},
		Client:             fakeClient,
		Scheme:             orbMigratorScheme(t),
		FieldOwner:         "test-owner",
	}

	res, err := m.Migrate(context.Background(), ext, objLbls)
	require.NoError(t, err)
	require.NotNil(t, res)

	require.NotNil(t, appliedCOS, "a new revision should be created")
	assert.Equal(t, "my-ext-2", *appliedCOS.GetName())
	assert.Equal(t, uint32(2), *appliedCOS.Spec.Revision)
}

func TestOrbStorageMigrator_LargeRelease_ExternalizesToSlices(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "my-ext", UID: "ext-uid"}}
	ctrl := gomock.NewController(t)

	// Build a manifest large enough to exceed the externalization threshold.
	var sb strings.Builder
	for i := range 30 {
		if i > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"cm-%d","namespace":"ns"},"data":{"payload":%q}}`, i, strings.Repeat("x", 60*1024))
	}
	ag := newOrbActionGetter(ctrl, orbActionGetterConfig{getRel: deployedRelease(1, sb.String())})

	var applied []string
	var cosPhases []orbac.PhaseApplyConfiguration
	fakeClient := fake.NewClientBuilder().
		WithScheme(orbMigratorScheme(t)).
		WithInterceptorFuncs(interceptor.Funcs{
			Apply: func(_ context.Context, _ client.WithWatch, obj runtime.ApplyConfiguration, _ ...client.ApplyOption) error {
				switch o := obj.(type) {
				case *orbac.ClusterObjectSliceApplyConfiguration:
					applied = append(applied, "cosl")
				case *orbac.ClusterObjectSetApplyConfiguration:
					applied = append(applied, "cos")
					cosPhases = o.Spec.Phases
				}
				return nil
			},
		}).Build()

	m := &applier.OrbStorageMigrator{
		ActionClientGetter: ag,
		RevisionGenerator:  &applier.SimpleOrbRevisionGenerator{},
		Client:             fakeClient,
		Scheme:             orbMigratorScheme(t),
		FieldOwner:         "test-owner",
	}

	_, err := m.Migrate(context.Background(), ext, objectLabelsFor(ext))
	require.NoError(t, err)

	require.NotEmpty(t, applied)
	assert.Equal(t, "cos", applied[len(applied)-1], "ClusterObjectSet must be applied after its slices")
	assert.Contains(t, applied, "cosl", "slices should be produced for a large release")

	// Phases should reference slices via objectRef, not carry inline objects.
	sawRef := false
	for _, p := range cosPhases {
		for _, obj := range p.Objects {
			if obj.ObjectRef != nil {
				sawRef = true
			}
			assert.Nil(t, obj.Object, "large release phases should not carry inline objects")
		}
	}
	assert.True(t, sawRef, "phases should use objectRefs after externalization")
}

func TestOrbStorageMigrator_FallsBackToDeployedInHistory(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "my-ext", UID: "ext-uid"}}
	ctrl := gomock.NewController(t)

	// Latest release is FAILED; a deployed release exists earlier in history.
	failed := &release.Release{Name: "my-ext", Version: 2, Info: &release.Info{Status: release.StatusFailed}}
	deployed := deployedRelease(1, cmManifest)
	ag := newOrbActionGetter(ctrl, orbActionGetterConfig{
		getRel:  failed,
		history: []*release.Release{deployed, failed},
	})

	var appliedCOS *orbac.ClusterObjectSetApplyConfiguration
	fakeClient := fake.NewClientBuilder().
		WithScheme(orbMigratorScheme(t)).
		WithInterceptorFuncs(interceptor.Funcs{
			Apply: func(_ context.Context, _ client.WithWatch, obj runtime.ApplyConfiguration, _ ...client.ApplyOption) error {
				if cos, ok := obj.(*orbac.ClusterObjectSetApplyConfiguration); ok {
					appliedCOS = cos
				}
				return nil
			},
		}).Build()

	m := &applier.OrbStorageMigrator{
		ActionClientGetter: ag,
		RevisionGenerator:  &applier.SimpleOrbRevisionGenerator{},
		Client:             fakeClient,
		Scheme:             orbMigratorScheme(t),
		FieldOwner:         "test-owner",
	}

	_, err := m.Migrate(context.Background(), ext, objectLabelsFor(ext))
	require.NoError(t, err)
	require.NotNil(t, appliedCOS, "should migrate from the deployed release found in history")
	assert.Equal(t, "my-ext.v1.0.0", appliedCOS.Annotations[labels.BundleNameKey])
}

func TestOrbStorageMigrator_NoDeployedInHistory_NoOp(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "my-ext"}}
	ctrl := gomock.NewController(t)

	failed := &release.Release{Name: "my-ext", Version: 1, Info: &release.Info{Status: release.StatusFailed}}
	ag := newOrbActionGetter(ctrl, orbActionGetterConfig{
		getRel:  failed,
		history: []*release.Release{failed},
	})

	var applied int
	fakeClient := fake.NewClientBuilder().
		WithScheme(orbMigratorScheme(t)).
		WithInterceptorFuncs(interceptor.Funcs{
			Apply: func(context.Context, client.WithWatch, runtime.ApplyConfiguration, ...client.ApplyOption) error {
				applied++
				return nil
			},
		}).Build()

	m := &applier.OrbStorageMigrator{
		ActionClientGetter: ag,
		RevisionGenerator:  &applier.SimpleOrbRevisionGenerator{},
		Client:             fakeClient,
		Scheme:             orbMigratorScheme(t),
		FieldOwner:         "test-owner",
	}

	res, err := m.Migrate(context.Background(), ext, objectLabelsFor(ext))
	require.NoError(t, err)
	assert.Nil(t, res)
	assert.Zero(t, applied)
}

func TestOrbStorageMigrator_CompletedRevision_DeletesHelmStorage(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "my-ext", UID: "ext-uid"}}
	rel := deployedRelease(1, cmManifest)
	objLbls := objectLabelsFor(ext)
	ctrl := gomock.NewController(t)

	st := storage.Init(driver.NewMemory())
	require.NoError(t, st.Create(rel))

	ag := newOrbActionGetter(ctrl, orbActionGetterConfig{getRel: rel, storage: st})

	existing := seededRevision(t, ext, rel, objLbls)
	now := metav1.Now()
	existing.Status.CompletedAt = &now

	var applied int
	fakeClient := fake.NewClientBuilder().
		WithScheme(orbMigratorScheme(t)).
		WithObjects(existing).
		WithInterceptorFuncs(interceptor.Funcs{
			Apply: func(context.Context, client.WithWatch, runtime.ApplyConfiguration, ...client.ApplyOption) error {
				applied++
				return nil
			},
		}).Build()

	m := &applier.OrbStorageMigrator{
		ActionClientGetter: ag,
		RevisionGenerator:  &applier.SimpleOrbRevisionGenerator{},
		Client:             fakeClient,
		Scheme:             orbMigratorScheme(t),
		FieldOwner:         "test-owner",
	}

	res, err := m.Migrate(context.Background(), ext, objLbls)
	require.NoError(t, err)
	assert.Nil(t, res, "pipeline should no longer be gated once adoption completed")
	assert.Zero(t, applied, "no new revision when reusing a completed revision")

	// Helm release storage should be gone.
	_, err = st.History("my-ext")
	assert.ErrorIs(t, err, driver.ErrReleaseNotFound)
}

// failAfterNDeletes wraps a driver and fails the (n+1)-th Delete call, to
// simulate a partial delete failure.
type failAfterNDeletes struct {
	driver.Driver
	remaining int
}

func (d *failAfterNDeletes) Delete(key string) (*release.Release, error) {
	if d.remaining <= 0 {
		return nil, fmt.Errorf("simulated delete failure")
	}
	d.remaining--
	return d.Driver.Delete(key)
}

func (d *failAfterNDeletes) Name() string { return d.Driver.Name() }

func TestOrbStorageMigrator_PartialDelete_LeavesNewestPresent(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "my-ext", UID: "ext-uid"}}
	rel := deployedRelease(2, cmManifest)
	objLbls := objectLabelsFor(ext)
	ctrl := gomock.NewController(t)

	mem := driver.NewMemory()
	base := storage.Init(mem)
	// Seed history: v1 superseded (oldest), v2 deployed (newest).
	require.NoError(t, base.Create(&release.Release{Name: "my-ext", Version: 1, Info: &release.Info{Status: release.StatusSuperseded}}))
	require.NoError(t, base.Create(rel))

	// Fail on the second delete (the newest deployed version).
	st := storage.Init(&failAfterNDeletes{Driver: mem, remaining: 1})

	ag := newOrbActionGetter(ctrl, orbActionGetterConfig{getRel: rel, storage: st})

	existing := seededRevision(t, ext, rel, objLbls)
	now := metav1.Now()
	existing.Status.CompletedAt = &now

	fakeClient := fake.NewClientBuilder().
		WithScheme(orbMigratorScheme(t)).
		WithObjects(existing).
		Build()

	m := &applier.OrbStorageMigrator{
		ActionClientGetter: ag,
		RevisionGenerator:  &applier.SimpleOrbRevisionGenerator{},
		Client:             fakeClient,
		Scheme:             orbMigratorScheme(t),
		FieldOwner:         "test-owner",
	}

	_, err := m.Migrate(context.Background(), ext, objLbls)
	require.Error(t, err, "a mid-delete failure should surface")

	// v1 (oldest) deleted first, v2 (newest deployed) delete failed -> still present.
	_, err = base.Get("my-ext", 1)
	require.ErrorIs(t, err, driver.ErrReleaseNotFound, "oldest version should have been deleted")
	newest, err := base.Get("my-ext", 2)
	require.NoError(t, err, "newest deployed version should remain present after partial failure")
	assert.Equal(t, 2, newest.Version)
}
