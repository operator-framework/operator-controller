package applier_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	helmstorage "github.com/operator-framework/helm-operator-plugins/pkg/storage"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	coscontrollers "github.com/operator-framework/operator-controller/internal/object-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
	hashutil "github.com/operator-framework/operator-controller/internal/shared/util/hash"
	"github.com/operator-framework/operator-controller/internal/testutil"
)

const migrationNamespace = "olmv1-system"

func TestHelmMigrationUsesOwnedHistory(t *testing.T) {
	ext := migrationExtension()
	type expected struct {
		errContains    string
		pending        *applier.PendingMigration
		historyReadErr error
	}
	testCases := []struct {
		name   string
		change func(*corev1.Secret)
		want   expected
	}{
		{name: "matching failed-only history blocks", want: expected{errContains: "no deployed revision to migrate"}},
		{name: "different CE UID", change: func(s *corev1.Secret) { s.OwnerReferences[0].UID = "old-ce" }},
		{name: "different release", change: func(s *corev1.Secret) { s.Labels["name"] = "other" }},
		{name: "different namespace", change: func(s *corev1.Secret) { s.Namespace = "other" }},
		{name: "different type", change: func(s *corev1.Secret) { s.Type = labels.SecretTypeObjectData }},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			index := migrationIndex(ext, 1, release.StatusFailed)
			if tc.change != nil {
				tc.change(index)
			}
			m := newMigrationTest(t, interceptor.Funcs{}, index)

			pending, err := m.Migrate(t.Context(), ext, nil)

			if tc.want.errContains == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.want.errContains)
			}
			require.Equal(t, tc.want.pending, pending)
			require.ErrorIs(t, m.Client.Get(t.Context(), client.ObjectKeyFromObject(index), &corev1.Secret{}), tc.want.historyReadErr)
		})
	}
}

func TestHelmMigrationSelectsNewestDeployedRelease(t *testing.T) {
	ext := migrationExtension()
	old := migrationIndex(ext, 2, release.StatusDeployed)
	deployed := migrationIndex(ext, 10, release.StatusDeployed)
	failed := migrationIndex(ext, 11, release.StatusFailed)
	foreign := migrationIndex(ext, 12, release.StatusDeployed)
	foreign.OwnerReferences[0].UID = "old-ce"
	releaseVersion := "10.0.0"
	want := struct {
		err     error
		pending bool
		version string
	}{err: nil, pending: true, version: releaseVersion}

	m := newMigrationTest(t, interceptor.Funcs{}, old, deployed, failed, foreign)
	seedMigrationRelease(t, m, ext, deployed, 10, releaseVersion)

	pending, err := m.Migrate(t.Context(), ext, nil)

	require.ErrorIs(t, err, want.err)
	require.Equal(t, want.pending, pending != nil)
	require.Equal(t, want.version, pending.Deployment.Spec.Template.Metadata.Annotations[labels.BundleVersionKey])
}

func TestHelmMigrationCreatesOwnershipChain(t *testing.T) {
	ext := migrationExtension()
	index := migrationIndex(ext, 1, release.StatusDeployed)
	releaseVersion := "1.0.0"
	want := struct {
		err          error
		pending      bool
		protection   ocv1.CollisionProtection
		manager      string
		hasProbes    bool
		secretCount  int
		codOwned     bool
		contentOwned bool
		immutable    bool
		contentType  corev1.SecretType
	}{
		err:          nil,
		pending:      true,
		protection:   ocv1.CollisionProtectionNone,
		manager:      labels.ManagedByOperatorController,
		hasProbes:    true,
		secretCount:  2,
		codOwned:     true,
		contentOwned: true,
		immutable:    true,
		contentType:  labels.SecretTypeObjectData,
	}
	m := newMigrationTest(t, interceptor.Funcs{}, index)
	seedMigrationRelease(t, m, ext, index, 1, releaseVersion)

	pending, err := m.Migrate(t.Context(), ext, nil)

	require.ErrorIs(t, err, want.err)
	require.Equal(t, want.pending, pending != nil)
	cod := pending.Deployment
	require.Equal(t, want.codOwned, metav1.IsControlledBy(cod, ext))
	require.Equal(t, want.manager, cod.Labels[labels.ManagedByKey])
	require.Equal(t, want.protection, cod.Spec.Template.Spec.CollisionProtection)
	require.Equal(t, want.hasProbes, len(cod.Spec.Template.Spec.ProgressionProbes) > 0)
	var secrets corev1.SecretList
	require.NoError(t, m.Client.List(t.Context(), &secrets, client.InNamespace(migrationNamespace)))
	require.Len(t, secrets.Items, want.secretCount)
	for i := range secrets.Items {
		secret := &secrets.Items[i]
		if secret.Name != index.Name {
			require.Equal(t, want.contentOwned, metav1.IsControlledBy(secret, ext))
			require.Equal(t, want.immutable, ptr.Deref(secret.Immutable, false))
			require.Equal(t, want.contentType, secret.Type)
		}
	}
}

func TestHelmMigrationRetriesAfterStoringContent(t *testing.T) {
	ext := migrationExtension()
	index := migrationIndex(ext, 1, release.StatusDeployed)
	createErr := errors.New("deployment create unavailable")
	releaseVersion := "1.0.0"
	type expected struct {
		firstErr       error
		secretCount    int
		retryErr       string
		pendingOnRetry bool
	}
	testCases := []struct {
		name     string
		ownerUID types.UID
		want     expected
	}{
		{name: "reuse immutable content", want: expected{firstErr: createErr, secretCount: 2, pendingOnRetry: true}},
		{name: "refuse content owned by a different CE", ownerUID: "other",
			want: expected{firstErr: createErr, secretCount: 2, retryErr: "does not match the extension's immutable content"}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fail := true
			m := newMigrationTest(t, interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					if _, ok := obj.(*ocv1.ClusterObjectDeployment); ok && fail {
						return createErr
					}
					return c.Create(ctx, obj, opts...)
				},
			}, index)
			seedMigrationRelease(t, m, ext, index, 1, releaseVersion)

			_, err := m.Migrate(t.Context(), ext, nil)
			require.ErrorIs(t, err, tc.want.firstErr)
			var secrets corev1.SecretList
			require.NoError(t, m.Client.List(t.Context(), &secrets, client.InNamespace(migrationNamespace)))
			require.Len(t, secrets.Items, tc.want.secretCount)
			if tc.ownerUID != "" {
				for i := range secrets.Items {
					if secrets.Items[i].Type == labels.SecretTypeObjectData {
						secrets.Items[i].OwnerReferences[0].UID = tc.ownerUID
						require.NoError(t, m.Client.Update(t.Context(), &secrets.Items[i]))
					}
				}
			}

			fail = false
			pending, err := m.Migrate(t.Context(), ext, nil)

			if tc.want.retryErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.want.retryErr)
			}
			require.Equal(t, tc.want.pendingOnRetry, pending != nil)
		})
	}
}

func TestHelmMigrationLeavesConcurrentDeploymentUntouched(t *testing.T) {
	ext := migrationExtension()
	index := migrationIndex(ext, 1, release.StatusDeployed)
	foreign := &ocv1.ClusterObjectDeployment{ObjectMeta: metav1.ObjectMeta{Name: ext.Name, Labels: map[string]string{"foreign": "true"}}}
	releaseVersion := "1.0.0"
	// Create fills the snapshot with the resource version assigned to the foreign COD.
	created := foreign.DeepCopy()
	want := struct {
		alreadyExists bool
		retryErr      string
		deployment    *ocv1.ClusterObjectDeployment
	}{alreadyExists: true, retryErr: "not managed by operator-controller", deployment: created}
	m := newMigrationTest(t, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if _, ok := obj.(*ocv1.ClusterObjectDeployment); ok {
				if err := c.Create(ctx, created); err != nil {
					return err
				}
			}
			return c.Create(ctx, obj, opts...)
		},
	}, index)
	seedMigrationRelease(t, m, ext, index, 1, releaseVersion)

	_, createErr := m.Migrate(t.Context(), ext, nil)
	_, retryErr := m.Migrate(t.Context(), ext, nil)

	require.Equal(t, want.alreadyExists, apierrors.IsAlreadyExists(createErr), "got %v", createErr)
	require.ErrorContains(t, retryErr, want.retryErr)
	actual := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, m.Client.Get(t.Context(), client.ObjectKeyFromObject(foreign), actual))
	require.Equal(t, want.deployment, actual)
}

func TestHelmMigrationBlocksAnExistingRevisionGroup(t *testing.T) {
	ext := migrationExtension()
	index := migrationIndex(ext, 1, release.StatusDeployed)
	set := &ocv1.ClusterObjectSet{
		ObjectMeta: metav1.ObjectMeta{Name: ext.Name + "-1"},
		Spec:       ocv1.ClusterObjectSetSpec{Group: ext.Name},
	}
	want := struct {
		errContains string
		pending     *applier.PendingMigration
	}{errContains: "ClusterObjectSet group already exists", pending: nil}
	m := newMigrationTest(t, interceptor.Funcs{}, index, set)

	pending, err := m.Migrate(t.Context(), ext, nil)

	require.ErrorContains(t, err, want.errContains)
	require.Equal(t, want.pending, pending)
}

func TestHelmMigrationRejectsInvalidDeployment(t *testing.T) {
	ext := migrationExtension()
	index := migrationIndex(ext, 1, release.StatusDeployed)
	initialProtection := ocv1.CollisionProtectionNone
	type expected struct {
		errContains    string
		pending        *applier.PendingMigration
		historyReadErr error
	}
	testCases := []struct {
		name   string
		change func(*ocv1.ClusterObjectDeployment)
		want   expected
	}{
		{
			name:   "foreign owner",
			change: func(c *ocv1.ClusterObjectDeployment) { c.OwnerReferences[0].UID = "other" },
			want:   expected{errContains: "not managed by operator-controller"},
		},
		{
			name:   "unmanaged deployment",
			change: func(c *ocv1.ClusterObjectDeployment) { delete(c.Labels, labels.ManagedByKey) },
			want:   expected{errContains: "not managed by operator-controller"},
		},
		{
			name: "invalid protection",
			change: func(c *ocv1.ClusterObjectDeployment) {
				c.Spec.Template.Spec.CollisionProtection = ocv1.CollisionProtectionIfNoController
			},
			want: expected{errContains: "unexpected collision protection"},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cod := migrationDeployment(ext, initialProtection)
			tc.change(cod)
			m := newMigrationTest(t, interceptor.Funcs{}, index, cod)

			pending, err := m.Migrate(t.Context(), ext, nil)

			require.ErrorContains(t, err, tc.want.errContains)
			require.Equal(t, tc.want.pending, pending)
			require.ErrorIs(t, m.Client.Get(t.Context(), client.ObjectKeyFromObject(index), &corev1.Secret{}), tc.want.historyReadErr)
		})
	}
}

func TestHelmMigrationWaitsForCurrentRolloutCompletion(t *testing.T) {
	ext := migrationExtension()
	index := migrationIndex(ext, 1, release.StatusDeployed)
	type expected struct {
		err            error
		pending        bool
		protection     ocv1.CollisionProtection
		historyReadErr error
	}
	wantAdopting := expected{pending: true, protection: ocv1.CollisionProtectionNone}
	wantProtecting := expected{pending: true, protection: ocv1.CollisionProtectionPrevent}
	testCases := []struct {
		name       string
		protection ocv1.CollisionProtection
		condition  *metav1.Condition
		want       expected
	}{
		{name: "adoption has no status", protection: ocv1.CollisionProtectionNone, want: wantAdopting},
		{name: "protection has no status", protection: ocv1.CollisionProtectionPrevent, want: wantProtecting},
		{name: "stale adoption completion", protection: ocv1.CollisionProtectionNone, condition: migrationProgress(1), want: wantAdopting},
		{name: "stale protection completion", protection: ocv1.CollisionProtectionPrevent, condition: migrationProgress(1), want: wantProtecting},
		{name: "failed adoption", protection: ocv1.CollisionProtectionNone, condition: &metav1.Condition{
			Type: ocv1.ClusterObjectDeploymentTypeProgressing, Status: metav1.ConditionFalse,
			Reason: ocv1.ClusterObjectDeploymentReasonProgressDeadlineExceeded, ObservedGeneration: 2,
		}, want: wantAdopting},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cod := migrationDeployment(ext, tc.protection)
			if tc.condition != nil {
				cod.Status.Conditions = []metav1.Condition{*tc.condition}
			}
			m := newMigrationTest(t, interceptor.Funcs{}, index, cod)

			pending, err := m.Migrate(t.Context(), ext, nil)

			require.ErrorIs(t, err, tc.want.err)
			require.Equal(t, tc.want.pending, pending != nil)
			require.Equal(t, tc.want.protection, pending.Deployment.Spec.Template.Spec.CollisionProtection)
			require.ErrorIs(t, m.Client.Get(t.Context(), client.ObjectKeyFromObject(index), &corev1.Secret{}), tc.want.historyReadErr)
		})
	}
}

func TestHelmMigrationWaitsForCachedProtectedRevision(t *testing.T) {
	ext := migrationExtension()
	index := migrationIndex(ext, 1, release.StatusDeployed)
	cod := migrationDeployment(ext, ocv1.CollisionProtectionPrevent)
	cod.Status.Conditions = []metav1.Condition{*migrationProgress(cod.Generation)}
	want := struct {
		err            error
		pending        bool
		historyReadErr error
	}{err: nil, pending: true, historyReadErr: nil}
	m := newMigrationTest(t, interceptor.Funcs{}, index, cod)

	pending, err := m.Migrate(t.Context(), ext, nil)

	require.ErrorIs(t, err, want.err)
	require.Equal(t, want.pending, pending != nil)
	require.ErrorIs(t, m.Client.Get(t.Context(), client.ObjectKeyFromObject(index), &corev1.Secret{}), want.historyReadErr)
}

func TestHelmMigrationResumesPartialCleanup(t *testing.T) {
	ext := migrationExtension()
	cod := migrationDeployment(ext, ocv1.CollisionProtectionPrevent)
	cod.Status.Conditions = []metav1.Condition{*migrationProgress(cod.Generation)}
	set := completedProtectedRevision(cod)
	index := migrationIndex(ext, 1, release.StatusDeployed)
	failed := migrationIndex(ext, 2, release.StatusFailed)
	chunk := migrationChunk(index, "chunk-a")
	retryChunk := migrationChunk(index, "chunk-b")
	foreign := migrationChunk(index, "foreign-chunk")
	foreign.OwnerReferences[0].UID = "old-index"
	content := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "content", Namespace: migrationNamespace, OwnerReferences: index.OwnerReferences},
		Type:       labels.SecretTypeObjectData,
	}
	deleteErr := errors.New("delete unavailable")
	want := struct {
		initialErr      error
		indexReadErr    error
		retryErr        error
		pendingOnRetry  []bool
		removed         map[client.ObjectKey]types.UID
		removedNotFound bool
		kept            []*corev1.Secret
		keptReadErr     error
	}{
		initialErr:     deleteErr,
		indexReadErr:   nil,
		retryErr:       nil,
		pendingOnRetry: []bool{true, true, false},
		removed: map[client.ObjectKey]types.UID{
			client.ObjectKeyFromObject(chunk):      chunk.UID,
			client.ObjectKeyFromObject(retryChunk): retryChunk.UID,
			client.ObjectKeyFromObject(index):      index.UID,
			client.ObjectKeyFromObject(failed):     failed.UID,
		},
		removedNotFound: true,
		kept:            []*corev1.Secret{foreign, content},
		keptReadErr:     nil,
	}

	fail := true
	m := newMigrationTest(t, interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			options := (&client.DeleteOptions{}).ApplyOptions(opts)
			require.NotNil(t, options.Preconditions)
			require.NotNil(t, options.Preconditions.UID)
			require.Equal(t, want.removed[client.ObjectKeyFromObject(obj)], *options.Preconditions.UID)
			if fail && obj.GetUID() == retryChunk.UID {
				return deleteErr
			}
			return c.Delete(ctx, obj, opts...)
		},
	}, cod, set, index, failed, chunk, retryChunk, foreign, content)

	_, err := m.Migrate(t.Context(), ext, nil)
	require.ErrorIs(t, err, want.initialErr)
	require.ErrorIs(t, m.Client.Get(t.Context(), client.ObjectKeyFromObject(index), &corev1.Secret{}), want.indexReadErr)

	fail = false
	restarted := &applier.BoxcutterStorageMigrator{Client: m.Client, APIReader: m.APIReader, SystemNamespace: migrationNamespace}
	pendingOnRetry := make([]bool, 0, len(want.pendingOnRetry))
	for range want.pendingOnRetry {
		pending, err := restarted.Migrate(t.Context(), ext, nil)
		require.ErrorIs(t, err, want.retryErr)
		pendingOnRetry = append(pendingOnRetry, pending != nil)
	}
	require.Equal(t, want.pendingOnRetry, pendingOnRetry)
	for key := range want.removed {
		err := m.Client.Get(t.Context(), key, &corev1.Secret{})
		require.Equal(t, want.removedNotFound, apierrors.IsNotFound(err), "%s: %v", key, err)
	}
	for _, secret := range want.kept {
		require.ErrorIs(t, m.Client.Get(t.Context(), client.ObjectKeyFromObject(secret), &corev1.Secret{}), want.keptReadErr)
	}
}

func newMigrationTest(t *testing.T, intercept interceptor.Funcs, objects ...client.Object) *applier.BoxcutterStorageMigrator {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, ocv1.AddToScheme(scheme))
	builder := testutil.WithIndexes(t, fake.NewClientBuilder().WithScheme(scheme), coscontrollers.SetupIndexes).
		WithObjects(objects...).WithInterceptorFuncs(intercept)
	stored := builder.Build()
	return &applier.BoxcutterStorageMigrator{
		Client: stored, APIReader: stored, Scheme: scheme, SystemNamespace: migrationNamespace, FieldOwner: "test-ce",
		RevisionGenerator: &applier.SimpleRevisionGenerator{Scheme: scheme, SystemNamespace: migrationNamespace},
	}
}

func migrationExtension() *ocv1.ClusterExtension {
	return &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: "migration", UID: "extension-uid"},
		Spec:       ocv1.ClusterExtensionSpec{Namespace: "installed"},
	}
}

func migrationIndex(ext *ocv1.ClusterExtension, version int, status release.Status) *corev1.Secret {
	name := fmt.Sprintf("sh.helm.release.v1.%s.v%d", ext.Name, version)
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: migrationNamespace, UID: types.UID(name),
			Labels:          map[string]string{"owner": "operator-controller", "name": ext.Name, "type": "index", "version": strconv.Itoa(version), "status": status.String()},
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(ext, ocv1.GroupVersion.WithKind(ocv1.ClusterExtensionKind))}},
		Type: helmstorage.SecretTypeChunkedIndex,
	}
}

func migrationChunk(index *corev1.Secret, name string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: index.Namespace, UID: types.UID(name),
			Labels:          map[string]string{"owner": "operator-controller", "type": "chunk", "key": index.Name},
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(index, corev1.SchemeGroupVersion.WithKind("Secret"))}},
		Type: helmstorage.SecretTypeChunkedChunk,
	}
}

func seedMigrationRelease(t *testing.T, m *applier.BoxcutterStorageMigrator, ext *ocv1.ClusterExtension, index *corev1.Secret, revision int, version string) {
	t.Helper()
	d := driver.NewMemory()
	rel := &release.Release{
		Name: ext.Name, Version: revision, Info: &release.Info{Status: release.StatusDeployed},
		Manifest: `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"installed","namespace":"installed"}}`,
		Labels:   map[string]string{labels.BundleNameKey: "test.v" + version, labels.BundleVersionKey: version},
	}
	require.NoError(t, d.Create(index.Name, rel))
	m.ReleaseDriver = func(context.Context, client.Object, *rest.Config) (driver.Driver, error) { return d, nil }
}

func migrationDeployment(ext *ocv1.ClusterExtension, protection ocv1.CollisionProtection) *ocv1.ClusterObjectDeployment {
	return &ocv1.ClusterObjectDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: ext.Name, UID: "deployment-uid", Generation: 2,
			Labels:          map[string]string{labels.ManagedByKey: labels.ManagedByOperatorController},
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(ext, ocv1.GroupVersion.WithKind(ocv1.ClusterExtensionKind))}},
		Spec: ocv1.ClusterObjectDeploymentSpec{Template: ocv1.ClusterObjectDeploymentTemplate{
			Spec: ocv1.ClusterObjectDeploymentTemplateSpec{ClusterObjectSetContent: ocv1.ClusterObjectSetContent{CollisionProtection: protection}},
		}},
	}
}

func migrationProgress(generation int64) *metav1.Condition {
	return &metav1.Condition{
		Type: ocv1.ClusterObjectDeploymentTypeProgressing, Status: metav1.ConditionFalse,
		Reason: ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed, ObservedGeneration: generation,
	}
}

func completedProtectedRevision(cod *ocv1.ClusterObjectDeployment) *ocv1.ClusterObjectSet {
	return &ocv1.ClusterObjectSet{
		ObjectMeta: metav1.ObjectMeta{Name: cod.Name + "-2",
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(cod, ocv1.GroupVersion.WithKind(ocv1.ClusterObjectDeploymentKind))},
			Labels:          map[string]string{labels.TemplateHashKey: hashutil.DeepHashObject(cod.Spec.Template)}},
		Spec:   ocv1.ClusterObjectSetSpec{Group: cod.Name, Revision: 2},
		Status: ocv1.ClusterObjectSetStatus{CompletedAt: metav1.Now()},
	}
}
