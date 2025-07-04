package image

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"regexp"
	"strings"
	"time"

	"github.com/containers/image/v5/docker/reference"
	"github.com/containers/image/v5/pkg/blobinfocache/none"
	"github.com/containers/image/v5/types"
	ocispecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/registry"
)

func hasChart(imgCloser types.ImageCloser) bool {
	config := imgCloser.ConfigInfo()
	return config.MediaType == registry.ConfigMediaType
}

func pullChart(ctx context.Context, ownerID string, srcRef reference.Named, canonicalRef reference.Canonical, imgSrc types.ImageSource, cache Cache) (fs.FS, time.Time, error) {
	imgDigest := canonicalRef.Digest()
	raw, _, err := imgSrc.GetManifest(ctx, &imgDigest)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("get OCI helm chart manifest; %w", err)
	}

	chartManifest := ocispecv1.Manifest{}
	if err := json.Unmarshal(raw, &chartManifest); err != nil {
		return nil, time.Time{}, fmt.Errorf("unmarshaling chart manifest; %w", err)
	}

	layerIter := iter.Seq[LayerData](func(yield func(LayerData) bool) {
		for i, layer := range chartManifest.Layers {
			ld := LayerData{Index: i, MediaType: layer.MediaType}
			if layer.MediaType == registry.ChartLayerMediaType {
				ld.Reader, _, ld.Err = imgSrc.GetBlob(ctx,
					types.BlobInfo{
						Annotations: layer.Annotations,
						MediaType:   layer.MediaType,
						Digest:      layer.Digest,
						Size:        layer.Size,
					},
					none.NoCache)
			}
			// Ignore the Helm provenance data layer
			if layer.MediaType == registry.ProvLayerMediaType {
				continue
			}
			if !yield(ld) {
				return
			}
		}
	})

	return cache.Store(ctx, ownerID, srcRef, canonicalRef, ocispecv1.Image{}, layerIter)
}

func IsValidChart(chart *chart.Chart) error {
	if chart.Metadata == nil {
		return errors.New("chart metadata is missing")
	}
	if chart.Metadata.Name == "" {
		return errors.New("chart name is required")
	}
	if chart.Metadata.Version == "" {
		return errors.New("chart version is required")
	}
	return chart.Metadata.Validate()
}

type chartInspectionResult struct {
	// templatesExist is set to true if the templates
	// directory exists in the chart archive
	templatesExist bool
	// chartfileExists is set to true if the Chart.yaml
	// file exists in the chart archive
	chartfileExists bool
}

func inspectChart(data []byte, metadata *chart.Metadata) (chartInspectionResult, error) {
	report := chartInspectionResult{}
	chart, err := loader.LoadArchive(bytes.NewBuffer(data))
	if err != nil {
		return report, fmt.Errorf("loading chart archive: %w", err)
	}

	report.templatesExist = len(chart.Templates) > 0
	report.chartfileExists = chart.Metadata != nil

	if metadata != nil && chart.Metadata != nil {
		*metadata = *chart.Metadata
	}

	return report, nil
}

func IsBundleSourceChart(bundleFS fs.FS, metadata *chart.Metadata) (bool, error) {
	var chartPath string
	files, _ := fs.ReadDir(bundleFS, ".")
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".tgz") ||
			strings.HasSuffix(file.Name(), ".tar.gz") {
			chartPath = file.Name()
			break
		}
	}

	chartData, err := fs.ReadFile(bundleFS, chartPath)
	if err != nil {
		return false, err
	}

	result, err := inspectChart(chartData, metadata)
	if err != nil {
		return false, fmt.Errorf("reading %s from fs: %w", chartPath, err)
	}

	return (result.templatesExist && result.chartfileExists), nil
}

type ChartOption func(*chart.Chart)

func WithInstallNamespace(namespace string) ChartOption {
	re := regexp.MustCompile(`{{\W+\.Release\.Namespace\W+}}`)

	return func(chrt *chart.Chart) {
		for i, template := range chrt.Templates {
			chrt.Templates[i].Data = re.ReplaceAll(template.Data, []byte(namespace))
		}
	}
}

func LoadChartFSWithOptions(bundleFS fs.FS, filename string, options ...ChartOption) (*chart.Chart, error) {
	ch, err := loadChartFS(bundleFS, filename)
	if err != nil {
		return nil, err
	}

	return enrichChart(ch, options...)
}

func enrichChart(chart *chart.Chart, options ...ChartOption) (*chart.Chart, error) {
	if chart == nil {
		return nil, fmt.Errorf("chart can not be nil")
	}
	for _, f := range options {
		f(chart)
	}
	return chart, nil
}

var LoadChartFS = loadChartFS

// loadChartFS loads a chart archive from a filesystem of
// type fs.FS with the provided filename
func loadChartFS(bundleFS fs.FS, filename string) (*chart.Chart, error) {
	if filename == "" {
		return nil, fmt.Errorf("chart file name was not provided")
	}

	tarball, err := fs.ReadFile(bundleFS, filename)
	if err != nil {
		return nil, fmt.Errorf("reading chart %s; %+v", filename, err)
	}
	return loader.LoadArchive(bytes.NewBuffer(tarball))
}
