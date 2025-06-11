package crdupgradesafety

import (
	"bytes"
	"cmp"
	"fmt"
	"reflect"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

type resetFunc func(diff FieldDiff) FieldDiff

func isHandled(diff FieldDiff, reset resetFunc) bool {
	diff = reset(diff)
	return reflect.DeepEqual(diff.Old, diff.New)
}

func Enum(diff FieldDiff) (bool, error) {
	reset := func(diff FieldDiff) FieldDiff {
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

func Required(diff FieldDiff) (bool, error) {
	reset := func(diff FieldDiff) FieldDiff {
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

func maxVerification[T cmp.Ordered](older *T, newer *T) error {
	var err error
	switch {
	case older == nil && newer != nil:
		err = fmt.Errorf("constraint %v added when there were no restrictions previously", *newer)
	case older != nil && newer != nil && *newer < *older:
		err = fmt.Errorf("constraint decreased from %v to %v", *older, *newer)
	}
	return err
}

func Maximum(diff FieldDiff) (bool, error) {
	reset := func(diff FieldDiff) FieldDiff {
		diff.Old.Maximum = nil
		diff.New.Maximum = nil
		return diff
	}

	err := maxVerification(diff.Old.Maximum, diff.New.Maximum)
	if err != nil {
		err = fmt.Errorf("maximum: %s", err.Error())
	}

	return isHandled(diff, reset), err
}

func MaxItems(diff FieldDiff) (bool, error) {
	reset := func(diff FieldDiff) FieldDiff {
		diff.Old.MaxItems = nil
		diff.New.MaxItems = nil
		return diff
	}

	err := maxVerification(diff.Old.MaxItems, diff.New.MaxItems)
	if err != nil {
		err = fmt.Errorf("maxItems: %s", err.Error())
	}

	return isHandled(diff, reset), err
}

func MaxLength(diff FieldDiff) (bool, error) {
	reset := func(diff FieldDiff) FieldDiff {
		diff.Old.MaxLength = nil
		diff.New.MaxLength = nil
		return diff
	}

	err := maxVerification(diff.Old.MaxLength, diff.New.MaxLength)
	if err != nil {
		err = fmt.Errorf("maxLength: %s", err.Error())
	}

	return isHandled(diff, reset), err
}

func MaxProperties(diff FieldDiff) (bool, error) {
	reset := func(diff FieldDiff) FieldDiff {
		diff.Old.MaxProperties = nil
		diff.New.MaxProperties = nil
		return diff
	}

	err := maxVerification(diff.Old.MaxProperties, diff.New.MaxProperties)
	if err != nil {
		err = fmt.Errorf("maxProperties: %s", err.Error())
	}

	return isHandled(diff, reset), err
}

func minVerification[T cmp.Ordered](older *T, newer *T) error {
	var err error
	switch {
	case older == nil && newer != nil:
		err = fmt.Errorf("constraint %v added when there were no restrictions previously", *newer)
	case older != nil && newer != nil && *newer > *older:
		err = fmt.Errorf("constraint increased from %v to %v", *older, *newer)
	}
	return err
}

func Minimum(diff FieldDiff) (bool, error) {
	reset := func(diff FieldDiff) FieldDiff {
		diff.Old.Minimum = nil
		diff.New.Minimum = nil
		return diff
	}

	err := minVerification(diff.Old.Minimum, diff.New.Minimum)
	if err != nil {
		err = fmt.Errorf("minimum: %s", err.Error())
	}

	return isHandled(diff, reset), err
}

func MinItems(diff FieldDiff) (bool, error) {
	reset := func(diff FieldDiff) FieldDiff {
		diff.Old.MinItems = nil
		diff.New.MinItems = nil
		return diff
	}

	err := minVerification(diff.Old.MinItems, diff.New.MinItems)
	if err != nil {
		err = fmt.Errorf("minItems: %s", err.Error())
	}

	return isHandled(diff, reset), err
}

func MinLength(diff FieldDiff) (bool, error) {
	reset := func(diff FieldDiff) FieldDiff {
		diff.Old.MinLength = nil
		diff.New.MinLength = nil
		return diff
	}

	err := minVerification(diff.Old.MinLength, diff.New.MinLength)
	if err != nil {
		err = fmt.Errorf("minLength: %s", err.Error())
	}

	return isHandled(diff, reset), err
}

func MinProperties(diff FieldDiff) (bool, error) {
	reset := func(diff FieldDiff) FieldDiff {
		diff.Old.MinProperties = nil
		diff.New.MinProperties = nil
		return diff
	}

	err := minVerification(diff.Old.MinProperties, diff.New.MinProperties)
	if err != nil {
		err = fmt.Errorf("minProperties: %s", err.Error())
	}

	return isHandled(diff, reset), err
}

func Default(diff FieldDiff) (bool, error) {
	reset := func(diff FieldDiff) FieldDiff {
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

func Type(diff FieldDiff) (bool, error) {
	reset := func(diff FieldDiff) FieldDiff {
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

// Description changes are considered safe and non-breaking.
func Description(diff FieldDiff) (bool, error) {
	reset := func(diff FieldDiff) FieldDiff {
		diff.Old.Description = ""
		diff.New.Description = ""
		return diff
	}
	return isHandled(diff, reset), nil
}
