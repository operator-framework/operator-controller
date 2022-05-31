package applier

import (
	"context"

	"github.com/timflannagan/platform-operators/api/v1alpha1"
	"github.com/timflannagan/platform-operators/internal/sourcer"
)

type Applier interface {
	Apply(context.Context, *v1alpha1.PlatformOperator, *sourcer.Bundle) error
}
