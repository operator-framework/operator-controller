package variables_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
)

func TestRequiredPackageVariable(t *testing.T) {
	packageName := "test-package"
	bundles := []*catalogmetadata.Bundle{
		{Bundle: declcfg.Bundle{Name: "bundle-1", Properties: []property.Property{
			{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "1.0.0"}`)},
		}}},
		{Bundle: declcfg.Bundle{Name: "bundle-2", Properties: []property.Property{
			{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "2.0.0"}`)},
		}}},
		{Bundle: declcfg.Bundle{Name: "bundle-3", Properties: []property.Property{
			{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "3.0.0"}`)},
		}}},
	}
	rpv := olmvariables.NewRequiredPackageVariable(packageName, bundles)

	id := deppy.IdentifierFromString(fmt.Sprintf("required package %s", packageName))
	if rpv.Identifier() != id {
		t.Errorf("package name '%v' does not match expected '%v'", rpv.Identifier(), id)
	}

	for i, e := range rpv.Bundles() {
		if e != bundles[i] {
			t.Errorf("bundle entity[%v] '%v' does not match expected '%v'", i, e, bundles[i])
		}
	}

	// TODO: add this test once https://github.com/operator-framework/deppy/pull/85 gets merged
	//       then we'll be able to inspect constraint types
	//       "should contain both mandatory and dependency constraints"
}
