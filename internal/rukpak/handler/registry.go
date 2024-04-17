package handler

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"gopkg.in/yaml.v2"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/rukpak/convert"
	"github.com/operator-framework/operator-controller/internal/rukpak/util"
)

const (
	manifestsDir = "manifests"
)

func HandleClusterExtension(ctx context.Context, fsys fs.FS, ext *ocv1alpha1.ClusterExtension) (*chart.Chart, chartutil.Values, error) {
	plainFS, err := convert.RegistryV1ToPlain(fsys, ext.Spec.WatchNamespaces)
	if err != nil {
		return nil, nil, fmt.Errorf("convert registry+v1 bundle to plain+v0 bundle: %v", err)
	}
	if err := ValidateBundle(plainFS); err != nil {
		return nil, nil, err
	}

	chrt, err := chartFromBundle(plainFS)
	if err != nil {
		return nil, nil, err
	}
	return chrt, nil, nil
}

func ValidateBundle(fsys fs.FS) error {
	objects, err := getBundleObjects(fsys)
	if err != nil {
		return fmt.Errorf("get objects from bundle manifests: %v", err)
	}
	if len(objects) == 0 {
		return errors.New("invalid bundle: found zero objects: plain+v0 bundles are required to contain at least one object")
	}
	return nil
}

func getBundleObjects(bundleFS fs.FS) ([]client.Object, error) {
	entries, err := fs.ReadDir(bundleFS, manifestsDir)
	if err != nil {
		return nil, err
	}

	var bundleObjects []client.Object
	for _, e := range entries {
		if e.IsDir() {
			return nil, fmt.Errorf("subdirectories are not allowed within the %q directory of the bundle image filesystem: found %q", manifestsDir, filepath.Join(manifestsDir, e.Name()))
		}

		manifestObjects, err := getObjects(bundleFS, e)
		if err != nil {
			return nil, err
		}
		bundleObjects = append(bundleObjects, manifestObjects...)
	}
	return bundleObjects, nil
}

func getObjects(bundle fs.FS, manifest fs.DirEntry) ([]client.Object, error) {
	manifestPath := filepath.Join(manifestsDir, manifest.Name())
	manifestReader, err := bundle.Open(manifestPath)
	if err != nil {
		return nil, err
	}
	defer manifestReader.Close()
	return util.ManifestObjects(manifestReader, manifestPath)
}

func chartFromBundle(fsys fs.FS) (*chart.Chart, error) {
	objects, err := getBundleObjects(fsys)
	if err != nil {
		return nil, fmt.Errorf("read bundle objects from bundle: %v", err)
	}

	chrt := &chart.Chart{
		Metadata: &chart.Metadata{},
	}
	for _, obj := range objects {
		yamlData, err := yaml.Marshal(obj)
		if err != nil {
			return nil, err
		}
		hash := sha256.Sum256(yamlData)
		chrt.Templates = append(chrt.Templates, &chart.File{
			Name: fmt.Sprintf("object-%x.yaml", hash[0:8]),
			Data: yamlData,
		})
	}
	return chrt, nil
}
