package helm

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/registry"
)

type HelmCheckResponse struct {
	// Chart returns true if helm chart
	Chart bool
	// Oci returns true if resource is stored
	// in an OCI registry
	Oci bool
}

func IsChart(ctx context.Context, chartURI string) (HelmCheckResponse, error) {
	addr, err := url.Parse(chartURI)
	if err != nil {
		return HelmCheckResponse{}, err
	}

	if addr.Scheme != "" {
		if !strings.HasPrefix(addr.Scheme, "http") {
			return HelmCheckResponse{}, fmt.Errorf("unexpected scheme; %s", addr.Scheme)
		}

		helmchart, err := validateHelmChart(addr.String())
		if err != nil {
			return HelmCheckResponse{}, err
		}

		if helmchart != nil &&
			helmchart.Metadata != nil &&
			helmchart.Metadata.Name != "" {
			return HelmCheckResponse{
				Chart: true,
				Oci:   false,
			}, err
		}
	}

	ociRe := regexp.MustCompile("^(?P<host>[a-zA-Z0-9-_.:]+)([/]?)(?P<org>[a-zA-Z0-9-_/]+)?([/](?P<chart>[a-zA-Z0-9-_.:@]+))$")
	if !ociRe.MatchString(chartURI) {
		return HelmCheckResponse{
			Chart: false,
			Oci:   false,
		}, fmt.Errorf("does not conform to OCI url format")
	}

	ociCheck, err := helmOciCheck(ctx, chartURI)
	if err != nil {
		return HelmCheckResponse{
			Chart: false,
			Oci:   true,
		}, err
	}

	return HelmCheckResponse{
		Chart: ociCheck,
		Oci:   true,
	}, nil
}

// helmOciCheck() pull a helm chart using the provided chartURI from an
// OCI registiry and inspects its media type to determine if a Helm chart
func helmOciCheck(_ context.Context, chartURI string) (bool, error) {
	helmclient, err := registry.NewClient()
	if err != nil {
		return false, err
	}

	summary, err := helmclient.Pull(chartURI,
		registry.PullOptWithProv(false),
		registry.PullOptWithChart(true),
		registry.PullOptIgnoreMissingProv(true),
	)
	if err != nil {
		return false, err
	}

	return summary != nil && summary.Ref != "", nil
}

func validateHelmChart(chartURI string) (*chart.Chart, error) {
	// Download helm chart from HTTP
	req, err := http.NewRequest(http.MethodGet, chartURI, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request failed; %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("loading URL failed; %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected response; %w", err)
	}

	if !slices.Contains(resp.Header["Content-Type"], "application/octet-stream") {
		return nil, fmt.Errorf("unknown contype-type")
	}

	return loader.LoadArchive(resp.Body)
}
