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

func TestInstalledPackageVariable(t *testing.T) {
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
	ipv := olmvariables.NewInstalledPackageVariable(packageName, bundles)

	id := deppy.IdentifierFromString(fmt.Sprintf("installed package %s", packageName))
	if ipv.Identifier() != id {
		t.Errorf("package name '%v' does not match expected '%v'", ipv.Identifier(), id)
	}

	for i, e := range ipv.Bundles() {
		if e != bundles[i] {
			t.Errorf("bundle[%v] '%v' does not match expected '%v'", i, e, bundles[i])
		}
	}
}
