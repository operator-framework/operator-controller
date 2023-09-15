package variablesources

import (
	"context"
	"fmt"
	"sort"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	catalogclient "github.com/operator-framework/operator-controller/internal/catalogmetadata/client"
	catalogfilter "github.com/operator-framework/operator-controller/internal/catalogmetadata/filter"
	catalogsort "github.com/operator-framework/operator-controller/internal/catalogmetadata/sort"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
)

var _ input.VariableSource = &BundlesAndDepsVariableSource{}

type BundlesAndDepsVariableSource struct {
	catalog         catalogclient.CatalogClient
	variableSources []input.VariableSource
}

func NewBundlesAndDepsVariableSource(catalog catalogclient.CatalogClient, inputVariableSources ...input.VariableSource) *BundlesAndDepsVariableSource {
	return &BundlesAndDepsVariableSource{
		catalog:         catalog,
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
			bundleQueue = append(bundleQueue, v.BundleEntities()...)
		case *olmvariables.InstalledPackageVariable:
			bundleQueue = append(bundleQueue, v.BundleEntities()...)
		}
	}

	allBundles, err := b.catalog.Bundles(ctx)
	if err != nil {
		return nil, err
	}

	// build bundle and dependency variables
	visited := map[deppy.Identifier]struct{}{}
	for len(bundleQueue) > 0 {
		// pop head of queue
		var head *catalogmetadata.Bundle
		head, bundleQueue = bundleQueue[0], bundleQueue[1:]

		for _, id := range olmvariables.BundleToBundleVariableIDs(head) {
			// ignore bundles that have already been processed
			if _, ok := visited[id]; ok {
				continue
			}
			visited[id] = struct{}{}

			// get bundle dependencies
			dependencies, err := b.filterBundleDependencies(allBundles, head)
			if err != nil {
				return nil, fmt.Errorf("could not determine dependencies for entity with id '%s': %w", id, err)
			}

			// add bundle dependencies to queue for processing
			bundleQueue = append(bundleQueue, dependencies...)

			// create variable
			variables = append(variables, olmvariables.NewBundleVariable(id, head, dependencies))
		}
	}

	return variables, nil
}

func (b *BundlesAndDepsVariableSource) filterBundleDependencies(allBundles []*catalogmetadata.Bundle, bundle *catalogmetadata.Bundle) ([]*catalogmetadata.Bundle, error) {
	var dependencies []*catalogmetadata.Bundle
	added := map[deppy.Identifier]struct{}{}

	// gather required package dependencies
	// todo(perdasilva): disambiguate between not found and actual errors
	requiredPackages, _ := bundle.RequiredPackages()
	for _, requiredPackage := range requiredPackages {
		packageDependencyBundles := catalogfilter.Filter(allBundles, catalogfilter.And(catalogfilter.WithPackageName(requiredPackage.PackageName), catalogfilter.InBlangSemverRange(*requiredPackage.SemverRange)))
		if len(packageDependencyBundles) == 0 {
			return nil, fmt.Errorf("could not find package dependencies for bundle '%s'", bundle.Name)
		}
		for i := 0; i < len(packageDependencyBundles); i++ {
			bundle := packageDependencyBundles[i]
			for _, id := range olmvariables.BundleToBundleVariableIDs(bundle) {
				if _, ok := added[id]; !ok {
					dependencies = append(dependencies, bundle)
					added[id] = struct{}{}
				}
			}
		}
	}

	// sort bundles in version order
	sort.SliceStable(dependencies, func(i, j int) bool {
		return catalogsort.ByVersion(dependencies[i], dependencies[j])
	})

	return dependencies, nil
}
