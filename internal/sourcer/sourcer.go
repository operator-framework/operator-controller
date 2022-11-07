package sourcer

import (
	"context"

	"k8s.io/apimachinery/pkg/types"

	platformtypes "github.com/operator-framework/operator-controller/api/v1alpha1"
)

type Bundle struct {
	Version    string
	Image      string
	Replaces   string
	Skips      []string
	SourceInfo types.NamespacedName
}

type Sourcer interface {
	Source(context.Context, *platformtypes.Operator) (*Bundle, error)
}
