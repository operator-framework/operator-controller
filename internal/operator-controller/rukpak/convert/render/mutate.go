package render

import (
	"iter"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ResourceMutator func(client.Object) error

func (r ResourceMutator) Mutate(obj client.Object) error {
	return r(obj)
}

func (r ResourceMutator) MutateObjects(objs iter.Seq[client.Object]) error {
	for obj := range objs {
		if err := r.Mutate(obj); err != nil {
			return err
		}
	}
	return nil
}

type ResourceMutators []ResourceMutator

func (g *ResourceMutators) Append(generator ...ResourceMutator) {
	*g = append(*g, generator...)
}

func (g *ResourceMutators) Mutate(obj client.Object) error {
	for _, mutator := range *g {
		if err := mutator.Mutate(obj); err != nil {
			return err
		}
	}
	return nil
}

func (g *ResourceMutators) MutateObjects(objs iter.Seq[client.Object]) error {
	for obj := range objs {
		if err := g.Mutate(obj); err != nil {
			return err
		}
	}
	return nil
}

type ResourceMutatorFactory func() (ResourceMutators, error)

func (m ResourceMutatorFactory) MakeResourceMutators() (ResourceMutators, error) {
	return m()
}

type ChainedResourceMutatorFactory []ResourceMutatorFactory

func (c ChainedResourceMutatorFactory) MakeResourceMutators() (ResourceMutators, error) {
	var resourceMutators []ResourceMutator
	for _, mutatorFactory := range c {
		mutators, err := mutatorFactory.MakeResourceMutators()
		if err != nil {
			return nil, err
		}
		resourceMutators = append(resourceMutators, mutators...)
	}
	return resourceMutators, nil
}

func CustomResourceDefinitionMutator(name string, mutator func(crd *apiextensionsv1.CustomResourceDefinition) error) ResourceMutator {
	return func(obj client.Object) error {
		crd, ok := obj.(*apiextensionsv1.CustomResourceDefinition)
		if obj.GetName() != name || !ok {
			return nil
		}
		return mutator(crd)
	}
}
