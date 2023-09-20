package client_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	catalogClient "github.com/operator-framework/operator-controller/internal/catalogmetadata/client"
)

var (
	scheme *runtime.Scheme
)

func init() {
	scheme = runtime.NewScheme()
	utilruntime.Must(catalogd.AddToScheme(scheme))
}

func TestClient(t *testing.T) {
	t.Run("Bundles", func(t *testing.T) {
		for _, tt := range []struct {
			name        string
			fakeCatalog func() ([]client.Object, []*catalogmetadata.Bundle)
			wantErr     string
		}{
			{
				name:        "valid catalog",
				fakeCatalog: defaultFakeCatalog,
			},
			{
				name: "channel has a ref to a missing bundle",
				fakeCatalog: func() ([]client.Object, []*catalogmetadata.Bundle) {
					objs, _ := defaultFakeCatalog()

					objs = append(objs, &catalogd.CatalogMetadata{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "catalog-1-fake1-channel-with-missing-bundle",
							Labels: map[string]string{"schema": declcfg.SchemaChannel, "catalog": "catalog-1"},
						},
						Spec: catalogd.CatalogMetadataSpec{
							Content: json.RawMessage(`{
								"schema": "olm.channel",
								"name": "channel-with-missing-bundle",
								"package": "fake1",
								"entries": [
									{
										"name": "fake1.v9.9.9"
									}
								]
							}`),
						},
					})

					return objs, nil
				},
				wantErr: `bundle "fake1.v9.9.9" not found in catalog "catalog-1" (package "fake1", channel "channel-with-missing-bundle")`,
			},
			{
				name: "invalid bundle",
				fakeCatalog: func() ([]client.Object, []*catalogmetadata.Bundle) {
					objs, _ := defaultFakeCatalog()

					objs = append(objs, &catalogd.CatalogMetadata{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "catalog-1-broken-bundle",
							Labels: map[string]string{"schema": declcfg.SchemaBundle, "catalog": "catalog-1"},
						},
						Spec: catalogd.CatalogMetadataSpec{
							Content: json.RawMessage(`{"name":123123123}`),
						},
					})

					return objs, nil
				},
				wantErr: "error unmarshalling catalog metadata: json: cannot unmarshal number into Go struct field Bundle.name of type string",
			},
			{
				name: "invalid channel",
				fakeCatalog: func() ([]client.Object, []*catalogmetadata.Bundle) {
					objs, _ := defaultFakeCatalog()

					objs = append(objs, &catalogd.CatalogMetadata{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "catalog-1-fake1-broken-channel",
							Labels: map[string]string{"schema": declcfg.SchemaChannel, "catalog": "catalog-1"},
						},
						Spec: catalogd.CatalogMetadataSpec{
							Content: json.RawMessage(`{"name":123123123}`),
						},
					})

					return objs, nil
				},
				wantErr: "error unmarshalling catalog metadata: json: cannot unmarshal number into Go struct field Channel.name of type string",
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				ctx := context.Background()
				objs, expectedBundles := tt.fakeCatalog()

				fakeCatalogClient := catalogClient.New(
					fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build(),
				)

				bundles, err := fakeCatalogClient.Bundles(ctx)
				if tt.wantErr == "" {
					assert.NoError(t, err)
				} else {
					assert.EqualError(t, err, tt.wantErr)
				}
				assert.Equal(t, expectedBundles, bundles)
			})
		}
	})
}

func defaultFakeCatalog() ([]client.Object, []*catalogmetadata.Bundle) {
	package1 := `{
		"schema": "olm.bundle",
		"name": "fake1"
	}`

	bundle1 := `{
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

	stableChannel := `{
		"schema": "olm.channel",
		"name": "stable",
		"package": "fake1",
		"entries": [
			{
				"name": "fake1.v1.0.0"
			}
		]
	}`

	betaChannel := `{
		"schema": "olm.channel",
		"name": "beta",
		"package": "fake1",
		"entries": [
			{
				"name": "fake1.v1.0.0"
			}
		]
	}`

	objs := []client.Object{
		&catalogd.Catalog{
			ObjectMeta: metav1.ObjectMeta{
				Name: "catalog-1",
			},
		},
		&catalogd.Catalog{
			ObjectMeta: metav1.ObjectMeta{
				Name: "catalog-2",
			},
		},
		&catalogd.CatalogMetadata{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "catalog-1-fake1",
				Labels: map[string]string{"schema": declcfg.SchemaPackage, "catalog": "catalog-1"},
			},
			Spec: catalogd.CatalogMetadataSpec{
				Content: json.RawMessage(package1),
			},
		},
		&catalogd.CatalogMetadata{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "catalog-1-fake1-channel-stable",
				Labels: map[string]string{"schema": declcfg.SchemaChannel, "catalog": "catalog-1"},
			},
			Spec: catalogd.CatalogMetadataSpec{
				Content: json.RawMessage(stableChannel),
			},
		},
		&catalogd.CatalogMetadata{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "catalog-1-fake1-channel-beta",
				Labels: map[string]string{"schema": declcfg.SchemaChannel, "catalog": "catalog-1"},
			},
			Spec: catalogd.CatalogMetadataSpec{
				Content: json.RawMessage(betaChannel),
			},
		},
		&catalogd.CatalogMetadata{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "catalog-1-fake1-bundle-1",
				Labels: map[string]string{"schema": declcfg.SchemaBundle, "catalog": "catalog-1"},
			},
			Spec: catalogd.CatalogMetadataSpec{
				Content: json.RawMessage(bundle1),
			},
		},
		&catalogd.CatalogMetadata{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "catalog-2-fake1",
				Labels: map[string]string{"schema": declcfg.SchemaPackage, "catalog": "catalog-2"},
			},
			Spec: catalogd.CatalogMetadataSpec{
				Content: json.RawMessage(package1),
			},
		},
		&catalogd.CatalogMetadata{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "catalog-2-fake1-channel-stable",
				Labels: map[string]string{"schema": declcfg.SchemaChannel, "catalog": "catalog-2"},
			},
			Spec: catalogd.CatalogMetadataSpec{
				Content: json.RawMessage(stableChannel),
			},
		},
		&catalogd.CatalogMetadata{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "catalog-2-fake1-bundle-1",
				Labels: map[string]string{"schema": declcfg.SchemaBundle, "catalog": "catalog-2"},
			},
			Spec: catalogd.CatalogMetadataSpec{
				Content: json.RawMessage(bundle1),
			},
		},
	}

	expectedBundles := []*catalogmetadata.Bundle{
		{
			CatalogName: "catalog-1",
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
			InChannels: []*catalogmetadata.Channel{
				{
					Channel: declcfg.Channel{
						Schema:  declcfg.SchemaChannel,
						Name:    "beta",
						Package: "fake1",
						Entries: []declcfg.ChannelEntry{
							{
								Name: "fake1.v1.0.0",
							},
						},
					},
				},
				{
					Channel: declcfg.Channel{
						Schema:  declcfg.SchemaChannel,
						Name:    "stable",
						Package: "fake1",
						Entries: []declcfg.ChannelEntry{
							{
								Name: "fake1.v1.0.0",
							},
						},
					},
				},
			},
		},
		{
			CatalogName: "catalog-2",
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
			InChannels: []*catalogmetadata.Channel{
				{
					Channel: declcfg.Channel{
						Schema:  declcfg.SchemaChannel,
						Name:    "stable",
						Package: "fake1",
						Entries: []declcfg.ChannelEntry{
							{
								Name: "fake1.v1.0.0",
							},
						},
					},
				},
			},
		},
	}

	return objs, expectedBundles
}
