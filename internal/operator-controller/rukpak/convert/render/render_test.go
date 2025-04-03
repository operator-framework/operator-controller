package render_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/convert"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/convert/render"
)

func Test_BundleRenderer_NoConfig(t *testing.T) {
	renderer := render.BundleRenderer{}
	objs, err := renderer.Render(convert.RegistryV1{}, "", nil)
	require.NoError(t, err)
	require.Empty(t, objs)
}

func Test_BundleRenderer_CallsResourceGenerators(t *testing.T) {
	renderer := render.BundleRenderer{
		ResourceGenerators: []render.ResourceGenerator{
			func(rv1 *convert.RegistryV1, opts render.Options) ([]client.Object, error) {
				return []client.Object{&corev1.Namespace{}, &corev1.Service{}}, nil
			},
			func(rv1 *convert.RegistryV1, opts render.Options) ([]client.Object, error) {
				return []client.Object{&appsv1.Deployment{}}, nil
			},
		},
	}
	objs, err := renderer.Render(convert.RegistryV1{}, "", nil)
	require.NoError(t, err)
	require.Equal(t, []client.Object{&corev1.Namespace{}, &corev1.Service{}, &appsv1.Deployment{}}, objs)
}

func Test_BundleRenderer_CallsResourceMutators(t *testing.T) {
	renderer := render.BundleRenderer{
		ResourceGenerators: []render.ResourceGenerator{
			func(rv1 *convert.RegistryV1, opts render.Options) ([]client.Object, error) {
				return []client.Object{&corev1.Namespace{}, &corev1.Service{}}, nil
			},
		},
		ResourceMutatorFactories: []render.ResourceMutatorFactory{
			func(rv1 *convert.RegistryV1, opts render.Options) (render.ResourceMutators, error) {
				return []render.ResourceMutator{
					func(object client.Object) error {
						switch object.(type) {
						case *corev1.Namespace:
							object.SetName("some-namespace")
						case *corev1.Service:
							object.SetName("some-service")
						}
						return nil
					},
					func(object client.Object) error {
						object.SetLabels(map[string]string{
							"some": "label",
						})
						return nil
					},
				}, nil
			},
			func(rv1 *convert.RegistryV1, opts render.Options) (render.ResourceMutators, error) {
				return []render.ResourceMutator{
					func(object client.Object) error {
						object.SetAnnotations(map[string]string{
							"some": "annotation",
						})
						return nil
					},
				}, nil
			},
		},
	}
	objs, err := renderer.Render(convert.RegistryV1{}, "", nil)
	require.NoError(t, err)
	require.Equal(t, []client.Object{
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "some-namespace",
				Labels: map[string]string{
					"some": "label",
				},
				Annotations: map[string]string{
					"some": "annotation",
				},
			},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "some-service",
				Labels: map[string]string{
					"some": "label",
				},
				Annotations: map[string]string{
					"some": "annotation",
				},
			},
		},
	}, objs, objs)
}

func Test_BundleRenderer_ReturnsResourceGeneratorErrors(t *testing.T) {
	renderer := render.BundleRenderer{
		ResourceGenerators: []render.ResourceGenerator{
			func(rv1 *convert.RegistryV1, opts render.Options) ([]client.Object, error) {
				return []client.Object{&corev1.Namespace{}, &corev1.Service{}}, nil
			},
			func(rv1 *convert.RegistryV1, opts render.Options) ([]client.Object, error) {
				return nil, fmt.Errorf("generator error")
			},
		},
	}
	objs, err := renderer.Render(convert.RegistryV1{}, "", nil)
	require.Nil(t, objs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "generator error")
}

func Test_BundleRenderer_ReturnsResourceMutatorFactoryErrors(t *testing.T) {
	renderer := render.BundleRenderer{
		ResourceGenerators: []render.ResourceGenerator{
			func(rv1 *convert.RegistryV1, opts render.Options) ([]client.Object, error) {
				return []client.Object{&corev1.Namespace{}, &corev1.Service{}}, nil
			},
		},
		ResourceMutatorFactories: []render.ResourceMutatorFactory{
			func(rv1 *convert.RegistryV1, opts render.Options) (render.ResourceMutators, error) {
				return nil, errors.New("mutator factory error")
			},
		},
	}
	objs, err := renderer.Render(convert.RegistryV1{}, "", nil)
	require.Nil(t, objs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "mutator factory error")
}

func Test_BundleRenderer_ReturnsResourceMutatorErrors(t *testing.T) {
	renderer := render.BundleRenderer{
		ResourceGenerators: []render.ResourceGenerator{
			func(rv1 *convert.RegistryV1, opts render.Options) ([]client.Object, error) {
				return []client.Object{&corev1.Namespace{}, &corev1.Service{}}, nil
			},
		},
		ResourceMutatorFactories: []render.ResourceMutatorFactory{
			func(rv1 *convert.RegistryV1, opts render.Options) (render.ResourceMutators, error) {
				return []render.ResourceMutator{
					func(object client.Object) error {
						return errors.New("mutator error")
					},
				}, nil
			},
		},
	}
	objs, err := renderer.Render(convert.RegistryV1{}, "", nil)
	require.Nil(t, objs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "mutator error")
}
