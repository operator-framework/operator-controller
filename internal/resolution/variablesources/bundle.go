package variablesources

import (
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/operator-framework/deppy/pkg/deppy"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	catalogfilter "github.com/operator-framework/operator-controller/internal/catalogmetadata/filter"
	catalogsort "github.com/operator-framework/operator-controller/internal/catalogmetadata/sort"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
)

func MakeBundleVariables(
	allBundles []*catalogmetadata.Bundle,
	requiredPackages []*olmvariables.RequiredPackageVariable,
	installedPackages []*olmvariables.InstalledPackageVariable,
) ([]*olmvariables.BundleVariable, error) {
	var bundleQueue []*catalogmetadata.Bundle
	for _, variable := range requiredPackages {
		bundleQueue = append(bundleQueue, variable.Bundles()...)
	}
	for _, variable := range installedPackages {
		bundleQueue = append(bundleQueue, variable.Bundles()...)
	}

	// build bundle and dependency variables
	var result []*olmvariables.BundleVariable
	visited := sets.Set[deppy.Identifier]{}
	for len(bundleQueue) > 0 {
		// pop head of queue
		var head *catalogmetadata.Bundle
		head, bundleQueue = bundleQueue[0], bundleQueue[1:]

		id := olmvariables.BundleVariableID(head)

		// ignore bundles that have already been processed
		if visited.Has(id) {
			continue
		}
		visited.Insert(id)

		// get bundle dependencies
		dependencies, err := filterBundleDependencies(allBundles, head)
		if err != nil {
			return nil, fmt.Errorf("could not determine dependencies for bundle with id '%s': %w", id, err)
		}

		// add bundle dependencies to queue for processing
		bundleQueue = append(bundleQueue, dependencies...)

		// create variable
		result = append(result, olmvariables.NewBundleVariable(head, dependencies))
	}

	return result, nil
}

func filterBundleDependencies(allBundles []*catalogmetadata.Bundle, bundle *catalogmetadata.Bundle) ([]*catalogmetadata.Bundle, error) {
	var dependencies []*catalogmetadata.Bundle
	added := sets.Set[deppy.Identifier]{}

	// gather required package dependencies
	requiredPackages, _ := bundle.RequiredPackages()
	for _, requiredPackage := range requiredPackages {
		packageDependencyBundles := catalogfilter.Filter(allBundles, catalogfilter.And(
			catalogfilter.WithPackageName(requiredPackage.PackageName),
			catalogfilter.InBlangSemverRange(requiredPackage.SemverRange),
		))
		if len(packageDependencyBundles) == 0 {
			return nil, fmt.Errorf("could not find package dependencies for bundle '%s'", bundle.Name)
		}
		for i := 0; i < len(packageDependencyBundles); i++ {
			bundle := packageDependencyBundles[i]
			id := olmvariables.BundleVariableID(bundle)
			if !added.Has(id) {
				dependencies = append(dependencies, bundle)
				added.Insert(id)
			}
		}
	}

	// sort bundles in version order
	sort.SliceStable(dependencies, func(i, j int) bool {
		return catalogsort.ByVersion(dependencies[i], dependencies[j])
	})

	return dependencies, nil
}
