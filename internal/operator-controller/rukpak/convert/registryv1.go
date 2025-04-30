package convert

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"helm.sh/helm/v3/pkg/chart"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cli-runtime/pkg/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/operator-framework/operator-registry/alpha/property"

	registry "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/operator-registry"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/generators"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/validators"
)

type Plain struct {
	Objects []client.Object
}

func RegistryV1ToHelmChart(rv1 fs.FS, installNamespace string, watchNamespace string) (*chart.Chart, error) {
	reg, err := ParseFS(rv1)
	if err != nil {
		return nil, err
	}

	plain, err := PlainConverter.Convert(reg, installNamespace, []string{watchNamespace})
	if err != nil {
		return nil, err
	}

	chrt := &chart.Chart{Metadata: &chart.Metadata{}}
	chrt.Metadata.Annotations = reg.CSV.GetAnnotations()
	for _, obj := range plain.Objects {
		jsonData, err := json.Marshal(obj)
		if err != nil {
			return nil, err
		}
		hash := sha256.Sum256(jsonData)
		chrt.Templates = append(chrt.Templates, &chart.File{
			Name: fmt.Sprintf("object-%x.json", hash[0:8]),
			Data: jsonData,
		})
	}

	return chrt, nil
}

// ParseFS converts the rv1 filesystem into a render.RegistryV1.
// ParseFS expects the filesystem to conform to the registry+v1 format:
// metadata/annotations.yaml
// manifests/
//   - csv.yaml
//   - ...
//
// manifests directory does not contain subdirectories
func ParseFS(rv1 fs.FS) (render.RegistryV1, error) {
	reg := render.RegistryV1{}
	annotationsFileData, err := fs.ReadFile(rv1, filepath.Join("metadata", "annotations.yaml"))
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
	if err := fs.WalkDir(rv1, manifestsDir, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			if path == manifestsDir {
				return nil
			}
			return fmt.Errorf("subdirectories are not allowed within the %q directory of the bundle image filesystem: found %q", manifestsDir, path)
		}
		manifestFile, err := rv1.Open(path)
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

	if err := copyMetadataPropertiesToCSV(&reg.CSV, rv1); err != nil {
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
	type registryV1Properties struct {
		Properties []property.Property `json:"properties"`
	}

	var metadataProperties registryV1Properties
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

func validateTargetNamespaces(supportedInstallModes sets.Set[string], installNamespace string, targetNamespaces []string) error {
	set := sets.New[string](targetNamespaces...)
	switch {
	case set.Len() == 0 || (set.Len() == 1 && set.Has("")):
		if supportedInstallModes.Has(string(v1alpha1.InstallModeTypeAllNamespaces)) {
			return nil
		}
		return fmt.Errorf("supported install modes %v do not support targeting all namespaces", sets.List(supportedInstallModes))
	case set.Len() == 1 && !set.Has(""):
		if supportedInstallModes.Has(string(v1alpha1.InstallModeTypeSingleNamespace)) {
			return nil
		}
		if supportedInstallModes.Has(string(v1alpha1.InstallModeTypeOwnNamespace)) && targetNamespaces[0] == installNamespace {
			return nil
		}
	default:
		if supportedInstallModes.Has(string(v1alpha1.InstallModeTypeMultiNamespace)) && !set.Has("") {
			return nil
		}
	}
	return fmt.Errorf("supported install modes %v do not support target namespaces %v", sets.List[string](supportedInstallModes), targetNamespaces)
}

var PlainConverter = Converter{
	BundleRenderer: render.BundleRenderer{
		BundleValidator: validators.RegistryV1BundleValidator,
		ResourceGenerators: []render.ResourceGenerator{
			generators.BundleCSVRBACResourceGenerator.ResourceGenerator(),
			generators.BundleCRDGenerator,
			generators.BundleAdditionalResourcesGenerator,
			generators.BundleCSVDeploymentGenerator,
		},
	},
}

type Converter struct {
	render.BundleRenderer
}

func (c Converter) Convert(rv1 render.RegistryV1, installNamespace string, targetNamespaces []string) (*Plain, error) {
	if installNamespace == "" {
		installNamespace = rv1.CSV.Annotations["operatorframework.io/suggested-namespace"]
	}
	if installNamespace == "" {
		installNamespace = fmt.Sprintf("%s-system", rv1.PackageName)
	}
	supportedInstallModes := sets.New[string]()
	for _, im := range rv1.CSV.Spec.InstallModes {
		if im.Supported {
			supportedInstallModes.Insert(string(im.Type))
		}
	}
	if len(targetNamespaces) == 0 {
		if supportedInstallModes.Has(string(v1alpha1.InstallModeTypeAllNamespaces)) {
			targetNamespaces = []string{""}
		} else if supportedInstallModes.Has(string(v1alpha1.InstallModeTypeOwnNamespace)) {
			targetNamespaces = []string{installNamespace}
		}
	}

	if err := validateTargetNamespaces(supportedInstallModes, installNamespace, targetNamespaces); err != nil {
		return nil, err
	}

	if len(rv1.CSV.Spec.APIServiceDefinitions.Owned) > 0 {
		return nil, fmt.Errorf("apiServiceDefintions are not supported")
	}

	if len(rv1.CSV.Spec.WebhookDefinitions) > 0 {
		return nil, fmt.Errorf("webhookDefinitions are not supported")
	}

	objs, err := c.BundleRenderer.Render(rv1, installNamespace, render.WithTargetNamespaces(targetNamespaces...))
	if err != nil {
		return nil, err
	}
	return &Plain{Objects: objs}, nil
}
