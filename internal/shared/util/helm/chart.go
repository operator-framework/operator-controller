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

func IsChart(ctx context.Context, chartUri string) (chart, oci bool, err error) {
	addr, err := url.Parse(chartUri)
	if err != nil {
		return chart, oci, err
	}

	if addr.Scheme != "" {
		if !strings.HasPrefix(addr.Scheme, "http") {
			err = fmt.Errorf("unexpected Url scheme; %s\n", addr.Scheme)
			return
		}

		oci = false
		helmchart, err := validateHelmChart(addr.String())
		if err != nil {
			chart = false
			return chart, oci, err
		}

		if helmchart != nil &&
			helmchart.Metadata != nil &&
			helmchart.Metadata.Name != "" {
			chart = true
		}

		return chart, oci, err
	}

	ociRe := regexp.MustCompile("^(?P<host>[a-zA-Z0-9-_.:]+)([/]?)(?P<org>[a-zA-Z0-9-_/]+)?([/](?P<chart>[a-zA-Z0-9-_.:@]+))$")
	if ociRe.MatchString(chartUri) {
		oci = true

		chart, err = helmOciCheck(ctx, chartUri)
		if err != nil {
			return chart, oci, err
		}
	}

	return
}

// helmOciCheck() pull a helm chart using the provided chartUri from an
// OCI registiry and inspects its media type to determine if a Helm chart
func helmOciCheck(ctx context.Context, chartUri string) (bool, error) {
	helmclient, err := registry.NewClient()
	if err != nil {
		return false, err
	}

	summary, err := helmclient.Pull(chartUri,
		registry.PullOptWithProv(false),
		registry.PullOptWithChart(true),
		registry.PullOptIgnoreMissingProv(true),
	)
	if err != nil {
		return false, err
	}

	return summary != nil && summary.Ref != "", nil
}

func validateHelmChart(chartUri string) (*chart.Chart, error) {
	// Download helm chart from HTTP
	resp, err := http.Get(chartUri)
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
