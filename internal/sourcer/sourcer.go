package sourcer

import (
	"context"

	platformv1alpha1 "github.com/openshift/api/platform/v1alpha1"
)

type Bundle struct {
	Version  string
	Image    string
	Replaces string
	Skips    []string
}

type Sourcer interface {
	Source(context.Context, *platformv1alpha1.PlatformOperator) (*Bundle, error)
}
