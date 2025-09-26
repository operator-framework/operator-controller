package testing

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

type bundleData struct {
	annotations *registry.Annotations
	properties  []property.Property
	resources   map[string]client.Object
}

type BundleFSOption func(*bundleData)

// WithPackageName is an option for NewBundleFS used to set the package name annotation in the
// bundle filesystem metadata/annotations.yaml file
func WithPackageName(packageName string) BundleFSOption {
	return func(data *bundleData) {
		if data.annotations == nil {
			data.annotations = &registry.Annotations{}
		}
		data.annotations.PackageName = packageName
	}
}

// WithChannels is an option for NewBundleFS used to set the channels annotation in the
// bundle filesystem metadata/annotations.yaml file
func WithChannels(channels ...string) BundleFSOption {
	return func(data *bundleData) {
		if data.annotations == nil {
			data.annotations = &registry.Annotations{}
		}
		data.annotations.Channels = strings.Join(channels, ",")
	}
}

// WithDefaultChannel is an option for NewBundleFS used to set the channel annotation in the
// bundle filesystem metadata/annotations.yaml file
func WithDefaultChannel(channel string) BundleFSOption {
	return func(data *bundleData) {
		if data.annotations == nil {
			data.annotations = &registry.Annotations{}
		}
		data.annotations.DefaultChannelName = channel
	}
}

// WithBundleProperty is an options for NewBundleFS used to add a property to the list of properties
// in the bundle filesystem metadata/properties.yaml file
func WithBundleProperty(propertyType string, value string) BundleFSOption {
	return func(data *bundleData) {
		data.properties = append(data.properties, property.Property{
			Type:  propertyType,
			Value: []byte(`"` + value + `"`),
		})
	}
}

// WithBundleResource is an option for NewBundleFS use to add the yaml representation of resource to the
// path manifests/<resourceName>.yaml on the bundles filesystem
func WithBundleResource(resourceName string, resource client.Object) BundleFSOption {
	return func(data *bundleData) {
		if data.resources == nil {
			data.resources = make(map[string]client.Object)
		}
		data.resources[resourceName] = resource
	}
}

// WithCSV is an optiona for NewBundleFS used to add the yaml representation of csv to the
// path manifests/csv.yaml on the bundle filesystem
func WithCSV(csv v1alpha1.ClusterServiceVersion) BundleFSOption {
	return func(data *bundleData) {
		if data.resources == nil {
			data.resources = make(map[string]client.Object)
		}
		data.resources["csv.yaml"] = &csv
	}
}

// NewBundleFS creates a registry+v1 bundle filesystem with the applied options
// By default, an empty registry+v1 bundle filesystem will be returned
func NewBundleFS(opts ...BundleFSOption) fstest.MapFS {
	bundleData := &bundleData{}
	for _, applyOpt := range opts {
		applyOpt(bundleData)
	}
	bundleFS := fstest.MapFS{}

	// Add annotations metadata
	if bundleData.annotations != nil {
		annotationsYml, err := yaml.Marshal(registry.AnnotationsFile{
			Annotations: *bundleData.annotations,
		})
		if err != nil {
			panic(fmt.Errorf("error building bundle fs: %w", err))
		}
		bundleFS[BundlePathAnnotations] = &fstest.MapFile{Data: annotationsYml}
	}

	// Add property metadata
	if len(bundleData.properties) > 0 {
		propertiesYml, err := yaml.Marshal(source.RegistryV1Properties{
			Properties: bundleData.properties,
		})
		if err != nil {
			panic(fmt.Errorf("error building bundle fs: %w", err))
		}
		bundleFS[BundlePathProperties] = &fstest.MapFile{Data: propertiesYml}
	}

	// Add resources
	for name, obj := range bundleData.resources {
		resourcePath := filepath.Join(BundlePathManifests, name)
		resourceYml, err := yaml.Marshal(obj)
		if err != nil {
			panic(fmt.Errorf("error building bundle fs: %w", err))
		}
		bundleFS[resourcePath] = &fstest.MapFile{Data: resourceYml}
	}

	return bundleFS
}
