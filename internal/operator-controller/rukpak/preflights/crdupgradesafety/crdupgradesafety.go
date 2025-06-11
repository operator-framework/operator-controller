package crdupgradesafety

import (
	"context"
	"errors"
	"fmt"
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
		Description,
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
