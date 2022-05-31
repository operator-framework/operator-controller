package sourcer

import (
	"context"
	"fmt"

	platformv1alpha1 "github.com/timflannagan/platform-operators/api/v1alpha1"
)

type Bundle struct {
	Version  string
	Image    string
	Replaces string
	Skips    []string
}

func (b Bundle) String() string {
	return fmt.Sprintf("Version: %s; Image: %s; Replaces %s", b.Version, b.Image, b.Replaces)
}

type Sourcer interface {
	Source(context.Context, *platformv1alpha1.PlatformOperator) (*Bundle, error)
}
