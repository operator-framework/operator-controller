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

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
)

func Test_BundleRenderer_NoConfig(t *testing.T) {
	renderer := render.BundleRenderer{}
	objs, err := renderer.Render(render.RegistryV1{}, "", nil)
	require.NoError(t, err)
	require.Empty(t, objs)
}

func Test_BundleRenderer_ValidatesBundle(t *testing.T) {
	renderer := render.BundleRenderer{
		BundleValidator: render.BundleValidator{
			func(v1 *render.RegistryV1) []error {
				return []error{errors.New("this bundle is invalid")}
			},
		},
	}
	objs, err := renderer.Render(render.RegistryV1{}, "")
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
			func(rv1 *render.RegistryV1, opts render.Options) ([]client.Object, error) {
				require.Equal(t, expectedInstallNamespace, opts.InstallNamespace)
				require.Equal(t, expectedTargetNamespaces, opts.TargetNamespaces)
				require.Equal(t, reflect.ValueOf(expectedUniqueNameGenerator).Pointer(), reflect.ValueOf(render.DefaultUniqueNameGenerator).Pointer(), "options has unexpected default unique name generator")
				return nil, nil
			},
		},
	}

	_, _ = renderer.Render(render.RegistryV1{}, expectedInstallNamespace)
}

func Test_BundleRenderer_AppliesUserOptions(t *testing.T) {
	isOptionApplied := false
	_, _ = render.BundleRenderer{}.Render(render.RegistryV1{}, "install-namespace", func(options *render.Options) {
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
	render.WithUniqueNameGenerator(func(s string, i interface{}) (string, error) {
		return "a man needs a name", nil
	})(opts)
	generatedName, _ := opts.UniqueNameGenerator("", nil)
	require.Equal(t, "a man needs a name", generatedName)
}

func Test_BundleRenderer_CallsResourceGenerators(t *testing.T) {
	renderer := render.BundleRenderer{
		ResourceGenerators: []render.ResourceGenerator{
			func(rv1 *render.RegistryV1, opts render.Options) ([]client.Object, error) {
				return []client.Object{&corev1.Namespace{}, &corev1.Service{}}, nil
			},
			func(rv1 *render.RegistryV1, opts render.Options) ([]client.Object, error) {
				return []client.Object{&appsv1.Deployment{}}, nil
			},
		},
	}
	objs, err := renderer.Render(render.RegistryV1{}, "")
	require.NoError(t, err)
	require.Equal(t, []client.Object{&corev1.Namespace{}, &corev1.Service{}, &appsv1.Deployment{}}, objs)
}

func Test_BundleRenderer_ReturnsResourceGeneratorErrors(t *testing.T) {
	renderer := render.BundleRenderer{
		ResourceGenerators: []render.ResourceGenerator{
			func(rv1 *render.RegistryV1, opts render.Options) ([]client.Object, error) {
				return []client.Object{&corev1.Namespace{}, &corev1.Service{}}, nil
			},
			func(rv1 *render.RegistryV1, opts render.Options) ([]client.Object, error) {
				return nil, fmt.Errorf("generator error")
			},
		},
	}
	objs, err := renderer.Render(render.RegistryV1{}, "")
	require.Nil(t, objs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "generator error")
}

func Test_BundleValidatorCallsAllValidationFnsInOrder(t *testing.T) {
	actual := ""
	val := render.BundleValidator{
		func(v1 *render.RegistryV1) []error {
			actual += "h"
			return nil
		},
		func(v1 *render.RegistryV1) []error {
			actual += "i"
			return nil
		},
	}
	require.NoError(t, val.Validate(nil))
	require.Equal(t, "hi", actual)
}
