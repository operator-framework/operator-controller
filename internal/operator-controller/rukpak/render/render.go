package render

import (
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/config"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util"
	hashutil "github.com/operator-framework/operator-controller/internal/shared/util/hash"
)

// BundleValidator validates a RegistryV1 bundle by executing a series of
// checks on it and collecting any errors that were found
type BundleValidator []func(v1 *bundle.RegistryV1) []error

func (v BundleValidator) Validate(rv1 *bundle.RegistryV1) error {
	errs := make([]error, 0, len(v))
	for _, validator := range v {
		errs = append(errs, validator(rv1)...)
	}
	return errors.Join(errs...)
}

// Context is the shared state threaded through the mutator pipeline. Each mutator reads
// and writes it. It subsumes the render Options and accumulates the rendered object set:
// configuration is seeded from the Options, InstallNamespace is resolved by the
// NamespaceMutator, and Objects is grown by the object mutators.
type Context struct {
	RV1 *bundle.RegistryV1

	// Configuration, seeded from the render Options / With* funcs.
	TargetNamespaces    []string
	UniqueNameGenerator UniqueNameGenerator
	CertificateProvider CertificateProvider
	// DeploymentConfig contains optional customizations to apply to CSV deployments.
	// If nil, no customizations are applied.
	DeploymentConfig *config.DeploymentConfig
	// selfManagedNamespace records that the install namespace was supplied by the caller.
	selfManagedNamespace bool

	// InstallNamespace is resolved by the NamespaceMutator and read by every later mutator.
	InstallNamespace string

	// Objects is the accumulating rendered object set.
	Objects []client.Object
}

// validate checks the context configuration once the install namespace is known.
func (c *Context) validate() error {
	var errs []error
	if c.UniqueNameGenerator == nil {
		errs = append(errs, errors.New("unique name generator must be specified"))
	}
	if err := validateTargetNamespaces(c.RV1, c.InstallNamespace, c.TargetNamespaces); err != nil {
		errs = append(errs, fmt.Errorf("invalid target namespaces %v: %w", c.TargetNamespaces, err))
	}
	if len(errs) > 0 {
		return fmt.Errorf("invalid option(s): %w", errors.Join(errs...))
	}
	return nil
}

// Mutator reads and mutates the shared render Context. Order in the pipeline is significant:
// the NamespaceMutator runs first (so later mutators can read ctx.InstallNamespace) and the
// certificate mutator runs last (so it can decorate every object the others produced).
type Mutator func(*Context) error

type UniqueNameGenerator func(string, interface{}) string

type Options struct {
	InstallNamespace    string
	TargetNamespaces    []string
	UniqueNameGenerator UniqueNameGenerator
	CertificateProvider CertificateProvider
	// DeploymentConfig contains optional customizations to apply to CSV deployments.
	// If nil, no customizations are applied.
	DeploymentConfig *config.DeploymentConfig

	// selfManagedNamespace records that the install namespace is managed by the
	// caller (i.e. it was supplied via WithSelfManagedInstallNamespace). When false,
	// the renderer resolves a system-managed namespace and emits a Namespace object.
	selfManagedNamespace bool
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

// WithSelfManagedInstallNamespace declares that the install namespace is managed by
// the caller (e.g. the user set spec.namespace on the ClusterExtension). The renderer
// renders resources into ns and does NOT emit a Namespace object. When this option is
// absent, the renderer resolves a system-managed namespace from the bundle and emits
// the corresponding Namespace object.
func WithSelfManagedInstallNamespace(ns string) Option {
	return func(o *Options) {
		o.InstallNamespace = ns
		o.selfManagedNamespace = true
	}
}

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

// WithDeploymentConfig sets the deployment configuration to apply to CSV deployments.
// If deploymentConfig is nil, no customizations are applied.
func WithDeploymentConfig(deploymentConfig *config.DeploymentConfig) Option {
	return func(o *Options) {
		o.DeploymentConfig = deploymentConfig
	}
}

// newContext builds a render Context seeded from these Options.
func (o *Options) newContext(rv1 *bundle.RegistryV1) *Context {
	return &Context{
		RV1:                  rv1,
		TargetNamespaces:     o.TargetNamespaces,
		UniqueNameGenerator:  o.UniqueNameGenerator,
		CertificateProvider:  o.CertificateProvider,
		DeploymentConfig:     o.DeploymentConfig,
		selfManagedNamespace: o.selfManagedNamespace,
		InstallNamespace:     o.InstallNamespace,
	}
}

// NewContext builds a render Context seeded from opts. It is exported for tests that need to
// exercise individual mutators without going through the full Render pipeline.
func NewContext(rv1 *bundle.RegistryV1, opts Options) *Context {
	return opts.newContext(rv1)
}

type BundleRenderer struct {
	BundleValidator BundleValidator
	// Mutators is the ordered pipeline applied to the shared render Context.
	Mutators []Mutator
}

func (r BundleRenderer) Render(rv1 bundle.RegistryV1, opts ...Option) ([]client.Object, error) {
	// validate bundle
	if err := r.BundleValidator.Validate(&rv1); err != nil {
		return nil, err
	}

	genOpts := (&Options{
		// default options
		TargetNamespaces:    defaultTargetNamespacesForBundle(&rv1),
		UniqueNameGenerator: DefaultUniqueNameGenerator,
		CertificateProvider: nil,
	}).apply(opts...)

	ctx := genOpts.newContext(&rv1)
	for _, mutate := range r.Mutators {
		if err := mutate(ctx); err != nil {
			return nil, err
		}
	}

	return ctx.Objects, nil
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
