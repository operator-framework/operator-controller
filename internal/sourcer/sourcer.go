package sourcer

import (
	"context"

	platformtypes "github.com/timflannagan/platform-operators/api/v1alpha1"
)

type Bundle struct {
	Version  string
	Image    string
	Replaces string
	Skips    []string
}

type Sourcer interface {
	Source(context.Context, *platformtypes.Operator) (*Bundle, error)
}
