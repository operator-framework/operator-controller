package crdupgradesafety

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"helm.sh/helm/v3/pkg/release"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1client "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util"
)

type Option func(p *Preflight)

func WithValidator(v *Validator) Option {
	return func(p *Preflight) {
		p.validator = v
	}
}

type Preflight struct {
	crdClient apiextensionsv1client.CustomResourceDefinitionInterface
	validator *Validator
}

func NewPreflight(crdCli apiextensionsv1client.CustomResourceDefinitionInterface, opts ...Option) *Preflight {
	changeValidations := []ChangeValidation{
		Enum,
		Required,
		Maximum,
		MaxItems,
		MaxLength,
		MaxProperties,
		Minimum,
		MinItems,
		MinLength,
		MinProperties,
		Default,
		Type,
	}
	p := &Preflight{
		crdClient: crdCli,
		// create a default validator. Can be overridden via the options
		validator: &Validator{
			Validations: []Validation{
				NewValidationFunc("NoScopeChange", NoScopeChange),
				NewValidationFunc("NoStoredVersionRemoved", NoStoredVersionRemoved),
				NewValidationFunc("NoExistingFieldRemoved", NoExistingFieldRemoved),
				&ServedVersionValidator{Validations: changeValidations},
				&ChangeValidator{Validations: changeValidations},
			},
		},
	}

	for _, o := range opts {
		o(p)
	}

	return p
}

func (p *Preflight) Install(ctx context.Context, rel *release.Release) error {
	return p.runPreflight(ctx, rel)
}

func (p *Preflight) Upgrade(ctx context.Context, rel *release.Release) error {
	return p.runPreflight(ctx, rel)
}

func (p *Preflight) runPreflight(ctx context.Context, rel *release.Release) error {
	if rel == nil {
		return nil
	}

	relObjects, err := util.ManifestObjects(strings.NewReader(rel.Manifest), fmt.Sprintf("%s-release-manifest", rel.Name))
	if err != nil {
		return fmt.Errorf("parsing release %q objects: %w", rel.Name, err)
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

		err = p.validator.Validate(*oldCrd, *newCrd)
		if err != nil {
			validateErrors = append(validateErrors, fmt.Errorf("validating upgrade for CRD %q failed: %w", newCrd.Name, err))
		}
	}

	return errors.Join(validateErrors...)
}

// orderKappsValidateErr is meant as a temporary solution to the problem
// of randomly ordered multi-line validation error returned by kapp's validator.Validate()
//
// The problem is that kapp's field validations are performed in map iteration order, which is not fixed.
// Errors from those validations are then error.Join'ed, fmt.Errorf'ed and error.Join'ed again,
// which means original messages are available at 3rd level of nesting, and this is where we need to
// sort them to ensure we do not enter into constant reconciliation loop because of random order of
// failure message we ultimately set in ClusterExtension's status conditions.
//
// This helper attempts to do that and falls back to original unchanged error message
// in case of any unforeseen issues which likely mean that the internals of validator.Validate
// have changed.
//
// For full context see:
// github.com/operator-framework/operator-controller/issues/1456 (original issue and comments)
// github.com/carvel-dev/kapp/pull/1047 (PR to ensure order in upstream)
//
// TODO: remove this once ordering has been handled by the upstream.
func orderKappsValidateErr(err error) error {
	joinedValidationErrs, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return err
	}

	// nolint: prealloc
	var errs []error
	for _, validationErr := range joinedValidationErrs.Unwrap() {
		unwrappedValidationErr := errors.Unwrap(validationErr)
		// validator.Validate did not error.Join'ed validation errors
		// kapp's internals changed - fallback to original error
		if unwrappedValidationErr == nil {
			return err
		}

		prefix, _, ok := strings.Cut(validationErr.Error(), ":")
		// kapp's internal error format changed - fallback to original error
		if !ok {
			return err
		}

		// attempt to unwrap and sort field errors
		joinedFieldErrs, ok := unwrappedValidationErr.(interface{ Unwrap() []error })
		// ChangeValidator did not error.Join'ed field validation errors
		// kapp's internals changed - fallback to original error
		if !ok {
			return err
		}

		// ensure order of the field validation errors
		unwrappedFieldErrs := joinedFieldErrs.Unwrap()
		slices.SortFunc(unwrappedFieldErrs, func(a, b error) int {
			return cmp.Compare(a.Error(), b.Error())
		})

		// re-join the sorted field errors keeping the original error prefix from kapp
		errs = append(errs, fmt.Errorf("%s: %w", prefix, errors.Join(unwrappedFieldErrs...)))
	}

	return errors.Join(errs...)
}
