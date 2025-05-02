package image

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/containers/image/v5/docker/reference"
	"github.com/containers/image/v5/types"
	"github.com/opencontainers/go-digest"
	specsv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"gopkg.in/yaml.v2"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/registry"

	fsutil "github.com/operator-framework/operator-controller/internal/shared/util/fs"
)

func hasChart(imgCloser types.ImageCloser) bool {
	config := imgCloser.ConfigInfo()
	return config.MediaType == registry.ConfigMediaType
}

type ExtendCache interface {
	StoreChart(string, string, reference.Canonical, io.Reader) (fs.FS, time.Time, error)
}

func (a *diskCache) StoreChart(ownerID, filename string, canonicalRef reference.Canonical, src io.Reader) (fs.FS, time.Time, error) {
	dest := a.unpackPath(ownerID, canonicalRef.Digest())

	if err := fsutil.EnsureEmptyDirectory(dest, 0700); err != nil {
		return nil, time.Time{}, fmt.Errorf("error ensuring empty charts directory: %w", err)
	}

	// Destination file
	chart, err := os.Create(filepath.Join(dest, filename))
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("creating chart file; %w", err)
	}
	defer chart.Close()

	_, err = io.Copy(chart, src)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("copying chart to %s; %w", filename, err)
	}

	modTime, err := fsutil.GetDirectoryModTime(dest)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("error getting mod time of unpack directory: %w", err)
	}
	return os.DirFS(filepath.Dir(dest)), modTime, nil
}

func pullChart(ctx context.Context, ownerID string, img types.ImageSource, canonicalRef reference.Canonical, cache Cache, imgRef types.ImageReference) (fs.FS, time.Time, error) {
	raw, _, err := img.GetManifest(ctx, nil)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("get OCI helm chart manifest; %w", err)
	}

	chartManifest := specsv1.Manifest{}
	if err := json.Unmarshal(raw, &chartManifest); err != nil {
		return nil, time.Time{}, fmt.Errorf("unmarshaling chart manifest; %w", err)
	}

	if len(chartManifest.Layers) == 0 {
		return nil, time.Time{}, fmt.Errorf("manifest has no layers; expected at least one chart layer")
	}

	var chartDataLayerDigest digest.Digest
	var chartDataLayerFound bool
	for i, layer := range chartManifest.Layers {
		if layer.MediaType == registry.ChartLayerMediaType {
			chartDataLayerDigest = chartManifest.Layers[i].Digest
			chartDataLayerFound = true
			break
		}
	}

	if !chartDataLayerFound {
		return nil, time.Time{}, fmt.Errorf(
			"no layer with media type %q found in manifest (total layers: %d)",
			registry.ChartLayerMediaType,
			len(chartManifest.Layers),
		)
	}

	// Source file
	tarball, err := os.Open(filepath.Join(
		imgRef.PolicyConfigurationIdentity(), "blobs",
		"sha256", chartDataLayerDigest.Encoded()),
	)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("opening chart data; %w", err)
	}
	defer tarball.Close()

	filename := fmt.Sprintf("%s-%s.tgz",
		chartManifest.Annotations["org.opencontainers.image.title"],
		chartManifest.Annotations["org.opencontainers.image.version"],
	)
	return cache.StoreChart(ownerID, filename, canonicalRef, tarball)
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
	gzReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return chartInspectionResult{}, err
	}
	defer gzReader.Close()

	report := chartInspectionResult{}
	tarReader := tar.NewReader(gzReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			if !report.chartfileExists && !report.templatesExist {
				return report, errors.New("neither Chart.yaml nor templates directory were found")
			}

			if !report.chartfileExists {
				return report, errors.New("the Chart.yaml file was not found")
			}

			if !report.templatesExist {
				return report, errors.New("templates directory not found")
			}

			return report, nil
		}

		if strings.HasSuffix(header.Name, filepath.Join("templates", filepath.Base(header.Name))) {
			report.templatesExist = true
		}

		if filepath.Base(header.Name) == "Chart.yaml" {
			report.chartfileExists = true
			if err := loadMetadataArchive(tarReader, metadata); err != nil {
				return report, err
			}
		}
	}
}

func loadMetadataArchive(r io.Reader, metadata *chart.Metadata) error {
	if metadata == nil {
		return nil
	}

	content, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("reading Chart.yaml; %w", err)
	}

	if err := yaml.Unmarshal(content, metadata); err != nil {
		return fmt.Errorf("unmarshaling Chart.yaml; %w", err)
	}

	return nil
}

func IsBundleSourceChart(bundleFS fs.FS, metadata *chart.Metadata) (bool, error) {
	var chartPath string
	files, _ := fs.ReadDir(bundleFS, ".")
	for _, file := range files {
		if slices.Contains([]string{".tar.gz", ".tgz"}, filepath.Ext(file.Name())) {
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
		return false, err
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
	if chart != nil {
		for _, f := range options {
			f(chart)
		}
		return chart, nil
	}
	return nil, fmt.Errorf("chart can not be nil")
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
