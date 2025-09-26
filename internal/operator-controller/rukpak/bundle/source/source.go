package source

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/resource"
	"sigs.k8s.io/yaml"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/operator-framework/operator-registry/alpha/property"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	registry "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/operator-registry"
)

type BundleSource interface {
	GetBundle() (bundle.RegistryV1, error)
}

type RegistryV1Properties struct {
	Properties []property.Property `json:"properties"`
}

// identitySource is a bundle source that returns itself
type identitySource bundle.RegistryV1

func (r identitySource) GetBundle() (bundle.RegistryV1, error) {
	return bundle.RegistryV1(r), nil
}

func FromBundle(rv1 bundle.RegistryV1) BundleSource {
	return identitySource(rv1)
}

// FromFS returns a BundleSource that loads a registry+v1 bundle from a filesystem.
// The filesystem is expected to conform to the registry+v1 format:
// metadata/annotations.yaml
// metadata/properties.yaml
// manifests/
//   - csv.yaml
//   - ...
//
// manifests directory should not contain subdirectories
func FromFS(fs fs.FS) BundleSource {
	return fsBundleSource{
		FS: fs,
	}
}

type fsBundleSource struct {
	FS fs.FS
}

func (f fsBundleSource) GetBundle() (bundle.RegistryV1, error) {
	reg := bundle.RegistryV1{}
	annotationsFileData, err := fs.ReadFile(f.FS, filepath.Join("metadata", "annotations.yaml"))
	if err != nil {
		return reg, err
	}
	annotationsFile := registry.AnnotationsFile{}
	if err := yaml.Unmarshal(annotationsFileData, &annotationsFile); err != nil {
		return reg, err
	}
	reg.PackageName = annotationsFile.Annotations.PackageName

	const manifestsDir = "manifests"
	foundCSV := false
	if err := fs.WalkDir(f.FS, manifestsDir, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			if path == manifestsDir {
				return nil
			}
			return fmt.Errorf("subdirectories are not allowed within the %q directory of the bundle image filesystem: found %q", manifestsDir, path)
		}
		manifestFile, err := f.FS.Open(path)
		if err != nil {
			return err
		}
		defer manifestFile.Close()

		result := resource.NewLocalBuilder().Unstructured().Flatten().Stream(manifestFile, path).Do()
		if err := result.Err(); err != nil {
			return err
		}
		if err := result.Visit(func(info *resource.Info, err error) error {
			if err != nil {
				return err
			}
			switch info.Object.GetObjectKind().GroupVersionKind().Kind {
			case "ClusterServiceVersion":
				csv := v1alpha1.ClusterServiceVersion{}
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(info.Object.(*unstructured.Unstructured).Object, &csv); err != nil {
					return err
				}
				reg.CSV = csv
				foundCSV = true
			case "CustomResourceDefinition":
				crd := apiextensionsv1.CustomResourceDefinition{}
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(info.Object.(*unstructured.Unstructured).Object, &crd); err != nil {
					return err
				}
				reg.CRDs = append(reg.CRDs, crd)
			default:
				reg.Others = append(reg.Others, *info.Object.(*unstructured.Unstructured))
			}
			return nil
		}); err != nil {
			return fmt.Errorf("error parsing objects in %q: %v", path, err)
		}
		return nil
	}); err != nil {
		return reg, err
	}

	if !foundCSV {
		return reg, fmt.Errorf("no ClusterServiceVersion found in %q", manifestsDir)
	}

	if err := copyMetadataPropertiesToCSV(&reg.CSV, f.FS); err != nil {
		return reg, err
	}

	return reg, nil
}

// copyMetadataPropertiesToCSV copies properties from `metadata/propeties.yaml` (in the filesystem fsys) into
// the CSV's `.metadata.annotations['olm.properties']` value, preserving any properties that are already
// present in the annotations.
func copyMetadataPropertiesToCSV(csv *v1alpha1.ClusterServiceVersion, fsys fs.FS) error {
	var allProperties []property.Property

	// First load existing properties from the CSV. We want to preserve these.
	if csvPropertiesJSON, ok := csv.Annotations["olm.properties"]; ok {
		var csvProperties []property.Property
		if err := json.Unmarshal([]byte(csvPropertiesJSON), &csvProperties); err != nil {
			return fmt.Errorf("failed to unmarshal csv.metadata.annotations['olm.properties']: %w", err)
		}
		allProperties = append(allProperties, csvProperties...)
	}

	// Next, load properties from the metadata/properties.yaml file, if it exists.
	metadataPropertiesJSON, err := fs.ReadFile(fsys, filepath.Join("metadata", "properties.yaml"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to read properties.yaml file: %w", err)
	}

	// If there are no properties, we can stick with whatever
	// was already present in the CSV annotations.
	if len(metadataPropertiesJSON) == 0 {
		return nil
	}

	// Otherwise, we need to parse the properties.yaml file and
	// append its properties into the CSV annotation.
	var metadataProperties RegistryV1Properties
	if err := yaml.Unmarshal(metadataPropertiesJSON, &metadataProperties); err != nil {
		return fmt.Errorf("failed to unmarshal metadata/properties.yaml: %w", err)
	}
	allProperties = append(allProperties, metadataProperties.Properties...)

	// Lastly re-marshal all the properties back into a JSON array and update the CSV annotation
	allPropertiesJSON, err := json.Marshal(allProperties)
	if err != nil {
		return fmt.Errorf("failed to marshal registry+v1 properties to json: %w", err)
	}
	csv.Annotations["olm.properties"] = string(allPropertiesJSON)
	return nil
}
