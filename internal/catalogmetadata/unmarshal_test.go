package catalogmetadata_test

import (
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"
	"github.com/stretchr/testify/assert"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
)

var (
	scheme *runtime.Scheme
)

func init() {
	scheme = runtime.NewScheme()
	utilruntime.Must(catalogd.AddToScheme(scheme))
}

func TestFetchByScheme(t *testing.T) {
	fakeCatalogName := "fake-catalog"

	validBundle := `{
		"schema": "olm.bundle",
		"name": "fake1.v1.0.0",
		"package": "fake1",
		"image": "fake-image",
		"properties": [
			{
				"type": "olm.package",
				"value": {"packageName":"fake1","version":"1.0.0"}
			}
		]
	}`

	for _, tt := range []struct {
		name     string
		objs     []catalogd.CatalogMetadata
		wantData []*catalogmetadata.Bundle
		wantErr  string
	}{
		{
			name: "valid objects",
			objs: []catalogd.CatalogMetadata{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "obj-1",
						Labels: map[string]string{"schema": declcfg.SchemaBundle, "catalog": fakeCatalogName},
					},
					Spec: catalogd.CatalogMetadataSpec{
						Content: json.RawMessage(validBundle),
					},
				},
			},
			wantData: []*catalogmetadata.Bundle{
				{
					Bundle: declcfg.Bundle{
						Schema:  declcfg.SchemaBundle,
						Name:    "fake1.v1.0.0",
						Package: "fake1",
						Image:   "fake-image",
						Properties: []property.Property{
							{
								Type:  property.TypePackage,
								Value: json.RawMessage(`{"packageName":"fake1","version":"1.0.0"}`),
							},
						},
					},
				},
			},
		},
		{
			name: "invalid objects",
			objs: []catalogd.CatalogMetadata{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "obj-1",
						Labels: map[string]string{"schema": declcfg.SchemaBundle, "catalog": fakeCatalogName},
					},
					Spec: catalogd.CatalogMetadataSpec{
						Content: json.RawMessage(`{"name":123123123}`),
					},
				},
			},
			wantErr: "json: cannot unmarshal number into Go struct field Bundle.name of type string",
		},
		{
			name:     "not found",
			wantData: []*catalogmetadata.Bundle{},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			data, err := catalogmetadata.Unmarshal[catalogmetadata.Bundle](tt.objs)
			assert.Equal(t, tt.wantData, data)
			if tt.wantErr != "" {
				assert.EqualError(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
