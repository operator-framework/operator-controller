package render

import (
	"iter"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/convert"
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

type ResourceMutatorFactory func(rv1 *convert.RegistryV1, opts Options) (ResourceMutators, error)

func (m ResourceMutatorFactory) MakeResourceMutators(rv1 *convert.RegistryV1, opts Options) (ResourceMutators, error) {
	return m(rv1, opts)
}

type ChainedResourceMutatorFactory []ResourceMutatorFactory

func (c ChainedResourceMutatorFactory) MakeResourceMutators(rv1 *convert.RegistryV1, opts Options) (ResourceMutators, error) {
	var resourceMutators []ResourceMutator
	for _, mutatorFactory := range c {
		mutators, err := mutatorFactory.MakeResourceMutators(rv1, opts)
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

func ValidatingWebhookConfigurationMutator(name string, mutator func(wh *admissionregistrationv1.ValidatingWebhookConfiguration) error) ResourceMutator {
	return func(obj client.Object) error {
		wh, ok := obj.(*admissionregistrationv1.ValidatingWebhookConfiguration)
		if !ok || wh.GetName() != name {
			return nil
		}
		return mutator(wh)
	}
}

func MutatingWebhookConfigurationMutator(name string, mutator func(wh *admissionregistrationv1.MutatingWebhookConfiguration) error) ResourceMutator {
	return func(obj client.Object) error {
		wh, ok := obj.(*admissionregistrationv1.MutatingWebhookConfiguration)
		if !ok || wh.GetName() != name {
			return nil
		}
		return mutator(wh)
	}
}

func DeploymentResourceMutator(name string, namespace string, mutator func(dep *appsv1.Deployment) error) ResourceMutator {
	return func(obj client.Object) error {
		dep, ok := obj.(*appsv1.Deployment)
		if !ok || dep.GetName() != name || dep.GetNamespace() != namespace {
			return nil
		}
		return mutator(dep)
	}
}

func ServiceResourceMutator(name string, namespace string, mutator func(svc *corev1.Service) error) ResourceMutator {
	return func(obj client.Object) error {
		svc, ok := obj.(*corev1.Service)
		if !ok || svc.GetName() != name || svc.GetNamespace() != namespace {
			return nil
		}
		return mutator(svc)
	}
}
