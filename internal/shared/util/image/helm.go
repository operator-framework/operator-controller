package image

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/containers/image/v5/docker/reference"
	"github.com/containers/image/v5/types"
	"github.com/opencontainers/go-digest"
	specsv1 "github.com/opencontainers/image-spec/specs-go/v1"
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

	var chartDataLayerDigest digest.Digest
	if len(chartManifest.Layers) == 1 &&
		(chartManifest.Layers[0].MediaType == registry.ChartLayerMediaType) {
		chartDataLayerDigest = chartManifest.Layers[0].Digest
	}

	filename := fmt.Sprintf("%s-%s.tgz",
		chartManifest.Annotations["org.opencontainers.image.title"],
		chartManifest.Annotations["org.opencontainers.image.version"],
	)

	// Source file
	tarball, err := os.Open(filepath.Join(
		imgRef.PolicyConfigurationIdentity(), "blobs",
		"sha256", chartDataLayerDigest.Encoded()),
	)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("opening chart data; %w", err)
	}
	defer tarball.Close()

	return cache.StoreChart(ownerID, filename, canonicalRef, tarball)
}
