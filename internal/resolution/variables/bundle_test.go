package variables_test

import (
	"encoding/json"
	"testing"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/constraint"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
)

func TestBundleVariable(t *testing.T) {
	bundle := &catalogmetadata.Bundle{
		Bundle: declcfg.Bundle{Name: "bundle-1", Properties: []property.Property{
			{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "1.0.0"}`)},
		}},
	}
	dependencies := []*catalogmetadata.Bundle{
		{
			Bundle: declcfg.Bundle{Name: "bundle-2", Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "2.0.0"}`)},
			}},
		},
		{
			Bundle: declcfg.Bundle{Name: "bundle-3", Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "3.0.0"}`)},
			}},
		},
	}
	bv := olmvariables.NewBundleVariable(bundle, dependencies)

	if bv.Bundle() != bundle {
		t.Errorf("bundle '%v' does not match expected '%v'", bv.Bundle(), bundle)
	}

	for i, d := range bv.Dependencies() {
		if d != dependencies[i] {
			t.Errorf("dependency[%v] '%v' does not match expected '%v'", i, d, dependencies[i])
		}
	}
}

func TestBundleUniquenessVariable(t *testing.T) {
	id := deppy.IdentifierFromString("test-id")
	atMostIDs := []deppy.Identifier{
		deppy.IdentifierFromString("test-at-most-id-1"),
		deppy.IdentifierFromString("test-at-most-id-2"),
	}
	globalConstraintVariable := olmvariables.NewBundleUniquenessVariable(id, atMostIDs...)

	if globalConstraintVariable.Identifier() != id {
		t.Errorf("identifier '%v' does not match expected '%v'", globalConstraintVariable.Identifier(), id)
	}

	constraints := []deppy.Constraint{constraint.AtMost(1, atMostIDs...)}
	for i, c := range globalConstraintVariable.Constraints() {
		if c.String("test") != constraints[i].String("test") {
			t.Errorf("constraint[%v] '%v' does not match expected '%v'", i, c, constraints[i])
		}
	}
}
