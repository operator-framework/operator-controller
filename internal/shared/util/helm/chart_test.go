package helm_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	helmutils "github.com/operator-framework/operator-controller/internal/shared/util/helm"
)

func TestIsChart(t *testing.T) {
	type response struct {
		Oci   bool
		Chart bool
	}

	tt := []struct {
		name    string
		url     string
		want    response
		wantErr bool
	}{
		{
			name: "pull helm chart using image tag",
			url:  "quay.io/eochieng/metrics-server:3.12.0",
			want: response{
				Oci:   true,
				Chart: true,
			},
			wantErr: false,
		},
		{
			name: "pull helm chart using image digest",
			url:  "quay.io/eochieng/metrics-server@sha256:dd56f2ccc6e29ba7a2c5492e12c8210fb7367771eca93380a8dd64a6c9c985cb",
			want: response{
				Oci:   true,
				Chart: true,
			},
			wantErr: false,
		},
		{
			name: "pull helm chart from HTTP repository",
			url:  "https://github.com/kubernetes-sigs/metrics-server/releases/download/metrics-server-helm-chart-3.12.0/metrics-server-3.12.0.tgz",
			want: response{
				Oci:   false,
				Chart: true,
			},
			wantErr: false,
		},
		{
			name: "pull helm chart with oci scheme",
			url:  "oci://quay.io/eochieng/metrics-server@sha256:dd56f2ccc6e29ba7a2c5492e12c8210fb7367771eca93380a8dd64a6c9c985cb",
			want: response{
				Oci:   false,
				Chart: false,
			},
			wantErr: true,
		},
		{
			name: "pull kubernetes web page",
			url:  "https://kubernetes.io",
			want: response{
				Oci:   false,
				Chart: false,
			},
			wantErr: true,
		},
		{
			name: "pull busybox image from OCI registry",
			url:  "quay.io/opdev/busybox:latest",
			want: response{
				Oci:   true,
				Chart: false,
			},
			wantErr: true,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			response, err := helmutils.IsChart(context.Background(), tc.url)
			assert.Equal(t, response.Oci, tc.want.Oci)
			assert.Equal(t, response.Chart, tc.want.Chart)

			if testing.Verbose() {
				t.Logf("IsChart() is checking if %s is a helm chart.\n", tc.url)
			}

			if !tc.wantErr {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err, "helm chart not found")
			}
		})
	}
}
