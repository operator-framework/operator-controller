package crdupgradesafety

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"slices"

	kappcus "carvel.dev/kapp/pkg/kapp/crdupgradesafety"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	versionhelper "k8s.io/apimachinery/pkg/version"
)

type ServedVersionValidator struct {
	Validations []kappcus.ChangeValidation
}

func (c *ServedVersionValidator) Validate(old, new apiextensionsv1.CustomResourceDefinition) error {
	// If conversion webhook is specified, pass check
	if new.Spec.Conversion != nil && new.Spec.Conversion.Strategy == apiextensionsv1.WebhookConverter {
		return nil
	}

	errs := []error{}
	servedVersions := []apiextensionsv1.CustomResourceDefinitionVersion{}
	for _, version := range new.Spec.Versions {
		if version.Served {
			servedVersions = append(servedVersions, version)
		}
	}

	slices.SortFunc(servedVersions, func(a, b apiextensionsv1.CustomResourceDefinitionVersion) int {
		return versionhelper.CompareKubeAwareVersionStrings(a.Name, b.Name)
	})

	for i, oldVersion := range servedVersions[:len(servedVersions)-1] {
		for _, newVersion := range servedVersions[i+1:] {
			flatOld := kappcus.FlattenSchema(oldVersion.Schema.OpenAPIV3Schema)
			flatNew := kappcus.FlattenSchema(newVersion.Schema.OpenAPIV3Schema)
			diffs, err := kappcus.CalculateFlatSchemaDiff(flatOld, flatNew)
			if err != nil {
				errs = append(errs, fmt.Errorf("calculating schema diff between CRD versions %q and %q", oldVersion.Name, newVersion.Name))
				continue
			}

			for field, diff := range diffs {
				handled := false
				for _, validation := range c.Validations {
					ok, err := validation(diff)
					if err != nil {
						errs = append(errs, fmt.Errorf("version upgrade %q to %q, field %q: %w", oldVersion.Name, newVersion.Name, field, err))
					}
					if ok {
						handled = true
						break
					}
				}

				if !handled {
					errs = append(errs, fmt.Errorf("version %q, field %q has unknown change, refusing to determine that change is safe", oldVersion.Name, field))
				}
			}
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (c *ServedVersionValidator) Name() string {
	return "ServedVersionValidator"
}

type resetFunc func(diff kappcus.FieldDiff) kappcus.FieldDiff

func isHandled(diff kappcus.FieldDiff, reset resetFunc) bool {
	diff = reset(diff)
	return reflect.DeepEqual(diff.Old, diff.New)
}

func Enum(diff kappcus.FieldDiff) (bool, error) {
	reset := func(diff kappcus.FieldDiff) kappcus.FieldDiff {
		diff.Old.Enum = []apiextensionsv1.JSON{}
		diff.New.Enum = []apiextensionsv1.JSON{}
		return diff
	}

	oldEnums := sets.New[string]()
	for _, json := range diff.Old.Enum {
		oldEnums.Insert(string(json.Raw))
	}

	newEnums := sets.New[string]()
	for _, json := range diff.New.Enum {
		newEnums.Insert(string(json.Raw))
	}
	diffEnums := oldEnums.Difference(newEnums)
	var err error

	switch {
	case oldEnums.Len() == 0 && newEnums.Len() > 0:
		err = fmt.Errorf("enum constraints %v added when there were no restrictions previously", newEnums.UnsortedList())
	case diffEnums.Len() > 0:
		err = fmt.Errorf("enums %v removed from the set of previously allowed values", diffEnums.UnsortedList())
	}

	return isHandled(diff, reset), err
}

func Required(diff kappcus.FieldDiff) (bool, error) {
	reset := func(diff kappcus.FieldDiff) kappcus.FieldDiff {
		diff.Old.Required = []string{}
		diff.New.Required = []string{}
		return diff
	}

	oldRequired := sets.New(diff.Old.Required...)
	newRequired := sets.New(diff.New.Required...)
	diffRequired := newRequired.Difference(oldRequired)
	var err error

	if diffRequired.Len() > 0 {
		err = fmt.Errorf("new required fields %v added", diffRequired.UnsortedList())
	}

	return isHandled(diff, reset), err
}

func Maximum(diff kappcus.FieldDiff) (bool, error) {
	reset := func(diff kappcus.FieldDiff) kappcus.FieldDiff {
		diff.Old.Maximum = nil
		diff.New.Maximum = nil
		return diff
	}

	var err error

	switch {
	case diff.Old.Maximum == nil && diff.New.Maximum != nil:
		err = fmt.Errorf("maximum constraint %v added when there were no restrictions previously", *diff.New.Maximum)
	case diff.Old.Maximum != nil && diff.New.Maximum != nil && *diff.New.Maximum < *diff.Old.Maximum:
		err = fmt.Errorf("maximum constraint decreased from %v to %v", *diff.Old.Maximum, *diff.New.Maximum)
	}

	return isHandled(diff, reset), err
}

func MaxItems(diff kappcus.FieldDiff) (bool, error) {
	reset := func(diff kappcus.FieldDiff) kappcus.FieldDiff {
		diff.Old.MaxItems = nil
		diff.New.MaxItems = nil
		return diff
	}

	var err error

	switch {
	case diff.Old.MaxItems == nil && diff.New.MaxItems != nil:
		err = fmt.Errorf("maxItems constraint %v added when there were no restrictions previously", *diff.New.MaxItems)
	case diff.Old.MaxItems != nil && diff.New.MaxItems != nil && *diff.New.MaxItems < *diff.Old.MaxItems:
		err = fmt.Errorf("maxItems constraint decreased from %v to %v", *diff.Old.MaxItems, *diff.New.MaxItems)
	}

	return isHandled(diff, reset), err
}

func MaxLength(diff kappcus.FieldDiff) (bool, error) {
	reset := func(diff kappcus.FieldDiff) kappcus.FieldDiff {
		diff.Old.MaxLength = nil
		diff.New.MaxLength = nil
		return diff
	}

	var err error

	switch {
	case diff.Old.MaxLength == nil && diff.New.MaxLength != nil:
		err = fmt.Errorf("maxLength constraint %v added when there were no restrictions previously", *diff.New.MaxLength)
	case diff.Old.MaxLength != nil && diff.New.MaxLength != nil && *diff.New.MaxLength < *diff.Old.MaxLength:
		err = fmt.Errorf("maxLength constraint decreased from %v to %v", *diff.Old.MaxLength, *diff.New.MaxLength)
	}

	return isHandled(diff, reset), err
}

func MaxProperties(diff kappcus.FieldDiff) (bool, error) {
	reset := func(diff kappcus.FieldDiff) kappcus.FieldDiff {
		diff.Old.MaxProperties = nil
		diff.New.MaxProperties = nil
		return diff
	}

	var err error

	switch {
	case diff.Old.MaxProperties == nil && diff.New.MaxProperties != nil:
		err = fmt.Errorf("maxProperties constraint %v added when there were no restrictions previously", *diff.New.MaxProperties)
	case diff.Old.MaxProperties != nil && diff.New.MaxProperties != nil && *diff.New.MaxProperties < *diff.Old.MaxProperties:
		err = fmt.Errorf("maxProperties constraint decreased from %v to %v", *diff.Old.MaxProperties, *diff.New.MaxProperties)
	}

	return isHandled(diff, reset), err
}

func Minimum(diff kappcus.FieldDiff) (bool, error) {
	reset := func(diff kappcus.FieldDiff) kappcus.FieldDiff {
		diff.Old.Minimum = nil
		diff.New.Minimum = nil
		return diff
	}

	var err error

	switch {
	case diff.Old.Minimum == nil && diff.New.Minimum != nil:
		err = fmt.Errorf("minimum constraint %v added when there were no restrictions previously", *diff.New.Minimum)
	case diff.Old.Minimum != nil && diff.New.Minimum != nil && *diff.New.Minimum > *diff.Old.Minimum:
		err = fmt.Errorf("minimum constraint increased from %v to %v", *diff.Old.Minimum, *diff.New.Minimum)
	}

	return isHandled(diff, reset), err
}

func MinItems(diff kappcus.FieldDiff) (bool, error) {
	reset := func(diff kappcus.FieldDiff) kappcus.FieldDiff {
		diff.Old.MinItems = nil
		diff.New.MinItems = nil
		return diff
	}

	var err error

	switch {
	case diff.Old.MinItems == nil && diff.New.MinItems != nil:
		err = fmt.Errorf("minItems constraint %v added when there were no restrictions previously", *diff.New.MinItems)
	case diff.Old.MinItems != nil && diff.New.MinItems != nil && *diff.New.MinItems > *diff.Old.MinItems:
		err = fmt.Errorf("minItems constraint increased from %v to %v", *diff.Old.MinItems, *diff.New.MinItems)
	}

	return isHandled(diff, reset), err
}

func MinLength(diff kappcus.FieldDiff) (bool, error) {
	reset := func(diff kappcus.FieldDiff) kappcus.FieldDiff {
		diff.Old.MinLength = nil
		diff.New.MinLength = nil
		return diff
	}

	var err error

	switch {
	case diff.Old.MinLength == nil && diff.New.MinLength != nil:
		err = fmt.Errorf("minLength constraint %v added when there were no restrictions previously", *diff.New.MinLength)
	case diff.Old.MinLength != nil && diff.New.MinLength != nil && *diff.New.MinLength > *diff.Old.MinLength:
		err = fmt.Errorf("minLength constraint increased from %v to %v", *diff.Old.MinLength, *diff.New.MinLength)
	}

	return isHandled(diff, reset), err
}

func MinProperties(diff kappcus.FieldDiff) (bool, error) {
	reset := func(diff kappcus.FieldDiff) kappcus.FieldDiff {
		diff.Old.MinProperties = nil
		diff.New.MinProperties = nil
		return diff
	}

	var err error

	switch {
	case diff.Old.MinProperties == nil && diff.New.MinProperties != nil:
		err = fmt.Errorf("minProperties constraint %v added when there were no restrictions previously", *diff.New.MinProperties)
	case diff.Old.MinProperties != nil && diff.New.MinProperties != nil && *diff.New.MinProperties > *diff.Old.MinProperties:
		err = fmt.Errorf("minProperties constraint increased from %v to %v", *diff.Old.MinProperties, *diff.New.MinProperties)
	}

	return isHandled(diff, reset), err
}

func Default(diff kappcus.FieldDiff) (bool, error) {
	reset := func(diff kappcus.FieldDiff) kappcus.FieldDiff {
		diff.Old.Default = nil
		diff.New.Default = nil
		return diff
	}

	var err error

	switch {
	case diff.Old.Default == nil && diff.New.Default != nil:
		err = fmt.Errorf("default value %q added when there was no default previously", string(diff.New.Default.Raw))
	case diff.Old.Default != nil && diff.New.Default == nil:
		err = fmt.Errorf("default value %q removed", string(diff.Old.Default.Raw))
	case diff.Old.Default != nil && diff.New.Default != nil && !bytes.Equal(diff.Old.Default.Raw, diff.New.Default.Raw):
		err = fmt.Errorf("default value changed from %q to %q", string(diff.Old.Default.Raw), string(diff.New.Default.Raw))
	}

	return isHandled(diff, reset), err
}

func Type(diff kappcus.FieldDiff) (bool, error) {
    reset := func(diff kappcus.FieldDiff) kappcus.FieldDiff {
        diff.Old.Type = ""
        diff.New.Type = ""
        return diff
    }

    var err error
    if diff.Old.Type != diff.New.Type {
        err = fmt.Errorf("type changed from %q to %q", diff.Old.Type, diff.New.Type)
    }

    return isHandled(diff, reset), err
}
