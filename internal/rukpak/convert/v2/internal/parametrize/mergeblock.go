package parametrize

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/util/uuid"
	"sigs.k8s.io/yaml"
)

var _ Instruction = (*mergeBlock)(nil)

// Wrap the given field path into a template block merging data structures.
func MergeBlock(pipeline string, fieldPath string) Instruction {
	return &mergeBlock{
		block: block{
			marker:    string(uuid.NewUUID()),
			pipeline:  pipeline,
			fieldPath: fieldPath,
		},
	}
}

// mergeBlock represents the instruction to wrap the YAML structure
// at the given fieldPath into a template block merging data structures.
type mergeBlock struct {
	block
}

func (b *mergeBlock) Replace(in []byte) ([]byte, error) {
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
	var replacement bytes.Buffer
	if _, err = fmt.Fprintf(&replacement, "%s{{- define %q }}\n", i, b.marker); err != nil {
		return nil, err
	}
	for _, l := range bytes.Split(bytes.TrimSpace(origB), []byte("\n")) {
		if _, err = fmt.Fprintf(&replacement, "%s%s\n", i, l); err != nil {
			return nil, err
		}
	}
	if _, err = fmt.Fprintf(&replacement, `%s{{- end }}{{"\n"}}`+"\n", i); err != nil {
		return nil, err
	}
	if _, isSlice := b.originalValue.([]interface{}); isSlice {
		// assume data is a slice.
		if _, err = fmt.Fprintf(&replacement,
			`%s{{- dict %q (concat (fromYaml (include %q .)).%s (%s)) | toYaml | indent %d }}`+"\n",
			i, b.mapKey, b.marker, b.mapKey, b.pipeline, len(i),
		); err != nil {
			return nil, err
		}
		return re.ReplaceAll(in, replacement.Bytes()), nil
	}
	// assume data is a map.
	if _, err = fmt.Fprintf(&replacement,
		`%s{{- mergeOverwrite (fromYaml (include %q .)) (%s)  | toYaml | indent %d }}`+"\n",
		i, b.marker, b.pipeline, len(i),
	); err != nil {
		return nil, err
	}
	return re.ReplaceAll(in, replacement.Bytes()), nil
}
