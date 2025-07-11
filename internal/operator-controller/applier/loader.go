package applier

import (
	"fmt"
	"io/fs"

	"helm.sh/helm/v3/pkg/chart"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
	imageutil "github.com/operator-framework/operator-controller/internal/shared/util/image"
)

const (
	BundleTypeRegistryV1 BundleType = "registry-v1"
	BundleTypeHelm       BundleType = "helm"
)

type BundleType string

type HelmChartLoaderMap map[BundleType]HelmChartLoader

type BundleFSChartLoader struct {
	HelmChartLoaders HelmChartLoaderMap
}

func (b BundleFSChartLoader) Load(bundleFS fs.FS, installNamespace string, watchNamespace string) (*chart.Chart, error) {
	bundleType := b.determineBundleType(bundleFS)
	if loader, ok := b.HelmChartLoaders[bundleType]; ok {
		return loader.Load(bundleFS, installNamespace, watchNamespace)
	}
	return nil, fmt.Errorf("unsupported bundle type: %s", bundleType)
}

// determineBundleType is a stand-in method to helm determine the bundle type until
// the bundle metadata can be given together with the FS to the Load method
func (b BundleFSChartLoader) determineBundleType(bundleFS fs.FS) BundleType {
	if imageutil.IsBundleSourceChart(bundleFS) {
		return BundleTypeHelm
	}
	return BundleTypeRegistryV1
}

type HelmBundleLoader struct{}

func (h HelmBundleLoader) Load(bundleFS fs.FS, installNamespace string, _ string) (*chart.Chart, error) {
	return imageutil.LoadChartFSWithOptions(bundleFS, imageutil.WithInstallNamespace(installNamespace))
}

type RegistryV1BundleToHelmChartConverter interface {
	ToHelmChart(bundleSource source.BundleSource, installNamespace string, watchNamespace string) (*chart.Chart, error)
}

type RegistryV1BundleLoader struct {
	RegistryV1BundleToHelmChartConverter
}

func (r RegistryV1BundleLoader) Load(bundleFS fs.FS, installNamespace string, watchNamespace string) (*chart.Chart, error) {
	return r.ToHelmChart(source.FromFS(bundleFS), installNamespace, watchNamespace)
}
