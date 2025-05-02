package image

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"helm.sh/helm/v3/pkg/chart"
)

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
	testRoot := t.TempDir()
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
			chartFS, _ := createTempFS(testRoot, tc.path)
			got, err := IsBundleSourceChart(chartFS, tc.meta)
			assert.Equal(t, tc.want, got, "validata helm chart")
			if tc.wantErr {
				assert.EqualError(t, err, tc.errMsg, "chart validation error")
			}
		})
	}
}

func createTempFS(path, filename string) (fs.FS, error) {
	tmpDir, _ := os.MkdirTemp(path, "bundlefs-")

	if filename == "" {
		return os.DirFS(tmpDir), nil
	}

	fmt.Println("Code reached here")
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	dest, err := os.Create(filepath.Join(tmpDir, filepath.Base(filename)))
	if err != nil {
		return nil, err
	}

	if _, err := io.Copy(dest, f); err != nil {
		return nil, err
	}

	return os.DirFS(tmpDir), nil
}

func Test_loadChartFS(t *testing.T) {
	testRoot := t.TempDir()
	testdataCharts := filepath.Join("../../../../", "testdata", "charts")
	type args struct {
		filename string
		filepath string
	}
	type want struct {
		name    string
		version string
	}
	tests := []struct {
		name    string
		args    args
		want    want
		wantErr bool
		errMsg  string
	}{
		{
			name: "empty filename is provided",
			args: args{
				filename: "",
				filepath: "",
			},
			wantErr: true,
			errMsg:  "chart file name was not provided",
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
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			chartFS, _ := createTempFS(testRoot, tc.args.filepath)

			got, err := loadChartFS(chartFS, tc.args.filename)
			if (err != nil) && tc.wantErr {
				assert.EqualError(t, err, "chart file name was not provided")
				return
			}
			assert.Equal(t, tc.want.name, got.Metadata.Name, "verify chart name")
			assert.Equal(t, tc.want.version, got.Metadata.Version, "verify chart version")
		})
	}
}

func TestLoadChartFSWithOptions(t *testing.T) {
	testRoot := t.TempDir()
	testdataCharts := filepath.Join("../../../../", "testdata", "charts")
	type args struct {
		filename   string
		fileSource string
	}
	type want struct {
		name    string
		version string
	}
	tests := []struct {
		name    string
		args    args
		want    want
		wantErr bool
		errMsg  string
	}{
		{
			name: "empty filename is provided",
			args: args{
				filename:   "",
				fileSource: "",
			},
			wantErr: true,
			errMsg:  "chart file name was not provided",
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
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			chartFS, _ := createTempFS(testRoot, tc.args.fileSource)
			got, err := LoadChartFSWithOptions(chartFS, tc.args.filename, WithInstallNamespace("metrics-server-system"))
			if (err != nil) && tc.wantErr {
				assert.EqualError(t, err, "chart file name was not provided")
				return
			}
			assert.Equal(t, tc.want.name, got.Metadata.Name, "verify chart name")
			assert.Equal(t, tc.want.version, got.Metadata.Version, "verify chart version")
		})
	}
}
