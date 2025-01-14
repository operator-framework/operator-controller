package manifestcomparators

import (
	"fmt"
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

type noNewRequiredFields struct{}

func NoNewRequiredFields() CRDComparator {
	return noNewRequiredFields{}
}

func (noNewRequiredFields) Name() string {
	return "NoNewRequiredFields"
}

func (noNewRequiredFields) WhyItMatters() string {
	return "If new fields are required, then old clients will not function properly.  Even if CRD defaulting is used, " +
		"CRD defaulting requires allowing an object with an empty or missing value to then get defaulted."
}

func (b noNewRequiredFields) Compare(existingCRD, newCRD *apiextensionsv1.CustomResourceDefinition) (ComparisonResults, error) {
	if existingCRD == nil {
		return ComparisonResults{
			Name:         b.Name(),
			WhyItMatters: b.WhyItMatters(),

			Errors:   nil,
			Warnings: nil,
			Infos:    nil,
		}, nil
	}
	errsToReport := []string{}

	for _, newVersion := range newCRD.Spec.Versions {

		existingVersion := GetVersionByName(existingCRD, newVersion.Name)
		if existingVersion == nil {
			continue
		}

		existingRequiredFields := map[string]sets.String{}
		existingSimpleLocationToJSONSchemaProps := map[string]*apiextensionsv1.JSONSchemaProps{}
		SchemaHas(existingVersion.Schema.OpenAPIV3Schema, field.NewPath("^"), field.NewPath("^"), nil,
			func(s *apiextensionsv1.JSONSchemaProps, fldPath, simpleLocation *field.Path, _ []*apiextensionsv1.JSONSchemaProps) bool {
				existingRequiredFields[simpleLocation.String()] = sets.NewString(s.Required...)
				existingSimpleLocationToJSONSchemaProps[simpleLocation.String()] = s
				return false
			})

		// New fields can be required if they are wrapped inside new structs that are themselves optional.
		// For instance, you cannot add .spec.thingy as required, but if you add .spec.top as optional and at the same
		// time add .spec.top.thingy as required, this is allowed.
		// Similar logic exists for adding an array with minlength > 0
		newRequiredFields := sets.NewString()
		newSimpleLocationToRequiredFields := map[string]sets.String{}
		newToSimpleLocation := map[*apiextensionsv1.JSONSchemaProps]*field.Path{}
		SchemaHas(newVersion.Schema.OpenAPIV3Schema, field.NewPath("^"), field.NewPath("^"), nil,
			func(s *apiextensionsv1.JSONSchemaProps, fldPath, simpleLocation *field.Path, ancestors []*apiextensionsv1.JSONSchemaProps) bool {
				newSimpleLocationToRequiredFields[simpleLocation.String()] = sets.NewString(s.Required...)
				newToSimpleLocation[s] = simpleLocation

				if s.Type == "array" {
					// if it's an array, we have a different property to check.  A new array cannot be required unless it's ancestor is new.
					if s.MinLength == nil || *s.MinLength == 0 {
						// if there is no required length, this is fine
						return false
					}
					// this means we're an array with a minLength, check to see if any parent wrapper is both new and optional.
					if isAnyAncestorNewAndNullable(ancestors, existingSimpleLocationToJSONSchemaProps, newToSimpleLocation, newSimpleLocationToRequiredFields) {
						return false
					}

					// if we search all ancestors and couldn't find a new, optional element, then the current array cannot
					// have a minLength greater than zero.
					newRequiredFields.Insert(fmt.Sprintf("%s", simpleLocation.String()))
					return false
				}

				if len(s.Required) == 0 {
					// if nothing is required, nothing to check.
					return false
				}

				existingRequired, existedBefore := existingRequiredFields[simpleLocation.String()]
				if !existedBefore && s.Nullable {
					// if the parent of the required field (current element) didn't exist in the schema before AND
					// if the parent of the required field is nullable (client doesn't have to set it),
					// then we can allow a child to be required.
					return false
				}

				if isAnyAncestorNewAndNullable(ancestors, existingSimpleLocationToJSONSchemaProps, newToSimpleLocation, newSimpleLocationToRequiredFields) {
					// if any ancestor of the parent of the required field is new and nullable, then required is allowed.
					return false
				}

				// this covers newly required fields.
				newRequired := sets.NewString(s.Required...)
				if disallowedRequired := newRequired.Difference(existingRequired); len(disallowedRequired) > 0 {
					for _, curr := range disallowedRequired.List() {
						newRequiredFields.Insert(fmt.Sprintf("%s.%s", simpleLocation.String(), curr))
					}
					return false
				}

				return false
			})

		for _, newRequiredField := range newRequiredFields.List() {
			errsToReport = append(errsToReport, fmt.Sprintf("crd/%v version/%v field/%v is new and may not be required", newCRD.Name, newVersion.Name, newRequiredField))
		}

	}

	return ComparisonResults{
		Name:         b.Name(),
		WhyItMatters: b.WhyItMatters(),

		Errors:   errsToReport,
		Warnings: nil,
		Infos:    nil,
	}, nil
}

func isAnyAncestorNewAndNullable(
	ancestors []*apiextensionsv1.JSONSchemaProps,
	existingSimpleLocationToJSONSchemaProps map[string]*apiextensionsv1.JSONSchemaProps,
	newToSimpleLocation map[*apiextensionsv1.JSONSchemaProps]*field.Path,
	newSimpleLocationToRequiredFields map[string]sets.String) bool {

	for i := len(ancestors) - 1; i >= 0; i-- {
		ancestor := ancestors[i]
		ancestorSimpleName := newToSimpleLocation[ancestor]
		isOptionalArray := ancestor.Type == "array" && (ancestor.MinLength == nil || *ancestor.MinLength == 0)
		isAncestoryOptional := ancestor.Nullable || isOptionalArray
		if !isAncestoryOptional {
			// if this ancestor isn't nullable, then it cannot allow the current element to be required
			continue
		}

		if _, existed := existingSimpleLocationToJSONSchemaProps[ancestorSimpleName.String()]; existed {
			// if this ancestor previously existed, then it cannot allow the current element to be required
			continue
		}
		if i == 0 {
			// if the current accessor is the top level and Nullable, then it isn't required
			return true
		}

		// does the current ancestor require
		parentOfAncestor := ancestors[i-1]
		tokens := strings.Split(ancestorSimpleName.String(), ".")
		lastStep := tokens[len(tokens)-1]
		prevAncestorRequiredFields := newSimpleLocationToRequiredFields[newToSimpleLocation[parentOfAncestor].String()]
		if !prevAncestorRequiredFields.Has(lastStep) {
			// the current ancestor is not required, then we're ok and don't need to search further
			return true
		}
	}

	return false
}
