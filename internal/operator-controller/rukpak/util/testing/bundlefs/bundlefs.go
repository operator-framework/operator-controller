package bundlefs

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing/fstest"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/operator-framework/operator-registry/alpha/property"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
	registry "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/operator-registry"
)

const (
	BundlePathAnnotations = "metadata/annotations.yaml"
	BundlePathProperties  = "metadata/properties.yaml"
	BundlePathManifests   = "manifests"
)

// BundleFSBuilder builds a registry+v1 bundle filesystem
type BundleFSBuilder interface {
	WithPackageName(packageName string) BundleFSBuilder
	WithChannels(channels ...string) BundleFSBuilder
	WithDefaultChannel(channel string) BundleFSBuilder
	WithBundleProperty(propertyType string, value string) BundleFSBuilder
	WithBundleResource(resourceName string, resource client.Object) BundleFSBuilder
	WithCSV(csv v1alpha1.ClusterServiceVersion) BundleFSBuilder
	Build() fstest.MapFS
}

// bundleFSBuilder builds a registry+v1 bundle filesystem
type bundleFSBuilder struct {
	annotations *registry.Annotations
	properties  []property.Property
	resources   map[string]client.Object
}

func Builder() BundleFSBuilder {
	return &bundleFSBuilder{}
}

// WithPackageName is an option for NewBundleFS used to set the package name annotation in the
// bundle filesystem metadata/annotations.yaml file
func (b *bundleFSBuilder) WithPackageName(packageName string) BundleFSBuilder {
	if b.annotations == nil {
		b.annotations = &registry.Annotations{}
	}
	b.annotations.PackageName = packageName
	return b
}

// WithChannels is an option for NewBundleFS used to set the channels annotation in the
// bundle filesystem metadata/annotations.yaml file
func (b *bundleFSBuilder) WithChannels(channels ...string) BundleFSBuilder {
	if b.annotations == nil {
		b.annotations = &registry.Annotations{}
	}
	b.annotations.Channels = strings.Join(channels, ",")
	return b
}

// WithDefaultChannel is an option for NewBundleFS used to set the channel annotation in the
// bundle filesystem metadata/annotations.yaml file
func (b *bundleFSBuilder) WithDefaultChannel(channel string) BundleFSBuilder {
	if b.annotations == nil {
		b.annotations = &registry.Annotations{}
	}
	b.annotations.DefaultChannelName = channel
	return b
}

// WithBundleProperty is an options for NewBundleFS used to add a property to the list of properties
// in the bundle filesystem metadata/properties.yaml file
func (b *bundleFSBuilder) WithBundleProperty(propertyType string, value string) BundleFSBuilder {
	b.properties = append(b.properties, property.Property{
		Type:  propertyType,
		Value: []byte(`"` + value + `"`),
	})
	return b
}

// WithBundleResource is an option for NewBundleFS use to add the yaml representation of resource to the
// path manifests/<resourceName>.yaml on the bundles filesystem
func (b *bundleFSBuilder) WithBundleResource(resourceName string, resource client.Object) BundleFSBuilder {
	if b.resources == nil {
		b.resources = make(map[string]client.Object)
	}
	b.resources[resourceName] = resource
	return b
}

// WithCSV is an optiona for NewBundleFS used to add the yaml representation of csv to the
// path manifests/csv.yaml on the bundle filesystem
func (b *bundleFSBuilder) WithCSV(csv v1alpha1.ClusterServiceVersion) BundleFSBuilder {
	if b.resources == nil {
		b.resources = make(map[string]client.Object)
	}
	b.resources["csv.yaml"] = &csv
	return b
}

// Build creates a registry+v1 bundle filesystem with the applied options
// By default, an empty registry+v1 bundle filesystem will be returned
func (b *bundleFSBuilder) Build() fstest.MapFS {
	bundleFS := fstest.MapFS{}

	// Add annotations metadata
	if b.annotations != nil {
		annotationsYml, err := yaml.Marshal(registry.AnnotationsFile{
			Annotations: *b.annotations,
		})
		if err != nil {
			panic(fmt.Errorf("error building bundle fs: %w", err))
		}
		bundleFS[BundlePathAnnotations] = &fstest.MapFile{Data: annotationsYml}
	}

	// Add property metadata
	if len(b.properties) > 0 {
		propertiesYml, err := yaml.Marshal(source.RegistryV1Properties{
			Properties: b.properties,
		})
		if err != nil {
			panic(fmt.Errorf("error building bundle fs: %w", err))
		}
		bundleFS[BundlePathProperties] = &fstest.MapFile{Data: propertiesYml}
	}

	// Add resources
	for name, obj := range b.resources {
		resourcePath := filepath.Join(BundlePathManifests, name)
		resourceYml, err := yaml.Marshal(obj)
		if err != nil {
			panic(fmt.Errorf("error building bundle fs: %w", err))
		}
		bundleFS[resourcePath] = &fstest.MapFile{Data: resourceYml}
	}

	return bundleFS
}
