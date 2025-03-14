package crdupgradesafety

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	versionhelper "k8s.io/apimachinery/pkg/version"
)

type ServedVersionValidator struct {
	Validations []ChangeValidation
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
			flatOld := FlattenSchema(oldVersion.Schema.OpenAPIV3Schema)
			flatNew := FlattenSchema(newVersion.Schema.OpenAPIV3Schema)
			diffs, err := CalculateFlatSchemaDiff(flatOld, flatNew)
			if err != nil {
				errs = append(errs, fmt.Errorf("calculating schema diff between CRD versions %q and %q", oldVersion.Name, newVersion.Name))
				continue
			}

			for _, field := range slices.Sorted(maps.Keys(diffs)) {
				diff := diffs[field]

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
