package parametrize

import (
	"bytes"
	"fmt"

	"github.com/joeycumines/go-dotnotation/dotnotation"
	"k8s.io/apimachinery/pkg/util/uuid"
)

var _ Instruction = (*pipeline)(nil)

// Insert a go template Pipeline at the given location.
func Pipeline(exp string, fieldPath string) Instruction {
	return &pipeline{
		marker:    string(uuid.NewUUID()),
		exp:       exp,
		fieldPath: fieldPath,
	}
}

// Pipeline represents the instruction to insert a go
// template pipeline as a property value.
type pipeline struct {
	marker    string
	exp       string
	fieldPath string
}

func (e *pipeline) Mark(obj map[string]interface{}) error {
	return dotnotation.Set(obj, e.fieldPath, e.marker)
}

func (e *pipeline) Replace(in []byte) ([]byte, error) {
	return bytes.Replace(in, []byte(e.marker),
		[]byte(fmt.Sprintf("{{ %s }}", e.exp)), 1), nil
}

func (e *pipeline) Priority() int {
	return 10
}
