package variables_test

import (
	"fmt"
	"testing"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-registry/alpha/property"

	olmentity "github.com/operator-framework/operator-controller/internal/resolution/entities"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
)

func TestInstalledPackageVariable(t *testing.T) {
	packageName := "test-package"
	bundleEntities := []*olmentity.BundleEntity{
		olmentity.NewBundleEntity(input.NewEntity("bundle-1", map[string]string{
			property.TypePackage: `{"packageName": "test-package", "version": "1.0.0"}`,
			property.TypeChannel: `{"channelName":"stable","priority":0}`,
		})),
		olmentity.NewBundleEntity(input.NewEntity("bundle-2", map[string]string{
			property.TypePackage: `{"packageName": "test-package", "version": "2.0.0"}`,
			property.TypeChannel: `{"channelName":"stable","priority":0}`,
		})),
		olmentity.NewBundleEntity(input.NewEntity("bundle-3", map[string]string{
			property.TypePackage: `{"packageName": "test-package", "version": "3.0.0"}`,
			property.TypeChannel: `{"channelName":"stable","priority":0}`,
		})),
	}
	ipv := olmvariables.NewInstalledPackageVariable(packageName, bundleEntities)

	id := deppy.IdentifierFromString(fmt.Sprintf("installed package %s", packageName))
	if ipv.Identifier() != id {
		t.Errorf("package name '%v' does not match expected '%v'", ipv.Identifier(), id)
	}

	for i, e := range ipv.BundleEntities() {
		if e != bundleEntities[i] {
			t.Errorf("bundle entity[%v] '%v' does not match expected '%v'", i, e, bundleEntities[i])
		}
	}
}
