package variablesources

import (
	"context"
	"encoding/json"
	"fmt"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"
	"sigs.k8s.io/controller-runtime/pkg/client"

	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
)

// TODO update comment
// CatalogdVariableSource is a source for(/collection of) deppy defined input.Entity, built from content
// made accessible on-cluster by https://github.com/operator-framework/catalogd.
// It is an implementation of deppy defined input.EntitySource
type CatalogdVariableSource struct {
	client client.Client
}

func NewCatalogdVariableSource(client client.Client) *CatalogdVariableSource {
	return &CatalogdVariableSource{client: client}
}

func (vs *CatalogdVariableSource) GetVariables(ctx context.Context) ([]deppy.Variable, error) {
	var variables []deppy.Variable
	var catalogList catalogd.CatalogList
	if err := vs.client.List(ctx, &catalogList); err != nil {
		return nil, err
	}
	for _, catalog := range catalogList.Items {
		channels, bundles, err := fetchCatalogMetadata(ctx, vs.client, catalog.Name)
		if err != nil {
			return nil, err
		}

		catalogVariablesList, err := MetadataToVariables(catalog.Name, channels, bundles)
		if err != nil {
			return nil, err
		}

		variables = append(variables, catalogVariablesList...)
	}

	return variables, nil
}

func (es *CatalogdVariableSource) Filter(ctx context.Context, filter input.Predicate) ([]deppy.Variable, error) {
	resultSet := make([]deppy.Variable, 0)
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

func (es *CatalogdVariableSource) GroupBy(ctx context.Context, fn input.GroupByFunction) (map[string][]deppy.Variable, error) {
	entities, err := getEntities(ctx, es.client)
	if err != nil {
		return nil, err
	}
	resultSet := map[string][]deppy.Variable{}
	for i := range entities {
		keys := fn(&entities[i])
		for _, key := range keys {
			resultSet[key] = append(resultSet[key], entities[i])
		}
	}
	return resultSet, nil
}

func (es *CatalogdVariableSource) Iterate(ctx context.Context, fn input.IteratorFunction) error {
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

func getEntities(ctx context.Context, cl client.Client) ([]deppy.Variable, error) {
	allEntitiesList := make([]deppy.Variable, 0)

	var catalogList catalogd.CatalogList
	if err := cl.List(ctx, &catalogList); err != nil {
		return nil, err
	}
	for _, catalog := range catalogList.Items {
		channels, bundles, err := fetchCatalogMetadata(ctx, cl, catalog.Name)
		if err != nil {
			return nil, err
		}

		catalogEntitiesList, err := MetadataToVariables(catalog.Name, channels, bundles)
		if err != nil {
			return nil, err
		}

		allEntitiesList = append(allEntitiesList, catalogEntitiesList...)
	}

	return allEntitiesList, nil
}

func MetadataToVariables(catalogName string, channels []declcfg.Channel, bundles []declcfg.Bundle) ([]deppy.Variable, error) {
	var variables []deppy.Variable

	bundlesMap := map[string]*declcfg.Bundle{}
	for i := range bundles {
		bundleKey := fmt.Sprintf("%s-%s", bundles[i].Package, bundles[i].Name)
		bundlesMap[bundleKey] = &bundles[i]
	}

	for _, ch := range channels {
		for _, chEntry := range ch.Entries {
			bundleKey := fmt.Sprintf("%s-%s", ch.Package, chEntry.Name)
			bundle, ok := bundlesMap[bundleKey]
			if !ok {
				return nil, fmt.Errorf("bundle %q not found in catalog %q (package %q, channel %q)", chEntry.Name, catalogName, ch.Package, ch.Name)
			}

			props := map[string]string{}

			for _, prop := range bundle.Properties {
				switch prop.Type {
				case property.TypePackage:
					// this is already a json marshalled object, so it doesn't need to be marshalled
					// like the other ones
					props[property.TypePackage] = string(prop.Value)
				case olmvariables.PropertyBundleMediaType:
					props[olmvariables.PropertyBundleMediaType] = string(prop.Value)
				}
			}

			imgValue, err := json.Marshal(bundle.Image)
			if err != nil {
				return nil, err
			}
			props[olmvariables.PropertyBundlePath] = string(imgValue)

			channelValue, _ := json.Marshal(property.Channel{ChannelName: ch.Name, Priority: 0})
			props[property.TypeChannel] = string(channelValue)
			replacesValue, _ := json.Marshal(olmvariables.ChannelEntry{
				Name:     bundle.Name,
				Replaces: chEntry.Replaces,
			})
			props[olmvariables.PropertyBundleChannelEntry] = string(replacesValue)

			catalogScopedEntryName := fmt.Sprintf("%s-%s", catalogName, bundle.Name)
			variable := olmvariables.NewBundleVariable(
				input.NewSimpleVariable(deppy.IdentifierFromString(fmt.Sprintf("%s%s%s", catalogScopedEntryName, bundle.Package, ch.Name))),
				make([]*olmvariables.BundleVariable, 0),
				props)
			variables = append(variables, variable)
		}
	}

	return variables, nil
}

func fetchCatalogMetadata(ctx context.Context, cl client.Client, catalogName string) ([]declcfg.Channel, []declcfg.Bundle, error) {
	channels, err := fetchCatalogMetadataByScheme[declcfg.Channel](ctx, cl, declcfg.SchemaChannel, catalogName)
	if err != nil {
		return nil, nil, err
	}
	bundles, err := fetchCatalogMetadataByScheme[declcfg.Bundle](ctx, cl, declcfg.SchemaBundle, catalogName)
	if err != nil {
		return nil, nil, err
	}

	return channels, bundles, nil
}

type declcfgSchema interface {
	declcfg.Package | declcfg.Bundle | declcfg.Channel
}

// TODO: Cleanup once https://github.com/golang/go/issues/45380 implemented
// We should be able to get rid of the schema arg and switch based on the type passed to this generic
func fetchCatalogMetadataByScheme[T declcfgSchema](ctx context.Context, cl client.Client, schema, catalogName string) ([]T, error) {
	cmList := catalogd.CatalogMetadataList{}
	if err := cl.List(ctx, &cmList, client.MatchingLabels{"schema": schema, "catalog": catalogName}); err != nil {
		return nil, err
	}

	contents := []T{}
	for _, cm := range cmList.Items {
		var content T
		if err := json.Unmarshal(cm.Spec.Content, &content); err != nil {
			return nil, err
		}
		contents = append(contents, content)
	}

	return contents, nil
}
