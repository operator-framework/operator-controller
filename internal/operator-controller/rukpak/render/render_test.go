package render_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	. "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing"
)

func Test_BundleRenderer_NoConfig(t *testing.T) {
	renderer := render.BundleRenderer{}
	objs, err := renderer.Render(
		bundle.RegistryV1{
			CSV: MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces)),
		}, "", nil)
	require.NoError(t, err)
	require.Empty(t, objs)
}

func Test_BundleRenderer_ValidatesBundle(t *testing.T) {
	renderer := render.BundleRenderer{
		BundleValidator: render.BundleValidator{
			func(v1 *bundle.RegistryV1) []error {
				return []error{errors.New("this bundle is invalid")}
			},
		},
	}
	objs, err := renderer.Render(bundle.RegistryV1{}, "")
	require.Nil(t, objs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "this bundle is invalid")
}

func Test_BundleRenderer_CreatesCorrectDefaultOptions(t *testing.T) {
	expectedInstallNamespace := "install-namespace"
	expectedTargetNamespaces := []string{""}
	expectedUniqueNameGenerator := render.DefaultUniqueNameGenerator

	renderer := render.BundleRenderer{
		ResourceGenerators: []render.ResourceGenerator{
			func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
				require.Equal(t, expectedInstallNamespace, opts.InstallNamespace)
				require.Equal(t, expectedTargetNamespaces, opts.TargetNamespaces)
				require.Equal(t, reflect.ValueOf(expectedUniqueNameGenerator).Pointer(), reflect.ValueOf(render.DefaultUniqueNameGenerator).Pointer(), "options has unexpected default unique name generator")
				return nil, nil
			},
		},
	}

	_, _ = renderer.Render(bundle.RegistryV1{}, expectedInstallNamespace)
}

func Test_BundleRenderer_ValidatesRenderOptions(t *testing.T) {
	for _, tc := range []struct {
		name             string
		installNamespace string
		csv              v1alpha1.ClusterServiceVersion
		opts             []render.Option
		err              error
	}{
		{
			name:             "accepts empty targetNamespaces (because it is ignored)",
			installNamespace: "install-namespace",
			csv:              MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces)),
			opts: []render.Option{
				render.WithTargetNamespaces(),
			},
		}, {
			name:             "rejects nil unique name generator",
			installNamespace: "install-namespace",
			csv:              MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces)),
			opts: []render.Option{
				render.WithUniqueNameGenerator(nil),
			},
			err: errors.New("invalid option(s): unique name generator must be specified"),
		}, {
			name:             "rejects all namespace install if AllNamespaces install mode is not supported",
			installNamespace: "install-namespace",
			csv:              MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeSingleNamespace)),
			opts: []render.Option{
				render.WithTargetNamespaces(corev1.NamespaceAll),
			},
			err: errors.New("invalid option(s): invalid target namespaces []: supported install modes [SingleNamespace] do not support targeting all namespaces"),
		}, {
			name:             "rejects own namespace install if only AllNamespace install mode is supported",
			installNamespace: "install-namespace",
			csv:              MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces)),
			opts: []render.Option{
				render.WithTargetNamespaces("install-namespace"),
			},
			err: errors.New("invalid option(s): invalid target namespaces [install-namespace]: supported install modes [AllNamespaces] do not support targeting own namespace"),
		}, {
			name:             "rejects install out of own namespace if only OwnNamespace install mode is supported",
			installNamespace: "install-namespace",
			csv:              MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeOwnNamespace)),
			opts: []render.Option{
				render.WithTargetNamespaces("not-install-namespace"),
			},
			err: errors.New("invalid option(s): invalid target namespaces [not-install-namespace]: supported install modes [OwnNamespace] do not support target namespaces [not-install-namespace]"),
		}, {
			name:             "rejects multi-namespace install if MultiNamespace install mode is not supported",
			installNamespace: "install-namespace",
			csv:              MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces)),
			opts: []render.Option{
				render.WithTargetNamespaces("ns1", "ns2", "ns3"),
			},
			err: errors.New("invalid option(s): invalid target namespaces [ns1 ns2 ns3]: supported install modes [AllNamespaces] do not support target namespaces [ns1 ns2 ns3]"),
		}, {
			name:             "rejects if bundle supports no install modes",
			installNamespace: "install-namespace",
			csv:              MakeCSV(),
			opts: []render.Option{
				render.WithTargetNamespaces("some-namespace"),
			},
			err: errors.New("invalid option(s): invalid target namespaces [some-namespace]: supported install modes [] do not support target namespaces [some-namespace]"),
		}, {
			name:             "accepts all namespace render if AllNamespaces install mode is supported",
			installNamespace: "install-namespace",
			csv:              MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces)),
			opts: []render.Option{
				render.WithTargetNamespaces(""),
			},
		}, {
			name:             "accepts install namespace render if SingleNamespace install mode is supported",
			installNamespace: "install-namespace",
			csv:              MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeSingleNamespace)),
			opts: []render.Option{
				render.WithTargetNamespaces("some-namespace"),
			},
		}, {
			name:             "accepts all install namespace render if OwnNamespace install mode is supported",
			installNamespace: "install-namespace",
			csv:              MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeOwnNamespace)),
			opts: []render.Option{
				render.WithTargetNamespaces("install-namespace"),
			},
		}, {
			name:             "accepts single namespace render if SingleNamespace install mode is supported",
			installNamespace: "install-namespace",
			csv:              MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeSingleNamespace)),
			opts: []render.Option{
				render.WithTargetNamespaces("some-namespace"),
			},
		}, {
			name:             "accepts multi namespace render if MultiNamespace install mode is supported",
			installNamespace: "install-namespace",
			csv:              MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeMultiNamespace)),
			opts: []render.Option{
				render.WithTargetNamespaces("n1", "n2", "n3"),
			},
		}, {
			name:             "reject multi namespace render if OwnNamespace install mode is not supported and target namespaces include install namespace",
			installNamespace: "install-namespace",
			csv:              MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeMultiNamespace)),
			opts: []render.Option{
				render.WithTargetNamespaces("n1", "n2", "n3", "install-namespace"),
			},
			err: errors.New("invalid option(s): invalid target namespaces [n1 n2 n3 install-namespace]: supported install modes [MultiNamespace] do not support targeting own namespace"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			renderer := render.BundleRenderer{}
			_, err := renderer.Render(
				bundle.RegistryV1{CSV: tc.csv},
				tc.installNamespace,
				tc.opts...,
			)
			if tc.err == nil {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Equal(t, tc.err.Error(), err.Error())
			}
		})
	}
}

func Test_BundleRenderer_AppliesUserOptions(t *testing.T) {
	isOptionApplied := false
	_, _ = render.BundleRenderer{}.Render(bundle.RegistryV1{}, "install-namespace", func(options *render.Options) {
		isOptionApplied = true
	})
	require.True(t, isOptionApplied)
}

func Test_WithTargetNamespaces(t *testing.T) {
	opts := &render.Options{
		TargetNamespaces: []string{"target-namespace"},
	}
	render.WithTargetNamespaces("a", "b", "c")(opts)
	require.Equal(t, []string{"a", "b", "c"}, opts.TargetNamespaces)
}

func Test_WithUniqueNameGenerator(t *testing.T) {
	opts := &render.Options{
		UniqueNameGenerator: render.DefaultUniqueNameGenerator,
	}
	render.WithUniqueNameGenerator(func(s string, i interface{}) string {
		return "a man needs a name"
	})(opts)
	generatedName := opts.UniqueNameGenerator("", nil)
	require.Equal(t, "a man needs a name", generatedName)
}

func Test_WithCertificateProvide(t *testing.T) {
	opts := &render.Options{}
	expectedCertProvider := FakeCertProvider{}
	render.WithCertificateProvider(expectedCertProvider)(opts)
	require.Equal(t, expectedCertProvider, opts.CertificateProvider)
}

func Test_BundleRenderer_CallsResourceGenerators(t *testing.T) {
	renderer := render.BundleRenderer{
		ResourceGenerators: []render.ResourceGenerator{
			func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
				return []client.Object{&corev1.Namespace{}, &corev1.Service{}}, nil
			},
			func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
				return []client.Object{&appsv1.Deployment{}}, nil
			},
		},
	}
	objs, err := renderer.Render(
		bundle.RegistryV1{
			CSV: MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces)),
		}, "")
	require.NoError(t, err)
	require.Equal(t, []client.Object{&corev1.Namespace{}, &corev1.Service{}, &appsv1.Deployment{}}, objs)
}

func Test_BundleRenderer_ReturnsResourceGeneratorErrors(t *testing.T) {
	renderer := render.BundleRenderer{
		ResourceGenerators: []render.ResourceGenerator{
			func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
				return []client.Object{&corev1.Namespace{}, &corev1.Service{}}, nil
			},
			func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
				return nil, fmt.Errorf("generator error")
			},
		},
	}
	objs, err := renderer.Render(
		bundle.RegistryV1{
			CSV: MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces)),
		}, "")
	require.Nil(t, objs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "generator error")
}

func Test_BundleValidatorCallsAllValidationFnsInOrder(t *testing.T) {
	actual := ""
	val := render.BundleValidator{
		func(v1 *bundle.RegistryV1) []error {
			actual += "h"
			return nil
		},
		func(v1 *bundle.RegistryV1) []error {
			actual += "i"
			return nil
		},
	}
	require.NoError(t, val.Validate(nil))
	require.Equal(t, "hi", actual)
}
