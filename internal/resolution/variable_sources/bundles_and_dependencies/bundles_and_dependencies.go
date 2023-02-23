package bundles_and_dependencies

import (
	"context"
	"fmt"
	"sort"

	"github.com/blang/semver/v4"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/constraint"
	"github.com/operator-framework/deppy/pkg/deppy/input"

	olmentity "github.com/operator-framework/operator-controller/internal/resolution/variable_sources/entity"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/required_package"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/util/predicates"
	entitysort "github.com/operator-framework/operator-controller/internal/resolution/variable_sources/util/sort"
)

type BundleVariable struct {
	*input.SimpleVariable
	bundleEntity *olmentity.BundleEntity
	dependencies []*olmentity.BundleEntity
}

func (b *BundleVariable) BundleEntity() *olmentity.BundleEntity {
	return b.bundleEntity
}

func (b *BundleVariable) Dependencies() []*olmentity.BundleEntity {
	return b.dependencies
}

func NewBundleVariable(bundleEntity *olmentity.BundleEntity, dependencyBundleEntities []*olmentity.BundleEntity) *BundleVariable {
	var dependencyIDs []deppy.Identifier
	for _, bundle := range dependencyBundleEntities {
		dependencyIDs = append(dependencyIDs, bundle.ID)
	}
	var constraints []deppy.Constraint
	if len(dependencyIDs) > 0 {
		constraints = append(constraints, constraint.Dependency(dependencyIDs...))
	}
	return &BundleVariable{
		SimpleVariable: input.NewSimpleVariable(bundleEntity.ID, constraints...),
		bundleEntity:   bundleEntity,
		dependencies:   dependencyBundleEntities,
	}
}

var _ input.VariableSource = &BundlesAndDepsVariableSource{}

type BundlesAndDepsVariableSource struct {
	variableSources []input.VariableSource
}

func NewBundlesAndDepsVariableSource(inputVariableSources ...input.VariableSource) *BundlesAndDepsVariableSource {
	return &BundlesAndDepsVariableSource{
		variableSources: inputVariableSources,
	}
}

func (b *BundlesAndDepsVariableSource) GetVariables(ctx context.Context, entitySource input.EntitySource) ([]deppy.Variable, error) {
	var variables []deppy.Variable

	// extract required package variables
	for _, variableSource := range b.variableSources {
		inputVariables, err := variableSource.GetVariables(ctx, entitySource)
		if err != nil {
			return nil, err
		}
		variables = append(variables, inputVariables...)
	}

	// create bundle queue for dependency resolution
	var bundleEntityQueue []*olmentity.BundleEntity
	for _, variable := range variables {
		switch v := variable.(type) {
		case *required_package.RequiredPackageVariable:
			bundleEntityQueue = append(bundleEntityQueue, v.BundleEntities()...)
		}
	}

	// build bundle and dependency variables
	visited := map[deppy.Identifier]struct{}{}
	for len(bundleEntityQueue) > 0 {
		// pop head of queue
		var head *olmentity.BundleEntity
		head, bundleEntityQueue = bundleEntityQueue[0], bundleEntityQueue[1:]

		// ignore bundles that have already been processed
		if _, ok := visited[head.ID]; ok {
			continue
		}
		visited[head.ID] = struct{}{}

		// get bundle dependencies
		dependencyEntityBundles, err := b.getEntityDependencies(ctx, head, entitySource)
		if err != nil {
			return nil, fmt.Errorf("could not determine dependencies for entity with id '%s': %w", head.ID, err)
		}

		// add bundle dependencies to queue for processing
		bundleEntityQueue = append(bundleEntityQueue, dependencyEntityBundles...)

		// create variable
		variables = append(variables, NewBundleVariable(head, dependencyEntityBundles))
	}

	return variables, nil
}

func (b *BundlesAndDepsVariableSource) getEntityDependencies(ctx context.Context, bundleEntity *olmentity.BundleEntity, entitySource input.EntitySource) ([]*olmentity.BundleEntity, error) {
	var dependencies []*olmentity.BundleEntity
	added := map[deppy.Identifier]struct{}{}

	// gather required package dependencies
	// todo(perdasilva): disambiguate between not found and actual errors
	requiredPackages, _ := bundleEntity.RequiredPackages()
	for _, requiredPackage := range requiredPackages {
		semverRange, err := semver.ParseRange(requiredPackage.VersionRange)
		if err != nil {
			return nil, err
		}
		packageDependencyBundles, err := entitySource.Filter(ctx, input.And(predicates.WithPackageName(requiredPackage.PackageName), predicates.InSemverRange(semverRange)))
		if err != nil {
			return nil, err
		}
		if len(packageDependencyBundles) == 0 {
			return nil, fmt.Errorf("could not find package dependencies for bundle '%s'", bundleEntity.ID)
		}
		for i := 0; i < len(packageDependencyBundles); i++ {
			entity := packageDependencyBundles[i]
			if _, ok := added[entity.ID]; !ok {
				dependencies = append(dependencies, olmentity.NewBundleEntity(&entity))
				added[entity.ID] = struct{}{}
			}
		}
	}

	// gather required gvk dependencies
	// todo(perdasilva): disambiguate between not found and actual errors
	gvkDependencies, _ := bundleEntity.RequiredGVKs()
	for i := 0; i < len(gvkDependencies); i++ {
		providedGvk := gvkDependencies[i].AsGVK()
		gvkDependencyBundles, err := entitySource.Filter(ctx, predicates.ProvidesGVK(&providedGvk))
		if err != nil {
			return nil, err
		}
		if len(gvkDependencyBundles) == 0 {
			return nil, fmt.Errorf("could not find gvk dependencies for bundle '%s'", bundleEntity.ID)
		}
		for i := 0; i < len(gvkDependencyBundles); i++ {
			entity := gvkDependencyBundles[i]
			if _, ok := added[entity.ID]; !ok {
				dependencies = append(dependencies, olmentity.NewBundleEntity(&entity))
				added[entity.ID] = struct{}{}
			}
		}
	}

	// sort bundles in version order
	sort.SliceStable(dependencies, func(i, j int) bool {
		return entitysort.ByChannelAndVersion(dependencies[i].Entity, dependencies[j].Entity)
	})

	return dependencies, nil
}
