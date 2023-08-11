package variables

import (
	"github.com/operator-framework/deppy/pkg/deppy"

	"github.com/operator-framework/operator-controller/internal/resolution/v2/store"
)

var _ deppy.Variable = &Bundle{}

type Bundle struct {
	ID deppy.Identifier
	*store.Bundle
}

func (b *Bundle) Identifier() deppy.Identifier {
	return b.ID
}

func (b *Bundle) Constraints() []deppy.Constraint {
	return nil
}

func NewBundle(id deppy.Identifier, bundle *store.Bundle) *Bundle {
	return &Bundle{
		ID:     id,
		Bundle: bundle,
	}
}
