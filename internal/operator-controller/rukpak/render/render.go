package render

import (
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util"
	hashutil "github.com/operator-framework/operator-controller/internal/shared/util/hash"
)

// BundleValidator validates a RegistryV1 bundle by executing a series of
// checks on it and collecting any errors that were found
type BundleValidator []func(v1 *bundle.RegistryV1) []error

func (v BundleValidator) Validate(rv1 *bundle.RegistryV1) error {
	var errs []error
	for _, validator := range v {
		errs = append(errs, validator(rv1)...)
	}
	return errors.Join(errs...)
}

// ResourceGenerator generates resources given a registry+v1 bundle and options
type ResourceGenerator func(rv1 *bundle.RegistryV1, opts Options) ([]client.Object, error)

func (g ResourceGenerator) GenerateResources(rv1 *bundle.RegistryV1, opts Options) ([]client.Object, error) {
	return g(rv1, opts)
}

// ResourceGenerators aggregates generators. Its GenerateResource method will call all of its generators and return
// generated resources.
type ResourceGenerators []ResourceGenerator

func (r ResourceGenerators) GenerateResources(rv1 *bundle.RegistryV1, opts Options) ([]client.Object, error) {
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

type UniqueNameGenerator func(string, interface{}) string

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

func (o *Options) validate(rv1 *bundle.RegistryV1) (*Options, []error) {
	var errs []error
	if o.UniqueNameGenerator == nil {
		errs = append(errs, errors.New("unique name generator must be specified"))
	}
	if err := validateTargetNamespaces(rv1, o.InstallNamespace, o.TargetNamespaces); err != nil {
		errs = append(errs, fmt.Errorf("invalid target namespaces %v: %w", o.TargetNamespaces, err))
	}
	return o, errs
}

type Option func(*Options)

// WithTargetNamespaces sets the target namespaces to be used when rendering the bundle
// The value will only be used if len(namespaces) > 0. Otherwise, the default value for the bundle
// derived from its install mode support will be used (if such a value can be defined).
func WithTargetNamespaces(namespaces ...string) Option {
	return func(o *Options) {
		if len(namespaces) > 0 {
			o.TargetNamespaces = namespaces
		}
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

func (r BundleRenderer) Render(rv1 bundle.RegistryV1, installNamespace string, opts ...Option) ([]client.Object, error) {
	// validate bundle
	if err := r.BundleValidator.Validate(&rv1); err != nil {
		return nil, err
	}

	// generate bundle objects
	genOpts, errs := (&Options{
		// default options
		InstallNamespace:    installNamespace,
		TargetNamespaces:    defaultTargetNamespacesForBundle(&rv1),
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

func DefaultUniqueNameGenerator(base string, o interface{}) string {
	hashStr := hashutil.DeepHashObject(o)
	return util.ObjectNameForBaseAndSuffix(base, hashStr)
}

func validateTargetNamespaces(rv1 *bundle.RegistryV1, installNamespace string, targetNamespaces []string) error {
	supportedInstallModes := supportedBundleInstallModes(rv1)

	set := sets.New[string](targetNamespaces...)
	switch {
	case set.Len() == 0:
		// Note: this function generally expects targetNamespace to contain at least one value set by default
		// in case the user does not specify the value. The option to set the targetNamespace is a no-op if it is empty.
		// The only case for which a default targetNamespace is undefined is in the case of a bundle that only
		// supports SingleNamespace install mode. The if statement here is added to provide a more friendly error
		// message than just the generic (at least one target namespace must be specified) which would occur
		// in case only the MultiNamespace install mode is supported by the bundle.
		// If AllNamespaces mode is supported, the default will be [""] -> watch all namespaces
		// If only OwnNamespace is supported, the default will be [install-namespace] -> only watch the install/own namespace
		if supportedInstallModes.Has(v1alpha1.InstallModeTypeMultiNamespace) {
			return errors.New("at least one target namespace must be specified")
		}
		return errors.New("exactly one target namespace must be specified")
	case set.Len() == 1 && set.Has(""):
		if supportedInstallModes.Has(v1alpha1.InstallModeTypeAllNamespaces) {
			return nil
		}
		return fmt.Errorf("supported install modes %v do not support targeting all namespaces", sets.List(supportedInstallModes))
	case set.Len() == 1 && !set.Has(""):
		if targetNamespaces[0] == installNamespace {
			if !supportedInstallModes.Has(v1alpha1.InstallModeTypeOwnNamespace) {
				return fmt.Errorf("supported install modes %v do not support targeting own namespace", sets.List(supportedInstallModes))
			}
			return nil
		}
		if supportedInstallModes.Has(v1alpha1.InstallModeTypeSingleNamespace) {
			return nil
		}
	default:
		if !supportedInstallModes.Has(v1alpha1.InstallModeTypeOwnNamespace) && set.Has(installNamespace) {
			return fmt.Errorf("supported install modes %v do not support targeting own namespace", sets.List(supportedInstallModes))
		}
		if supportedInstallModes.Has(v1alpha1.InstallModeTypeMultiNamespace) && !set.Has("") {
			return nil
		}
	}
	return fmt.Errorf("supported install modes %v do not support target namespaces %v", sets.List[v1alpha1.InstallModeType](supportedInstallModes), targetNamespaces)
}

func defaultTargetNamespacesForBundle(rv1 *bundle.RegistryV1) []string {
	supportedInstallModes := supportedBundleInstallModes(rv1)

	if supportedInstallModes.Has(v1alpha1.InstallModeTypeAllNamespaces) {
		return []string{corev1.NamespaceAll}
	}

	return nil
}

func supportedBundleInstallModes(rv1 *bundle.RegistryV1) sets.Set[v1alpha1.InstallModeType] {
	supportedInstallModes := sets.New[v1alpha1.InstallModeType]()
	for _, im := range rv1.CSV.Spec.InstallModes {
		if im.Supported {
			supportedInstallModes.Insert(im.Type)
		}
	}
	return supportedInstallModes
}
