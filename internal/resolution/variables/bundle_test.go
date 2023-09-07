package variables_test

import (
	"testing"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/constraint"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-registry/alpha/property"

	olmentity "github.com/operator-framework/operator-controller/internal/resolution/entities"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
)

func TestBundleVariable(t *testing.T) {
	bundleEntity := olmentity.NewBundleEntity(input.NewEntity("bundle-1", map[string]string{
		property.TypePackage: `{"packageName": "test-package", "version": "1.0.0"}`,
		property.TypeChannel: `{"channelName":"stable","priority":0}`,
	}))
	dependencies := []*olmentity.BundleEntity{
		olmentity.NewBundleEntity(input.NewEntity("bundle-2", map[string]string{
			property.TypePackage: `{"packageName": "test-package-2", "version": "2.0.0"}`,
			property.TypeChannel: `{"channelName":"stable","priority":0}`,
		})),
		olmentity.NewBundleEntity(input.NewEntity("bundle-3", map[string]string{
			property.TypePackage: `{"packageName": "test-package-3", "version": "2.0.0"}`,
			property.TypeChannel: `{"channelName":"stable","priority":0}`,
		})),
	}
	bv := olmvariables.NewBundleVariable(bundleEntity, dependencies)

	if bv.BundleEntity() != bundleEntity {
		t.Errorf("bundle entity '%v' does not match expected '%v'", bv.BundleEntity(), bundleEntity)
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
