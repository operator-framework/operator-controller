package variables

import (
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
)

var _ deppy.Variable = &CatalogdVariable{}

type CatalogdVariable struct {
	*input.SimpleVariable
	dependencies []*BundleVariable
}
