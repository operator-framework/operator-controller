package image

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/containerd/containerd/archive"
	"github.com/containers/image/v5/docker/reference"
	"github.com/containers/image/v5/types"
	goregistry "github.com/google/go-containerregistry/pkg/registry"
	"github.com/opencontainers/go-digest"
	ocispecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/registry"

	fsutil "github.com/operator-framework/operator-controller/internal/shared/util/fs"
)

func Test_pullChart(t *testing.T) {
	const myOwner = "myOwner"
	myChartName := "sample-chart-0.1.0.tgz"
	sampleChartPath := filepath.Join("../../../../", "testdata", "charts", "sample-chart-0.1.0.tgz")

	myTagRef, myCanonicalRef, cleanup := setupChartRegistry(t, sampleChartPath)
	defer cleanup()

	tests := []struct {
		name        string
		ownerID     string
		srcRef      string
		cache       Cache
		contextFunc func(context.Context) (*types.SystemContext, error)
		expect      func(*testing.T, fs.FS, reference.Canonical, time.Time, error)
	}{
		{
			name:    "pull helm chart from OCI registry",
			ownerID: myOwner,
			srcRef:  myTagRef.String(),
			cache: &diskCache{
				basePath: t.TempDir(),
				filterFunc: func(ctx context.Context, named reference.Named, image ocispecv1.Image) (archive.Filter, error) {
					return forceOwnershipRWX(), nil
				},
			},
			contextFunc: buildSourceContextFunc(t, myTagRef),
			expect: func(t *testing.T, fsys fs.FS, canonical reference.Canonical, modTime time.Time, err error) {
				require.NoError(t, err)

				actualChartData, err := fs.ReadFile(fsys, myChartName)
				require.NoError(t, err)

				chartData, err := os.ReadFile(sampleChartPath)
				require.NoError(t, err)

				assert.Equal(t, chartData, actualChartData)

				assert.Equal(t, myCanonicalRef.String(), canonical.String())
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			puller := ContainersImagePuller{
				SourceCtxFunc: tc.contextFunc,
			}
			fsys, canonicalRef, modTime, err := puller.Pull(context.Background(), tc.ownerID, tc.srcRef, tc.cache)
			require.NotNil(t, tc.expect, "expect function must be defined")

			tc.expect(t, fsys, canonicalRef, modTime, err)

			if dc, ok := tc.cache.(*diskCache); ok && dc.basePath != "" {
				require.NoError(t, fsutil.DeleteReadOnlyRecursive(dc.basePath))
			}
		})
	}
}

func TestIsValidChart(t *testing.T) {
	tt := []struct {
		name    string
		target  *chart.Chart
		wantErr bool
		errMsg  string
	}{
		{
			name: "helm chart with required metadata",
			target: &chart.Chart{
				Metadata: &chart.Metadata{
					APIVersion: "v2",
					Name:       "sample-chart",
					Version:    "0.1.2",
				},
			},
			wantErr: false,
		},
		{
			name: "helm chart without name",
			target: &chart.Chart{
				Metadata: &chart.Metadata{
					APIVersion: "v2",
					Name:       "",
					Version:    "0.1.2",
				},
			},
			wantErr: true,
			errMsg:  "chart name is required",
		},
		{
			name: "helm chart with missing version",
			target: &chart.Chart{
				Metadata: &chart.Metadata{
					APIVersion: "v2",
					Name:       "sample-chart",
					Version:    "",
				},
			},
			wantErr: true,
			errMsg:  "chart version is required",
		},
		{
			name: "helm chart with missing metadata",
			target: &chart.Chart{
				Metadata: nil,
			},
			wantErr: true,
			errMsg:  "chart metadata is missing",
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			err := IsValidChart(tc.target)
			if tc.wantErr && assert.Error(t, err, "checking valid chart") {
				assert.EqualError(t, err, tc.errMsg, "validating chart")
			}
		})
	}
}

func TestIsBundleSourceChart(t *testing.T) {
	testdataCharts := filepath.Join("../../../../", "testdata", "charts")
	tt := []struct {
		name    string
		path    string
		meta    *chart.Metadata
		want    bool
		wantErr bool
		errMsg  string
	}{
		{
			name:    "complete helm chart with nil *chart.Metadata",
			path:    filepath.Join(testdataCharts, "sample-chart-0.1.0.tgz"),
			meta:    nil,
			want:    true,
			wantErr: false,
		},
		{
			name:    "complete helm chart",
			path:    filepath.Join(testdataCharts, "sample-chart-0.1.0.tgz"),
			meta:    &chart.Metadata{},
			want:    true,
			wantErr: false,
		},
		{
			name:    "helm chart without templates",
			path:    filepath.Join(testdataCharts, "broken-chart-0.1.0.tgz"),
			meta:    nil,
			want:    false,
			wantErr: true,
			errMsg:  "templates directory not found",
		},
		{
			name:    "helm chart without a Chart.yaml",
			path:    filepath.Join(testdataCharts, "missing-meta-0.1.0.tgz"),
			meta:    nil,
			want:    false,
			wantErr: true,
			errMsg:  "the Chart.yaml file was not found",
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			chartFS, _ := createTempFS(t, tc.path)
			got, err := IsBundleSourceChart(chartFS, tc.meta)
			assert.Equal(t, tc.want, got, "validata helm chart")
			if tc.wantErr {
				assert.EqualError(t, err, tc.errMsg, "chart validation error")
			}
		})
	}
}

func createTempFS(t *testing.T, filename string) (fs.FS, error) {
	tmpDir, _ := os.MkdirTemp(t.TempDir(), "bundlefs-")

	if filename == "" {
		return os.DirFS(tmpDir), nil
	}

	f, err := os.Open(filename)
	if err != nil {
		return os.DirFS(tmpDir), err
	}
	defer f.Close()

	dest, err := os.Create(filepath.Join(tmpDir, filepath.Base(filename)))
	if err != nil {
		return nil, err
	}
	defer dest.Close()

	if _, err := io.Copy(dest, f); err != nil {
		return nil, err
	}

	return os.DirFS(tmpDir), nil
}

func Test_loadChartFS(t *testing.T) {
	testdataCharts := filepath.Join("../../../../", "testdata", "charts")
	type args struct {
		filename string
		filepath string
	}
	type want struct {
		name    string
		version string
		errMsg  string
	}
	tests := []struct {
		name   string
		args   args
		want   want
		expect func(*chart.Chart, want, error)
	}{
		{
			name: "empty filename is provided",
			args: args{
				filename: "",
				filepath: "",
			},
			want: want{
				name:   "",
				errMsg: "chart file name was not provided",
			},
			expect: func(chart *chart.Chart, want want, err error) {
				require.EqualError(t, err, want.errMsg)
				assert.Nil(t, chart, "no chart would be returned")
			},
		},
		{
			name: "load sample chart",
			args: args{
				filename: "sample-chart-0.1.0.tgz",
				filepath: filepath.Join(testdataCharts, "sample-chart-0.1.0.tgz"),
			},
			want: want{
				name:    "sample-chart",
				version: "0.1.0",
			},
			expect: func(chart *chart.Chart, want want, err error) {
				require.NoError(t, err, "chart should load successfully")
				assert.Equal(t, want.name, chart.Metadata.Name, "verify chart name")
				assert.Equal(t, want.version, chart.Metadata.Version, "verify chart version")
			},
		},
		{
			name: "load nonexistent chart",
			args: args{
				filename: "nonexistent-chart-0.1.0.tgz",
				filepath: filepath.Join(testdataCharts, "nonexistent-chart-0.1.0.tgz"),
			},
			want: want{
				name:    "nonexistent-chart",
				version: "0.1.0",
			},
			expect: func(chart *chart.Chart, want want, err error) {
				assert.Nil(t, chart, "chart does not exist on filesystem")
				require.Error(t, err, "reading chart nonexistent-chart-0.1.0.tgz; open nonexistent-chart-0.1.0.tgz: no such file or directory")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			chartFS, _ := createTempFS(t, tc.args.filepath)

			got, err := loadChartFS(chartFS, tc.args.filename)
			assert.NotNil(t, tc.expect, "validation function")
			tc.expect(got, tc.want, err)
		})
	}
}

func TestLoadChartFSWithOptions(t *testing.T) {
	testdataCharts := filepath.Join("../../../../", "testdata", "charts")
	type args struct {
		filename   string
		fileSource string
	}
	type want struct {
		name    string
		version string
		errMsg  string
	}
	tests := []struct {
		name   string
		args   args
		want   want
		expect func(*chart.Chart, want, error)
	}{
		{
			name: "empty filename is provided",
			args: args{
				filename:   "",
				fileSource: "",
			},
			want: want{
				errMsg: "chart file name was not provided",
			},
			expect: func(chart *chart.Chart, want want, err error) {
				require.Error(t, err, want.errMsg)
			},
		},
		{
			name: "load sample chart",
			args: args{
				filename:   "sample-chart-0.1.0.tgz",
				fileSource: filepath.Join(testdataCharts, "sample-chart-0.1.0.tgz"),
			},
			want: want{
				name:    "sample-chart",
				version: "0.1.0",
			},
			expect: func(chart *chart.Chart, want want, err error) {
				require.NoError(t, err)
				assert.Equal(t, want.name, chart.Metadata.Name, "chart name")
				assert.Equal(t, want.version, chart.Metadata.Version, "chart version")
			},
		},
		{
			name: "load nonexistent chart",
			args: args{
				filename:   "nonexistent-chart-0.1.0.tgz",
				fileSource: filepath.Join(testdataCharts, "nonexistent-chart-0.1.0.tgz"),
			},
			want: want{
				errMsg: "reading chart nonexistent-chart-0.1.0.tgz; open nonexistent-chart-0.1.0.tgz: no such file or directory",
			},
			expect: func(chart *chart.Chart, want want, err error) {
				require.Error(t, err, want.errMsg)
				assert.Nil(t, chart)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			chartFS, _ := createTempFS(t, tc.args.fileSource)
			got, err := LoadChartFSWithOptions(chartFS, tc.args.filename, WithInstallNamespace("metrics-server-system"))
			require.NotNil(t, tc.expect)
			tc.expect(got, tc.want, err)
		})
	}
}

func Test_enrichChart(t *testing.T) {
	type args struct {
		chart   *chart.Chart
		options []ChartOption
	}
	tests := []struct {
		name    string
		args    args
		want    *chart.Chart
		wantErr bool
	}{
		{
			name: "enrich empty chart object",
			args: args{
				chart: nil,
				options: []ChartOption{
					WithInstallNamespace("test-namespace-system"),
				},
			},
			wantErr: true,
			want:    nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := enrichChart(tc.args.chart, tc.args.options...)
			if (err != nil) != tc.wantErr {
				t.Errorf("enrichChart() error = %v, wantErr %v", err, tc.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("enrichChart() = %v, want %v", got, tc.want)
			}
		})
	}
}

func setupChartRegistry(t *testing.T, chartPath string) (reference.NamedTagged, reference.Canonical, func()) {
	server := httptest.NewServer(goregistry.New())
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	clientOpts := []registry.ClientOption{
		registry.ClientOptDebug(true),
		registry.ClientOptEnableCache(true),
	}
	client, err := registry.NewClient(clientOpts...)
	require.NoError(t, err)

	chart, err := os.ReadFile(chartPath)
	require.NoError(t, err)

	testCreationTime := "1977-09-02T22:04:05Z"
	ref := fmt.Sprintf("%s/testrepo/sample-chart:%s", serverURL.Host, "0.1.0")
	result, err := client.Push(chart, ref, registry.PushOptCreationTime(testCreationTime))
	require.NoError(t, err)

	imageTagRef, err := newReference(serverURL.Host, "testrepo/sample-chart", "0.1.0")
	require.NoError(t, err)

	imageDigestRef, err := reference.WithDigest(
		reference.TrimNamed(imageTagRef),
		digest.Digest(result.Manifest.Digest),
	)
	require.NoError(t, err)

	return imageTagRef, imageDigestRef, func() {
		server.Close()
	}
}
