package render

import (
	"errors"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util"
)

type RegistryV1 struct {
	PackageName string
	CSV         v1alpha1.ClusterServiceVersion
	CRDs        []apiextensionsv1.CustomResourceDefinition
	Others      []unstructured.Unstructured
}

// BundleValidator validates a RegistryV1 bundle by executing a series of
// checks on it and collecting any errors that were found
type BundleValidator []func(v1 *RegistryV1) []error

func (v BundleValidator) Validate(rv1 *RegistryV1) error {
	var errs []error
	for _, validator := range v {
		errs = append(errs, validator(rv1)...)
	}
	return errors.Join(errs...)
}

// ResourceGenerator generates resources given a registry+v1 bundle and options
type ResourceGenerator func(rv1 *RegistryV1, opts Options) ([]client.Object, error)

func (g ResourceGenerator) GenerateResources(rv1 *RegistryV1, opts Options) ([]client.Object, error) {
	return g(rv1, opts)
}

// ResourceGenerators aggregates generators. Its GenerateResource method will call all of its generators and return
// generated resources.
type ResourceGenerators []ResourceGenerator

func (r ResourceGenerators) GenerateResources(rv1 *RegistryV1, opts Options) ([]client.Object, error) {
	//nolint:prealloc
	var renderedObjects []client.Object
	for _, generator := range r {
		objs, err := generator.GenerateResources(rv1, opts)
		if err != nil {
			return nil, err
		}
		renderedObjects = append(renderedObjects, objs...)
	}
	return renderedObjects, nil
}

func (r ResourceGenerators) ResourceGenerator() ResourceGenerator {
	return r.GenerateResources
}

type UniqueNameGenerator func(string, interface{}) (string, error)

type Options struct {
	InstallNamespace    string
	TargetNamespaces    []string
	UniqueNameGenerator UniqueNameGenerator
}

func (o *Options) apply(opts ...Option) *Options {
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
	return o
}

type Option func(*Options)

func WithTargetNamespaces(namespaces ...string) Option {
	return func(o *Options) {
		o.TargetNamespaces = namespaces
	}
}

func WithUniqueNameGenerator(generator UniqueNameGenerator) Option {
	return func(o *Options) {
		o.UniqueNameGenerator = generator
	}
}

type BundleRenderer struct {
	BundleValidator    BundleValidator
	ResourceGenerators []ResourceGenerator
}

func (r BundleRenderer) Render(rv1 RegistryV1, installNamespace string, opts ...Option) ([]client.Object, error) {
	// validate bundle
	if err := r.BundleValidator.Validate(&rv1); err != nil {
		return nil, err
	}

	genOpts := (&Options{
		InstallNamespace:    installNamespace,
		TargetNamespaces:    []string{metav1.NamespaceAll},
		UniqueNameGenerator: DefaultUniqueNameGenerator,
	}).apply(opts...)

	// generate bundle objects
	objs, err := ResourceGenerators(r.ResourceGenerators).GenerateResources(&rv1, *genOpts)
	if err != nil {
		return nil, err
	}

	return objs, nil
}

func DefaultUniqueNameGenerator(base string, o interface{}) (string, error) {
	hashStr, err := util.DeepHashObject(o)
	if err != nil {
		return "", err
	}
	return util.ObjectNameForBaseAndSuffix(base, hashStr), nil
}
