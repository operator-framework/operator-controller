package resolution

import (
	"context"
	"fmt"

	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/deppy/pkg/deppy/solver"
	"github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/olm"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type OperatorResolver struct {
	entitySource input.EntitySource
	client       client.Client
}

func NewOperatorResolver(client client.Client, entitySource input.EntitySource) *OperatorResolver {
	return &OperatorResolver{
		entitySource: entitySource,
		client:       client,
	}
}

func (o *OperatorResolver) Resolve(ctx context.Context) (solver.Solution, error) {
	packageNames, err := o.getPackageNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get package names for resolution: %s", err)
	}
	olmVariableSource := olm.NewOLMVariableSource(packageNames...)
	deppySolver, err := solver.NewDeppySolver(o.entitySource, olmVariableSource)
	solution, err := deppySolver.Solve(ctx)
	if err != nil {
		return nil, err
	}
	return solution, nil
}

func (o *OperatorResolver) getPackageNames(ctx context.Context) ([]string, error) {
	operatorList := v1alpha1.OperatorList{}
	if err := o.client.List(ctx, &operatorList); err != nil {
		return nil, err
	}
	var packageNames []string
	for _, operator := range operatorList.Items {
		packageNames = append(packageNames, operator.Spec.PackageName)
	}
	return packageNames, nil
}
