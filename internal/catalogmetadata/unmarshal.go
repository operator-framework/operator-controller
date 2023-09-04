package catalogmetadata

import (
	"encoding/json"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
)

func Unmarshal[T Schemas](cm []catalogd.CatalogMetadata) ([]*T, error) {
	contents := make([]*T, 0, len(cm))
	for _, cm := range cm {
		var content T
		if err := json.Unmarshal(cm.Spec.Content, &content); err != nil {
			return nil, err
		}
		contents = append(contents, &content)
	}

	return contents, nil
}
