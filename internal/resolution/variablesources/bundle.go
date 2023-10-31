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
	var variables []deppy.Variable

	// extract required package variables
	for _, variableSource := range b.variableSources {
		inputVariables, err := variableSource.GetVariables(ctx)
		if err != nil {
			return nil, err
		}
		variables = append(variables, inputVariables...)
	}

	// create bundle queue for dependency resolution
	var bundleQueue []*catalogmetadata.Bundle
	for _, variable := range variables {
		switch v := variable.(type) {
		case *olmvariables.RequiredPackageVariable:
			bundleQueue = append(bundleQueue, v.Bundles()...)
		case *olmvariables.InstalledPackageVariable:
			bundleQueue = append(bundleQueue, v.Bundles()...)
		}
	}

	// build bundle and dependency variables
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
		dependencies, err := b.filterBundleDependencies(b.allBundles, head)
		if err != nil {
			return nil, fmt.Errorf("could not determine dependencies for bundle with id '%s': %w", id, err)
		}

		// add bundle dependencies to queue for processing
		bundleQueue = append(bundleQueue, dependencies...)

		// create variable
		variables = append(variables, olmvariables.NewBundleVariable(head, dependencies))
	}

	return variables, nil
}

func (b *BundlesAndDepsVariableSource) filterBundleDependencies(allBundles []*catalogmetadata.Bundle, bundle *catalogmetadata.Bundle) ([]*catalogmetadata.Bundle, error) {
	var dependencies []*catalogmetadata.Bundle
	added := sets.Set[deppy.Identifier]{}

	// gather required package dependencies
	// todo(perdasilva): disambiguate between not found and actual errors
	requiredPackages, _ := bundle.RequiredPackages()
	for _, requiredPackage := range requiredPackages {
		packageDependencyBundles := catalogfilter.Filter(allBundles, catalogfilter.And(catalogfilter.WithPackageName(requiredPackage.PackageName), catalogfilter.InBlangSemverRange(requiredPackage.SemverRange)))
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
