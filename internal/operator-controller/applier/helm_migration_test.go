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
	type input struct {
		latest  *release.Release
		history []*release.Release
	}
	type expected struct {
		release       *release.Release
		historyLookup bool
		pending       bool
		err           error
	}

	testCases := []struct {
		name  string
		input input
		want  expected
	}{
		{
			name: "latest release is deployed",
			input: input{
				latest:  deployed,
				history: nil,
			},
			want: expected{
				release:       deployed,
				historyLookup: false,
				pending:       true,
				err:           nil,
			},
		},
		{
			name: "failed latest with older deployed entry first",
			input: input{
				latest:  failed,
				history: []*release.Release{older, nil, deployed, failed, {Version: 5}},
			},
			want: expected{
				release:       deployed,
				historyLookup: true,
				pending:       true,
				err:           nil,
			},
		},
		{
			name: "failed latest with newer deployed entry first",
			input: input{
				latest:  failed,
				history: []*release.Release{deployed, nil, older, failed, {Version: 5}},
			},
			want: expected{
				release:       deployed,
				historyLookup: true,
				pending:       true,
				err:           nil,
			},
		},
		{
			name: "nil latest",
			input: input{
				latest:  nil,
				history: []*release.Release{nil, older, failed, {Version: 5}},
			},
			want: expected{
				release:       older,
				historyLookup: true,
				pending:       true,
				err:           nil,
			},
		},
		{
			name: "missing latest info",
			input: input{
				latest:  &release.Release{},
				history: []*release.Release{older, failed, {Version: 5}, nil, deployed},
			},
			want: expected{
				release:       deployed,
				historyLookup: true,
				pending:       true,
				err:           nil,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Given the latest Helm record and history declared by this case.
			ctrl := gomock.NewController(t)
			ext := migrationExtension()
			helm := mockhelmclient.NewMockActionClientGetterAndInterface(ctrl)
			helm.EXPECT().ActionClientFor(gomock.Any(), ext).Return(helm, nil)
			helm.EXPECT().Get(ext.Name).Return(tc.input.latest, nil)
			if tc.want.historyLookup {
				helm.EXPECT().History(ext.Name).Return(tc.input.history, nil)
			}
			scheme := codTestScheme(t)
			realGenerator := &applier.SimpleRevisionGenerator{Scheme: scheme, SystemNamespace: "system"}
			generator := mockapplier.NewMockRevisionGenerator(ctrl)
			// Verify the selected record, then generate normal content so migration can succeed.
			generator.EXPECT().GenerateClusterObjectDeploymentFromHelmRelease(gomock.Any(), tc.want.release, ext, nil).
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
			require.ErrorIs(t, err, tc.want.err)
			require.Equal(t, tc.want.pending, pending != nil)
		})
	}
}

func TestHelmMigrationSkipsExistingInstallation(t *testing.T) {
	type input struct {
		objects []client.Object
	}
	type expected struct {
		pending *applier.PendingMigration
		err     error
	}

	testCases := []struct {
		name  string
		input input
		want  expected
	}{
		{
			name: "unmarked deployment stays under ordinary management",
			input: input{
				objects: []client.Object{&ocv1.ClusterObjectDeployment{
					ObjectMeta: metav1.ObjectMeta{Name: "migration"},
				}},
			},
			want: expected{
				pending: nil,
				err:     nil,
			},
		},
		{
			name: "existing revisions do not authorize reconstructing a deployment",
			input: input{
				objects: []client.Object{&ocv1.ClusterObjectSet{
					ObjectMeta: metav1.ObjectMeta{Name: "migration-1"},
					Spec:       ocv1.ClusterObjectSetSpec{Group: "migration", Revision: 1},
				}},
			},
			want: expected{
				pending: nil,
				err:     nil,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Given an existing installation, allow API reads but no writes.
			ctrl := gomock.NewController(t)
			stored := migrationClient(t, interceptor.Funcs{}, tc.input.objects...)
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
			require.ErrorIs(t, err, tc.want.err)
			require.Equal(t, tc.want.pending, pending)
		})
	}
}

func TestHelmMigrationSkipsManagedNamespace(t *testing.T) {
	input := struct {
		namespace string
	}{namespace: ""}
	want := struct {
		pending         *applier.PendingMigration
		err             error
		deploymentReads int
	}{pending: nil, err: nil, deploymentReads: 1}

	// Given a managed-namespace CE, there is no Helm-era installation to recover.
	ext := migrationExtension()
	ext.Spec.Namespace = input.namespace
	ctrl := gomock.NewController(t)
	stored := migrationClient(t, interceptor.Funcs{})
	api := mockctrlclient.NewMockClient(ctrl)
	api.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(stored.Get).Times(want.deploymentReads)
	m := &applier.BoxcutterStorageMigrator{
		Client:             api,
		ActionClientGetter: mockhelmclient.NewMockActionClientGetter(ctrl),
		RevisionGenerator:  mockapplier.NewMockRevisionGenerator(ctrl),
	}

	// When migration is attempted, only the initial COD lookup is allowed.
	pending, err := m.Migrate(t.Context(), ext, nil)

	// Then no migration is pending and no Helm lookup, rendering, or write occurs.
	require.ErrorIs(t, err, want.err)
	require.Equal(t, want.pending, pending)
}

func TestHelmMigrationSkipsMissingDeployedRelease(t *testing.T) {
	type input struct {
		latest     *release.Release
		getErr     error
		history    []*release.Release
		historyErr error
	}
	type expected struct {
		historyLookup bool
		pending       *applier.PendingMigration
		err           error
	}

	testCases := []struct {
		name  string
		input input
		want  expected
	}{
		{
			name: "release missing",
			input: input{
				latest:     nil,
				getErr:     driver.ErrReleaseNotFound,
				history:    nil,
				historyErr: nil,
			},
			want: expected{
				historyLookup: false,
				pending:       nil,
				err:           nil,
			},
		},
		{
			name: "history missing",
			input: input{
				latest:     nil,
				getErr:     nil,
				history:    nil,
				historyErr: driver.ErrReleaseNotFound,
			},
			want: expected{
				historyLookup: true,
				pending:       nil,
				err:           nil,
			},
		},
		{
			name: "history has no deployed release",
			input: input{
				latest:     nil,
				getErr:     nil,
				history:    []*release.Release{nil, {}, {Info: &release.Info{Status: release.StatusFailed}}},
				historyErr: nil,
			},
			want: expected{
				historyLookup: true,
				pending:       nil,
				err:           nil,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Given Helm responses that contain no deployed installation.
			ctrl := gomock.NewController(t)
			ext := migrationExtension()
			helm := mockhelmclient.NewMockActionClientGetterAndInterface(ctrl)
			helm.EXPECT().ActionClientFor(gomock.Any(), ext).Return(helm, nil)
			helm.EXPECT().Get(ext.Name).Return(tc.input.latest, tc.input.getErr)
			if tc.want.historyLookup {
				helm.EXPECT().History(ext.Name).Return(tc.input.history, tc.input.historyErr)
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
			require.ErrorIs(t, err, tc.want.err)
			require.Equal(t, tc.want.pending, pending)
		})
	}
}

func TestHelmMigrationCreatesDeploymentOwnershipChain(t *testing.T) {
	input := struct {
		extension        *ocv1.ClusterExtension
		release          *release.Release
		objectLabels     map[string]string
		contentNamespace string
	}{
		extension:        migrationExtension(),
		release:          deployedMigrationRelease(),
		objectLabels:     nil,
		contentNamespace: "system",
	}
	want := struct {
		pending                    bool
		err                        error
		deploymentOwnedByExtension bool
		deploymentOwnerCount       int
		deploymentOwnerUID         types.UID
		contentOwnedByExtension    bool
		managedBy                  string
		protection                 ocv1.CollisionProtection
		secretCount                int
		immutable                  *bool
		revisionCount              int
	}{
		pending:                    true,
		err:                        nil,
		deploymentOwnedByExtension: false,
		deploymentOwnerCount:       1,
		deploymentOwnerUID:         input.extension.UID,
		contentOwnedByExtension:    true,
		managedBy:                  "",
		protection:                 ocv1.CollisionProtectionNone,
		secretCount:                1,
		immutable:                  ptr.To(true),
		revisionCount:              0,
	}

	// Given a deployed Helm installation and an empty Kubernetes store.
	ext, rel := input.extension, input.release
	stored := migrationClient(t, interceptor.Funcs{})
	ctrl := gomock.NewController(t)
	helm := mockhelmclient.NewMockActionClientGetterAndInterface(ctrl)
	helm.EXPECT().ActionClientFor(gomock.Any(), ext).Return(helm, nil)
	helm.EXPECT().Get(ext.Name).Return(rel, nil)
	scheme := codTestScheme(t)
	realGenerator := &applier.SimpleRevisionGenerator{Scheme: scheme, SystemNamespace: input.contentNamespace}
	generator := mockapplier.NewMockRevisionGenerator(ctrl)
	generator.EXPECT().GenerateClusterObjectDeploymentFromHelmRelease(gomock.Any(), rel, ext, input.objectLabels).
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
	pending, err := m.Migrate(t.Context(), ext, input.objectLabels)

	// Then the CE is a non-controller owner of the staged COD and controls its content Secrets.
	require.ErrorIs(t, err, want.err)
	require.Equal(t, want.pending, pending != nil)
	cod := pending.Deployment
	require.Equal(t, want.deploymentOwnedByExtension, metav1.IsControlledBy(cod, ext))
	require.Len(t, cod.OwnerReferences, want.deploymentOwnerCount)
	require.Equal(t, want.deploymentOwnerUID, cod.OwnerReferences[0].UID)
	require.Equal(t, want.managedBy, cod.Labels[labels.ManagedByKey])
	require.Equal(t, want.protection, cod.Spec.Template.Spec.CollisionProtection)
	secrets := &corev1.SecretList{}
	require.NoError(t, stored.List(t.Context(), secrets))
	require.Len(t, secrets.Items, want.secretCount)
	for i := range secrets.Items {
		require.Equal(t, want.contentOwnedByExtension, metav1.IsControlledBy(&secrets.Items[i], ext))
		require.Equal(t, want.immutable, secrets.Items[i].Immutable)
	}
	sets := &ocv1.ClusterObjectSetList{}
	require.NoError(t, stored.List(t.Context(), sets))
	require.Len(t, sets.Items, want.revisionCount)
}

func TestHelmMigrationWaitsForCurrentCompletion(t *testing.T) {
	type input struct {
		generation  int64
		progressing metav1.Condition
		available   metav1.Condition
		protection  ocv1.CollisionProtection
	}
	type expected struct {
		pending   bool
		unchanged bool
		err       error
	}

	testCases := []struct {
		name  string
		input input
		want  expected
	}{
		{
			name: "adopting is pending",
			input: input{
				protection: ocv1.CollisionProtectionNone,
				generation: 2,
				progressing: metav1.Condition{
					Type:               ocv1.ClusterObjectDeploymentTypeProgressing,
					Status:             metav1.ConditionTrue,
					Reason:             ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing,
					ObservedGeneration: 2,
				},
				available: metav1.Condition{
					Type:               ocv1.ClusterObjectDeploymentTypeAvailable,
					Status:             metav1.ConditionTrue,
					Reason:             "Available",
					ObservedGeneration: 2,
				},
			},
			want: expected{
				pending:   true,
				unchanged: true,
				err:       nil,
			},
		},
		{
			name: "protecting is pending",
			input: input{
				protection: ocv1.CollisionProtectionPrevent,
				generation: 2,
				progressing: metav1.Condition{
					Type:               ocv1.ClusterObjectDeploymentTypeProgressing,
					Status:             metav1.ConditionTrue,
					Reason:             ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing,
					ObservedGeneration: 2,
				},
				available: metav1.Condition{
					Type:               ocv1.ClusterObjectDeploymentTypeAvailable,
					Status:             metav1.ConditionTrue,
					Reason:             "Available",
					ObservedGeneration: 2,
				},
			},
			want: expected{
				pending:   true,
				unchanged: true,
				err:       nil,
			},
		},
		{
			name: "stale adoption completion",
			input: input{
				protection: ocv1.CollisionProtectionNone,
				generation: 2,
				progressing: metav1.Condition{
					Type:               ocv1.ClusterObjectDeploymentTypeProgressing,
					Status:             metav1.ConditionFalse,
					Reason:             ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed,
					ObservedGeneration: 1,
				},
				available: metav1.Condition{
					Type:               ocv1.ClusterObjectDeploymentTypeAvailable,
					Status:             metav1.ConditionTrue,
					Reason:             "Available",
					ObservedGeneration: 2,
				},
			},
			want: expected{
				pending:   true,
				unchanged: true,
				err:       nil,
			},
		},
		{
			name: "stale protected completion",
			input: input{
				protection: ocv1.CollisionProtectionPrevent,
				generation: 2,
				progressing: metav1.Condition{
					Type:               ocv1.ClusterObjectDeploymentTypeProgressing,
					Status:             metav1.ConditionFalse,
					Reason:             ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed,
					ObservedGeneration: 1,
				},
				available: metav1.Condition{
					Type:               ocv1.ClusterObjectDeploymentTypeAvailable,
					Status:             metav1.ConditionTrue,
					Reason:             "Available",
					ObservedGeneration: 2,
				},
			},
			want: expected{
				pending:   true,
				unchanged: true,
				err:       nil,
			},
		},
		{
			name: "deadline failure remains pending",
			input: input{
				protection: ocv1.CollisionProtectionPrevent,
				generation: 2,
				progressing: metav1.Condition{
					Type:               ocv1.ClusterObjectDeploymentTypeProgressing,
					Status:             metav1.ConditionFalse,
					Reason:             ocv1.ClusterObjectDeploymentReasonProgressDeadlineExceeded,
					ObservedGeneration: 2,
				},
				available: metav1.Condition{
					Type:               ocv1.ClusterObjectDeploymentTypeAvailable,
					Status:             metav1.ConditionTrue,
					Reason:             "Available",
					ObservedGeneration: 2,
				},
			},
			want: expected{
				pending:   true,
				unchanged: true,
				err:       nil,
			},
		},
		{
			name: "success reason with wrong status",
			input: input{
				protection: ocv1.CollisionProtectionNone,
				generation: 2,
				progressing: metav1.Condition{
					Type:               ocv1.ClusterObjectDeploymentTypeProgressing,
					Status:             metav1.ConditionTrue,
					Reason:             ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed,
					ObservedGeneration: 2,
				},
				available: metav1.Condition{
					Type:               ocv1.ClusterObjectDeploymentTypeAvailable,
					Status:             metav1.ConditionTrue,
					Reason:             "Available",
					ObservedGeneration: 2,
				},
			},
			want: expected{
				pending:   true,
				unchanged: true,
				err:       nil,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Given a stored migration with the generation and rollout conditions declared above.
			stored := migrationClient(t, interceptor.Funcs{})
			cod := startMigration(t, stored)
			cod.Generation = tc.input.generation
			cod.Spec.Template.Spec.CollisionProtection = tc.input.protection
			require.NoError(t, stored.Update(t.Context(), cod))
			cod.Status.Conditions = []metav1.Condition{tc.input.progressing, tc.input.available}
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
			require.ErrorIs(t, err, tc.want.err)
			require.Equal(t, tc.want.pending, pending != nil)
			if tc.want.unchanged {
				require.Equal(t, before, pending.Deployment)
			}
		})
	}
}

func TestHelmMigrationProtectsFrozenTemplate(t *testing.T) {
	input := struct {
		extension             *ocv1.ClusterExtension
		deadlineAfterSnapshot int32
		objectLabels          map[string]string
	}{
		extension:             migrationExtension(),
		deadlineAfterSnapshot: 30,
		objectLabels:          map[string]string{"changed": "ignored"},
	}

	// Given completed adoption and a CE edited after the migration snapshot was stored.
	stored := migrationClient(t, interceptor.Funcs{})
	cod := startMigration(t, stored)
	completeMigrationRollout(t, stored, cod)
	want := struct {
		spec       *ocv1.ClusterObjectDeploymentSpec
		pending    bool
		err        error
		patchCalls int
	}{
		spec:       cod.Spec.DeepCopy(),
		pending:    true,
		err:        nil,
		patchCalls: 1,
	}
	want.spec.Template.Spec.CollisionProtection = ocv1.CollisionProtectionPrevent
	ext := input.extension
	ext.Spec.ProgressDeadlineMinutes = input.deadlineAfterSnapshot
	ctrl := gomock.NewController(t)
	api := mockctrlclient.NewMockClient(ctrl)
	api.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(cod), gomock.Any()).DoAndReturn(stored.Get)
	api.EXPECT().Patch(gomock.Any(), gomock.AssignableToTypeOf(cod), gomock.Any()).DoAndReturn(stored.Patch).Times(want.patchCalls)
	m := &applier.BoxcutterStorageMigrator{
		Client:             api,
		ActionClientGetter: mockhelmclient.NewMockActionClientGetter(ctrl),
		RevisionGenerator:  mockapplier.NewMockRevisionGenerator(ctrl),
	}

	// When migration advances using the stored snapshot, with exactly one COD patch.
	pending, err := m.Migrate(t.Context(), ext, input.objectLabels)

	// Then only protection changes; content, probes, metadata, and deadline stay frozen.
	require.ErrorIs(t, err, want.err)
	require.Equal(t, want.pending, pending != nil)
	require.Equal(t, *want.spec, pending.Deployment.Spec)
	actual := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, stored.Get(t.Context(), client.ObjectKeyFromObject(cod), actual))
	require.Equal(t, pending.Deployment, actual)
}

func TestHelmMigrationClaimsDeploymentAfterProtectedCompletion(t *testing.T) {
	input := struct {
		extension       *ocv1.ClusterExtension
		protection      ocv1.CollisionProtection
		annotationKey   string
		annotationValue string
	}{
		extension:       migrationExtension(),
		protection:      ocv1.CollisionProtectionPrevent,
		annotationKey:   "unrelated",
		annotationValue: "preserved",
	}

	// Given a protected rollout that completed successfully.
	stored := migrationClient(t, interceptor.Funcs{})
	cod := startMigration(t, stored)
	cod.Annotations = map[string]string{input.annotationKey: input.annotationValue}
	cod.Spec.Template.Spec.CollisionProtection = input.protection
	require.NoError(t, stored.Update(t.Context(), cod))
	completeMigrationRollout(t, stored, cod)
	want := struct {
		pending         bool
		err             error
		managedBy       string
		controllerUID   types.UID
		annotationValue string
		spec            ocv1.ClusterObjectDeploymentSpec
		patchCalls      int
	}{
		pending:         true,
		err:             nil,
		managedBy:       labels.ManagedByOperatorController,
		controllerUID:   input.extension.UID,
		annotationValue: "preserved",
		spec:            *cod.Spec.DeepCopy(),
		patchCalls:      1,
	}
	ctrl := gomock.NewController(t)
	api := mockctrlclient.NewMockClient(ctrl)
	api.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(cod), gomock.Any()).DoAndReturn(stored.Get)
	api.EXPECT().Patch(gomock.Any(), gomock.AssignableToTypeOf(cod), gomock.Any()).DoAndReturn(stored.Patch).Times(want.patchCalls)
	m := &applier.BoxcutterStorageMigrator{
		Client:             api,
		Scheme:             codTestScheme(t),
		ActionClientGetter: mockhelmclient.NewMockActionClientGetter(ctrl),
		RevisionGenerator:  mockapplier.NewMockRevisionGenerator(ctrl),
	}

	// When the migrator claims the protected deployment in one patch.
	pending, err := m.Migrate(t.Context(), input.extension, nil)

	// Then the same write persists both the managed-by label and controller reference.
	require.ErrorIs(t, err, want.err)
	require.Equal(t, want.pending, pending != nil)
	actual := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, stored.Get(t.Context(), client.ObjectKeyFromObject(cod), actual))
	require.Equal(t, want.managedBy, actual.Labels[labels.ManagedByKey])
	owner := metav1.GetControllerOf(actual)
	require.NotNil(t, owner)
	require.Equal(t, want.controllerUID, owner.UID)
	require.Equal(t, want.annotationValue, actual.Annotations[input.annotationKey])
	require.Equal(t, want.spec, actual.Spec)
}

func TestHelmMigrationRetriesInterruptedCreation(t *testing.T) {
	connectionLost := errors.New("connection lost")
	type input struct {
		writeErr             error
		failedKind           string
		persistBeforeFailure bool
	}
	type expected struct {
		firstPending         bool
		failureInjected      bool
		retryPending         bool
		firstDeploymentCalls int
		firstErr             error
		retryHelmCalls       int
		retrySecretCalls     int
		retryDeploymentCalls int
		retryErr             error
		secretCount          int
		deploymentCount      int
	}

	testCases := []struct {
		name  string
		input input
		want  expected
	}{
		{
			name: "Secret request failed",
			input: input{
				writeErr:             connectionLost,
				failedKind:           "Secret",
				persistBeforeFailure: false,
			},
			want: expected{
				firstPending:         false,
				failureInjected:      true,
				retryPending:         true,
				firstDeploymentCalls: 0,
				firstErr:             connectionLost,
				retryHelmCalls:       1,
				retrySecretCalls:     1,
				retryDeploymentCalls: 1,
				retryErr:             nil,
				secretCount:          1,
				deploymentCount:      1,
			},
		},
		{
			name: "Secret response lost",
			input: input{
				writeErr:             connectionLost,
				failedKind:           "Secret",
				persistBeforeFailure: true,
			},
			want: expected{
				firstPending:         false,
				failureInjected:      true,
				retryPending:         true,
				firstDeploymentCalls: 0,
				firstErr:             connectionLost,
				retryHelmCalls:       1,
				retrySecretCalls:     1,
				retryDeploymentCalls: 1,
				retryErr:             nil,
				secretCount:          1,
				deploymentCount:      1,
			},
		},
		{
			name: "deployment request failed after Secret",
			input: input{
				writeErr:             connectionLost,
				failedKind:           "ClusterObjectDeployment",
				persistBeforeFailure: false,
			},
			want: expected{
				firstPending:         false,
				failureInjected:      true,
				retryPending:         true,
				firstDeploymentCalls: 1,
				firstErr:             connectionLost,
				retryHelmCalls:       1,
				retrySecretCalls:     1,
				retryDeploymentCalls: 1,
				retryErr:             nil,
				secretCount:          1,
				deploymentCount:      1,
			},
		},
		{
			name: "deployment response lost",
			input: input{
				writeErr:             connectionLost,
				failedKind:           "ClusterObjectDeployment",
				persistBeforeFailure: true,
			},
			want: expected{
				firstPending:         false,
				failureInjected:      true,
				retryPending:         true,
				firstDeploymentCalls: 1,
				firstErr:             connectionLost,
				retryHelmCalls:       0,
				retrySecretCalls:     0,
				retryDeploymentCalls: 0,
				retryErr:             nil,
				secretCount:          1,
				deploymentCount:      1,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Given a one-time failed write, optionally persisted before its response is lost.
			failed := false
			scheme := codTestScheme(t)
			stored := migrationClient(t, interceptor.Funcs{Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				gvks, _, err := scheme.ObjectKinds(obj)
				require.NoError(t, err)
				if !failed && gvks[0].Kind == tc.input.failedKind {
					failed = true
					if tc.input.persistBeforeFailure {
						require.NoError(t, c.Create(ctx, obj, opts...))
					}
					return tc.input.writeErr
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
				api.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&ocv1.ClusterObjectDeployment{})).DoAndReturn(stored.Create).Times(tc.want.firstDeploymentCalls),
			)
			m := &applier.BoxcutterStorageMigrator{
				Client: api, Scheme: scheme,
				ActionClientGetter: helm,
				RevisionGenerator:  generator,
			}

			// When the first attempt encounters the interrupted write.
			pending, err := m.Migrate(t.Context(), ext, nil)

			// Then the error is surfaced, leaving any successful writes available to retry.
			require.ErrorIs(t, err, tc.want.firstErr)
			require.Equal(t, tc.want.firstPending, pending != nil)
			require.Equal(t, tc.want.failureInjected, failed, "the configured failure must be exercised")

			// Given a reconstructed migrator: only an absent COD permits Helm and generation again.
			retryHelm := mockhelmclient.NewMockActionClientGetterAndInterface(ctrl)
			retryHelm.EXPECT().ActionClientFor(gomock.Any(), ext).Return(retryHelm, nil).Times(tc.want.retryHelmCalls)
			retryHelm.EXPECT().Get(ext.Name).Return(rel, nil).Times(tc.want.retryHelmCalls)
			retryGenerator := mockapplier.NewMockRevisionGenerator(ctrl)
			retryGenerator.EXPECT().GenerateClusterObjectDeploymentFromHelmRelease(gomock.Any(), rel, ext, nil).
				DoAndReturn(realGenerator.GenerateClusterObjectDeploymentFromHelmRelease).Times(tc.want.retryHelmCalls)
			retryAPI := mockctrlclient.NewMockClient(ctrl)
			retryAPI.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(stored.Get).AnyTimes()
			retryAPI.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(stored.List).Times(tc.want.retryHelmCalls)
			gomock.InOrder(
				retryAPI.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&corev1.Secret{})).DoAndReturn(stored.Create).Times(tc.want.retrySecretCalls),
				retryAPI.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&ocv1.ClusterObjectDeployment{})).DoAndReturn(stored.Create).Times(tc.want.retryDeploymentCalls),
			)
			retry := &applier.BoxcutterStorageMigrator{
				Client:             retryAPI,
				Scheme:             scheme,
				ActionClientGetter: retryHelm,
				RevisionGenerator:  retryGenerator,
			}

			// When the same migration is retried.
			pending, err = retry.Migrate(t.Context(), ext, nil)

			// Then a single adopting COD and a single content Secret remain.
			require.ErrorIs(t, err, tc.want.retryErr)
			require.Equal(t, tc.want.retryPending, pending != nil)
			secrets := &corev1.SecretList{}
			require.NoError(t, stored.List(t.Context(), secrets))
			require.Len(t, secrets.Items, tc.want.secretCount)
			cods := &ocv1.ClusterObjectDeploymentList{}
			require.NoError(t, stored.List(t.Context(), cods))
			require.Len(t, cods.Items, tc.want.deploymentCount)
		})
	}
}

func TestHelmMigrationRetriesLostTransitionResponse(t *testing.T) {
	responseLost := errors.New("response lost")
	type input struct {
		generation         int64
		patchErr           error
		labeled            bool
		protection         ocv1.CollisionProtection
		simulateGeneration bool
	}
	type expected struct {
		firstErr     error
		managedBy    string
		protection   ocv1.CollisionProtection
		retryPending bool
		retryErr     error
	}

	testCases := []struct {
		name  string
		input input
		want  expected
	}{
		{
			name: "adoption-to-protection response lost",
			input: input{
				generation:         1,
				patchErr:           responseLost,
				protection:         ocv1.CollisionProtectionNone,
				simulateGeneration: true,
			},
			want: expected{
				firstErr:     responseLost,
				managedBy:    "",
				protection:   ocv1.CollisionProtectionPrevent,
				retryPending: true,
				retryErr:     nil,
			},
		},
		{
			name: "controller claim response lost",
			input: input{
				generation: 1,
				patchErr:   responseLost,
				protection: ocv1.CollisionProtectionPrevent,
			},
			want: expected{
				firstErr:     responseLost,
				managedBy:    labels.ManagedByOperatorController,
				protection:   ocv1.CollisionProtectionPrevent,
				retryPending: false,
				retryErr:     nil,
			},
		},
		{
			name: "controller claim response lost on previously labeled deployment",
			input: input{
				generation:         1,
				patchErr:           responseLost,
				protection:         ocv1.CollisionProtectionPrevent,
				simulateGeneration: false,
				labeled:            true,
			},
			want: expected{
				firstErr:     responseLost,
				managedBy:    labels.ManagedByOperatorController,
				protection:   ocv1.CollisionProtectionPrevent,
				retryPending: false,
				retryErr:     nil,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Given a completed stage and a patch that persists but loses its response.
			stored := migrationClient(t, interceptor.Funcs{})
			cod := startMigration(t, stored)
			cod.Generation = tc.input.generation
			if tc.input.labeled {
				cod.Labels[labels.ManagedByKey] = labels.ManagedByOperatorController
			}
			cod.Spec.Template.Spec.CollisionProtection = tc.input.protection
			require.NoError(t, stored.Update(t.Context(), cod))
			completeMigrationRollout(t, stored, cod)
			ctrl := gomock.NewController(t)
			api := mockctrlclient.NewMockClient(ctrl)
			api.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(cod), gomock.Any()).DoAndReturn(stored.Get)
			api.EXPECT().Patch(gomock.Any(), gomock.AssignableToTypeOf(cod), gomock.Any()).
				DoAndReturn(func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					require.NoError(t, stored.Patch(ctx, obj, patch, opts...))
					return tc.input.patchErr
				})
			m := &applier.BoxcutterStorageMigrator{
				Client:             api,
				Scheme:             codTestScheme(t),
				ActionClientGetter: mockhelmclient.NewMockActionClientGetter(ctrl),
				RevisionGenerator:  mockapplier.NewMockRevisionGenerator(ctrl),
			}

			// When migration attempts the transition.
			_, err := m.Migrate(t.Context(), migrationExtension(), nil)

			// Then the caller sees the lost response although the intended state is stored.
			require.ErrorIs(t, err, tc.want.firstErr)
			actual := &ocv1.ClusterObjectDeployment{}
			require.NoError(t, stored.Get(t.Context(), client.ObjectKeyFromObject(cod), actual))
			require.Equal(t, tc.want.managedBy, actual.Labels[labels.ManagedByKey])
			require.Equal(t, tc.want.protection, actual.Spec.Template.Spec.CollisionProtection)

			// Given a restart, simulate the API generation bump for a changed template.
			if tc.input.simulateGeneration {
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
			require.ErrorIs(t, err, tc.want.retryErr)
			require.Equal(t, tc.want.retryPending, pending != nil)
			if tc.want.retryPending {
				require.Equal(t, actual, pending.Deployment)
			}
		})
	}
}

func TestHelmMigrationTransitionUsesOptimisticLock(t *testing.T) {
	input := struct {
		annotationKey   string
		annotationValue string
	}{
		annotationKey:   "concurrent",
		annotationValue: "change",
	}
	want := struct {
		conflict        bool
		pending         *applier.PendingMigration
		protection      ocv1.CollisionProtection
		annotationValue string
	}{
		conflict:        true,
		pending:         nil,
		protection:      ocv1.CollisionProtectionNone,
		annotationValue: "change",
	}

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
			concurrent.Annotations = map[string]string{input.annotationKey: input.annotationValue}
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
	require.Equal(t, want.conflict, apierrors.IsConflict(err), "%v", err)
	require.Equal(t, want.pending, pending)
	actual := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, stored.Get(t.Context(), client.ObjectKeyFromObject(cod), actual))
	require.Equal(t, want.protection, actual.Spec.Template.Spec.CollisionProtection)
	require.Equal(t, want.annotationValue, actual.Annotations[input.annotationKey])
}

func TestHelmMigrationRejectsInvalidPersistedState(t *testing.T) {
	ext := migrationExtension()
	cases := []struct {
		name       string
		ownerUID   types.UID
		controller *metav1.OwnerReference
		deleting   bool
		label      string
		protection ocv1.CollisionProtection
		wantErr    string
	}{
		{
			name: "another controller owns staged COD", ownerUID: ext.UID,
			controller: metav1.NewControllerRef(&metav1.ObjectMeta{Name: "other", UID: "other"}, ocv1.GroupVersion.WithKind(ocv1.ClusterExtensionKind)),
			wantErr:    "no longer staged",
		},
		{name: "recreated CE", ownerUID: "old-uid", wantErr: "no longer staged"},
		{name: "deleting COD", ownerUID: ext.UID, deleting: true, wantErr: "is deleting"},
		{name: "foreign managed-by label", ownerUID: ext.UID, label: "other", protection: ocv1.CollisionProtectionPrevent, wantErr: "unexpected collision protection"},
		{name: "premature adoption label", ownerUID: ext.UID, label: labels.ManagedByOperatorController, protection: ocv1.CollisionProtectionNone, wantErr: "unexpected collision protection"},
		{name: "unexpected protection", ownerUID: ext.UID, protection: ocv1.CollisionProtectionIfNoController, wantErr: "unexpected collision protection"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cod := startMigration(t, migrationClient(t, interceptor.Funcs{}))
			cod.OwnerReferences[0].UID = tc.ownerUID
			if tc.controller != nil {
				cod.OwnerReferences = append(cod.OwnerReferences, *tc.controller)
			}
			if tc.deleting {
				cod.DeletionTimestamp = ptr.To(metav1.Now())
				cod.Finalizers = []string{"retain"}
			}
			if tc.label != "" {
				cod.Labels[labels.ManagedByKey] = tc.label
			}
			cod.Spec.Template.Spec.CollisionProtection = tc.protection
			stored := migrationClient(t, interceptor.Funcs{}, cod)
			api := mockctrlclient.NewMockClient(gomock.NewController(t))
			api.EXPECT().Get(gomock.Any(), client.ObjectKeyFromObject(cod), gomock.Any()).DoAndReturn(stored.Get)
			m := &applier.BoxcutterStorageMigrator{Client: api}
			_, err := m.Migrate(t.Context(), ext, nil)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestHelmMigrationValidatesExistingContent(t *testing.T) {
	type input struct {
		ownerUID   string
		immutable  *bool
		secretType corev1.SecretType
		extraData  map[string][]byte
		deleting   bool
	}
	type expected struct {
		err             string
		pending         *applier.PendingMigration
		deploymentCount int
	}

	testCases := []struct {
		name  string
		input input
		want  expected
	}{
		{
			name: "foreign owner",
			input: input{
				ownerUID:   "foreign",
				immutable:  ptr.To(true),
				secretType: "",
				extraData:  nil,
				deleting:   false,
			},
			want: expected{
				err:             "does not match",
				pending:         nil,
				deploymentCount: 0,
			},
		},
		{
			name: "mutable",
			input: input{
				ownerUID:   "",
				immutable:  ptr.To(false),
				secretType: "",
				extraData:  nil,
				deleting:   false,
			},
			want: expected{
				err:             "does not match",
				pending:         nil,
				deploymentCount: 0,
			},
		},
		{
			name: "different type",
			input: input{
				ownerUID:   "",
				immutable:  ptr.To(true),
				secretType: corev1.SecretTypeOpaque,
				extraData:  nil,
				deleting:   false,
			},
			want: expected{
				err:             "does not match",
				pending:         nil,
				deploymentCount: 0,
			},
		},
		{
			name: "different data",
			input: input{
				ownerUID:   "",
				immutable:  ptr.To(true),
				secretType: "",
				extraData:  map[string][]byte{"extra": []byte("unexpected")},
				deleting:   false,
			},
			want: expected{
				err:             "does not match",
				pending:         nil,
				deploymentCount: 0,
			},
		},
		{
			name: "deleting",
			input: input{
				ownerUID:   "",
				immutable:  ptr.To(true),
				secretType: "",
				extraData:  nil,
				deleting:   true,
			},
			want: expected{
				err:             "does not match",
				pending:         nil,
				deploymentCount: 0,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Given an existing Secret with the mismatch declared in this case, and no COD.
			seed := migrationClient(t, interceptor.Funcs{})
			startMigration(t, seed)
			secrets := &corev1.SecretList{}
			require.NoError(t, seed.List(t.Context(), secrets))
			require.Len(t, secrets.Items, 1)
			secret := &secrets.Items[0]
			if tc.input.ownerUID != "" {
				secret.OwnerReferences[0].UID = types.UID(tc.input.ownerUID)
			}
			secret.Immutable = tc.input.immutable
			if tc.input.secretType != "" {
				secret.Type = tc.input.secretType
			}
			for key, value := range tc.input.extraData {
				secret.Data[key] = value
			}
			if tc.input.deleting {
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
			require.ErrorContains(t, err, tc.want.err)
			require.Equal(t, tc.want.pending, pending)
			cods := &ocv1.ClusterObjectDeploymentList{}
			require.NoError(t, stored.List(t.Context(), cods))
			require.Len(t, cods.Items, tc.want.deploymentCount)
		})
	}
}

func TestHelmMigrationNeverOverwritesConcurrentDeployment(t *testing.T) {
	input := struct {
		extension        *ocv1.ClusterExtension
		release          *release.Release
		foreignOwnerUID  types.UID
		contentNamespace string
	}{
		extension:        migrationExtension(),
		release:          deployedMigrationRelease(),
		foreignOwnerUID:  "foreign",
		contentNamespace: "system",
	}
	want := struct {
		alreadyExists                bool
		retryErrContains             string
		preserveConcurrentDeployment bool
		deploymentReads              int
	}{
		alreadyExists:                true,
		retryErrContains:             "no longer staged for ClusterExtension UID",
		preserveConcurrentDeployment: true,
		deploymentReads:              2,
	}

	// Given another controller creating a foreign COD just before our create reaches storage.
	var competing *ocv1.ClusterObjectDeployment
	stored := migrationClient(t, interceptor.Funcs{Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
		if cod, ok := obj.(*ocv1.ClusterObjectDeployment); ok {
			competing = cod.DeepCopy()
			competing.OwnerReferences[0].UID = input.foreignOwnerUID
			require.NoError(t, c.Create(ctx, competing))
		}
		return c.Create(ctx, obj, opts...)
	}})
	ctrl := gomock.NewController(t)
	ext, rel := input.extension, input.release
	helm := mockhelmclient.NewMockActionClientGetterAndInterface(ctrl)
	helm.EXPECT().ActionClientFor(gomock.Any(), ext).Return(helm, nil)
	helm.EXPECT().Get(ext.Name).Return(rel, nil)
	scheme := codTestScheme(t)
	realGenerator := &applier.SimpleRevisionGenerator{Scheme: scheme, SystemNamespace: input.contentNamespace}
	generator := mockapplier.NewMockRevisionGenerator(ctrl)
	generator.EXPECT().GenerateClusterObjectDeploymentFromHelmRelease(gomock.Any(), rel, ext, nil).
		DoAndReturn(realGenerator.GenerateClusterObjectDeploymentFromHelmRelease)
	api := mockctrlclient.NewMockClient(ctrl)
	api.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(stored.Get).Times(want.deploymentReads)
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

	// When our create loses the race and migration retries against the existing COD.
	_, createErr := m.Migrate(t.Context(), ext, nil)
	_, retryErr := m.Migrate(t.Context(), ext, nil)

	// Then AlreadyExists is surfaced, the retry rejects the owner, and nothing is overwritten.
	require.Equal(t, want.alreadyExists, apierrors.IsAlreadyExists(createErr), "%v", createErr)
	require.ErrorContains(t, retryErr, want.retryErrContains)
	actual := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, stored.Get(t.Context(), client.ObjectKeyFromObject(competing), actual))
	if want.preserveConcurrentDeployment {
		require.Equal(t, competing, actual)
	}
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
	pending, err := m.Migrate(t.Context(), ext, nil)
	require.NoError(t, err)
	require.NotNil(t, pending)
	return pending.Deployment
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
