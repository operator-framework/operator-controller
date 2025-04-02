package render

import (
	"fmt"
	"slices"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/convert"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util"
)

const maxNameLength = 63

type Option func(*options)

type options struct {
	UniqueNameGenerator UniqueNameGenerator
}

func (o *options) apply(opts ...Option) *options {
	for _, opt := range opts {
		opt(o)
	}
	return o
}

type BundleRenderer struct {
	ResourceGenerators       []ResourceGenerator
	ResourceMutatorFactories []ResourceMutatorFactory
}

func (r BundleRenderer) Render(rv1 convert.RegistryV1, installNamespace string, watchNamespaces []string, opts ...Option) ([]client.Object, error) {
	renderOptions := (&options{
		UniqueNameGenerator: DefaultUniqueNameGenerator,
	}).apply(opts...)

	// create generation options
	genOpts := Options{
		InstallNamespace:    installNamespace,
		TargetNamespaces:    watchNamespaces,
		UniqueNameGenerator: renderOptions.UniqueNameGenerator,
	}

	// generate object mutators
	objMutators, err := ChainedResourceMutatorFactory(r.ResourceMutatorFactories).MakeResourceMutators()
	if err != nil {
		return nil, err
	}

	// generate bundle objects
	objs, err := ChainedResourceGenerator(r.ResourceGenerators...).GenerateResources(&rv1, genOpts)
	if err != nil {
		return nil, err
	}

	// mutate objects
	if err := objMutators.MutateObjects(slices.Values(objs)); err != nil {
		return nil, err
	}

	return objs, nil
}

func DefaultUniqueNameGenerator(base string, o interface{}) (string, error) {
	hashStr, err := util.DeepHashObject(o)
	if err != nil {
		return "", err
	}
	if len(base)+len(hashStr) > maxNameLength {
		base = base[:maxNameLength-len(hashStr)-1]
	}

	return fmt.Sprintf("%s-%s", base, hashStr), nil
}
