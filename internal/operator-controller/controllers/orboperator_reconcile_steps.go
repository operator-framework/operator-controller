package controllers

import (
	"context"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

type OrbOperatorRevisionStatesGetter struct{}

func (o *OrbOperatorRevisionStatesGetter) GetRevisionStates(_ context.Context, _ *ocv1.ClusterExtension) (*RevisionStates, error) {
	return &RevisionStates{}, nil
}
