package applier_test

import (
	"errors"
	"maps"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
	bundlecsv "github.com/operator-framework/operator-controller/internal/testing/bundle/csv"
	bundlefs "github.com/operator-framework/operator-controller/internal/testing/bundle/fs"
	"github.com/operator-framework/operator-controller/internal/testutil"
	mockapplier "github.com/operator-framework/operator-controller/internal/testutil/mock/applier"
)

var (
	dummyBundle = bundlefs.Builder().
		WithPackageName("test-package").
		WithCSV(bundlecsv.Builder().WithName("test-csv").Build()).
		Build()
)

// codTestScheme returns a scheme with the types needed for COD-based Apply tests.
func codTestScheme(t *testing.T) *runtime.Scheme {
	s := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(s))
	require.NoError(t, corev1.AddToScheme(s))
	return s
}

// newCODBoxcutter builds a Boxcutter backed by a real SimpleRevisionGenerator whose
// ManifestProvider renders the given objects.
func newCODBoxcutter(t *testing.T, scheme *runtime.Scheme, c client.Client, objs []client.Object) *applier.Boxcutter {
	ctrl := gomock.NewController(t)
	mp := mockapplier.NewMockManifestProvider(ctrl)
	mp.EXPECT().Get(gomock.Any(), gomock.Any()).Return(objs, nil).AnyTimes()
	return &applier.Boxcutter{
		Client: c,
		Scheme: scheme,
		RevisionGenerator: &applier.SimpleRevisionGenerator{
			Scheme:           scheme,
			ManifestProvider: mp,
			SystemNamespace:  "olmv1-system",
		},
		FieldOwner:      "test-owner",
		SystemNamespace: "olmv1-system",
	}
}

func TestBoxcutterApply_WritesClusterObjectDeployment(t *testing.T) {
	scheme := codTestScheme(t)
	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ext", UID: "test-uid"},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	bc := newCODBoxcutter(t, scheme, cl, []client.Object{
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "test-ns"}},
	})

	completed, status, err := bc.Apply(t.Context(), dummyBundle, ext, map[string]string{"some": "label"}, map[string]string{})
	require.NoError(t, err)
	require.True(t, completed)
	require.Empty(t, status)

	t.Log("by checking exactly one ClusterObjectDeployment named after the extension exists")
	codList := &ocv1.ClusterObjectDeploymentList{}
	require.NoError(t, cl.List(t.Context(), codList))
	require.Len(t, codList.Items, 1)
	cod := codList.Items[0]
	assert.Equal(t, ext.Name, cod.Name)

	t.Log("by checking the COD owner reference points to the ClusterExtension")
	require.Len(t, cod.OwnerReferences, 1)
	assert.Equal(t, ext.Name, cod.OwnerReferences[0].Name)
	assert.Equal(t, ext.UID, cod.OwnerReferences[0].UID)
	assert.Equal(t, ocv1.GroupVersion.String(), cod.OwnerReferences[0].APIVersion)
	assert.Equal(t, ocv1.ClusterExtensionKind, cod.OwnerReferences[0].Kind)
	assert.Equal(t, ptr.To(true), cod.OwnerReferences[0].Controller)
	assert.Equal(t, ptr.To(true), cod.OwnerReferences[0].BlockOwnerDeletion)

	t.Log("by checking the template phases reference packed Secrets instead of inline objects")
	require.NotEmpty(t, cod.Spec.Template.Spec.Phases)
	refNames := map[string]struct{}{}
	sawRef := false
	for _, ph := range cod.Spec.Template.Spec.Phases {
		for _, o := range ph.Objects {
			require.Nil(t, o.InlineManifest.Object, "object should be externalized to a Secret ref")
			require.NotNil(t, o.SecretRef)
			refNames[o.SecretRef.Name] = struct{}{}
			sawRef = true
		}
	}
	require.True(t, sawRef)

	t.Log("by checking the referenced Secrets exist in the system namespace owned by the ClusterExtension")
	secretList := &corev1.SecretList{}
	require.NoError(t, cl.List(t.Context(), secretList, client.InNamespace("olmv1-system")))
	require.NotEmpty(t, secretList.Items)
	for _, s := range secretList.Items {
		require.Len(t, s.OwnerReferences, 1)
		assert.Equal(t, ext.Name, s.OwnerReferences[0].Name)
		assert.Equal(t, ext.UID, s.OwnerReferences[0].UID)
		assert.Equal(t, ocv1.ClusterExtensionKind, s.OwnerReferences[0].Kind)
		assert.Equal(t, ptr.To(true), s.OwnerReferences[0].Controller)
		_, ok := refNames[s.Name]
		assert.True(t, ok, "Secret %q should be referenced by the COD", s.Name)
	}

	t.Log("by checking no ClusterObjectSet is created directly by the applier")
	cosList := &ocv1.ClusterObjectSetList{}
	require.NoError(t, cl.List(t.Context(), cosList))
	assert.Empty(t, cosList.Items)
}

func TestBoxcutterApply_NilContentUsesExistingCOD(t *testing.T) {
	scheme := codTestScheme(t)
	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ext", UID: "test-uid"},
	}

	t.Run("errors when no deployment installed", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(scheme).Build()
		bc := newCODBoxcutter(t, scheme, cl, nil)
		completed, status, err := bc.Apply(t.Context(), nil, ext, nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "catalog content unavailable and no deployment installed")
		assert.False(t, completed)
		assert.Empty(t, status)
	})

	t.Run("maintains current state when a deployment exists", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			&ocv1.ClusterObjectDeployment{ObjectMeta: metav1.ObjectMeta{Name: ext.Name}},
		).Build()
		bc := newCODBoxcutter(t, scheme, cl, nil)
		completed, status, err := bc.Apply(t.Context(), nil, ext, nil, nil)
		require.NoError(t, err)
		assert.True(t, completed)
		assert.Empty(t, status)
	})
}

// TestBoxcutterApply_Adoption verifies the managed-by label, controller identity, and
// recovery after an ownerless deployment is relabelled.
func TestBoxcutterApply_Adoption(t *testing.T) {
	t.Parallel()
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "test-ext", UID: "test-uid"}}
	adoptionLabels := map[string]string{
		labels.ManagedByKey: labels.ManagedByOperatorController,
	}
	deploymentLabels := map[string]string{
		labels.ManagedByKey: labels.ManagedByOperatorController,
		labels.OwnerKindKey: ocv1.ClusterExtensionKind,
		labels.OwnerNameKey: ext.Name,
	}
	controllerRef := func(apiVersion, kind, name string, uid types.UID) *metav1.OwnerReference {
		return &metav1.OwnerReference{
			APIVersion: apiVersion, Kind: kind, Name: name, UID: uid, Controller: ptr.To(true),
		}
	}
	const missingLabelError = "has no controller and does not have the required app.kubernetes.io/managed-by=operator-controller label"
	type testCase struct {
		name       string
		codLabels  map[string]string
		controller *metav1.OwnerReference
		wantErr    string
	}
	tests := []testCase{
		{name: "adopts an ownerless deployment with only the managed-by label", codLabels: adoptionLabels},
		{name: "adopts a fully labelled ownerless deployment", codLabels: deploymentLabels},
		{
			name: "adopts an ownerless deployment with mismatched owner labels",
			codLabels: map[string]string{
				labels.ManagedByKey: labels.ManagedByOperatorController,
				labels.OwnerKindKey: "Namespace",
				labels.OwnerNameKey: "other-ext",
			},
		},
		{name: "refuses an unlabelled ownerless deployment", wantErr: missingLabelError},
		{
			name:      "refuses an ownerless deployment without the managed-by label",
			codLabels: map[string]string{labels.OwnerKindKey: ocv1.ClusterExtensionKind, labels.OwnerNameKey: ext.Name},
			wantErr:   missingLabelError,
		},
		{
			name: "refuses an ownerless deployment with a mismatched managed-by label",
			codLabels: map[string]string{
				labels.ManagedByKey: "another-controller",
				labels.OwnerKindKey: ocv1.ClusterExtensionKind,
				labels.OwnerNameKey: ext.Name,
			},
			wantErr: missingLabelError,
		},
		{
			name:       "adopts a deployment controlled by this UID without labels",
			controller: controllerRef(ocv1.GroupVersion.String(), ocv1.ClusterExtensionKind, ext.Name, ext.UID),
		},
		{
			name:       "refuses a stale controller UID without labels",
			controller: controllerRef(ocv1.GroupVersion.String(), ocv1.ClusterExtensionKind, ext.Name, "old-uid"),
			wantErr:    `already controlled by ClusterExtension "test-ext"`,
		},
		{
			name:       "refuses a stale controller UID with a different API version in the same group",
			codLabels:  adoptionLabels,
			controller: controllerRef(ocv1.GroupVersion.Group+"/v1beta1", ocv1.ClusterExtensionKind, ext.Name, "old-uid"),
			wantErr:    `already controlled by ClusterExtension "test-ext"`,
		},
		{
			name:       "refuses a stale controller UID with a mismatched owner-name label",
			codLabels:  map[string]string{labels.OwnerNameKey: "other-ext"},
			controller: controllerRef(ocv1.GroupVersion.String(), ocv1.ClusterExtensionKind, ext.Name, "old-uid"),
			wantErr:    `already controlled by ClusterExtension "test-ext"`,
		},
		{
			name:       "refuses a stale controller UID with a mismatched owner-kind label",
			codLabels:  map[string]string{labels.OwnerKindKey: "Namespace"},
			controller: controllerRef(ocv1.GroupVersion.String(), ocv1.ClusterExtensionKind, ext.Name, "old-uid"),
			wantErr:    `already controlled by ClusterExtension "test-ext"`,
		},
		{
			name:       "refuses a stale controller UID despite matching labels",
			codLabels:  deploymentLabels,
			controller: controllerRef(ocv1.GroupVersion.String(), ocv1.ClusterExtensionKind, ext.Name, "old-uid"),
			wantErr:    `already controlled by ClusterExtension "test-ext"`,
		},
		{
			name:       "refuses a foreign controller despite matching labels",
			codLabels:  deploymentLabels,
			controller: controllerRef(ocv1.GroupVersion.String(), ocv1.ClusterExtensionKind, "other-ext", "other-uid"),
			wantErr:    `already controlled by ClusterExtension "other-ext"`,
		},
		{
			name:       "refuses a foreign controller kind despite matching labels",
			codLabels:  deploymentLabels,
			controller: controllerRef("v1", "Namespace", ext.Name, "other-uid"),
			wantErr:    "already controlled by Namespace",
		},
		{
			name:       "refuses a foreign API group despite matching labels",
			codLabels:  deploymentLabels,
			controller: controllerRef("other.example.com/v1", ocv1.ClusterExtensionKind, ext.Name, "other-uid"),
			wantErr:    "already controlled by ClusterExtension",
		},
		{
			name:       "refuses a malformed controller API version",
			codLabels:  deploymentLabels,
			controller: controllerRef("invalid/group/version", ocv1.ClusterExtensionKind, ext.Name, "other-uid"),
			wantErr:    "already controlled by ClusterExtension",
		},
		{
			name:       "refuses an unlabelled foreign-controlled deployment",
			controller: controllerRef(ocv1.GroupVersion.String(), ocv1.ClusterExtensionKind, "other-ext", "other-uid"),
			wantErr:    `already controlled by ClusterExtension "other-ext"`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			scheme := codTestScheme(t)
			original := &ocv1.ClusterObjectDeployment{ObjectMeta: metav1.ObjectMeta{Name: ext.Name, Labels: maps.Clone(tc.codLabels)}}
			if tc.controller != nil {
				original.OwnerReferences = []metav1.OwnerReference{*tc.controller}
			}
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(original).Build()
			before := &ocv1.ClusterObjectDeployment{}
			require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Name: ext.Name}, before))
			bc := newCODBoxcutter(t, scheme, cl, []client.Object{
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "test-ns"}},
			})
			completed, status, err := bc.Apply(t.Context(), dummyBundle, ext, nil, nil)
			require.Empty(t, status)
			actual := &ocv1.ClusterObjectDeployment{}
			require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Name: ext.Name}, actual))
			if tc.wantErr == "" {
				require.NoError(t, err)
				require.True(t, completed)
				owner := metav1.GetControllerOf(actual)
				require.NotNil(t, owner)
				assert.Equal(t, ext.Name, owner.Name)
				assert.Equal(t, ext.UID, owner.UID)
				assert.Equal(t, ocv1.ClusterExtensionKind, owner.Kind)
				assert.Equal(t, ocv1.GroupVersion.String(), owner.APIVersion)
				assert.Equal(t, deploymentLabels, actual.Labels)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
			assert.False(t, completed)
			require.NotErrorIs(t, err, reconcile.TerminalError(nil), "adoption conflicts must retry")
			assert.Contains(t, err.Error(), ext.Name)
			assert.Equal(t, before, actual, "refused adoption must leave the deployment unchanged")
			secrets := &corev1.SecretList{}
			require.NoError(t, cl.List(t.Context(), secrets))
			assert.Empty(t, secrets.Items)

			if tc.controller == nil {
				actual.Labels = maps.Clone(adoptionLabels)
				require.NoError(t, cl.Update(t.Context(), actual))
				completed, _, err = bc.Apply(t.Context(), dummyBundle, ext, nil, nil)
				require.NoError(t, err, "relabelled deployments must recover on retry")
				assert.True(t, completed)
			}
		})
	}
}

func TestBoxcutterApply_RecreatesDeploymentAfterDeletion(t *testing.T) {
	t.Parallel()
	testEnv := testutil.NewEnv()
	config, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, testutil.StopWithRetry(testEnv, time.Minute, time.Second)) })
	scheme := codTestScheme(t)
	cl, err := client.New(config, client.Options{Scheme: scheme})
	require.NoError(t, err)
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "olmv1-system"}}
	require.NoError(t, cl.Create(t.Context(), namespace))

	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "restored-ext", UID: "new-uid"}}
	restored := &ocv1.ClusterObjectDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: ext.Name,
			Labels: map[string]string{
				labels.ManagedByKey: labels.ManagedByOperatorController,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: ocv1.GroupVersion.String(), Kind: ocv1.ClusterExtensionKind,
					Name: ext.Name, UID: "old-uid", Controller: ptr.To(true),
				},
			},
		},
		Spec: ocv1.ClusterObjectDeploymentSpec{Template: ocv1.ClusterObjectDeploymentTemplate{
			Spec: ocv1.ClusterObjectDeploymentTemplateSpec{
				ClusterObjectSetContent: ocv1.ClusterObjectSetContent{
					CollisionProtection: ocv1.CollisionProtectionPrevent,
					Phases:              []ocv1.ClusterObjectSetPhase{{Name: "resources", Objects: []ocv1.ClusterObjectSetObject{}}},
				},
			},
		}},
	}
	require.NoError(t, cl.Create(t.Context(), restored, client.FieldOwner("restore-tool")))
	bc := newCODBoxcutter(t, scheme, cl, []client.Object{
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "restored-cm", Namespace: namespace.Name}},
	})
	completed, _, err := bc.Apply(t.Context(), dummyBundle, ext, nil, nil)
	require.ErrorContains(t, err, `already controlled by ClusterExtension "restored-ext"`)
	assert.False(t, completed)
	require.NotErrorIs(t, err, reconcile.TerminalError(nil), "a stale controller UID must retry")
	actual := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Name: ext.Name}, actual))
	assert.Equal(t, restored, actual, "a stale controller UID must not be replaced")

	// Envtest has no garbage collector. Delete the single-owner COD to exercise
	// the ordinary recreate path after garbage collection removes a stale COD.
	require.NoError(t, cl.Delete(t.Context(), restored))
	for range 2 {
		completed, _, err := bc.Apply(t.Context(), dummyBundle, ext, nil, nil)
		require.NoError(t, err)
		require.True(t, completed)
	}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Name: ext.Name}, actual))
	assert.NotEqual(t, restored.UID, actual.UID, "recovery must create a new deployment")
	assert.Equal(t, labels.ManagedByOperatorController, actual.Labels[labels.ManagedByKey])
	require.Len(t, actual.OwnerReferences, 1)
	owner := metav1.GetControllerOf(actual)
	require.NotNil(t, owner)
	assert.Equal(t, ext.UID, owner.UID)
}

// TestBoxcutterApply_Preflights verifies which preflight hook runs for each rollout state
// and that a preflight failure propagates out of Apply. Each row is a sequence of applies
// against the same Boxcutter: a step renders its own objects, so identical content between
// steps is detected as unchanged (skip) and changed content is detected as an upgrade.
func TestBoxcutterApply_Preflights(t *testing.T) {
	cmA := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm-a", Namespace: "test-ns"}}
	cmB := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm-b", Namespace: "test-ns"}}

	type applyStep struct {
		objects []client.Object
		wantErr string // substring expected in the apply error; "" means the apply must succeed
	}
	for _, tc := range []struct {
		name        string
		installErr  error
		upgradeErr  error
		steps       []applyStep
		wantInstall int
		wantUpgrade int
	}{
		{
			name:        "fresh install runs the install preflight",
			steps:       []applyStep{{objects: []client.Object{cmA}}},
			wantInstall: 1,
		},
		{
			name:        "unchanged re-apply skips both preflights",
			steps:       []applyStep{{objects: []client.Object{cmA}}, {objects: []client.Object{cmA}}},
			wantInstall: 1,
		},
		{
			name:        "changed content runs the upgrade preflight",
			steps:       []applyStep{{objects: []client.Object{cmA}}, {objects: []client.Object{cmB}}},
			wantInstall: 1,
			wantUpgrade: 1,
		},
		{
			name:        "install preflight failure propagates out of Apply",
			installErr:  errors.New("install boom"),
			steps:       []applyStep{{objects: []client.Object{cmA}, wantErr: "install boom"}},
			wantInstall: 1,
		},
		{
			name:       "upgrade preflight failure propagates out of Apply",
			upgradeErr: errors.New("upgrade boom"),
			steps: []applyStep{
				{objects: []client.Object{cmA}},
				{objects: []client.Object{cmB}, wantErr: "upgrade boom"},
			},
			wantInstall: 1,
			wantUpgrade: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scheme := codTestScheme(t)
			cl := fake.NewClientBuilder().WithScheme(scheme).Build()
			ctrl := gomock.NewController(t)

			// One Get per apply; return each step's objects in call order.
			mp := mockapplier.NewMockManifestProvider(ctrl)
			for _, step := range tc.steps {
				mp.EXPECT().Get(gomock.Any(), gomock.Any()).Return(step.objects, nil).Times(1)
			}

			// A non-crdupgradesafety mock preflight is never skipped, so Install/Upgrade are
			// invoked exactly per the rollout state. Times(0) asserts the hook is not called.
			pf := mockapplier.NewMockPreflight(ctrl)
			pf.EXPECT().Install(gomock.Any(), gomock.Any()).Return(tc.installErr).Times(tc.wantInstall)
			pf.EXPECT().Upgrade(gomock.Any(), gomock.Any()).Return(tc.upgradeErr).Times(tc.wantUpgrade)

			bc := &applier.Boxcutter{
				Client: cl,
				Scheme: scheme,
				RevisionGenerator: &applier.SimpleRevisionGenerator{
					Scheme:           scheme,
					ManifestProvider: mp,
					SystemNamespace:  "olmv1-system",
				},
				Preflights:      []applier.Preflight{pf},
				FieldOwner:      "test-owner",
				SystemNamespace: "olmv1-system",
			}
			ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "test-ext", UID: "test-uid"}}

			for i, step := range tc.steps {
				_, _, err := bc.Apply(t.Context(), dummyBundle, ext, map[string]string{"some": "label"}, map[string]string{})
				if step.wantErr != "" {
					require.Errorf(t, err, "apply step %d", i)
					assert.Containsf(t, err.Error(), step.wantErr, "apply step %d", i)
					continue
				}
				require.NoErrorf(t, err, "apply step %d", i)
			}
		})
	}
}
