package applier_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	coscontrollers "github.com/operator-framework/operator-controller/internal/object-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
	"github.com/operator-framework/operator-controller/internal/testutil"
	mockapplier "github.com/operator-framework/operator-controller/internal/testutil/mock/applier"
	mockctrlclient "github.com/operator-framework/operator-controller/internal/testutil/mock/ctrlclient"
	mockhelmclient "github.com/operator-framework/operator-controller/internal/testutil/mock/helmclient"
)

func TestHelmMigrationSelectsDeployedRelease(t *testing.T) {
	deployed := deployedMigrationRelease()
	older := deployedMigrationRelease()
	older.Version = 1
	deployed.Version = 3
	failed := deployedMigrationRelease()
	failed.Info.Status = release.StatusFailed
	failed.Version = 4
	for _, tc := range []struct {
		name              string
		latest            *release.Release
		history           []*release.Release
		wantRelease       *release.Release
		wantHistoryLookup bool
		wantPending       bool
		wantErr           error
	}{
		{
			name:        "latest release is deployed",
			latest:      deployed,
			wantRelease: deployed,
			wantErr:     nil,
			wantPending: true,
		},
		{
			name:              "failed latest with older deployed entry first",
			latest:            failed,
			history:           []*release.Release{older, nil, deployed, failed, {Version: 5}},
			wantRelease:       deployed,
			wantHistoryLookup: true,
			wantErr:           nil,
			wantPending:       true,
		},
		{
			name:              "failed latest with newer deployed entry first",
			latest:            failed,
			history:           []*release.Release{deployed, nil, older, failed, {Version: 5}},
			wantRelease:       deployed,
			wantHistoryLookup: true,
			wantErr:           nil,
			wantPending:       true,
		},
		{
			name:              "nil latest",
			history:           []*release.Release{nil, older, failed, {Version: 5}},
			wantRelease:       older,
			wantHistoryLookup: true,
			wantErr:           nil,
			wantPending:       true,
		},
		{
			name:              "missing latest info",
			latest:            &release.Release{},
			history:           []*release.Release{older, failed, {Version: 5}, nil, deployed},
			wantRelease:       deployed,
			wantHistoryLookup: true,
			wantErr:           nil,
			wantPending:       true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given the latest Helm record and history declared by this case.
			ctrl := gomock.NewController(t)
			ext := migrationExtension()
			helm := mockhelmclient.NewMockActionClientGetterAndInterface(ctrl)
			helm.EXPECT().ActionClientFor(gomock.Any(), ext).Return(helm, nil)
			helm.EXPECT().Get(ext.Name).Return(tc.latest, nil)
			if tc.wantHistoryLookup {
				helm.EXPECT().History(ext.Name).Return(tc.history, nil)
			}
			scheme := codTestScheme(t)
			realGenerator := &applier.SimpleRevisionGenerator{Scheme: scheme, SystemNamespace: "system"}
			generator := mockapplier.NewMockRevisionGenerator(ctrl)
			// Verify the selected record, then generate normal content so migration can succeed.
			generator.EXPECT().GenerateClusterObjectDeploymentFromHelmRelease(gomock.Any(), tc.wantRelease, ext, nil).
				DoAndReturn(realGenerator.GenerateClusterObjectDeploymentFromHelmRelease)
			stored := migrationClient(t, interceptor.Funcs{})
			api := mockctrlclient.NewMockClient(ctrl)
			api.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(stored.Get)
			api.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(stored.List)
			gomock.InOrder(
				api.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&corev1.Secret{})).DoAndReturn(stored.Create),
				api.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&ocv1.ClusterObjectDeployment{})).DoAndReturn(stored.Create),
			)
			m := &applier.BoxcutterStorageMigrator{
				Client:             api,
				Scheme:             scheme,
				ActionClientGetter: helm,
				RevisionGenerator:  generator,
			}

			// When migration starts from these Helm records.
			pending, err := m.Migrate(t.Context(), ext, nil)

			// Then the expected record is used and migration starts successfully.
			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.wantPending, pending != nil)
		})
	}
}

func TestHelmMigrationSkipsExistingInstallation(t *testing.T) {
	for _, tc := range []struct {
		name        string
		objects     []client.Object
		wantPending *ocv1.ClusterObjectDeployment
		wantErr     error
	}{
		{
			name: "unmarked deployment stays under ordinary management",
			objects: []client.Object{&ocv1.ClusterObjectDeployment{
				ObjectMeta: metav1.ObjectMeta{Name: "migration"},
			}},
			wantPending: nil,
			wantErr:     nil,
		},
		{
			name: "existing revisions do not authorize reconstructing a deployment",
			objects: []client.Object{&ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{Name: "migration-1"},
				Spec:       ocv1.ClusterObjectSetSpec{Group: "migration", Revision: 1},
			}},
			wantPending: nil,
			wantErr:     nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given an existing installation, allow API reads but no writes.
			ctrl := gomock.NewController(t)
			stored := migrationClient(t, interceptor.Funcs{}, tc.objects...)
			api := mockctrlclient.NewMockClient(ctrl)
			api.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(stored.Get).AnyTimes()
			api.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(stored.List).AnyTimes()
			m := &applier.BoxcutterStorageMigrator{
				Client:             api,
				ActionClientGetter: mockhelmclient.NewMockActionClientGetter(ctrl),
				RevisionGenerator:  mockapplier.NewMockRevisionGenerator(ctrl),
			}

			// When migration is attempted, it must stop before consulting Helm.
			pending, err := m.Migrate(t.Context(), migrationExtension(), nil)

			// Then migration is skipped. Unconfigured Helm, generator, or API
			// write calls fail through gomock instead of a nil-pointer panic.
			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.wantPending, pending)
		})
	}
}

func TestHelmMigrationSkipsManagedNamespace(t *testing.T) {
	// Given a managed-namespace CE, there is no Helm-era installation to recover.
	ext := migrationExtension()
	ext.Spec.Namespace = ""
	ctrl := gomock.NewController(t)
	stored := migrationClient(t, interceptor.Funcs{})
	api := mockctrlclient.NewMockClient(ctrl)
	api.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(stored.Get)
	m := &applier.BoxcutterStorageMigrator{
		Client:             api,
		ActionClientGetter: mockhelmclient.NewMockActionClientGetter(ctrl),
		RevisionGenerator:  mockapplier.NewMockRevisionGenerator(ctrl),
	}

	// When migration is attempted, only the initial COD lookup is allowed.
	pending, err := m.Migrate(t.Context(), ext, nil)

	// Then no migration is pending and no Helm lookup, rendering, or write occurs.
	require.NoError(t, err)
	require.Nil(t, pending)
}

func TestHelmMigrationSkipsMissingDeployedRelease(t *testing.T) {
	for _, tc := range []struct {
		name              string
		latest            *release.Release
		getErr            error
		history           []*release.Release
		historyErr        error
		wantHistoryLookup bool
		wantPending       *ocv1.ClusterObjectDeployment
		wantErr           error
	}{
		{
			name:              "release missing",
			getErr:            driver.ErrReleaseNotFound,
			wantHistoryLookup: false,
			wantPending:       nil,
			wantErr:           nil,
		},
		{
			name:              "history missing",
			historyErr:        driver.ErrReleaseNotFound,
			wantHistoryLookup: true,
			wantPending:       nil,
			wantErr:           nil,
		},
		{
			name:              "history has no deployed release",
			history:           []*release.Release{nil, {}, {Info: &release.Info{Status: release.StatusFailed}}},
			wantHistoryLookup: true,
			wantPending:       nil,
			wantErr:           nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given Helm responses that contain no deployed installation.
			ctrl := gomock.NewController(t)
			ext := migrationExtension()
			helm := mockhelmclient.NewMockActionClientGetterAndInterface(ctrl)
			helm.EXPECT().ActionClientFor(gomock.Any(), ext).Return(helm, nil)
			helm.EXPECT().Get(ext.Name).Return(tc.latest, tc.getErr)
			if tc.wantHistoryLookup {
				helm.EXPECT().History(ext.Name).Return(tc.history, tc.historyErr)
			}
			stored := migrationClient(t, interceptor.Funcs{})
			api := mockctrlclient.NewMockClient(ctrl)
			api.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(stored.Get).AnyTimes()
			api.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(stored.List).AnyTimes()
			m := &applier.BoxcutterStorageMigrator{
				Client:             api,
				ActionClientGetter: helm,
				RevisionGenerator:  mockapplier.NewMockRevisionGenerator(ctrl),
			}

			// When migration is attempted.
			pending, err := m.Migrate(t.Context(), ext, nil)

			// Then it skips without rendering or writing Kubernetes objects;
			// the mocks permit only the reads configured above.
			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.wantPending, pending)
		})
	}
}

func TestHelmMigrationCreatesDeploymentOwnershipChain(t *testing.T) {
	// Given a deployed Helm installation and an empty Kubernetes store.
	ext, rel := migrationExtension(), deployedMigrationRelease()
	stored := migrationClient(t, interceptor.Funcs{})
	ctrl := gomock.NewController(t)
	helm := mockhelmclient.NewMockActionClientGetterAndInterface(ctrl)
	helm.EXPECT().ActionClientFor(gomock.Any(), ext).Return(helm, nil)
	helm.EXPECT().Get(ext.Name).Return(rel, nil)
	scheme := codTestScheme(t)
	realGenerator := &applier.SimpleRevisionGenerator{Scheme: scheme, SystemNamespace: "system"}
	generator := mockapplier.NewMockRevisionGenerator(ctrl)
	generator.EXPECT().GenerateClusterObjectDeploymentFromHelmRelease(gomock.Any(), rel, ext, nil).
		DoAndReturn(realGenerator.GenerateClusterObjectDeploymentFromHelmRelease)
	api := mockctrlclient.NewMockClient(ctrl)
	api.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(stored.Get)
	api.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(stored.List)
	// Content must exist before the COD can refer to it. No COS write is allowed.
	gomock.InOrder(
		api.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&corev1.Secret{})).DoAndReturn(stored.Create),
		api.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&ocv1.ClusterObjectDeployment{})).DoAndReturn(stored.Create),
	)
	m := &applier.BoxcutterStorageMigrator{
		Client: api, Scheme: scheme,
		ActionClientGetter: helm,
		RevisionGenerator:  generator,
	}

	// When migration starts.
	cod, err := m.Migrate(t.Context(), ext, nil)

	// Then the CE owns the adopting COD and immutable content; only COD creates revisions.
	require.NoError(t, err)
	require.NotNil(t, cod)
	require.True(t, metav1.IsControlledBy(cod, ext))
	require.Equal(t, labels.ManagedByOperatorController, cod.Labels[labels.ManagedByKey])
	require.Equal(t, labels.HelmMigrationAdopting, cod.Annotations[labels.HelmMigrationKey])
	require.Equal(t, ocv1.CollisionProtectionNone, cod.Spec.Template.Spec.CollisionProtection)
	secrets := &corev1.SecretList{}
	require.NoError(t, stored.List(t.Context(), secrets))
	require.Len(t, secrets.Items, 1)
	for i := range secrets.Items {
		require.True(t, metav1.IsControlledBy(&secrets.Items[i], ext))
		require.Equal(t, ptr.To(true), secrets.Items[i].Immutable)
	}
	sets := &ocv1.ClusterObjectSetList{}
	require.NoError(t, stored.List(t.Context(), sets))
	require.Empty(t, sets.Items)
}

func TestHelmMigrationWaitsForCurrentCompletion(t *testing.T) {
	for _, tc := range []struct {
		name          string
		stage         string
		protection    ocv1.CollisionProtection
		status        metav1.ConditionStatus
		reason        string
		observed      int64
		wantPending   bool
		wantUnchanged bool
		wantErr       error
	}{
		{
			name:          "adopting is pending",
			stage:         labels.HelmMigrationAdopting,
			protection:    ocv1.CollisionProtectionNone,
			status:        metav1.ConditionTrue,
			reason:        ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing,
			observed:      2,
			wantPending:   true,
			wantUnchanged: true,
			wantErr:       nil,
		},
		{
			name:          "protecting is pending",
			stage:         labels.HelmMigrationProtecting,
			protection:    ocv1.CollisionProtectionPrevent,
			status:        metav1.ConditionTrue,
			reason:        ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing,
			observed:      2,
			wantPending:   true,
			wantUnchanged: true,
			wantErr:       nil,
		},
		{
			name:          "stale adoption completion",
			stage:         labels.HelmMigrationAdopting,
			protection:    ocv1.CollisionProtectionNone,
			status:        metav1.ConditionFalse,
			reason:        ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed,
			observed:      1,
			wantPending:   true,
			wantUnchanged: true,
			wantErr:       nil,
		},
		{
			name:          "stale protected completion",
			stage:         labels.HelmMigrationProtecting,
			protection:    ocv1.CollisionProtectionPrevent,
			status:        metav1.ConditionFalse,
			reason:        ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed,
			observed:      1,
			wantPending:   true,
			wantUnchanged: true,
			wantErr:       nil,
		},
		{
			name:          "deadline failure remains pending",
			stage:         labels.HelmMigrationProtecting,
			protection:    ocv1.CollisionProtectionPrevent,
			status:        metav1.ConditionFalse,
			reason:        ocv1.ClusterObjectDeploymentReasonProgressDeadlineExceeded,
			observed:      2,
			wantPending:   true,
			wantUnchanged: true,
			wantErr:       nil,
		},
		{
			name:          "success reason with wrong status",
			stage:         labels.HelmMigrationAdopting,
			protection:    ocv1.CollisionProtectionNone,
			status:        metav1.ConditionTrue,
			reason:        ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed,
			observed:      2,
			wantPending:   true,
			wantUnchanged: true,
			wantErr:       nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given a stored migration whose rollout has not completed at generation 2.
			stored := migrationClient(t, interceptor.Funcs{})
			cod := startMigration(t, stored)
			cod.Generation = 2
			cod.Annotations[labels.HelmMigrationKey] = tc.stage
			cod.Spec.Template.Spec.CollisionProtection = tc.protection
			require.NoError(t, stored.Update(t.Context(), cod))
			cod.Status.Conditions = []metav1.Condition{
				{Type: ocv1.ClusterObjectDeploymentTypeProgressing, Status: tc.status, Reason: tc.reason, ObservedGeneration: tc.observed},
				{Type: ocv1.ClusterObjectDeploymentTypeAvailable, Status: metav1.ConditionTrue, Reason: "Available", ObservedGeneration: 2},
			}
			require.NoError(t, stored.Status().Update(t.Context(), cod))
			before := cod.DeepCopy()
			ctrl := gomock.NewController(t)
			api := mockctrlclient.NewMockClient(ctrl)
			api.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(cod), gomock.Any()).DoAndReturn(stored.Get)
			m := &applier.BoxcutterStorageMigrator{
				Client:             api,
				ActionClientGetter: mockhelmclient.NewMockActionClientGetter(ctrl),
				RevisionGenerator:  mockapplier.NewMockRevisionGenerator(ctrl),
			}

			// When a reconstructed migrator resumes; no Helm, generation, or write is allowed.
			pending, err := m.Migrate(t.Context(), migrationExtension(), nil)

			// Then availability alone does not advance the migration or alter stored content.
			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.wantPending, pending != nil)
			if tc.wantUnchanged {
				require.Equal(t, before, pending)
			}
		})
	}
}

func TestHelmMigrationProtectsFrozenTemplate(t *testing.T) {
	// Given completed adoption and a CE edited after the migration snapshot was stored.
	stored := migrationClient(t, interceptor.Funcs{})
	cod := startMigration(t, stored)
	completeMigrationRollout(t, stored, cod)
	wantSpec := cod.Spec.DeepCopy()
	wantSpec.Template.Spec.CollisionProtection = ocv1.CollisionProtectionPrevent
	ext := migrationExtension()
	ext.Spec.ProgressDeadlineMinutes = 30
	ctrl := gomock.NewController(t)
	api := mockctrlclient.NewMockClient(ctrl)
	api.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(cod), gomock.Any()).DoAndReturn(stored.Get)
	api.EXPECT().Patch(gomock.Any(), gomock.AssignableToTypeOf(cod), gomock.Any()).DoAndReturn(stored.Patch)
	m := &applier.BoxcutterStorageMigrator{
		Client:             api,
		ActionClientGetter: mockhelmclient.NewMockActionClientGetter(ctrl),
		RevisionGenerator:  mockapplier.NewMockRevisionGenerator(ctrl),
	}

	// When migration advances using the stored snapshot, with exactly one COD patch.
	pending, err := m.Migrate(t.Context(), ext, map[string]string{"changed": "ignored"})

	// Then only protection changes; content, probes, metadata, and deadline stay frozen.
	require.NoError(t, err)
	require.NotNil(t, pending)
	require.Equal(t, labels.HelmMigrationProtecting, pending.Annotations[labels.HelmMigrationKey])
	require.Equal(t, *wantSpec, pending.Spec)
	actual := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, stored.Get(t.Context(), client.ObjectKeyFromObject(cod), actual))
	require.Equal(t, pending, actual)
}

func TestHelmMigrationRemovesMarkerAfterProtectedCompletion(t *testing.T) {
	// Given a protected rollout that completed successfully.
	stored := migrationClient(t, interceptor.Funcs{})
	cod := startMigration(t, stored)
	cod.Annotations[labels.HelmMigrationKey] = labels.HelmMigrationProtecting
	cod.Annotations["unrelated"] = "preserved"
	cod.Spec.Template.Spec.CollisionProtection = ocv1.CollisionProtectionPrevent
	require.NoError(t, stored.Update(t.Context(), cod))
	completeMigrationRollout(t, stored, cod)
	ctrl := gomock.NewController(t)
	api := mockctrlclient.NewMockClient(ctrl)
	api.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(cod), gomock.Any()).DoAndReturn(stored.Get)
	api.EXPECT().Patch(gomock.Any(), gomock.AssignableToTypeOf(cod), gomock.Any()).DoAndReturn(stored.Patch)
	m := &applier.BoxcutterStorageMigrator{
		Client:             api,
		ActionClientGetter: mockhelmclient.NewMockActionClientGetter(ctrl),
		RevisionGenerator:  mockapplier.NewMockRevisionGenerator(ctrl),
	}

	// When migration finishes, allowing only one COD patch.
	pending, err := m.Migrate(t.Context(), migrationExtension(), nil)

	// Then the marker is removed without changing the template or unrelated annotations.
	require.NoError(t, err)
	require.Nil(t, pending)
	actual := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, stored.Get(t.Context(), client.ObjectKeyFromObject(cod), actual))
	require.NotContains(t, actual.Annotations, labels.HelmMigrationKey)
	require.Equal(t, "preserved", actual.Annotations["unrelated"])
	require.Equal(t, cod.Spec, actual.Spec)
}

func TestHelmMigrationRetriesInterruptedCreation(t *testing.T) {
	connectionLost := errors.New("connection lost")
	for _, tc := range []struct {
		name                     string
		failedKind               string
		persistBeforeFailure     bool
		wantFirstDeploymentCalls int
		wantFirstErr             error
		wantRetryHelmCalls       int
		wantRetrySecretCalls     int
		wantRetryDeploymentCalls int
		wantRetryErr             error
		wantStage                string
		wantSecretCount          int
		wantDeploymentCount      int
	}{
		{
			name: "Secret request failed", failedKind: "Secret", persistBeforeFailure: false,
			wantFirstDeploymentCalls: 0, wantFirstErr: connectionLost,
			wantRetryHelmCalls: 1, wantRetrySecretCalls: 1, wantRetryDeploymentCalls: 1,
			wantRetryErr: nil, wantStage: labels.HelmMigrationAdopting, wantSecretCount: 1, wantDeploymentCount: 1,
		},
		{
			name: "Secret response lost", failedKind: "Secret", persistBeforeFailure: true,
			wantFirstDeploymentCalls: 0, wantFirstErr: connectionLost,
			wantRetryHelmCalls: 1, wantRetrySecretCalls: 1, wantRetryDeploymentCalls: 1,
			wantRetryErr: nil, wantStage: labels.HelmMigrationAdopting, wantSecretCount: 1, wantDeploymentCount: 1,
		},
		{
			name: "deployment request failed after Secret", failedKind: "ClusterObjectDeployment", persistBeforeFailure: false,
			wantFirstDeploymentCalls: 1, wantFirstErr: connectionLost,
			wantRetryHelmCalls: 1, wantRetrySecretCalls: 1, wantRetryDeploymentCalls: 1,
			wantRetryErr: nil, wantStage: labels.HelmMigrationAdopting, wantSecretCount: 1, wantDeploymentCount: 1,
		},
		{
			name: "deployment response lost", failedKind: "ClusterObjectDeployment", persistBeforeFailure: true,
			wantFirstDeploymentCalls: 1, wantFirstErr: connectionLost,
			wantRetryHelmCalls: 0, wantRetrySecretCalls: 0, wantRetryDeploymentCalls: 0,
			wantRetryErr: nil, wantStage: labels.HelmMigrationAdopting, wantSecretCount: 1, wantDeploymentCount: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given a one-time failed write, optionally persisted before its response is lost.
			failed := false
			scheme := codTestScheme(t)
			stored := migrationClient(t, interceptor.Funcs{Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				gvks, _, err := scheme.ObjectKinds(obj)
				require.NoError(t, err)
				if !failed && gvks[0].Kind == tc.failedKind {
					failed = true
					if tc.persistBeforeFailure {
						require.NoError(t, c.Create(ctx, obj, opts...))
					}
					return connectionLost
				}
				return c.Create(ctx, obj, opts...)
			}})
			ext, rel := migrationExtension(), deployedMigrationRelease()
			ctrl := gomock.NewController(t)
			helm := mockhelmclient.NewMockActionClientGetterAndInterface(ctrl)
			helm.EXPECT().ActionClientFor(gomock.Any(), ext).Return(helm, nil)
			helm.EXPECT().Get(ext.Name).Return(rel, nil)
			realGenerator := &applier.SimpleRevisionGenerator{Scheme: scheme, SystemNamespace: "system"}
			generator := mockapplier.NewMockRevisionGenerator(ctrl)
			generator.EXPECT().GenerateClusterObjectDeploymentFromHelmRelease(gomock.Any(), rel, ext, nil).
				DoAndReturn(realGenerator.GenerateClusterObjectDeploymentFromHelmRelease)
			api := mockctrlclient.NewMockClient(ctrl)
			api.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(stored.Get)
			api.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(stored.List)
			gomock.InOrder(
				api.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&corev1.Secret{})).DoAndReturn(stored.Create),
				api.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&ocv1.ClusterObjectDeployment{})).DoAndReturn(stored.Create).Times(tc.wantFirstDeploymentCalls),
			)
			m := &applier.BoxcutterStorageMigrator{
				Client: api, Scheme: scheme,
				ActionClientGetter: helm,
				RevisionGenerator:  generator,
			}

			// When the first attempt encounters the interrupted write.
			pending, err := m.Migrate(t.Context(), ext, nil)

			// Then the error is surfaced, leaving any successful writes available to retry.
			require.ErrorIs(t, err, tc.wantFirstErr)
			require.Nil(t, pending)
			require.True(t, failed, "the configured failure must be exercised")

			// Given a reconstructed migrator: only an absent COD permits Helm and generation again.
			retryHelm := mockhelmclient.NewMockActionClientGetterAndInterface(ctrl)
			retryHelm.EXPECT().ActionClientFor(gomock.Any(), ext).Return(retryHelm, nil).Times(tc.wantRetryHelmCalls)
			retryHelm.EXPECT().Get(ext.Name).Return(rel, nil).Times(tc.wantRetryHelmCalls)
			retryGenerator := mockapplier.NewMockRevisionGenerator(ctrl)
			retryGenerator.EXPECT().GenerateClusterObjectDeploymentFromHelmRelease(gomock.Any(), rel, ext, nil).
				DoAndReturn(realGenerator.GenerateClusterObjectDeploymentFromHelmRelease).Times(tc.wantRetryHelmCalls)
			retryAPI := mockctrlclient.NewMockClient(ctrl)
			retryAPI.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(stored.Get).AnyTimes()
			retryAPI.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(stored.List).Times(tc.wantRetryHelmCalls)
			gomock.InOrder(
				retryAPI.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&corev1.Secret{})).DoAndReturn(stored.Create).Times(tc.wantRetrySecretCalls),
				retryAPI.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&ocv1.ClusterObjectDeployment{})).DoAndReturn(stored.Create).Times(tc.wantRetryDeploymentCalls),
			)
			retry := &applier.BoxcutterStorageMigrator{
				Client: retryAPI, Scheme: scheme,
				ActionClientGetter: retryHelm,
				RevisionGenerator:  retryGenerator,
			}

			// When the same migration is retried.
			pending, err = retry.Migrate(t.Context(), ext, nil)

			// Then a single adopting COD and a single content Secret remain.
			require.ErrorIs(t, err, tc.wantRetryErr)
			require.NotNil(t, pending)
			require.Equal(t, tc.wantStage, pending.Annotations[labels.HelmMigrationKey])
			secrets := &corev1.SecretList{}
			require.NoError(t, stored.List(t.Context(), secrets))
			require.Len(t, secrets.Items, tc.wantSecretCount)
			cods := &ocv1.ClusterObjectDeploymentList{}
			require.NoError(t, stored.List(t.Context(), cods))
			require.Len(t, cods.Items, tc.wantDeploymentCount)
		})
	}
}

func TestHelmMigrationRetriesLostTransitionResponse(t *testing.T) {
	responseLost := errors.New("response lost")
	for _, tc := range []struct {
		name               string
		stage              string
		protection         ocv1.CollisionProtection
		simulateGeneration bool
		wantFirstErr       error
		wantStoredStage    string
		wantProtection     ocv1.CollisionProtection
		wantRetryPending   bool
		wantRetryErr       error
	}{
		{
			name: "adoption-to-protection response lost", stage: labels.HelmMigrationAdopting,
			protection: ocv1.CollisionProtectionNone, simulateGeneration: true,
			wantFirstErr: responseLost, wantStoredStage: labels.HelmMigrationProtecting,
			wantProtection: ocv1.CollisionProtectionPrevent, wantRetryPending: true, wantRetryErr: nil,
		},
		{
			name: "marker-removal response lost", stage: labels.HelmMigrationProtecting,
			protection: ocv1.CollisionProtectionPrevent, simulateGeneration: false,
			wantFirstErr: responseLost, wantStoredStage: "",
			wantProtection: ocv1.CollisionProtectionPrevent, wantRetryPending: false, wantRetryErr: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given a completed stage and a patch that persists but loses its response.
			stored := migrationClient(t, interceptor.Funcs{})
			cod := startMigration(t, stored)
			cod.Generation = 1
			cod.Annotations[labels.HelmMigrationKey] = tc.stage
			cod.Spec.Template.Spec.CollisionProtection = tc.protection
			require.NoError(t, stored.Update(t.Context(), cod))
			completeMigrationRollout(t, stored, cod)
			ctrl := gomock.NewController(t)
			api := mockctrlclient.NewMockClient(ctrl)
			api.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(cod), gomock.Any()).DoAndReturn(stored.Get)
			api.EXPECT().Patch(gomock.Any(), gomock.AssignableToTypeOf(cod), gomock.Any()).
				DoAndReturn(func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					require.NoError(t, stored.Patch(ctx, obj, patch, opts...))
					return responseLost
				})
			m := &applier.BoxcutterStorageMigrator{
				Client:             api,
				ActionClientGetter: mockhelmclient.NewMockActionClientGetter(ctrl),
				RevisionGenerator:  mockapplier.NewMockRevisionGenerator(ctrl),
			}

			// When migration attempts the transition.
			_, err := m.Migrate(t.Context(), migrationExtension(), nil)

			// Then the caller sees the lost response although the intended state is stored.
			require.ErrorIs(t, err, tc.wantFirstErr)
			actual := &ocv1.ClusterObjectDeployment{}
			require.NoError(t, stored.Get(t.Context(), client.ObjectKeyFromObject(cod), actual))
			require.Equal(t, tc.wantStoredStage, actual.Annotations[labels.HelmMigrationKey])
			require.Equal(t, tc.wantProtection, actual.Spec.Template.Spec.CollisionProtection)

			// Given a restart, simulate the API generation bump for a changed template.
			if tc.simulateGeneration {
				actual.Generation++
				require.NoError(t, stored.Update(t.Context(), actual))
			}
			retryAPI := mockctrlclient.NewMockClient(ctrl)
			retryAPI.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(cod), gomock.Any()).DoAndReturn(stored.Get)
			retry := &applier.BoxcutterStorageMigrator{
				Client:             retryAPI,
				ActionClientGetter: mockhelmclient.NewMockActionClientGetter(ctrl),
				RevisionGenerator:  mockapplier.NewMockRevisionGenerator(ctrl),
			}

			// When the reconstructed migrator retries; no Helm, generation, or write is allowed.
			pending, err := retry.Migrate(t.Context(), migrationExtension(), nil)

			// Then it resumes the persisted stage or reports completion, without repeating the patch.
			require.ErrorIs(t, err, tc.wantRetryErr)
			require.Equal(t, tc.wantRetryPending, pending != nil)
			if tc.wantRetryPending {
				require.Equal(t, actual, pending)
			}
		})
	}
}

func TestHelmMigrationTransitionUsesOptimisticLock(t *testing.T) {
	// Given completed adoption and a concurrent COD edit just before the transition patch.
	stored := migrationClient(t, interceptor.Funcs{})
	cod := startMigration(t, stored)
	completeMigrationRollout(t, stored, cod)
	ctrl := gomock.NewController(t)
	api := mockctrlclient.NewMockClient(ctrl)
	api.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(cod), gomock.Any()).DoAndReturn(stored.Get)
	api.EXPECT().Patch(gomock.Any(), gomock.AssignableToTypeOf(cod), gomock.Any()).
		DoAndReturn(func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			concurrent := &ocv1.ClusterObjectDeployment{}
			require.NoError(t, stored.Get(ctx, client.ObjectKeyFromObject(obj), concurrent))
			concurrent.Annotations["concurrent"] = "change"
			require.NoError(t, stored.Update(ctx, concurrent))
			return stored.Patch(ctx, obj, patch, opts...)
		})
	m := &applier.BoxcutterStorageMigrator{
		Client:             api,
		ActionClientGetter: mockhelmclient.NewMockActionClientGetter(ctrl),
		RevisionGenerator:  mockapplier.NewMockRevisionGenerator(ctrl),
	}

	// When migration attempts its resource-version-guarded patch.
	pending, err := m.Migrate(t.Context(), migrationExtension(), nil)

	// Then the conflict is surfaced and the concurrent edit survives without advancing protection.
	require.True(t, apierrors.IsConflict(err), "%v", err)
	require.Nil(t, pending)
	actual := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, stored.Get(t.Context(), client.ObjectKeyFromObject(cod), actual))
	require.Equal(t, labels.HelmMigrationAdopting, actual.Annotations[labels.HelmMigrationKey])
	require.Equal(t, ocv1.CollisionProtectionNone, actual.Spec.Template.Spec.CollisionProtection)
	require.Equal(t, "change", actual.Annotations["concurrent"])
}

func TestHelmMigrationRejectsInvalidPersistedState(t *testing.T) {
	for _, tc := range []struct {
		name        string
		ownerUID    string
		ownerless   bool
		deleting    bool
		stage       string
		protection  ocv1.CollisionProtection
		wantErr     string
		wantPending *ocv1.ClusterObjectDeployment
	}{
		{
			name:        "foreign current owner",
			ownerUID:    "foreign",
			stage:       labels.HelmMigrationAdopting,
			protection:  ocv1.CollisionProtectionNone,
			wantErr:     "controlled by ClusterExtension UID",
			wantPending: nil,
		},
		{
			name:        "recreated extension owner",
			ownerUID:    "old-extension-uid",
			stage:       labels.HelmMigrationAdopting,
			protection:  ocv1.CollisionProtectionNone,
			wantErr:     "controlled by ClusterExtension UID",
			wantPending: nil,
		},
		{
			name:        "ownerless",
			ownerless:   true,
			stage:       labels.HelmMigrationAdopting,
			protection:  ocv1.CollisionProtectionNone,
			wantErr:     "controlled by ClusterExtension UID",
			wantPending: nil,
		},
		{
			name:        "deleting",
			ownerUID:    "extension-uid",
			deleting:    true,
			stage:       labels.HelmMigrationAdopting,
			protection:  ocv1.CollisionProtectionNone,
			wantErr:     "must be non-deleting",
			wantPending: nil,
		},
		{
			name:        "unknown stage",
			ownerUID:    "extension-uid",
			stage:       "Unknown",
			protection:  ocv1.CollisionProtectionNone,
			wantErr:     "inconsistent stage",
			wantPending: nil,
		},
		{
			name:        "empty stage",
			ownerUID:    "extension-uid",
			stage:       "",
			protection:  ocv1.CollisionProtectionNone,
			wantErr:     "inconsistent stage",
			wantPending: nil,
		},
		{
			name:        "adopting with protection",
			ownerUID:    "extension-uid",
			stage:       labels.HelmMigrationAdopting,
			protection:  ocv1.CollisionProtectionPrevent,
			wantErr:     "inconsistent stage",
			wantPending: nil,
		},
		{
			name:        "protecting without protection",
			ownerUID:    "extension-uid",
			stage:       labels.HelmMigrationProtecting,
			protection:  ocv1.CollisionProtectionNone,
			wantErr:     "inconsistent stage",
			wantPending: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given a persisted COD with the ownership or stage mismatch declared above.
			seed := migrationClient(t, interceptor.Funcs{})
			cod := startMigration(t, seed)
			cod.OwnerReferences[0].UID = types.UID(tc.ownerUID)
			if tc.ownerless {
				cod.OwnerReferences = nil
			}
			if tc.deleting {
				cod.DeletionTimestamp = ptr.To(metav1.Now())
				cod.Finalizers = []string{"retain"}
			}
			cod.Annotations[labels.HelmMigrationKey] = tc.stage
			cod.Spec.Template.Spec.CollisionProtection = tc.protection
			stored := migrationClient(t, interceptor.Funcs{}, cod)
			ctrl := gomock.NewController(t)
			api := mockctrlclient.NewMockClient(ctrl)
			api.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(cod), gomock.Any()).DoAndReturn(stored.Get)
			m := &applier.BoxcutterStorageMigrator{
				Client:             api,
				ActionClientGetter: mockhelmclient.NewMockActionClientGetter(ctrl),
				RevisionGenerator:  mockapplier.NewMockRevisionGenerator(ctrl),
			}

			// When migration resumes; no Helm, generation, or mutation is allowed.
			pending, err := m.Migrate(t.Context(), migrationExtension(), nil)

			// Then the mismatch is reported before returning a usable migration deployment.
			require.ErrorContains(t, err, tc.wantErr)
			require.Equal(t, tc.wantPending, pending)
		})
	}
}

func TestHelmMigrationValidatesExistingContent(t *testing.T) {
	for _, tc := range []struct {
		name                string
		ownerUID            string
		immutable           *bool
		secretType          corev1.SecretType
		extraData           map[string][]byte
		deleting            bool
		wantErr             string
		wantPending         *ocv1.ClusterObjectDeployment
		wantDeploymentCount int
	}{
		{
			name:                "foreign owner",
			ownerUID:            "foreign",
			immutable:           ptr.To(true),
			wantErr:             "does not match",
			wantPending:         nil,
			wantDeploymentCount: 0,
		},
		{
			name:                "mutable",
			immutable:           ptr.To(false),
			wantErr:             "does not match",
			wantPending:         nil,
			wantDeploymentCount: 0,
		},
		{
			name:                "different type",
			immutable:           ptr.To(true),
			secretType:          corev1.SecretTypeOpaque,
			wantErr:             "does not match",
			wantPending:         nil,
			wantDeploymentCount: 0,
		},
		{
			name:                "different data",
			immutable:           ptr.To(true),
			extraData:           map[string][]byte{"extra": []byte("unexpected")},
			wantErr:             "does not match",
			wantPending:         nil,
			wantDeploymentCount: 0,
		},
		{
			name:                "deleting",
			immutable:           ptr.To(true),
			deleting:            true,
			wantErr:             "does not match",
			wantPending:         nil,
			wantDeploymentCount: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given an existing Secret with the mismatch declared in this case, and no COD.
			seed := migrationClient(t, interceptor.Funcs{})
			startMigration(t, seed)
			secrets := &corev1.SecretList{}
			require.NoError(t, seed.List(t.Context(), secrets))
			require.Len(t, secrets.Items, 1)
			secret := &secrets.Items[0]
			if tc.ownerUID != "" {
				secret.OwnerReferences[0].UID = types.UID(tc.ownerUID)
			}
			secret.Immutable = tc.immutable
			if tc.secretType != "" {
				secret.Type = tc.secretType
			}
			for key, value := range tc.extraData {
				secret.Data[key] = value
			}
			if tc.deleting {
				secret.DeletionTimestamp = ptr.To(metav1.Now())
				secret.Finalizers = []string{"retain"}
			}
			stored := migrationClient(t, interceptor.Funcs{}, secret)
			ctrl := gomock.NewController(t)
			ext, rel := migrationExtension(), deployedMigrationRelease()
			helm := mockhelmclient.NewMockActionClientGetterAndInterface(ctrl)
			helm.EXPECT().ActionClientFor(gomock.Any(), ext).Return(helm, nil)
			helm.EXPECT().Get(ext.Name).Return(rel, nil)
			scheme := codTestScheme(t)
			realGenerator := &applier.SimpleRevisionGenerator{Scheme: scheme, SystemNamespace: "system"}
			generator := mockapplier.NewMockRevisionGenerator(ctrl)
			generator.EXPECT().GenerateClusterObjectDeploymentFromHelmRelease(gomock.Any(), rel, ext, nil).
				DoAndReturn(realGenerator.GenerateClusterObjectDeploymentFromHelmRelease)
			api := mockctrlclient.NewMockClient(ctrl)
			api.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(stored.Get).Times(2)
			api.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(stored.List)
			// A create attempt discovers AlreadyExists; no overwrite or COD creation is allowed.
			api.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&corev1.Secret{})).DoAndReturn(stored.Create)
			m := &applier.BoxcutterStorageMigrator{
				Client: api, Scheme: scheme,
				ActionClientGetter: helm,
				RevisionGenerator:  generator,
			}

			// When migration tries to reuse the stored content.
			pending, err := m.Migrate(t.Context(), ext, nil)

			// Then the mismatch blocks COD creation and leaves the Secret untouched.
			require.ErrorContains(t, err, tc.wantErr)
			require.Equal(t, tc.wantPending, pending)
			cods := &ocv1.ClusterObjectDeploymentList{}
			require.NoError(t, stored.List(t.Context(), cods))
			require.Len(t, cods.Items, tc.wantDeploymentCount)
		})
	}
}

func TestHelmMigrationNeverOverwritesConcurrentDeployment(t *testing.T) {
	// Given another controller creating a foreign COD just before our create reaches storage.
	var competing *ocv1.ClusterObjectDeployment
	stored := migrationClient(t, interceptor.Funcs{Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
		if cod, ok := obj.(*ocv1.ClusterObjectDeployment); ok {
			competing = cod.DeepCopy()
			competing.OwnerReferences[0].UID = "foreign"
			require.NoError(t, c.Create(ctx, competing))
		}
		return c.Create(ctx, obj, opts...)
	}})
	ctrl := gomock.NewController(t)
	ext, rel := migrationExtension(), deployedMigrationRelease()
	helm := mockhelmclient.NewMockActionClientGetterAndInterface(ctrl)
	helm.EXPECT().ActionClientFor(gomock.Any(), ext).Return(helm, nil)
	helm.EXPECT().Get(ext.Name).Return(rel, nil)
	scheme := codTestScheme(t)
	realGenerator := &applier.SimpleRevisionGenerator{Scheme: scheme, SystemNamespace: "system"}
	generator := mockapplier.NewMockRevisionGenerator(ctrl)
	generator.EXPECT().GenerateClusterObjectDeploymentFromHelmRelease(gomock.Any(), rel, ext, nil).
		DoAndReturn(realGenerator.GenerateClusterObjectDeploymentFromHelmRelease)
	api := mockctrlclient.NewMockClient(ctrl)
	api.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(stored.Get).Times(2)
	api.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(stored.List)
	gomock.InOrder(
		api.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&corev1.Secret{})).DoAndReturn(stored.Create),
		api.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&ocv1.ClusterObjectDeployment{})).DoAndReturn(stored.Create),
	)
	m := &applier.BoxcutterStorageMigrator{
		Client: api, Scheme: scheme,
		ActionClientGetter: helm,
		RevisionGenerator:  generator,
	}

	// When our create loses the race and migration retries against the existing COD.
	_, createErr := m.Migrate(t.Context(), ext, nil)
	_, retryErr := m.Migrate(t.Context(), ext, nil)

	// Then AlreadyExists is surfaced, the retry rejects the owner, and nothing is overwritten.
	require.True(t, apierrors.IsAlreadyExists(createErr), "%v", createErr)
	require.ErrorContains(t, retryErr, "controlled by ClusterExtension UID")
	actual := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, stored.Get(t.Context(), client.ObjectKeyFromObject(competing), actual))
	require.Equal(t, competing, actual)
}

// migrationExtension returns a fresh, minimal Helm-era CE. Its explicit target
// namespace makes it eligible for migration; its stable UID supports ownership
// checks. It is test data only and is not stored in a client.
func migrationExtension() *ocv1.ClusterExtension {
	return &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: "migration", UID: "extension-uid"},
		Spec:       ocv1.ClusterExtensionSpec{Namespace: "target", ProgressDeadlineMinutes: 15},
	}
}

// deployedMigrationRelease returns a fresh deployed Helm record for the CE named
// "migration": bundle version 1.0.0, Helm revision 1, and one ConfigMap manifest.
// The Helm revision counter and bundle version are independent.
func deployedMigrationRelease() *release.Release {
	return &release.Release{
		Name: "migration", Version: 1, Info: &release.Info{Status: release.StatusDeployed},
		Manifest: `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"config","namespace":"target"},"data":{"setting":"stored"}}` + "\n",
		Labels:   map[string]string{labels.BundleNameKey: "bundle.v1.0.0", labels.BundleVersionKey: "1.0.0", labels.PackageNameKey: "package", labels.BundleReferenceKey: "image:stored", labels.BundleReleaseKey: ""},
	}
}

// migrationClient creates an in-memory fake containing objects, with the COS group
// index and separate COD/COS status subresources. funcs can inject API failures.
// No API server, controllers, or readiness probes run behind this client.
func migrationClient(t *testing.T, funcs interceptor.Funcs, objects ...client.Object) client.Client {
	t.Helper()
	return testutil.WithIndexes(t, fake.NewClientBuilder().WithScheme(codTestScheme(t)), coscontrollers.SetupIndexes).
		WithStatusSubresource(&ocv1.ClusterObjectDeployment{}, &ocv1.ClusterObjectSet{}).
		WithObjects(objects...).WithInterceptorFuncs(funcs).Build()
}

// startMigration runs the real Migrate method once for the default CE and deployed
// Helm record. It stores the initial Adopting COD and its content Secrets in c,
// but does not run the COD/COS controllers or complete the rollout. Setup permits
// one Helm read, one generation, then one Secret create before one COD create.
// Tests wire a separate migrator for the action they are actually verifying.
func startMigration(t *testing.T, c client.Client) *ocv1.ClusterObjectDeployment {
	t.Helper()
	ext, rel := migrationExtension(), deployedMigrationRelease()
	ctrl := gomock.NewController(t)
	helm := mockhelmclient.NewMockActionClientGetterAndInterface(ctrl)
	helm.EXPECT().ActionClientFor(gomock.Any(), ext).Return(helm, nil)
	helm.EXPECT().Get(ext.Name).Return(rel, nil)
	scheme := codTestScheme(t)
	realGenerator := &applier.SimpleRevisionGenerator{Scheme: scheme, SystemNamespace: "system"}
	generator := mockapplier.NewMockRevisionGenerator(ctrl)
	generator.EXPECT().GenerateClusterObjectDeploymentFromHelmRelease(gomock.Any(), rel, ext, nil).
		DoAndReturn(realGenerator.GenerateClusterObjectDeploymentFromHelmRelease)
	api := mockctrlclient.NewMockClient(ctrl)
	api.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(c.Get)
	api.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(c.List)
	gomock.InOrder(
		api.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&corev1.Secret{})).DoAndReturn(c.Create),
		api.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&ocv1.ClusterObjectDeployment{})).DoAndReturn(c.Create),
	)
	m := &applier.BoxcutterStorageMigrator{
		Client: api, Scheme: scheme,
		ActionClientGetter: helm,
		RevisionGenerator:  generator,
	}
	cod, err := m.Migrate(t.Context(), ext, nil)
	require.NoError(t, err)
	require.NotNil(t, cod)
	return cod
}

// completeMigrationRollout simulates the COD controller reporting successful
// completion at cod's current generation. It writes test status only; it does not
// reconcile resources or check their readiness.
func completeMigrationRollout(t *testing.T, c client.Client, cod *ocv1.ClusterObjectDeployment) {
	t.Helper()
	cod.Status.Conditions = []metav1.Condition{{
		Type: ocv1.ClusterObjectDeploymentTypeProgressing, Status: metav1.ConditionFalse,
		Reason:             ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed,
		ObservedGeneration: cod.Generation, LastTransitionTime: metav1.Now(),
	}}
	require.NoError(t, c.Status().Update(t.Context(), cod))
}
