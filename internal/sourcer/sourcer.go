package sourcer

import (
	"context"
	"fmt"

	deppyv1alpha1 "github.com/operator-framework/deppy/api/v1alpha1"
)

type Bundle struct {
	Name            string
	PackageName     string
	ChannelName     string
	Version         string
	Image           string
	Replaces        string
	Skips           []string
	SourceName      string
	SourceNamespace string
	Properties      []deppyv1alpha1.Property
}

func (b Bundle) String() string {
	return fmt.Sprintf("Name: %s; Package: %s; Channel: %s; Version: %s; Image: %s; Replaces: %s", b.Name, b.PackageName, b.ChannelName, b.Version, b.Image, b.Replaces)
}

type Sourcer interface {
	Source(context.Context) ([]Bundle, error)
}
