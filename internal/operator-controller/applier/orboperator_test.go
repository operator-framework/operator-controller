package applier

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"testing"

	orbv1alpha1 "github.com/joelanford/orb-operator/api/v1alpha1"
	orbac "github.com/joelanford/orb-operator/applyconfigurations/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	orb "github.com/operator-framework/operator-controller/internal/operator-controller/applier/orb"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/preflights/crdupgradesafety"
	mockapplier "github.com/operator-framework/operator-controller/internal/testutil/mock/applier"
	mockcrdclient "github.com/operator-framework/operator-controller/internal/testutil/mock/crdclient"
)

func TestExtractObjectsFromCOD(t *testing.T) {
	crdJSON := `{"apiVersion":"apiextensions.k8s.io/v1","kind":"CustomResourceDefinition","metadata":{"name":"things.example.com"}}`
	deployJSON := `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"my-deploy","namespace":"ns1"}}`

	tests := []struct {
		name      string
		cod       *orbac.ClusterObjectDeploymentApplyConfiguration
		wantCount int
		wantErr   string
	}{
		{
			name:      "nil COD",
			cod:       nil,
			wantCount: 0,
		},
		{
			name: "nil spec",
			cod: orbac.ClusterObjectDeployment("test").
				WithSpec(nil),
			wantCount: 0,
		},
		{
			name: "empty phases",
			cod: orbac.ClusterObjectDeployment("test").
				WithSpec(orbac.ClusterObjectDeploymentSpec().
					WithTemplate(orbac.ClusterObjectDeploymentTemplate().
						WithSpec(orbac.ClusterObjectDeploymentTemplateSpec()))),
			wantCount: 0,
		},
		{
			name: "multi-phase with objects",
			cod: orbac.ClusterObjectDeployment("test").
				WithSpec(orbac.ClusterObjectDeploymentSpec().
					WithTemplate(orbac.ClusterObjectDeploymentTemplate().
						WithSpec(orbac.ClusterObjectDeploymentTemplateSpec().
							WithPhases(
								orbac.Phase().WithName("crds").WithObjects(
									orbac.PhaseObject().WithObject(runtime.RawExtension{Raw: []byte(crdJSON)}),
								),
								orbac.Phase().WithName("deploy").WithObjects(
									orbac.PhaseObject().WithObject(runtime.RawExtension{Raw: []byte(deployJSON)}),
								),
							)))),
			wantCount: 2,
		},
		{
			name: "nil RawExtension skipped",
			cod: orbac.ClusterObjectDeployment("test").
				WithSpec(orbac.ClusterObjectDeploymentSpec().
					WithTemplate(orbac.ClusterObjectDeploymentTemplate().
						WithSpec(orbac.ClusterObjectDeploymentTemplateSpec().
							WithPhases(
								orbac.Phase().WithName("p").WithObjects(
									orbac.PhaseObject(),
									orbac.PhaseObject().WithObject(runtime.RawExtension{Raw: []byte(crdJSON)}),
								),
							)))),
			wantCount: 1,
		},
		{
			name: "invalid JSON",
			cod: orbac.ClusterObjectDeployment("test").
				WithSpec(orbac.ClusterObjectDeploymentSpec().
					WithTemplate(orbac.ClusterObjectDeploymentTemplate().
						WithSpec(orbac.ClusterObjectDeploymentTemplateSpec().
							WithPhases(
								orbac.Phase().WithName("bad").WithObjects(
									orbac.PhaseObject().WithObject(runtime.RawExtension{Raw: []byte(`{not valid`)}),
								),
							)))),
			wantErr: `phase "bad" object 0`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			objs, err := extractObjectsFromCOD(tc.cod)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Len(t, objs, tc.wantCount)
		})
	}
}

func TestExtractObjectsFromCOD_PreservesGVK(t *testing.T) {
	crdJSON := `{"apiVersion":"apiextensions.k8s.io/v1","kind":"CustomResourceDefinition","metadata":{"name":"things.example.com"}}`
	cod := orbac.ClusterObjectDeployment("test").
		WithSpec(orbac.ClusterObjectDeploymentSpec().
			WithTemplate(orbac.ClusterObjectDeploymentTemplate().
				WithSpec(orbac.ClusterObjectDeploymentTemplateSpec().
					WithPhases(
						orbac.Phase().WithName("crds").WithObjects(
							orbac.PhaseObject().WithObject(runtime.RawExtension{Raw: []byte(crdJSON)}),
						),
					))))

	objs, err := extractObjectsFromCOD(cod)
	require.NoError(t, err)
	require.Len(t, objs, 1)
	assert.Equal(t, "apiextensions.k8s.io/v1", objs[0].GetObjectKind().GroupVersionKind().GroupVersion().String())
	assert.Equal(t, "CustomResourceDefinition", objs[0].GetObjectKind().GroupVersionKind().Kind)
	assert.Equal(t, "things.example.com", objs[0].GetName())
}

// existingNamespacedCRD is the "old" widgets CRD returned by the fake CRD client.
// The bundle's CRD (breakingCRDCOD) flips the scope to Cluster, which
// crdupgradesafety rejects when the preflight actually runs.
func existingNamespacedCRD() *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		TypeMeta:   metav1.TypeMeta{Kind: "CustomResourceDefinition", APIVersion: "apiextensions.k8s.io/v1"},
		ObjectMeta: metav1.ObjectMeta{Name: "widgets.example.com"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "example.com",
			Scope: apiextensionsv1.NamespaceScoped,
			Names: apiextensionsv1.CustomResourceDefinitionNames{Plural: "widgets", Singular: "widget", Kind: "Widget", ListKind: "WidgetList"},
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name: "v1", Served: true, Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{Type: "object"},
				},
			}},
		},
	}
}

// breakingCRDCOD is a COD whose only object is the widgets CRD with scope changed
// to Cluster - an upgrade crdupgradesafety fails on (vs existingNamespacedCRD).
func breakingCRDCOD() *orbac.ClusterObjectDeploymentApplyConfiguration {
	const crdJSON = `{"apiVersion":"apiextensions.k8s.io/v1","kind":"CustomResourceDefinition","metadata":{"name":"widgets.example.com"},"spec":{"group":"example.com","scope":"Cluster","names":{"plural":"widgets","singular":"widget","kind":"Widget","listKind":"WidgetList"},"versions":[{"name":"v1","served":true,"storage":true,"schema":{"openAPIV3Schema":{"type":"object"}}}]}}`
	return orbac.ClusterObjectDeployment("test").
		WithSpec(orbac.ClusterObjectDeploymentSpec().
			WithTemplate(orbac.ClusterObjectDeploymentTemplate().
				WithSpec(orbac.ClusterObjectDeploymentTemplateSpec().
					WithPhases(
						orbac.Phase().WithName("crds").WithObjects(
							orbac.PhaseObject().WithObject(runtime.RawExtension{Raw: []byte(crdJSON)}),
						),
					))))
}

func extWithCRDUpgradeSafety(enforcement ocv1.CRDUpgradeSafetyEnforcement) *ocv1.ClusterExtension {
	return &ocv1.ClusterExtension{
		Spec: ocv1.ClusterExtensionSpec{
			Install: &ocv1.ClusterExtensionInstallConfig{
				Preflight: &ocv1.PreflightConfig{
					CRDUpgradeSafety: &ocv1.CRDUpgradeSafetyPreflightConfig{
						Enforcement: enforcement,
					},
				},
			},
		},
	}
}

func TestRunPreflights(t *testing.T) {
	deployJSON := `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"d","namespace":"ns"}}`
	codWithObj := orbac.ClusterObjectDeployment("test").
		WithSpec(orbac.ClusterObjectDeploymentSpec().
			WithTemplate(orbac.ClusterObjectDeploymentTemplate().
				WithSpec(orbac.ClusterObjectDeploymentTemplateSpec().
					WithPhases(
						orbac.Phase().WithName("deploy").WithObjects(
							orbac.PhaseObject().WithObject(runtime.RawExtension{Raw: []byte(deployJSON)}),
						),
					))))

	t.Run("calls Upgrade on each preflight", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		pf1 := mockapplier.NewMockPreflight(ctrl)
		pf2 := mockapplier.NewMockPreflight(ctrl)
		// gomock verifies each Upgrade is called exactly once at test cleanup.
		pf1.EXPECT().Upgrade(gomock.Any(), gomock.Any()).Return(nil)
		pf2.EXPECT().Upgrade(gomock.Any(), gomock.Any()).Return(nil)
		ext := &ocv1.ClusterExtension{}

		err := runPreflights(context.Background(), ext, codWithObj, []Preflight{pf1, pf2})
		require.NoError(t, err)
	})

	t.Run("collects all errors", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		pf1 := mockapplier.NewMockPreflight(ctrl)
		pf2 := mockapplier.NewMockPreflight(ctrl)
		pf1.EXPECT().Upgrade(gomock.Any(), gomock.Any()).Return(errors.New("pf1 failed"))
		pf2.EXPECT().Upgrade(gomock.Any(), gomock.Any()).Return(errors.New("pf2 failed"))
		ext := &ocv1.ClusterExtension{}

		err := runPreflights(context.Background(), ext, codWithObj, []Preflight{pf1, pf2})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pf1 failed")
		assert.Contains(t, err.Error(), "pf2 failed")
	})

	t.Run("nil preflights returns nil", func(t *testing.T) {
		ext := &ocv1.ClusterExtension{}
		err := runPreflights(context.Background(), ext, codWithObj, nil)
		require.NoError(t, err)
	})

	t.Run("empty COD returns nil", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		pf := mockapplier.NewMockPreflight(ctrl)
		// Upgrade is still invoked (with an empty object set) even when the COD
		// has no objects.
		pf.EXPECT().Upgrade(gomock.Any(), gomock.Any()).Return(nil)
		ext := &ocv1.ClusterExtension{}
		emptyCOD := orbac.ClusterObjectDeployment("test")

		err := runPreflights(context.Background(), ext, emptyCOD, []Preflight{pf})
		require.NoError(t, err)
	})

	// The skip logic in shouldSkipPreflight is a type assertion on the concrete
	// *crdupgradesafety.Preflight, so these use a real one. breakingCRDCOD would
	// fail the check if it ran (proven by the "Strict" case), so the "None" case
	// asserting no error proves the check was skipped.
	t.Run("skips a real CRDUpgradeSafety preflight when enforcement is None", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		crdCli := mockcrdclient.NewMockCustomResourceDefinitionInterface(ctrl)
		crdCli.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(existingNamespacedCRD(), nil).AnyTimes()
		pf := crdupgradesafety.NewPreflight(crdCli)

		ext := extWithCRDUpgradeSafety(ocv1.CRDUpgradeSafetyEnforcementNone)
		err := runPreflights(context.Background(), ext, breakingCRDCOD(), []Preflight{pf})
		require.NoError(t, err)
	})

	t.Run("runs a real CRDUpgradeSafety preflight when enforcement is Strict", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		crdCli := mockcrdclient.NewMockCustomResourceDefinitionInterface(ctrl)
		crdCli.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(existingNamespacedCRD(), nil).AnyTimes()
		pf := crdupgradesafety.NewPreflight(crdCli)

		ext := extWithCRDUpgradeSafety(ocv1.CRDUpgradeSafetyEnforcementStrict)
		err := runPreflights(context.Background(), ext, breakingCRDCOD(), []Preflight{pf})
		require.Error(t, err)
		require.ErrorContains(t, err, "scope")
	})
}

// fakeCODGenerator returns a pre-built COD (or error) from GenerateCOD.
type fakeCODGenerator struct {
	cod *orbac.ClusterObjectDeploymentApplyConfiguration
	err error
}

func (f *fakeCODGenerator) GenerateCOD(_ context.Context, _ fs.FS, _ *ocv1.ClusterExtension, _, _ map[string]string) (*orbac.ClusterObjectDeploymentApplyConfiguration, error) {
	return f.cod, f.err
}

// testExtName is the ClusterExtension / COD name used by the Apply tests. The
// COD name doubles as the owner name, as it does in the reconcile pipeline.
const testExtName = "my-ext"

// codThatExternalizes builds a COD large enough to exceed the externalizer's
// size threshold, so ExternalizeCOD produces at least one ClusterObjectSlice.
func codThatExternalizes() *orbac.ClusterObjectDeploymentApplyConfiguration {
	obj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]interface{}{"name": "big", "namespace": "ns1"},
		"data":       map[string]interface{}{"payload": strings.Repeat("x", 1024*1024)},
	}
	raw, _ := json.Marshal(obj)
	return orbac.ClusterObjectDeployment(testExtName).
		WithLabels(map[string]string{
			labels.OwnerKindKey: ocv1.ClusterExtensionKind,
			labels.OwnerNameKey: testExtName,
		}).
		WithSpec(orbac.ClusterObjectDeploymentSpec().
			WithTemplate(orbac.ClusterObjectDeploymentTemplate().
				WithSpec(orbac.ClusterObjectDeploymentTemplateSpec().
					WithPhases(orbac.Phase().WithName("deploy").WithObjects(
						orbac.PhaseObject().WithObject(runtime.RawExtension{Raw: raw}),
					)))))
}

func orbTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, orbv1alpha1.AddToScheme(scheme))
	require.NoError(t, ocv1.AddToScheme(scheme))
	return scheme
}

// extUID derives the stable UID used for a ClusterExtension owner in tests, so
// the owner reference on a slice can be matched back to the owning ext.
func extUID(owner string) types.UID { return types.UID(owner + "-uid") }

// coslObject builds a ClusterObjectSlice owned (controller reference) by the
// named ClusterExtension, mirroring how the externalizer stamps the COD's owner
// reference onto each slice. The owner-name label is set alongside it.
func coslObject(name, owner string) *orbv1alpha1.ClusterObjectSlice {
	controller := true
	return &orbv1alpha1.ClusterObjectSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{labels.OwnerNameKey: owner},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: ocv1.GroupVersion.String(),
				Kind:       "ClusterExtension",
				Name:       owner,
				UID:        extUID(owner),
				Controller: &controller,
			}},
		},
		Count: 1,
		Objects: []orbv1alpha1.SliceObject{{
			ObjectKey: orbv1alpha1.ObjectKey{APIVersion: "v1", Kind: "ConfigMap", Name: "x"},
			Content:   []byte("{}"),
		}},
	}
}

func TestOrbOperatorApply_AppliesSlicesBeforeCOD(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "my-ext", UID: extUID("my-ext")}}

	var applied []string
	funcs := interceptor.Funcs{
		Apply: func(_ context.Context, _ client.WithWatch, obj runtime.ApplyConfiguration, _ ...client.ApplyOption) error {
			switch obj.(type) {
			case *orbac.ClusterObjectSliceApplyConfiguration:
				applied = append(applied, "cosl")
			case *orbac.ClusterObjectDeploymentApplyConfiguration:
				applied = append(applied, "cod")
			}
			return nil
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(orbTestScheme(t)).WithInterceptorFuncs(funcs).Build()

	o := &OrbOperator{
		Client:     fakeClient,
		Generator:  &fakeCODGenerator{cod: codThatExternalizes()},
		FieldOwner: "test-owner",
	}

	done, status, err := o.Apply(context.Background(), nil, ext, nil, nil)
	require.NoError(t, err)
	assert.True(t, done)
	assert.Empty(t, status)

	require.NotEmpty(t, applied)
	assert.Equal(t, "cod", applied[len(applied)-1], "COD must be applied last")
	for _, a := range applied[:len(applied)-1] {
		assert.Equal(t, "cosl", a, "all slices must be applied before the COD")
	}
}

func TestOrbOperatorApply_SliceApplyErrorSkipsCOD(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "my-ext", UID: extUID("my-ext")}}

	codApplied := false
	funcs := interceptor.Funcs{
		Apply: func(_ context.Context, _ client.WithWatch, obj runtime.ApplyConfiguration, _ ...client.ApplyOption) error {
			switch obj.(type) {
			case *orbac.ClusterObjectSliceApplyConfiguration:
				return errors.New("cosl apply failed")
			case *orbac.ClusterObjectDeploymentApplyConfiguration:
				codApplied = true
			}
			return nil
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(orbTestScheme(t)).WithInterceptorFuncs(funcs).Build()

	o := &OrbOperator{
		Client:     fakeClient,
		Generator:  &fakeCODGenerator{cod: codThatExternalizes()},
		FieldOwner: "test-owner",
	}

	done, status, err := o.Apply(context.Background(), nil, ext, nil, nil)
	require.Error(t, err)
	assert.False(t, done)
	assert.Empty(t, status)
	assert.Contains(t, err.Error(), "cosl apply failed")
	assert.False(t, codApplied, "COD must not be applied after a slice apply failure")
}

func TestOrbOperatorApply_CODApplyError(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "my-ext", UID: extUID("my-ext")}}

	funcs := interceptor.Funcs{
		Apply: func(_ context.Context, _ client.WithWatch, obj runtime.ApplyConfiguration, _ ...client.ApplyOption) error {
			if _, ok := obj.(*orbac.ClusterObjectDeploymentApplyConfiguration); ok {
				return errors.New("cod apply failed")
			}
			return nil
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(orbTestScheme(t)).WithInterceptorFuncs(funcs).Build()

	o := &OrbOperator{
		Client:     fakeClient,
		Generator:  &fakeCODGenerator{cod: codThatExternalizes()},
		FieldOwner: "test-owner",
	}

	done, status, err := o.Apply(context.Background(), nil, ext, nil, nil)
	require.Error(t, err)
	assert.False(t, done)
	assert.Empty(t, status)
	assert.Contains(t, err.Error(), "cod apply failed")
}

func TestOrbOperatorApply_Success(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "my-ext", UID: extUID("my-ext")}}

	funcs := interceptor.Funcs{
		Apply: func(_ context.Context, _ client.WithWatch, _ runtime.ApplyConfiguration, _ ...client.ApplyOption) error {
			return nil
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(orbTestScheme(t)).WithInterceptorFuncs(funcs).Build()

	o := &OrbOperator{
		Client:     fakeClient,
		Generator:  &fakeCODGenerator{cod: codThatExternalizes()},
		FieldOwner: "test-owner",
	}

	done, status, err := o.Apply(context.Background(), nil, ext, nil, nil)
	require.NoError(t, err)
	assert.True(t, done)
	assert.Empty(t, status)
}

func TestOrbOperatorApply_GCErrorIsNonFatal(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "my-ext", UID: extUID("my-ext")}}

	funcs := interceptor.Funcs{
		Apply: func(_ context.Context, _ client.WithWatch, _ runtime.ApplyConfiguration, _ ...client.ApplyOption) error {
			return nil
		},
		List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
			return errors.New("list failed")
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(orbTestScheme(t)).WithInterceptorFuncs(funcs).Build()

	o := &OrbOperator{
		Client:     fakeClient,
		Generator:  &fakeCODGenerator{cod: codThatExternalizes()},
		FieldOwner: "test-owner",
	}

	done, status, err := o.Apply(context.Background(), nil, ext, nil, nil)
	require.NoError(t, err, "GC errors must not fail the apply")
	assert.True(t, done)
	assert.Empty(t, status)
}

func TestGarbageCollectOrphanedSlices_DeletesOrphans(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "my-ext", UID: extUID("my-ext")}}

	existing := []client.Object{
		coslObject("my-ext-aaaa", "my-ext"),       // kept: in current set
		coslObject("my-ext-bbbb", "my-ext"),       // deleted: owned, not in current set
		coslObject("other-ext-cccc", "other-ext"), // kept: different owner
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(orbTestScheme(t)).
		WithObjects(existing...).
		Build()

	o := &OrbOperator{Client: fakeClient}

	slices := []*orbac.ClusterObjectSliceApplyConfiguration{
		orbac.ClusterObjectSlice("my-ext-aaaa"),
	}
	err := o.garbageCollectOrphanedSlices(context.Background(), ext, slices)
	require.NoError(t, err)

	var remaining orbv1alpha1.ClusterObjectSliceList
	require.NoError(t, fakeClient.List(context.Background(), &remaining))
	names := make([]string, 0, len(remaining.Items))
	for _, s := range remaining.Items {
		names = append(names, s.Name)
	}
	assert.ElementsMatch(t, []string{"my-ext-aaaa", "other-ext-cccc"}, names)
}

func TestGarbageCollectOrphanedSlices_EmptySetDeletesAllOwned(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "my-ext", UID: extUID("my-ext")}}

	existing := []client.Object{
		coslObject("my-ext-aaaa", "my-ext"),
		coslObject("my-ext-bbbb", "my-ext"),
		coslObject("other-ext-cccc", "other-ext"),
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(orbTestScheme(t)).
		WithObjects(existing...).
		Build()

	o := &OrbOperator{Client: fakeClient}

	err := o.garbageCollectOrphanedSlices(context.Background(), ext, nil)
	require.NoError(t, err)

	var remaining orbv1alpha1.ClusterObjectSliceList
	require.NoError(t, fakeClient.List(context.Background(), &remaining))
	names := make([]string, 0, len(remaining.Items))
	for _, s := range remaining.Items {
		names = append(names, s.Name)
	}
	assert.ElementsMatch(t, []string{"other-ext-cccc"}, names)
}

func TestGarbageCollectOrphanedSlices_PreservesForeignSliceSharingOwnerLabel(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "my-ext", UID: extUID("my-ext")}}

	// foreign carries our owner-name label but is controlled by a different
	// ClusterExtension. It must never be garbage-collected as one of ours.
	foreign := coslObject("my-ext-foreign", "my-ext")
	foreign.OwnerReferences[0].UID = types.UID("intruder-uid")

	existing := []client.Object{
		coslObject("my-ext-aaaa", "my-ext"), // kept: in current set
		coslObject("my-ext-bbbb", "my-ext"), // deleted: owned, not in current set
		foreign,                             // kept: label matches but not controlled by ext
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(orbTestScheme(t)).
		WithObjects(existing...).
		Build()

	o := &OrbOperator{Client: fakeClient}

	slices := []*orbac.ClusterObjectSliceApplyConfiguration{
		orbac.ClusterObjectSlice("my-ext-aaaa"),
	}
	require.NoError(t, o.garbageCollectOrphanedSlices(context.Background(), ext, slices))

	var remaining orbv1alpha1.ClusterObjectSliceList
	require.NoError(t, fakeClient.List(context.Background(), &remaining))
	names := make([]string, 0, len(remaining.Items))
	for _, s := range remaining.Items {
		names = append(names, s.Name)
	}
	assert.ElementsMatch(t, []string{"my-ext-aaaa", "my-ext-foreign"}, names)
}

func acToUnstructured(t *testing.T, ac runtime.ApplyConfiguration) *unstructured.Unstructured {
	t.Helper()
	raw, err := json.Marshal(ac)
	require.NoError(t, err)
	u := &unstructured.Unstructured{}
	require.NoError(t, u.UnmarshalJSON(raw))
	return u
}

func TestOrbOperatorApply_SkipsUnchangedObjects(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: testExtName}}

	// Compute what ExternalizeCOD will produce for an identical COD, and seed the
	// cache with it. ExternalizeCOD is deterministic (content-addressable slice
	// names), so the seeded objects match what Apply will produce.
	seedCOD, seedSlices, err := orb.ExternalizeCOD(codThatExternalizes())
	require.NoError(t, err)
	require.NotEmpty(t, seedSlices)

	seed := make([]client.Object, 0, len(seedSlices)+1)
	seed = append(seed, acToUnstructured(t, seedCOD))
	for _, s := range seedSlices {
		seed = append(seed, acToUnstructured(t, s))
	}

	var applyCount int
	funcs := interceptor.Funcs{
		Apply: func(_ context.Context, _ client.WithWatch, _ runtime.ApplyConfiguration, _ ...client.ApplyOption) error {
			applyCount++
			return nil
		},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(orbTestScheme(t)).
		WithObjects(seed...).
		WithInterceptorFuncs(funcs).
		Build()

	o := &OrbOperator{
		Client:     fakeClient,
		Generator:  &fakeCODGenerator{cod: codThatExternalizes()},
		FieldOwner: "test-owner",
	}

	done, status, err := o.Apply(context.Background(), nil, ext, nil, nil)
	require.NoError(t, err)
	assert.True(t, done)
	assert.Empty(t, status)
	assert.Zero(t, applyCount, "apply must be skipped when the live objects already match the desired state")
}

func TestOrbOperatorApply_AppliesWhenExistingDiffers(t *testing.T) {
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: testExtName}}

	// Seed a COD with the same name but no spec, so the desired COD is not a
	// derivative of it and the apply must proceed.
	stale := &unstructured.Unstructured{}
	stale.SetGroupVersionKind(orbv1alpha1.GroupVersion.WithKind("ClusterObjectDeployment"))
	stale.SetName(testExtName)

	var codApplied bool
	funcs := interceptor.Funcs{
		Apply: func(_ context.Context, _ client.WithWatch, obj runtime.ApplyConfiguration, _ ...client.ApplyOption) error {
			if _, ok := obj.(*orbac.ClusterObjectDeploymentApplyConfiguration); ok {
				codApplied = true
			}
			return nil
		},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(orbTestScheme(t)).
		WithObjects(stale).
		WithInterceptorFuncs(funcs).
		Build()

	o := &OrbOperator{
		Client:     fakeClient,
		Generator:  &fakeCODGenerator{cod: codThatExternalizes()},
		FieldOwner: "test-owner",
	}

	done, _, err := o.Apply(context.Background(), nil, ext, nil, nil)
	require.NoError(t, err)
	assert.True(t, done)
	assert.True(t, codApplied, "COD must be applied when the live object differs from desired")
}
