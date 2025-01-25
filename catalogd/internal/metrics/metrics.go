package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	RequestDurationMetricName = "catalogd_http_request_duration_seconds"
)

// Sets up the necessary metrics for calculating the Apdex Score
// If using Grafana for visualization connected to a Prometheus data
// source that is scraping these metrics, you can create a panel that
// uses the following queries + expressions for calculating the Apdex Score where T = 0.5:
// Query A: sum(catalogd_http_request_duration_seconds_bucket{code!~"5..",le="0.5"})
// Query B: sum(catalogd_http_request_duration_seconds_bucket{code!~"5..",le="2"})
// Query C: sum(catalogd_http_request_duration_seconds_count)
// Expression for Apdex Score: ($A + (($B - $A) / 2)) / $C
var (
	RequestDurationMetric = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: RequestDurationMetricName,
			Help: "Histogram of request duration in seconds",
			// create a bucket for each 100 ms up to 1s and ensure it multiplied by 4 also exists.
			// Include a 10s bucket to capture very long running requests. This allows us to easily
			// calculate Apdex Scores up to a T of 1 second, but using various mathmatical formulas we
			// should be able to estimate Apdex Scores up to a T of 2.5. Having a larger range of buckets
			// will allow us to more easily calculate health indicators other than the Apdex Score.
			Buckets: []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1, 1.2, 1.6, 2, 2.4, 2.8, 3.2, 3.6, 4, 10},
		},
		[]string{"code"},
	)
)

func AddMetricsToHandler(handler http.Handler) http.Handler {
	return promhttp.InstrumentHandlerDuration(RequestDurationMetric, handler)
}
