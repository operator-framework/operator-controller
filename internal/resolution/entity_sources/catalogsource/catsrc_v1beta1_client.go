package catalogsource

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	catsrcV1 "github.com/operator-framework/catalogd/pkg/apis/core/v1beta1"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-registry/alpha/property"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type v1beta1CatalogSourceConnector struct {
	client client.Client
}

func NewV1beta1CatalogSourceConnector(cl client.Client) *v1beta1CatalogSourceConnector {
	return &v1beta1CatalogSourceConnector{
		client: cl,
	}
}

// id --> metadata.Name
func (c *v1beta1CatalogSourceConnector) Get(ctx context.Context, id deppy.Identifier) *input.Entity {
	bmdname, ch, err := bundleMetadataNameFromID(id)
	if err != nil {
		fmt.Printf("could not get bundlemetadata name from id: %v\n", err)
		return nil
	}

	// TODO: modify the fn signature to return error
	bundleMetaData := &catsrcV1.BundleMetadata{}
	if err := c.client.Get(ctx, types.NamespacedName{Name: bmdname}, bundleMetaData); err != nil {
		fmt.Println("error fetching types %w", err)
	}

	packageData := &catsrcV1.Package{}
	if err := c.client.Get(ctx, types.NamespacedName{Name: bundleMetaData.Spec.Package}, packageData); err != nil {
		fmt.Println("error fetching types %w", err)
	}

	entity, err := convertBMDToEntities(*bundleMetaData, channelData{
		Channels:       []string{ch},
		DefaultChannel: packageData.Spec.DefaultChannel,
	})
	if err != nil {
		fmt.Println("error while converting to entity %w", err)
	}

	return &entity[0]
}

func (c *v1beta1CatalogSourceConnector) Filter(ctx context.Context, filter input.Predicate) (input.EntityList, error) {
	resultSet := input.EntityList{}
	if err := c.Iterate(ctx, func(entity *input.Entity) error {
		if filter(entity) {
			resultSet = append(resultSet, *entity)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return resultSet, nil
}

func (c *v1beta1CatalogSourceConnector) GroupBy(ctx context.Context, fn input.GroupByFunction) (input.EntityListMap, error) {
	resultSet := input.EntityListMap{}
	if err := c.Iterate(ctx, func(entity *input.Entity) error {
		keys := fn(entity)
		for _, key := range keys {
			resultSet[key] = append(resultSet[key], *entity)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return resultSet, nil
}

// 1. Using the client, it should get all the bundleMetadata.
// 2. Convert each bundleMetadata's property into an entity.
// 2.1 Create entity Id -> bundlemetadata.Name
// 3. Call filter on the list of entities and return if there is any error
func (c *v1beta1CatalogSourceConnector) Iterate(ctx context.Context, fn input.IteratorFunction) error {
	catsrcBundleMetadata, err := c.getBundleMetadataList(ctx)
	if err != nil {
		return err
	}

	bundleChannels, err := c.getChannelData(ctx)
	if err != nil {
		return err
	}

	entityList, err := convertBundleMetadataToEntities(catsrcBundleMetadata, bundleChannels)
	if err != nil {
		return err
	}

	for _, e := range entityList {
		if err := fn(&e); err != nil {
			return err
		}
	}
	return nil
}

// getBundleMetadaList fetches all the bundleMetda objects from cluster.
func (c *v1beta1CatalogSourceConnector) getBundleMetadataList(ctx context.Context) ([]catsrcV1.BundleMetadata, error) {
	var bundleMetadataList catsrcV1.BundleMetadataList

	if err := c.client.List(ctx, &bundleMetadataList); err != nil {
		return nil, fmt.Errorf("error fetching the list of bundleMetadata %v", err)
	}

	return bundleMetadataList.Items, nil
}

type channelData struct {
	Channels       []string
	DefaultChannel string
}

// TODO: expose channel priorities in Package API
// get package level properties to add to bundles for resolution
func (c *v1beta1CatalogSourceConnector) getChannelData(ctx context.Context) (map[string]channelData, error) {
	var packageList catsrcV1.PackageList

	if err := c.client.List(ctx, &packageList); err != nil {
		return nil, fmt.Errorf("error fetching the list of bundleMetadata %v", err)
	}

	// map[package.spec.channel.entry]map[property.Type] = {property.Value} for olm.channel, olm.defaultChannel property types
	bundleMetadata := make(map[string]channelData, 0)
	for _, pkg := range packageList.Items {
		for _, ch := range pkg.Spec.Channels {
			for _, b := range ch.Entries {
				var value channelData
				if _, ok := bundleMetadata[b.Name]; !ok {
					bundleMetadata[b.Name] = channelData{}
				}
				value = bundleMetadata[b.Name]
				value.DefaultChannel = pkg.Spec.DefaultChannel
				value.Channels = append(value.Channels, ch.Name)

				bundleMetadata[b.Name] = value
			}
		}
	}

	return bundleMetadata, nil
}

// convertBundleMetadataToEntities converts bundleMetadata into entities.
func convertBundleMetadataToEntities(bundlemetadataEntity []catsrcV1.BundleMetadata, channelMembershipData map[string]channelData) (input.EntityList, error) {
	var inputEntityList []input.Entity

	var errs []error
	for _, bmd := range bundlemetadataEntity {
		entities, err := convertBMDToEntities(bmd, channelMembershipData[bmd.GetName()])
		if err != nil {
			errs = append(errs, err)
			continue
		}

		inputEntityList = append(inputEntityList, entities...)
	}

	return inputEntityList, errors.NewAggregate(errs)
}

func convertBMDToEntities(bd catsrcV1.BundleMetadata, channelMembershipData channelData) ([]input.Entity, error) {
	properties := map[string]string{}
	var errs []error

	// Multivalue properties
	propsList := map[string]map[string]struct{}{}

	// TODO: repetitive code, remove in favor of what is present in internal/resolution/entity_sources/catalogSource
	for _, p := range bd.Spec.Properties {
		switch p.Type {
		case property.TypeBundleObject:
			// ignore - only need metadata for resolution and bundle path for installation
		case property.TypePackage:
			properties[p.Type] = string(p.Value)
		default:
			var v interface{}
			// the keys in the marshaled object may be out of order.
			// recreate the json object so this doesn't happen.
			pValue := p.Value
			err := json.Unmarshal(p.Value, &v)
			if err == nil {
				// don't force property values to be json
				// but if unmarshalled successfully,
				// marshaling again should not fail.
				pValue, err = jsonMarshal(v)
				if err != nil {
					errs = append(errs, err)
					continue
				}
			}
			if _, ok := propsList[p.Type]; !ok {
				propsList[p.Type] = map[string]struct{}{}
			}
			if _, ok := propsList[p.Type][string(pValue)]; !ok {
				propsList[p.Type][string(pValue)] = struct{}{}
			}
		}
	}

	for pType, pValues := range propsList {
		var prop []interface{}
		for pValue := range pValues {
			var v interface{}
			err := json.Unmarshal([]byte(pValue), &v)
			if err == nil {
				// the property value may not be json.
				// if unable to unmarshal, treat property value as a string
				prop = append(prop, v)
			} else {
				prop = append(prop, pValue)
			}
		}
		if len(prop) == 0 {
			continue
		}
		if len(prop) > 1 {
			sort.Slice(prop, func(i, j int) bool {
				// enforce some ordering for deterministic properties. Possibly a neater way to do this.
				return fmt.Sprintf("%v", prop[i]) < fmt.Sprintf("%v", prop[j])
			})
		}
		pValue, err := jsonMarshal(prop)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		properties[pType] = string(pValue)
	}

	properties[typeDefaultChannel] = channelMembershipData.DefaultChannel
	properties[typeBundleSource] = bd.Spec.Image

	if len(errs) > 0 {
		return nil, fmt.Errorf("failed to parse properties for bundle %s in %s: %v", bd.GetName(), bd.Spec.CatalogSource, errors.NewAggregate(errs))
	}

	entites := []input.Entity{}
	for _, ch := range channelMembershipData.Channels {
		pValue, err := jsonMarshal(property.Channel{
			ChannelName: ch,
		})
		if err != nil {
			errs = append(errs, err)
			continue
		}

		properties[property.TypeChannel] = string(pValue)

		entites = append(entites, *input.NewEntity(bundleMetadataID(bd.GetName(), ch), properties))
	}

	return entites, nil
}

func bundleMetadataID(name, channel string) deppy.Identifier {
	return deppy.Identifier(fmt.Sprintf("%s/%s", name, channel))
}

func bundleMetadataNameFromID(id deppy.Identifier) (name, channel string, err error) {
	parts := strings.Split(id.String(), "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid ID format %s", id)
	}
	return parts[0], parts[1], nil
}
