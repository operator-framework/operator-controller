/*
Copyright 2022 The Kubernetes Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package crd

import (
	"context"
	"fmt"
	"reflect"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Validate is a wrapper for doing four things:
// 	1. Retrieving the existing version of the specified CRD where it exists.
// 	2. Calling validateCRDCompatibility() on the newCrd.
// 	3. Calling safeStorageVersionUpgrade() on the newCrd.
// 	4. Reporting any errors that it encounters along the way.
func Validate(ctx context.Context, cl client.Client, newCrd *apiextensionsv1.CustomResourceDefinition) error {
	oldCRD := &apiextensionsv1.CustomResourceDefinition{}

	err := client.IgnoreNotFound(cl.Get(ctx, client.ObjectKeyFromObject(newCrd), oldCRD))
	if apierrors.IsNotFound(err) {
		// Return early if the CRD has not been created yet
		// as we know it is valid.
		return nil
	}
	if err != nil {
		return err
	}

	if err := validateCRDCompatibility(ctx, cl, oldCRD, newCrd); err != nil {
		return fmt.Errorf("error validating existing CRs against new CRD's schema for %q: %v", newCrd.Name, err)
	}

	// check to see if stored versions changed and whether the upgrade could cause potential data loss
	safe, err := safeStorageVersionUpgrade(oldCRD, newCrd)
	if !safe {
		return fmt.Errorf("risk of data loss updating %q: %v", newCrd.Name, err)
	}
	if err != nil {
		return fmt.Errorf("checking CRD for potential data loss updating %q: %v", newCrd.Name, err)
	}

	return nil
}

func keys(m map[string]apiextensionsv1.CustomResourceDefinitionVersion) sets.String {
	return sets.StringKeySet(m)
}

// validateCRDCompatibility runs through the following cases to test:
//   1. New CRD removes version that Old CRD had => Must ensure nothing is stored at removed version
//   2. New CRD changes a version that Old CRD has => Must validate existing CRs with new schema
//   3. New CRD adds a version that Old CRD does not have =>
//      - If conversion strategy is None, ensure existing CRs validate with new schema.
//      - If conversion strategy is Webhook, allow update (assume webhook handles conversion correctly)
func validateCRDCompatibility(ctx context.Context, cl client.Client, oldCRD *apiextensionsv1.CustomResourceDefinition, newCRD *apiextensionsv1.CustomResourceDefinition) error {
	oldVersions := map[string]apiextensionsv1.CustomResourceDefinitionVersion{}
	newVersions := map[string]apiextensionsv1.CustomResourceDefinitionVersion{}

	for _, v := range oldCRD.Spec.Versions {
		oldVersions[v.Name] = v
	}
	for _, v := range newCRD.Spec.Versions {
		newVersions[v.Name] = v
	}

	existingStoredVersions := sets.NewString(oldCRD.Status.StoredVersions...)
	removedVersions := keys(oldVersions).Difference(keys(newVersions))
	invalidRemovedVersions := existingStoredVersions.Intersection(removedVersions)
	if invalidRemovedVersions.Len() > 0 {
		return fmt.Errorf("cannot remove stored versions %v", invalidRemovedVersions.List())
	}

	similarVersions := keys(oldVersions).Intersection(keys(newVersions))
	diffVersions := sets.NewString()
	for _, v := range similarVersions.List() {
		if !reflect.DeepEqual(oldVersions[v].Schema, newVersions[v].Schema) {
			diffVersions.Insert(v)
		}
	}
	convertedCRD := &apiextensions.CustomResourceDefinition{}
	if err := apiextensionsv1.Convert_v1_CustomResourceDefinition_To_apiextensions_CustomResourceDefinition(newCRD, convertedCRD, nil); err != nil {
		return err
	}
	for _, v := range diffVersions.List() {
		oldV := oldVersions[v]
		if oldV.Served {
			listGVK := schema.GroupVersionKind{Group: oldCRD.Spec.Group, Version: v, Kind: oldCRD.Spec.Names.ListKind}
			err := validateExistingCRs(ctx, cl, listGVK, newVersions[v])
			if err != nil {
				return err
			}
		}
	}

	// If the new CRD has no conversion configured or a "None" conversion strategy, we need to check to be sure that the
	// new schema validates all of the existing CRs of the existing versions.
	addedVersions := keys(newVersions).Difference(keys(oldVersions))
	if addedVersions.Len() > 0 && (newCRD.Spec.Conversion == nil || newCRD.Spec.Conversion.Strategy == apiextensionsv1.NoneConverter) {
		for _, va := range addedVersions.List() {
			newV := newVersions[va]
			for _, vs := range similarVersions.List() {
				oldV := oldVersions[vs]
				if oldV.Served {
					listGVK := schema.GroupVersionKind{Group: oldCRD.Spec.Group, Version: oldV.Name, Kind: oldCRD.Spec.Names.ListKind}
					err := validateExistingCRs(ctx, cl, listGVK, newV)
					if err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

func validateExistingCRs(ctx context.Context, dynamicClient client.Client, listGVK schema.GroupVersionKind, newVersion apiextensionsv1.CustomResourceDefinitionVersion) error {
	convertedVersion := &apiextensions.CustomResourceDefinitionVersion{}
	if err := apiextensionsv1.Convert_v1_CustomResourceDefinitionVersion_To_apiextensions_CustomResourceDefinitionVersion(&newVersion, convertedVersion, nil); err != nil {
		return err
	}

	crList := &unstructured.UnstructuredList{}
	crList.SetGroupVersionKind(listGVK)
	if err := dynamicClient.List(ctx, crList); err != nil {
		return fmt.Errorf("error listing objects for %s: %v", listGVK, err)
	}
	for _, cr := range crList.Items {
		validator, _, err := validation.NewSchemaValidator(convertedVersion.Schema)
		if err != nil {
			return fmt.Errorf("error creating validator for the schema of version %q: %v", newVersion.Name, err)
		}
		err = validation.ValidateCustomResource(field.NewPath(""), cr.UnstructuredContent(), validator).ToAggregate()
		if err != nil {
			return fmt.Errorf("existing custom object %s/%s failed validation for new schema version %s: %v", cr.GetNamespace(), cr.GetName(), newVersion.Name, err)
		}
	}
	return nil
}

// safeStorageVersionUpgrade determines whether the new CRD spec includes all the storage versions of the existing on-cluster CRD.
// For each stored version in the status of the CRD on the cluster (there will be at least one) - each version must exist in the spec of the new CRD that is being installed.
// See https://kubernetes.io/docs/tasks/access-kubernetes-api/custom-resources/custom-resource-definition-versioning/#upgrade-existing-objects-to-a-new-stored-version.
func safeStorageVersionUpgrade(existingCRD, newCRD *apiextensionsv1.CustomResourceDefinition) (bool, error) {
	existingStatusVersions, newSpecVersions := getStoredVersions(existingCRD, newCRD)
	if newSpecVersions == nil {
		return false, fmt.Errorf("could not find any versions in the new CRD")
	}
	if existingStatusVersions == nil {
		// every on-cluster CRD should have at least one stored version in its status
		// in the case where there are no existing stored versions, checking against new versions is not relevant
		return true, nil
	}

	for name := range existingStatusVersions {
		if _, ok := newSpecVersions[name]; !ok {
			// a storage version in the status of the old CRD is not present in the spec of the new CRD
			// potential data loss of CRs without a storage migration - throw error and block the CRD upgrade
			return false, fmt.Errorf("new CRD removes version %s that is listed as a stored version on the existing CRD", name)
		}
	}

	return true, nil
}

// getStoredVersions returns the storage versions listed in the status of the old on-cluster CRD
// and all the versions listed in the spec of the new CRD.
func getStoredVersions(oldCRD, newCRD *apiextensionsv1.CustomResourceDefinition) (sets.String, sets.String) {
	newSpecVersions := sets.NewString()
	for _, version := range newCRD.Spec.Versions {
		newSpecVersions.Insert(version.Name)
	}

	return sets.NewString(oldCRD.Status.StoredVersions...), newSpecVersions
}
