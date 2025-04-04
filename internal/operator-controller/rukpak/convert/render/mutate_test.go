package render_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/convert"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/convert/render"
)

func Test_ResourceMutator_Mutate(t *testing.T) {
	var m render.ResourceMutator = func(obj client.Object) error {
		obj.SetName("some-name")
		return nil
	}
	svc := corev1.Service{}
	err := m.Mutate(&svc)
	require.NoError(t, err)
	require.Equal(t, "some-name", svc.Name)
}

func Test_ResourceMutator_MutateObjects(t *testing.T) {
	var m render.ResourceMutator = func(obj client.Object) error {
		obj.SetName("some-name")
		return nil
	}

	objs := []client.Object{&corev1.Service{}, &corev1.ConfigMap{}}
	err := m.MutateObjects(slices.Values(objs))
	require.NoError(t, err)
	for _, obj := range objs {
		require.Equal(t, "some-name", obj.GetName())
	}
}

func Test_ResourceMutator_MutateObjects_Errors(t *testing.T) {
	var m render.ResourceMutator = func(obj client.Object) error {
		return fmt.Errorf("some error")
	}

	objs := []client.Object{&corev1.Service{}, &corev1.ConfigMap{}}
	err := m.MutateObjects(slices.Values(objs))
	require.Error(t, err)
	require.Contains(t, err.Error(), "some error")
}

func Test_ResourceMutators_Mutate(t *testing.T) {
	ms := render.ResourceMutators{
		func(obj client.Object) error {
			obj.SetName("some-name")
			return nil
		},
		func(obj client.Object) error {
			obj.SetNamespace("some-namespace")
			return nil
		},
	}
	svc := corev1.Service{}
	err := ms.Mutate(&svc)
	require.NoError(t, err)
	require.Equal(t, "some-name", svc.Name)
	require.Equal(t, "some-namespace", svc.Namespace)
}

func Test_ResourceMutators_MutateObjects(t *testing.T) {
	ms := render.ResourceMutators{
		func(obj client.Object) error {
			obj.SetName("some-name")
			return nil
		},
		func(obj client.Object) error {
			obj.SetNamespace("some-namespace")
			return nil
		},
	}
	objs := []client.Object{&corev1.Service{}, &corev1.ConfigMap{}}
	err := ms.MutateObjects(slices.Values(objs))
	require.NoError(t, err)
	for _, obj := range objs {
		require.Equal(t, "some-name", obj.GetName())
		require.Equal(t, "some-namespace", obj.GetNamespace())
	}
}

func Test_ResourceMutators_MutateObjects_Errors(t *testing.T) {
	ms := render.ResourceMutators{
		func(obj client.Object) error {
			obj.SetName("some-name")
			return nil
		},
		func(obj client.Object) error {
			return fmt.Errorf("some error")
		},
	}
	objs := []client.Object{&corev1.Service{}, &corev1.ConfigMap{}}
	err := ms.MutateObjects(slices.Values(objs))
	require.Error(t, err)
	require.Contains(t, err.Error(), "some error")
}

func Test_ResourceMutatorFactory_MakeResourceMutators(t *testing.T) {
	var f render.ResourceMutatorFactory = func(rv1 *convert.RegistryV1, opts render.Options) (render.ResourceMutators, error) {
		return render.ResourceMutators{
			func(obj client.Object) error {
				obj.SetName("some-name")
				return nil
			},
		}, nil
	}

	ms, err := f.MakeResourceMutators(&convert.RegistryV1{}, render.Options{})
	require.NoError(t, err)
	require.NotNil(t, ms)
	require.Len(t, ms, 1)

	svc := &corev1.Service{}
	require.NoError(t, ms.Mutate(svc))
	require.Equal(t, "some-name", svc.Name)
}

func Test_ChainedResourceMutatorFactory(t *testing.T) {
	cf := render.ChainedResourceMutatorFactory{
		func(rv1 *convert.RegistryV1, opts render.Options) (render.ResourceMutators, error) {
			return render.ResourceMutators{
				func(object client.Object) error {
					object.SetName("some-name")
					return nil
				},
			}, nil
		},
		func(rv1 *convert.RegistryV1, opts render.Options) (render.ResourceMutators, error) {
			return render.ResourceMutators{
				func(object client.Object) error {
					object.SetNamespace("some-namespace")
					return nil
				},
			}, nil
		},
	}

	ms, err := cf.MakeResourceMutators(&convert.RegistryV1{}, render.Options{})
	require.NoError(t, err)
	require.NotNil(t, ms)
	require.Len(t, ms, 2)

	svc := &corev1.Service{}
	require.NoError(t, ms.Mutate(svc))
	require.Equal(t, "some-name", svc.Name)
	require.Equal(t, "some-namespace", svc.Namespace)
}

func Test_CustomResourceDefinitionMutator(t *testing.T) {
	m := render.CustomResourceDefinitionMutator("my-crd", func(crd *apiextensionsv1.CustomResourceDefinition) error {
		crd.SetAnnotations(map[string]string{
			"foo": "bar",
		})
		return nil
	})

	t.Log("Check matching crd is mutated")
	crd := &apiextensionsv1.CustomResourceDefinition{}
	crd.SetName("my-crd")
	require.NoError(t, m.Mutate(crd))
	require.Equal(t, map[string]string{"foo": "bar"}, crd.GetAnnotations())

	t.Log("Check non-matching crd is NOT mutated")
	crd = &apiextensionsv1.CustomResourceDefinition{}
	crd.SetName("not-my-crd")
	require.NoError(t, m.Mutate(crd))
	require.NotEqual(t, map[string]string{"foo": "bar"}, crd.GetAnnotations())

	t.Log("Check mutator handles nil")
	require.NoError(t, m.Mutate(nil))
}
