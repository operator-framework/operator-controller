package crdupgradesafety

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
			if len(resultErrs) > 0 {
				validateErrors = append(validateErrors, fmt.Errorf("validating upgrade for CRD %q: %w", newCrd.Name, errors.Join(resultErrs...)))
			}
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
					msg := err
					if result.Name == "unhandled" {
						msg = conciseUnhandledMessage(err)
					}
					errs = append(errs, fmt.Errorf("%s: %s: %s: %s", version, property, result.Name, msg))
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
					msg := err
					if result.Name == "unhandled" {
						msg = conciseUnhandledMessage(err)
					}
					errs = append(errs, fmt.Errorf("%s: %s: %s: %s", version, property, result.Name, msg))
				}
			}
		}
	}

	return errs
}

const unhandledSummaryPrefix = "unhandled changes found"

// conciseUnhandledMessage trims the CRD diff emitted by crdify's "unhandled" comparator
// into a short human readable description so operators get a hint of the change without
// the unreadable Go struct dump.
func conciseUnhandledMessage(raw string) string {
	if !strings.Contains(raw, unhandledSummaryPrefix) {
		return raw
	}

	details := extractUnhandledDetails(raw)
	if len(details) == 0 {
		return unhandledSummaryPrefix
	}

	return fmt.Sprintf("%s (%s)", unhandledSummaryPrefix, strings.Join(details, "; "))
}

func extractUnhandledDetails(raw string) []string {
	type diffEntry struct {
		before    string
		after     string
		beforeRaw string
		afterRaw  string
	}

	entries := map[string]*diffEntry{}
	order := []string{}

	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) < 2 {
			continue
		}

		sign := trimmed[0]
		if sign != '-' && sign != '+' {
			continue
		}

		field, value, rawValue := parseUnhandledDiffValue(trimmed[1:])
		if field == "" {
			continue
		}

		entry, ok := entries[field]
		if !ok {
			entry = &diffEntry{}
			entries[field] = entry
			order = append(order, field)
		}

		if sign == '-' {
			entry.before = value
			entry.beforeRaw = rawValue
		} else {
			entry.after = value
			entry.afterRaw = rawValue
		}
	}

	details := []string{}
	for _, field := range order {
		entry := entries[field]
		if entry.before == "" && entry.after == "" {
			continue
		}
		if entry.before == entry.after && entry.beforeRaw == entry.afterRaw {
			continue
		}

		before := entry.before
		if before == "" {
			before = "<empty>"
		}
		after := entry.after
		if after == "" {
			after = "<empty>"
		}
		if entry.before == entry.after && entry.beforeRaw != entry.afterRaw {
			after = after + " (changed)"
		}

		details = append(details, fmt.Sprintf("%s %s -> %s", field, before, after))
	}

	return details
}

func parseUnhandledDiffValue(fragment string) (string, string, string) {
	cleaned := strings.TrimSpace(fragment)
	cleaned = strings.TrimPrefix(cleaned, "\t")
	cleaned = strings.TrimSpace(cleaned)
	cleaned = strings.TrimSuffix(cleaned, ",")

	parts := strings.SplitN(cleaned, ":", 2)
	if len(parts) != 2 {
		return "", "", ""
	}

	field := strings.TrimSpace(parts[0])
	rawValue := strings.TrimSpace(parts[1])
	value := normalizeUnhandledValue(rawValue)

	if field == "" {
		return "", "", ""
	}

	return field, value, rawValue
}

func normalizeUnhandledValue(value string) string {
	value = strings.TrimSuffix(value, ",")
	value = strings.TrimSpace(value)

	switch value {
	case "":
		return "<empty>"
	case "\"\"":
		return "\"\""
	}

	value = strings.ReplaceAll(value, "v1.", "")
	if strings.Contains(value, "JSONSchemaProps") {
		return "<complex value>"
	}

	return value
}
