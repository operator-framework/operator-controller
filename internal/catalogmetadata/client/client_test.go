package client_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	catalogClient "github.com/operator-framework/operator-controller/internal/catalogmetadata/client"
	"github.com/operator-framework/operator-controller/pkg/scheme"
)

func TestClient(t *testing.T) {
	t.Run("Bundles", func(t *testing.T) {
		for _, tt := range []struct {
			name        string
			fakeCatalog func() ([]client.Object, []*catalogmetadata.Bundle, map[string][]byte)
			wantErr     string
			fetcher     *MockFetcher
		}{
			{
				name:        "valid catalog",
				fakeCatalog: defaultFakeCatalog,
				fetcher:     &MockFetcher{},
			},
			{
				name:        "cache error",
				fakeCatalog: defaultFakeCatalog,
				fetcher:     &MockFetcher{shouldError: true},
				wantErr:     "error fetching catalog contents: mock cache error",
			},
			{
				name: "channel has a ref to a missing bundle",
				fakeCatalog: func() ([]client.Object, []*catalogmetadata.Bundle, map[string][]byte) {
					objs, _, catalogContentMap := defaultFakeCatalog()

					catalogContentMap["catalog-1"] = append(catalogContentMap["catalog-1"], []byte(`{
								"schema": "olm.channel",
								"name": "channel-with-missing-bundle",
								"package": "fake1",
								"entries": [
									{
										"name": "fake1.v9.9.9"
									}
								]
							}`)...)

					return objs, nil, catalogContentMap
				},
				wantErr: `bundle "fake1.v9.9.9" not found in catalog "catalog-1" (package "fake1", channel "channel-with-missing-bundle")`,
				fetcher: &MockFetcher{},
			},
			{
				name: "invalid meta",
				fakeCatalog: func() ([]client.Object, []*catalogmetadata.Bundle, map[string][]byte) {
					objs, _, catalogContentMap := defaultFakeCatalog()

					catalogContentMap["catalog-1"] = append(catalogContentMap["catalog-1"], []byte(`{"schema": "olm.bundle", "name":123123123}`)...)

					return objs, nil, catalogContentMap
				},
				wantErr: `error processing response: error was provided to the WalkMetasReaderFunc: expected value for key "name" to be a string, got %!t(float64=1.23123123e+08): 1.23123123e+08`,
				fetcher: &MockFetcher{},
			},
			{
				name: "invalid bundle",
				fakeCatalog: func() ([]client.Object, []*catalogmetadata.Bundle, map[string][]byte) {
					objs, _, catalogContentMap := defaultFakeCatalog()

					catalogContentMap["catalog-1"] = append(catalogContentMap["catalog-1"],
						[]byte(`{"schema": "olm.bundle", "name":"foo", "package":"bar", "image":123123123}`)...)

					return objs, nil, catalogContentMap
				},
				wantErr: "error processing response: error unmarshalling bundle from catalog metadata: json: cannot unmarshal number into Go struct field Bundle.image of type string",
				fetcher: &MockFetcher{},
			},
			{
				name: "invalid channel",
				fakeCatalog: func() ([]client.Object, []*catalogmetadata.Bundle, map[string][]byte) {
					objs, _, catalogContentMap := defaultFakeCatalog()

					catalogContentMap["catalog-1"] = append(catalogContentMap["catalog-1"],
						[]byte(`{"schema": "olm.channel", "name":"foo", "package":"bar", "entries":[{"name":123123123}]}`)...)

					return objs, nil, catalogContentMap
				},
				wantErr: "error processing response: error unmarshalling channel from catalog metadata: json: cannot unmarshal number into Go struct field ChannelEntry.entries.name of type string",
				fetcher: &MockFetcher{},
			},
			{
				name: "skip catalog missing Unpacked status condition",
				fakeCatalog: func() ([]client.Object, []*catalogmetadata.Bundle, map[string][]byte) {
					objs, bundles, catalogContentMap := defaultFakeCatalog()
					objs = append(objs, &catalogd.ClusterCatalog{
						ObjectMeta: metav1.ObjectMeta{
							Name: "foobar",
						},
					})
					catalogContentMap["foobar"] = catalogContentMap["catalog-1"]

					return objs, bundles, catalogContentMap
				},
				fetcher: &MockFetcher{},
			},
			{
				name: "deprecated at the package, channel, and bundle level",
				fakeCatalog: func() ([]client.Object, []*catalogmetadata.Bundle, map[string][]byte) {
					objs, bundles, catalogContentMap := defaultFakeCatalog()

					catalogContentMap["catalog-1"] = append(catalogContentMap["catalog-1"],
						[]byte(`{"schema": "olm.deprecations", "package":"fake1", "entries":[{"message": "fake1 is deprecated", "reference": {"schema": "olm.package"}}, {"message":"channel stable is deprecated", "reference": {"schema": "olm.channel", "name": "stable"}}, {"message": "bundle fake1.v1.0.0 is deprecated", "reference":{"schema":"olm.bundle", "name":"fake1.v1.0.0"}}]}`)...)

					for i := range bundles {
						if bundles[i].Package == "fake1" && bundles[i].CatalogName == "catalog-1" && bundles[i].Name == "fake1.v1.0.0" {
							bundles[i].Deprecations = append(bundles[i].Deprecations, declcfg.DeprecationEntry{
								Reference: declcfg.PackageScopedReference{
									Schema: "olm.package",
								},
								Message: "fake1 is deprecated",
							})

							bundles[i].Deprecations = append(bundles[i].Deprecations, declcfg.DeprecationEntry{
								Reference: declcfg.PackageScopedReference{
									Schema: "olm.channel",
									Name:   "stable",
								},
								Message: "channel stable is deprecated",
							})

							bundles[i].Deprecations = append(bundles[i].Deprecations, declcfg.DeprecationEntry{
								Reference: declcfg.PackageScopedReference{
									Schema: "olm.bundle",
									Name:   "fake1.v1.0.0",
								},
								Message: "bundle fake1.v1.0.0 is deprecated",
							})
						}
					}

					return objs, bundles, catalogContentMap
				},
				fetcher: &MockFetcher{},
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				ctx := context.Background()
				objs, expectedBundles, catalogContentMap := tt.fakeCatalog()
				tt.fetcher.contentMap = catalogContentMap

				fakeCatalogClient := catalogClient.New(
					fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(objs...).Build(),
					tt.fetcher,
				)

				bundles, err := fakeCatalogClient.Bundles(ctx)
				if tt.wantErr == "" {
					assert.NoError(t, err)
					assert.Equal(t, expectedBundles, bundles)
				} else {
					assert.EqualError(t, err, tt.wantErr)
				}
			})
		}
	})
}

func defaultFakeCatalog() ([]client.Object, []*catalogmetadata.Bundle, map[string][]byte) {
	package1 := `{
		"schema": "olm.package",
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
		&catalogd.ClusterCatalog{
			ObjectMeta: metav1.ObjectMeta{
				Name: "catalog-1",
			},
			Status: catalogd.ClusterCatalogStatus{
				Conditions: []metav1.Condition{
					{
						Type:   catalogd.TypeUnpacked,
						Status: metav1.ConditionTrue,
						Reason: catalogd.ReasonUnpackSuccessful,
					},
				},
			},
		},
		&catalogd.ClusterCatalog{
			ObjectMeta: metav1.ObjectMeta{
				Name: "catalog-2",
			},
			Status: catalogd.ClusterCatalogStatus{
				Conditions: []metav1.Condition{
					{
						Type:   catalogd.TypeUnpacked,
						Status: metav1.ConditionTrue,
						Reason: catalogd.ReasonUnpackSuccessful,
					},
				},
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
						Name:    "stable",
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
						Name:    "beta",
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

	catalogContents := map[string][]byte{
		"catalog-1": []byte(strings.Join([]string{package1, bundle1, stableChannel, betaChannel}, "\n")),
		"catalog-2": []byte(strings.Join([]string{package1, bundle1, stableChannel}, "\n")),
	}

	return objs, expectedBundles, catalogContents
}

var _ catalogClient.Fetcher = &MockFetcher{}

type MockFetcher struct {
	contentMap  map[string][]byte
	shouldError bool
}

func (mc *MockFetcher) FetchCatalogContents(_ context.Context, catalog *catalogd.ClusterCatalog) (io.ReadCloser, error) {
	if mc.shouldError {
		return nil, errors.New("mock cache error")
	}

	data := mc.contentMap[catalog.Name]
	return io.NopCloser(bytes.NewReader(data)), nil
}
