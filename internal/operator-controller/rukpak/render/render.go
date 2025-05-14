package render

import (
	"errors"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
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
	CertificateProvider CertificateProvider
}

func (o *Options) apply(opts ...Option) *Options {
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
	return o
}

func (o *Options) validate(rv1 *RegistryV1) (*Options, []error) {
	var errs []error
	if len(o.TargetNamespaces) == 0 {
		errs = append(errs, errors.New("at least one target namespace must be specified"))
	}
	if o.UniqueNameGenerator == nil {
		errs = append(errs, errors.New("unique name generator must be specified"))
	}
	if err := validateTargetNamespaces(rv1, o.InstallNamespace, o.TargetNamespaces); err != nil {
		errs = append(errs, fmt.Errorf("invalid target namespaces %v: %w", o.TargetNamespaces, err))
	}
	return o, errs
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

func WithCertificateProvider(provider CertificateProvider) Option {
	return func(o *Options) {
		o.CertificateProvider = provider
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

	// generate bundle objects
	genOpts, errs := (&Options{
		// default options
		InstallNamespace:    installNamespace,
		TargetNamespaces:    []string{metav1.NamespaceAll},
		UniqueNameGenerator: DefaultUniqueNameGenerator,
		CertificateProvider: nil,
	}).apply(opts...).validate(&rv1)

	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid option(s): %w", errors.Join(errs...))
	}

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

func validateTargetNamespaces(rv1 *RegistryV1, installNamespace string, targetNamespaces []string) error {
	supportedInstallModes := sets.New[string]()
	for _, im := range rv1.CSV.Spec.InstallModes {
		if im.Supported {
			supportedInstallModes.Insert(string(im.Type))
		}
	}

	set := sets.New[string](targetNamespaces...)
	switch {
	case set.Len() == 0 || (set.Len() == 1 && set.Has("")):
		if supportedInstallModes.Has(string(v1alpha1.InstallModeTypeAllNamespaces)) {
			return nil
		}
		return fmt.Errorf("supported install modes %v do not support targeting all namespaces", sets.List(supportedInstallModes))
	case set.Len() == 1 && !set.Has(""):
		if supportedInstallModes.Has(string(v1alpha1.InstallModeTypeSingleNamespace)) {
			return nil
		}
		if supportedInstallModes.Has(string(v1alpha1.InstallModeTypeOwnNamespace)) && targetNamespaces[0] == installNamespace {
			return nil
		}
	default:
		if supportedInstallModes.Has(string(v1alpha1.InstallModeTypeMultiNamespace)) && !set.Has("") {
			return nil
		}
	}
	return fmt.Errorf("supported install modes %v do not support target namespaces %v", sets.List[string](supportedInstallModes), targetNamespaces)
}
