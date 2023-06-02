package resolution

import (
	"context"

	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/deppy/pkg/deppy/solver"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/olm"
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

func (o *OperatorResolver) Resolve(ctx context.Context) (*solver.Solution, error) {
	operatorList := v1alpha1.OperatorList{}
	if err := o.client.List(ctx, &operatorList); err != nil {
		return nil, err
	}
	if len(operatorList.Items) == 0 {
		return &solver.Solution{}, nil
	}

	olmVariableSource := olm.NewOLMVariableSource(operatorList.Items...)
	deppySolver := solver.NewDeppySolver(o.entitySource, olmVariableSource)

	solution, err := deppySolver.Solve(ctx)
	if err != nil {
		return nil, err
	}
	return solution, nil
}
