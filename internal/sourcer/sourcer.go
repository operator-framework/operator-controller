package sourcer

import (
	"context"

	"k8s.io/apimachinery/pkg/types"

	operatorv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

type Bundle struct {
	Version    string
	Image      string
	Replaces   string
	Skips      []string
	SourceInfo types.NamespacedName
}

type Sourcer interface {
	Source(context.Context, *operatorv1alpha1.Operator) (*Bundle, error)
}
