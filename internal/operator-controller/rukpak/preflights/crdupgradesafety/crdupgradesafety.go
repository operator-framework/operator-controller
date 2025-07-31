package crdupgradesafety

import (
	"context"
	"errors"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1client "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/crdify/pkg/config"
	"sigs.k8s.io/crdify/pkg/runner"
	"sigs.k8s.io/crdify/pkg/validations"
	"sigs.k8s.io/crdify/pkg/validations/property"
)

type Option func(p *Preflight)

func WithConfig(cfg *config.Config) Option {
	return func(p *Preflight) {
		p.config = cfg
	}
}

func WithRegistry(reg validations.Registry) Option {
	return func(p *Preflight) {
		p.registry = reg
	}
}

type Preflight struct {
	crdClient apiextensionsv1client.CustomResourceDefinitionInterface
	config    *config.Config
	registry  validations.Registry
}

func NewPreflight(crdCli apiextensionsv1client.CustomResourceDefinitionInterface, opts ...Option) *Preflight {
	p := &Preflight{
		crdClient: crdCli,
		config:    defaultConfig(),
		registry:  defaultRegistry(),
	}

	for _, o := range opts {
		o(p)
	}

	return p
}

func (p *Preflight) Install(ctx context.Context, objs []client.Object) error {
	return p.runPreflight(ctx, objs)
}

func (p *Preflight) Upgrade(ctx context.Context, objs []client.Object) error {
	return p.runPreflight(ctx, objs)
}

func (p *Preflight) runPreflight(ctx context.Context, relObjects []client.Object) error {
	if len(relObjects) == 0 {
		return nil
	}
	runner, err := runner.New(p.config, p.registry)
	if err != nil {
		return fmt.Errorf("creating CRD validation runner: %w", err)
	}

	validateErrors := make([]error, 0, len(relObjects))
	for _, obj := range relObjects {
		if obj.GetObjectKind().GroupVersionKind() != apiextensionsv1.SchemeGroupVersion.WithKind("CustomResourceDefinition") {
			continue
		}

		newCrd := &apiextensionsv1.CustomResourceDefinition{}
		uMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			return fmt.Errorf("converting object %q to unstructured: %w", obj.GetName(), err)
		}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(uMap, newCrd)
		if err != nil {
			return fmt.Errorf("converting unstructured to CRD object: %w", err)
		}

		oldCrd, err := p.crdClient.Get(ctx, newCrd.Name, metav1.GetOptions{})
		if err != nil {
			// if there is no existing CRD, there is nothing to break
			// so it is immediately successful.
			if apierrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("getting existing resource for CRD %q: %w", newCrd.Name, err)
		}

		results := runner.Run(oldCrd, newCrd)
		if results.HasFailures() {
			resultErrs := crdWideErrors(results)
			resultErrs = append(resultErrs, sameVersionErrors(results)...)
			resultErrs = append(resultErrs, servedVersionErrors(results)...)

			validateErrors = append(validateErrors, fmt.Errorf("validating upgrade for CRD %q: %w", newCrd.Name, errors.Join(resultErrs...)))
		}
	}

	return errors.Join(validateErrors...)
}

func defaultConfig() *config.Config {
	return &config.Config{
		// Ignore served version validations if conversion policy is set.
		Conversion: config.ConversionPolicyIgnore,
		// Fail-closed by default
		UnhandledEnforcement: config.EnforcementPolicyError,
		// Use the default validation configurations as they are
		// the strictest possible.
		Validations: []config.ValidationConfig{
			// Do not enforce the description validation
			// because OLM should not block on field description changes.
			{
				Name:        "description",
				Enforcement: config.EnforcementPolicyNone,
			},
			{
				Name:        "enum",
				Enforcement: config.EnforcementPolicyError,
				Configuration: map[string]interface{}{
					"additionPolicy": property.AdditionPolicyAllow,
				},
			},
		},
	}
}

func defaultRegistry() validations.Registry {
	return runner.DefaultRegistry()
}

func crdWideErrors(results *runner.Results) []error {
	if results == nil {
		return nil
	}

	errs := []error{}
	for _, result := range results.CRDValidation {
		for _, err := range result.Errors {
			errs = append(errs, fmt.Errorf("%s: %s", result.Name, err))
		}
	}

	return errs
}

func sameVersionErrors(results *runner.Results) []error {
	if results == nil {
		return nil
	}

	errs := []error{}
	for version, propertyResults := range results.SameVersionValidation {
		for property, comparisonResults := range propertyResults {
			for _, result := range comparisonResults {
				for _, err := range result.Errors {
					errs = append(errs, fmt.Errorf("%s: %s: %s: %s", version, property, result.Name, err))
				}
			}
		}
	}

	return errs
}

func servedVersionErrors(results *runner.Results) []error {
	if results == nil {
		return nil
	}

	errs := []error{}
	for version, propertyResults := range results.ServedVersionValidation {
		for property, comparisonResults := range propertyResults {
			for _, result := range comparisonResults {
				for _, err := range result.Errors {
					errs = append(errs, fmt.Errorf("%s: %s: %s: %s", version, property, result.Name, err))
				}
			}
		}
	}

	return errs
}
