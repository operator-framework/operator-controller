package variablesources

import (
	"context"
	"fmt"
	"sort"

	bsemver "github.com/blang/semver/v4"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"

	"github.com/operator-framework/operator-controller/internal/resolution/util/predicates"
	variablesort "github.com/operator-framework/operator-controller/internal/resolution/util/sort"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
)

var _ input.VariableSource = &BundlesAndDepsVariableSource{}

type BundlesAndDepsVariableSource struct {
	variableSources []input.VariableSource
}

func NewBundlesAndDepsVariableSource(inputVariableSources ...input.VariableSource) *BundlesAndDepsVariableSource {
	return &BundlesAndDepsVariableSource{
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
	var bundleVariableQueue []*olmvariables.BundleVariable
	for _, variable := range variables {
		switch v := variable.(type) {
		case *olmvariables.RequiredPackageVariable:
			bundleVariableQueue = append(bundleVariableQueue, v.BundleVariables()...)
		case *olmvariables.InstalledPackageVariable:
			bundleVariableQueue = append(bundleVariableQueue, v.BundleVariables()...)
		}
	}

	// build bundle and dependency variables
	visited := map[deppy.Identifier]struct{}{}
	for len(bundleVariableQueue) > 0 {
		// pop head of queue
		var head *olmvariables.BundleVariable
		head, bundleVariableQueue = bundleVariableQueue[0], bundleVariableQueue[1:]

		// ignore bundles that have already been processed
		if _, ok := visited[head.ID]; ok {
			continue
		}
		visited[head.ID] = struct{}{}

		// get bundle dependencies
		dependencyBundleVariables, err := b.getVariableDependencies(ctx, head)
		if err != nil {
			return nil, fmt.Errorf("could not determine dependencies for variable with id '%s': %w", head.ID, err)
		}

		// add bundle dependencies to queue for processing
		bundleVariableQueue = append(bundleVariableQueue, dependencyBundleVariables...)

		// create variable
		variables = append(variables, olmvariables.NewBundleVariable(head, dependencyBundleVariables, head.Properties))
	}

	return variables, nil
}

func (b *BundlesAndDepsVariableSource) getVariableDependencies(ctx context.Context, bundleVariable *olmvariables.BundleVariable) ([]*olmvariables.BundleVariable, error) {
	var dependencies []*olmvariables.BundleVariable
	added := map[deppy.Identifier]struct{}{}

	// gather required package dependencies
	// todo(perdasilva): disambiguate between not found and actual errors
	requiredPackages, _ := bundleVariable.RequiredPackages()
	for _, requiredPackage := range requiredPackages {
		semverRange, err := bsemver.ParseRange(requiredPackage.VersionRange)
		if err != nil {
			return nil, err
		}
		packageDependencyBundles, err := entitySource.Filter(ctx, input.And(predicates.WithPackageName(requiredPackage.PackageName), predicates.InSemverRange(semverRange)))
		if err != nil {
			return nil, err
		}
		if len(packageDependencyBundles) == 0 {
			return nil, fmt.Errorf("could not find package dependencies for bundle '%s'", bundleVariable.ID)
		}
		for i := 0; i < len(packageDependencyBundles); i++ {
			variable := packageDependencyBundles[i]
			if _, ok := added[variable.ID]; !ok {
				dependencies = append(dependencies, olmvariables.NewBundleVariable(&variable, make([]*olmvariables.BundleVariable, 0), variable.Properties))
				added[variable.ID] = struct{}{}
			}
		}
	}

	// gather required gvk dependencies
	// todo(perdasilva): disambiguate between not found and actual errors
	gvkDependencies, _ := bundleVariable.RequiredGVKs()
	for i := 0; i < len(gvkDependencies); i++ {
		providedGvk := gvkDependencies[i].AsGVK()
		gvkDependencyBundles, err := entitySource.Filter(ctx, predicates.ProvidesGVK(&providedGvk))
		if err != nil {
			return nil, err
		}
		if len(gvkDependencyBundles) == 0 {
			return nil, fmt.Errorf("could not find gvk dependencies for bundle '%s'", bundleVariable.ID)
		}
		for i := 0; i < len(gvkDependencyBundles); i++ {
			variable := gvkDependencyBundles[i]
			if _, ok := added[variable.ID]; !ok {
				dependencies = append(dependencies, olmvariables.NewBundleVariable(&variable, make([]*olmvariables.BundleVariable, 0), variable.Properties))
				added[variable.ID] = struct{}{}
			}
		}
	}

	// sort bundles in version order
	sort.SliceStable(dependencies, func(i, j int) bool {
		return variablesort.ByChannelAndVersion(dependencies[i], dependencies[j])
	})

	return dependencies, nil
}
