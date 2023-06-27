package entitysources

import (
	"context"
	"encoding/json"
	"fmt"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-registry/alpha/property"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/internal/resolution/entities"
)

// CatalogdEntitySource is a source for(/collection of) deppy defined input.Entity, built from content
// made accessible on-cluster by https://github.com/operator-framework/catalogd.
// It is an implementation of deppy defined input.EntitySource
type CatalogdEntitySource struct {
	client client.Client
}

func NewCatalogdEntitySource(client client.Client) *CatalogdEntitySource {
	return &CatalogdEntitySource{client: client}
}

func (es *CatalogdEntitySource) Get(_ context.Context, _ deppy.Identifier) (*input.Entity, error) {
	panic("not implemented")
}

func (es *CatalogdEntitySource) Filter(ctx context.Context, filter input.Predicate) (input.EntityList, error) {
	resultSet := input.EntityList{}
	entities, err := getEntities(ctx, es.client)
	if err != nil {
		return nil, err
	}
	for i := range entities {
		if filter(&entities[i]) {
			resultSet = append(resultSet, entities[i])
		}
	}
	return resultSet, nil
}

func (es *CatalogdEntitySource) GroupBy(ctx context.Context, fn input.GroupByFunction) (input.EntityListMap, error) {
	entities, err := getEntities(ctx, es.client)
	if err != nil {
		return nil, err
	}
	resultSet := input.EntityListMap{}
	for i := range entities {
		keys := fn(&entities[i])
		for _, key := range keys {
			resultSet[key] = append(resultSet[key], entities[i])
		}
	}
	return resultSet, nil
}

func (es *CatalogdEntitySource) Iterate(ctx context.Context, fn input.IteratorFunction) error {
	entities, err := getEntities(ctx, es.client)
	if err != nil {
		return err
	}
	for i := range entities {
		if err := fn(&entities[i]); err != nil {
			return err
		}
	}
	return nil
}

type replacesProperty struct {
	Replaces string `json:"replaces"`
}

func getEntities(ctx context.Context, client client.Client) (input.EntityList, error) {
	entityList := input.EntityList{}
	bundleMetadatas, packageMetdatas, err := fetchMetadata(ctx, client)
	if err != nil {
		return nil, err
	}
	for _, bundle := range bundleMetadatas.Items {
		props := map[string]string{}

		// TODO: We should make sure all properties are forwarded
		// through and avoid a lossy translation from FBC --> entity
		for _, prop := range bundle.Spec.Properties {
			switch prop.Type {
			case property.TypePackage:
				// this is already a json marshalled object, so it doesn't need to be marshalled
				// like the other ones
				props[property.TypePackage] = string(prop.Value)
			case entities.PropertyBundleMediaType:
				props[entities.PropertyBundleMediaType] = string(prop.Value)
			}
		}

		imgValue, err := json.Marshal(bundle.Spec.Image)
		if err != nil {
			return nil, err
		}
		props[entities.PropertyBundlePath] = string(imgValue)
		catalogScopedPkgName := fmt.Sprintf("%s-%s", bundle.Spec.Catalog.Name, bundle.Spec.Package)
		bundlePkg := packageMetdatas[catalogScopedPkgName]
		for _, ch := range bundlePkg.Spec.Channels {
			for _, b := range ch.Entries {
				catalogScopedEntryName := fmt.Sprintf("%s-%s", bundle.Spec.Catalog.Name, b.Name)
				if catalogScopedEntryName == bundle.Name {
					channelValue, _ := json.Marshal(property.Channel{ChannelName: ch.Name, Priority: 0})
					props[property.TypeChannel] = string(channelValue)
					// TODO(jmprusi): Add the proper PropertyType for this
					replacesValue, _ := json.Marshal(replacesProperty{Replaces: b.Replaces})
					props["olm.replaces"] = string(replacesValue)
					entity := input.Entity{
						ID:         deppy.IdentifierFromString(fmt.Sprintf("%s%s%s", bundle.Name, bundle.Spec.Package, ch.Name)),
						Properties: props,
					}
					entityList = append(entityList, entity)
				}
			}
		}
	}
	return entityList, nil
}

func fetchMetadata(ctx context.Context, client client.Client) (catalogd.BundleMetadataList, map[string]catalogd.Package, error) {
	packageMetdatas := catalogd.PackageList{}
	if err := client.List(ctx, &packageMetdatas); err != nil {
		return catalogd.BundleMetadataList{}, nil, err
	}
	bundleMetadatas := catalogd.BundleMetadataList{}
	if err := client.List(ctx, &bundleMetadatas); err != nil {
		return catalogd.BundleMetadataList{}, nil, err
	}
	packages := map[string]catalogd.Package{}
	for _, pkg := range packageMetdatas.Items {
		packages[pkg.Name] = pkg
	}
	return bundleMetadatas, packages, nil
}
