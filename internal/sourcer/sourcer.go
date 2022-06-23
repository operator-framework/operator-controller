package sourcer

import (
	"context"
	"fmt"
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
	Source(context.Context) ([]Bundle, error)
}
