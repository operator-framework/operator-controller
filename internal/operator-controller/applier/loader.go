package applier

import (
	"fmt"
	"io/fs"

	"helm.sh/helm/v3/pkg/chart"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/convert"
	imageutil "github.com/operator-framework/operator-controller/internal/shared/util/image"
)

const (
	BundleTypeRegistryV1 BundleType = "registry-v1"
	BundleTypeHelm       BundleType = "helm"
)

type BundleType string

type BundleFSChartLoader struct {
	HelmChartLoaders map[BundleType]HelmChartLoader
}

func (b BundleFSChartLoader) Load(bundleFS fs.FS, installNamespace string, watchNamespace string) (*chart.Chart, error) {
	bundleType, err := b.determineBundleType(bundleFS)
	if err != nil {
		return nil, fmt.Errorf("error determining bundle type: %w", err)
	}
	if loader, ok := b.HelmChartLoaders[bundleType]; ok {
		return loader.Load(bundleFS, installNamespace, watchNamespace)
	}
	return nil, fmt.Errorf("unsupported bundle type: %s", bundleType)
}

func (b BundleFSChartLoader) determineBundleType(bundleFS fs.FS) (BundleType, error) {
	isHelmChartBundle, err := imageutil.IsBundleSourceChart(bundleFS)
	if err != nil {
		return "", err
	}
	if isHelmChartBundle {
		return BundleTypeHelm, nil
	}
	return BundleTypeRegistryV1, nil
}

type HelmBundleLoader struct{}

func (h HelmBundleLoader) Load(bundleFS fs.FS, installNamespace string, _ string) (*chart.Chart, error) {
	isHelmChartBundle, err := imageutil.IsBundleSourceChart(bundleFS)
	if err != nil {
		return nil, err
	}
	if isHelmChartBundle {
		return imageutil.LoadChartFSWithOptions(bundleFS, imageutil.WithInstallNamespace(installNamespace))
	}
	return nil, nil
}

type RegistryV1BundleLoader struct {
	convert.BundleToHelmChartConverter
}

func (r RegistryV1BundleLoader) Load(bundleFS fs.FS, installNamespace string, watchNamespace string) (*chart.Chart, error) {
	return r.ToHelmChart(source.FromFS(bundleFS), installNamespace, watchNamespace)
}
