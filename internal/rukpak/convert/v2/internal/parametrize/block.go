package parametrize

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/joeycumines/go-dotnotation/dotnotation"
	"k8s.io/apimachinery/pkg/util/uuid"
	"sigs.k8s.io/yaml"
)

var _ Instruction = (*block)(nil)

// Wrap the given field path into a template block.
func Block(exp string, fieldPath string) Instruction {
	return &block{
		marker:    string(uuid.NewUUID()),
		pipeline:  exp,
		fieldPath: fieldPath,
	}
}

// block represents the instruction to wrap the YAML structure
// at the given fieldPath into a template block.
type block struct {
	marker    string
	pipeline  string
	fieldPath string

	mapKey        string      // map key to merge structures on.
	originalValue interface{} // YAML object found at position.
	writeValue    interface{} // YAML object to write out.
}

func (b *block) Mark(obj map[string]interface{}) error {
	var parentValue interface{}
	lastDotIdx := strings.LastIndex(b.fieldPath, ".")
	if lastDotIdx != -1 {
		var err error
		parentPath := b.fieldPath[:lastDotIdx]
		parentValue, err = dotnotation.Get(obj, parentPath)
		if err != nil {
			return err
		}
	} else {
		parentValue = obj
	}

	origValue, err := dotnotation.Get(obj, b.fieldPath)
	if err != nil {
		return err
	}
	b.originalValue = origValue
	if _, ok := parentValue.([]interface{}); ok {
		// wrap array items in an array again.
		b.mapKey = b.fieldPath
		if _, ok := origValue.([]interface{}); !ok {
			origValue = []interface{}{origValue}
		}
	} else {
		b.mapKey = b.fieldPath
		if lastDotIdx != -1 {
			b.mapKey = b.fieldPath[lastDotIdx+1:]
		}
		// wrap field values in map key.
		origValue = map[string]interface{}{
			b.mapKey: origValue,
		}
	}

	b.writeValue = origValue
	return dotnotation.Set(obj, b.fieldPath, b.marker)
}

func (b *block) Replace(in []byte) ([]byte, error) {
	re := regexp.MustCompile(`(?m).*` + b.marker + `\n*`)
	found := re.Find(in)
	if found == nil {
		// marker not found -> do nothing.
		return nil, nil
	}

	il := indentLevel(found)
	origB, err := yaml.Marshal(b.writeValue)
	if err != nil {
		return nil, err
	}

	i := strings.Repeat(" ", il)
	var replacement string
	replacement += fmt.Sprintf("%s{{- %s }}\n", i, b.pipeline)
	for _, l := range bytes.Split(bytes.TrimSpace(origB), []byte("\n")) {
		replacement += fmt.Sprintf("%s%s\n", i, l)
	}
	replacement += i + "{{- end }}\n"

	return re.ReplaceAll(in, []byte(replacement)), nil
}

func (b *block) Priority() int {
	return 100
}

func indentLevel(b []byte) int {
	var indentLevel int
	for _, c := range b {
		if rune(c) == rune(' ') {
			indentLevel++
		} else {
			break
		}
	}
	return indentLevel
}
