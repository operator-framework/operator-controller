package parametrize

import (
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// Instructions get executed to convert an object into a YAML template.
type Instruction interface {
	Mark(obj map[string]interface{}) error
	Replace(in []byte) ([]byte, error)
	Priority() int // instructions with lower priority get executed first.
}

// Run instructions to procuce a YAML template of the input object.
func Execute(obj unstructured.Unstructured, inst ...Instruction) ([]byte, error) {
	obj = *obj.DeepCopy()

	instByPrio := map[int][]Instruction{}
	priorities := map[int]struct{}{}

	for _, i := range inst {
		p := i.Priority()
		instByPrio[p] = append(instByPrio[p], i)
		priorities[p] = struct{}{}
	}

	instPrio := make([]int, len(priorities))
	var i int
	for p := range priorities {
		instPrio[i] = p
		i++
	}

	// Mark in priority order.
	slices.Sort(instPrio)
	for _, prio := range instPrio {
		for _, i := range instByPrio[prio] {
			if err := i.Mark(obj.Object); err != nil {
				return nil, fmt.Errorf("marking: %w", err)
			}
		}
	}

	b, err := yaml.Marshal(obj.Object)
	if err != nil {
		return nil, err
	}

	// Replace markers in reverse priority order.
	slices.Reverse(instPrio)
	for _, prio := range instPrio {
		for _, i := range instByPrio[prio] {
			var err error
			if b, err = i.Replace(b); err != nil {
				return nil, fmt.Errorf("replacing: %w", err)
			}
		}
	}
	return b, nil
}
