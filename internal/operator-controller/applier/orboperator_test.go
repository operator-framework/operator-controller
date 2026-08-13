package applier

import (
	"context"
	"errors"
	"testing"

	orbac "github.com/joelanford/orb-operator/applyconfigurations/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
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
