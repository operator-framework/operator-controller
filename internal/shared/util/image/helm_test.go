package image

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io/fs"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/containerd/containerd/archive"
	goregistry "github.com/google/go-containerregistry/pkg/registry"
	"github.com/opencontainers/go-digest"
	ocispecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/image/v5/docker"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/image"
	"go.podman.io/image/v5/types"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/registry"

	fsutil "github.com/operator-framework/operator-controller/internal/shared/util/fs"
)

func Test_hasChart(t *testing.T) {
	chartTagRef, _, cleanup := setupChartRegistry(t,
		mockHelmChartTgz(t,
			[]fileContent{
				{
					name:    "testchart/Chart.yaml",
					content: []byte("apiVersion: v2\nname: testchart\nversion: 0.1.0"),
				},
				{
					name:    "testchart/templates/deployment.yaml",
					content: []byte("kind: Deployment\napiVersion: apps/v1"),
				},
			},
		),
	)
	defer cleanup()

	imgTagRef, _, shutdown := setupRegistry(t)
	defer shutdown()

	type args struct {
		srcRef      string
		contextFunc func(context.Context) (*types.SystemContext, error)
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "returns true when image contains chart",
			args: args{
				srcRef:      chartTagRef.String(),
				contextFunc: buildSourceContextFunc(t, chartTagRef),
			},
			want: true,
		},
		{
			name: "returns false when image is not chart",
			args: args{
				srcRef:      imgTagRef.String(),
				contextFunc: buildSourceContextFunc(t, imgTagRef),
			},
			want: false,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srcRef, err := reference.ParseNamed(tc.args.srcRef)
			require.NoError(t, err)

			srcImgRef, err := docker.NewReference(srcRef)
			require.NoError(t, err)

			sysCtx, err := tc.args.contextFunc(ctx)
			require.NoError(t, err)

			imgSrc, err := srcImgRef.NewImageSource(ctx, sysCtx)
			require.NoError(t, err)

			img, err := image.FromSource(ctx, sysCtx, imgSrc)
			require.NoError(t, err)

			defer func() {
				if err := img.Close(); err != nil {
					panic(err)
				}
			}()

			got := hasChart(img)
			require.Equal(t, tc.want, got)
		})
	}
}

func Test_pullChart(t *testing.T) {
	const myOwner = "myOwner"
	myChartName := "testchart-0.1.0.tgz"
	testChart := mockHelmChartTgz(t,
		[]fileContent{
			{
				name:    "testchart/Chart.yaml",
				content: []byte("apiVersion: v2\nname: testchart\nversion: 0.1.0"),
			},
			{
				name:    "testchart/templates/deployment.yaml",
				content: []byte("kind: Deployment\napiVersion: apps/v1"),
			},
		},
	)

	myTagRef, myCanonicalRef, cleanup := setupChartRegistry(t, testChart)
	defer cleanup()

	tests := []struct {
		name        string
		ownerID     string
		srcRef      string
		cache       Cache
		contextFunc func(context.Context) (*types.SystemContext, error)
		expect      func(*testing.T, fs.FS, time.Time)
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
			expect: func(t *testing.T, fsys fs.FS, modTime time.Time) {
				now := time.Now()
				require.LessOrEqual(t, now.Sub(modTime), 3*time.Second, "modified time should less than 3 seconds")

				actualChartData, err := fs.ReadFile(fsys, myChartName)
				require.NoError(t, err)

				assert.Equal(t, testChart, actualChartData)
			},
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srcRef, err := reference.ParseNamed(tc.srcRef)
			require.NoError(t, err)

			srcImgRef, err := docker.NewReference(srcRef)
			require.NoError(t, err)

			sysCtx, err := tc.contextFunc(ctx)
			require.NoError(t, err)

			imgSrc, err := srcImgRef.NewImageSource(ctx, sysCtx)
			require.NoError(t, err)

			fsys, modTime, err := pullChart(ctx, tc.ownerID, srcRef, myCanonicalRef, imgSrc, tc.cache)
			require.NotNil(t, tc.expect, "expect function must be defined")
			require.NoError(t, err)

			tc.expect(t, fsys, modTime)

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
	type args struct {
		meta  *chart.Metadata
		files []fileContent
	}
	type want struct {
		value  bool
		errStr string
	}
	tt := []struct {
		name string
		args args
		want want
	}{
		{
			name: "complete helm chart with nil *chart.Metadata",
			args: args{
				meta: nil,
				files: []fileContent{
					{
						name:    "testchart/Chart.yaml",
						content: []byte("apiVersion: v2\nname: testchart\nversion: 0.1.0"),
					},
					{
						name:    "testchart/templates/deployment.yaml",
						content: []byte("kind: Deployment\napiVersion: apps/v1"),
					},
				},
			},
			want: want{
				value: true,
			},
		},
		{
			name: "complete helm chart",
			args: args{
				meta: &chart.Metadata{},
				files: []fileContent{
					{
						name:    "testchart/Chart.yaml",
						content: []byte("apiVersion: v2\nname: testchart\nversion: 0.1.0"),
					},
					{
						name:    "testchart/templates/deployment.yaml",
						content: []byte("kind: Deployment\napiVersion: apps/v1"),
					},
				},
			},
			want: want{
				value: true,
			},
		},
		{
			name: "helm chart without templates",
			args: args{
				meta: nil,
				files: []fileContent{
					{
						name:    "testchart/Chart.yaml",
						content: []byte("apiVersion: v2\nname: testchart\nversion: 0.1.0"),
					},
				},
			},
			want: want{
				value: false,
			},
		},
		{
			name: "helm chart without a Chart.yaml",
			args: args{
				meta: nil,
				files: []fileContent{
					{
						name:    "testchart/templates/deployment.yaml",
						content: []byte("kind: Deployment\napiVersion: apps/v1"),
					},
				},
			},
			want: want{
				value:  false,
				errStr: "reading testchart-0.1.0.tgz from fs: loading chart archive: Chart.yaml file is missing",
			},
		},
		{
			name: "invalid chart archive",
			args: args{
				meta: nil,
				files: []fileContent{
					{
						name:    "testchart/deployment.yaml",
						content: []byte("kind: Deployment\napiVersion: apps/v1"),
					},
				},
			},
			want: want{
				value:  false,
				errStr: "reading testchart-0.1.0.tgz from fs: loading chart archive: Chart.yaml file is missing",
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			chartFS, _ := createTempFS(t, mockHelmChartTgz(t, tc.args.files))
			got, err := IsBundleSourceChart(chartFS, tc.args.meta)
			if tc.want.errStr != "" {
				require.Error(t, err, "chart validation error required")
				require.EqualError(t, err, tc.want.errStr, "chart error")
			}
			require.Equal(t, tc.want.value, got, "validata helm chart")
		})
	}
}

func Test_loadChartFS(t *testing.T) {
	type args struct {
		filename string
		files    []fileContent
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
				files: []fileContent{
					{
						name:    "testchart/Chart.yaml",
						content: []byte("apiVersion: v2\nname: testchart\nversion: 0.1.0"),
					},
					{
						name:    "testchart/templates/deployment.yaml",
						content: []byte("kind: Deployment\napiVersion: apps/v1"),
					},
				},
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
				filename: "testchart-0.1.0.tgz",
				files: []fileContent{
					{
						name:    "testchart/Chart.yaml",
						content: []byte("apiVersion: v2\nname: testchart\nversion: 0.1.0"),
					},
					{
						name:    "testchart/templates/deployment.yaml",
						content: []byte("kind: Deployment\napiVersion: apps/v1"),
					},
				},
			},
			want: want{
				name:    "testchart",
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
				files: []fileContent{
					{
						name:    "testchart/Chart.yaml",
						content: []byte("apiVersion: v2\nname: testchart\nversion: 0.1.0"),
					},
					{
						name:    "testchart/templates/deployment.yaml",
						content: []byte("kind: Deployment\napiVersion: apps/v1"),
					},
				},
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
			chartFS, _ := createTempFS(t, mockHelmChartTgz(t, tc.args.files))

			got, err := loadChartFS(chartFS, tc.args.filename)
			assert.NotNil(t, tc.expect, "validation function")
			tc.expect(got, tc.want, err)
		})
	}
}

func TestLoadChartFSWithOptions(t *testing.T) {
	type args struct {
		filename string
		files    []fileContent
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
				files: []fileContent{
					{
						name:    "testchart/Chart.yaml",
						content: []byte("apiVersion: v2\nname: testchart\nversion: 0.1.0"),
					},
					{
						name:    "testchart/templates/deployment.yaml",
						content: []byte("kind: Deployment\napiVersion: apps/v1"),
					},
				},
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
				filename: "testchart-0.1.0.tgz",
				files: []fileContent{
					{
						name:    "testchart/Chart.yaml",
						content: []byte("apiVersion: v2\nname: testchart\nversion: 0.1.0"),
					},
					{
						name:    "testchart/templates/deployment.yaml",
						content: []byte("kind: Deployment\napiVersion: apps/v1"),
					},
				},
			},
			want: want{
				name:    "testchart",
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
				filename: "nonexistent-chart-0.1.0.tgz",
				files: []fileContent{
					{
						name:    "testchart/Chart.yaml",
						content: []byte("apiVersion: v2\nname: testchart\nversion: 0.1.0"),
					},
					{
						name:    "testchart/templates/deployment.yaml",
						content: []byte("kind: Deployment\napiVersion: apps/v1"),
					},
				},
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
			chartFS, _ := createTempFS(t, mockHelmChartTgz(t, tc.args.files))
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

func setupChartRegistry(t *testing.T, chart []byte) (reference.NamedTagged, reference.Canonical, func()) {
	server := httptest.NewServer(goregistry.New())
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	clientOpts := []registry.ClientOption{
		registry.ClientOptDebug(true),
		registry.ClientOptEnableCache(true),
	}
	client, err := registry.NewClient(clientOpts...)
	require.NoError(t, err)

	testCreationTime := "2020-09-22T22:04:05Z"
	ref := fmt.Sprintf("%s/testrepo/testchart:%s", serverURL.Host, "0.1.0")
	result, err := client.Push(chart, ref, registry.PushOptCreationTime(testCreationTime))
	require.NoError(t, err)

	imageTagRef, err := newReference(serverURL.Host, "testrepo/testchart", "0.1.0")
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

type fileContent struct {
	name    string
	content []byte
}

func mockHelmChartTgz(t *testing.T, contents []fileContent) []byte {
	require.NotEmpty(t, contents, "chart content required")
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Add files to the chart archive
	for _, file := range contents {
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name: file.name,
			Mode: 0600,
			Size: int64(len(file.content)),
		}))
		_, _ = tw.Write(file.content)
	}

	require.NoError(t, tw.Close())

	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	_, err := gz.Write(buf.Bytes())
	require.NoError(t, err)
	require.NoError(t, gz.Close())

	return gzBuf.Bytes()
}

func createTempFS(t *testing.T, data []byte) (fs.FS, error) {
	require.NotEmpty(t, data, "chart data")
	tmpDir, _ := os.MkdirTemp(t.TempDir(), "bundlefs-")
	if len(data) == 0 {
		return os.DirFS(tmpDir), nil
	}

	dest, err := os.Create(filepath.Join(tmpDir, "testchart-0.1.0.tgz"))
	if err != nil {
		return nil, err
	}
	defer dest.Close()

	if _, err := dest.Write(data); err != nil {
		return nil, err
	}

	return os.DirFS(tmpDir), nil
}
