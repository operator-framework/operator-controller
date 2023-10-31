package variablesources

import (
	"context"
	"fmt"
	"sort"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	catalogfilter "github.com/operator-framework/operator-controller/internal/catalogmetadata/filter"
	catalogsort "github.com/operator-framework/operator-controller/internal/catalogmetadata/sort"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
)

var _ input.VariableSource = &BundlesAndDepsVariableSource{}

type BundlesAndDepsVariableSource struct {
	allBundles      []*catalogmetadata.Bundle
	variableSources []input.VariableSource
}

func NewBundlesAndDepsVariableSource(allBundles []*catalogmetadata.Bundle, inputVariableSources ...input.VariableSource) *BundlesAndDepsVariableSource {
	return &BundlesAndDepsVariableSource{
		allBundles:      allBundles,
		variableSources: inputVariableSources,
	}
}

func (b *BundlesAndDepsVariableSource) GetVariables(ctx context.Context) ([]deppy.Variable, error) {
	variables := []deppy.Variable{}

	for _, variableSource := range b.variableSources {
		inputVariables, err := variableSource.GetVariables(ctx)
		if err != nil {
			return nil, err
		}
		variables = append(variables, inputVariables...)
	}

	requiredPackages := []*olmvariables.RequiredPackageVariable{}
	installedPackages := []*olmvariables.InstalledPackageVariable{}
	for _, variable := range variables {
		switch v := variable.(type) {
		case *olmvariables.RequiredPackageVariable:
			requiredPackages = append(requiredPackages, v)
		case *olmvariables.InstalledPackageVariable:
			installedPackages = append(installedPackages, v)
		}
	}

	bundles, err := MakeBundleVariables(b.allBundles, requiredPackages, installedPackages)
	if err != nil {
		return nil, err
	}

	for _, v := range bundles {
		variables = append(variables, v)
	}
	return variables, nil
}

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
			return nil, fmt.Errorf("could not determine dependencies for bundle with id %q: %w", id, err)
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
			return nil, fmt.Errorf("could not find package dependencies for bundle %q", bundle.Name)
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
