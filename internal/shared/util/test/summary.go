package test

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

var (
	summaryTemplate = "summary.md.tmpl"
	alertsTemplate  = "alert.md.tmpl"
	chartTemplate   = "mermaid_chart.md.tmpl"
	defaultPromUrl  = "http://localhost:30900"
)

type summaryAlerts struct {
	FiringAlerts  []summaryAlert
	PendingAlerts []summaryAlert
}

type summaryAlert struct {
	v1.Alert
	Name        string
	Description string
}

type xychart struct {
	Title  string
	YMax   float64
	YMin   float64
	YLabel string
	Data   string
}

type githubSummary struct {
	client       api.Client
	Pods         []string
	alertsFiring bool
}

func NewSummary(c api.Client, pods ...string) githubSummary {
	return githubSummary{
		client:       c,
		Pods:         pods,
		alertsFiring: false,
	}
}

// PerformanceQuery queries the prometheus server and generates a mermaid xychart with the data.
// title  - Display name of the xychart
// pod    - Pod name with which to filter results from prometheus
// query  - Prometheus query
// yLabel - Label of the Y axis i.e. "KB/s", "MB", etc.
// scaler - Constant by which to scale the results. For instance, cpu usage is more human-readable
// as "mCPU" vs "CPU", so we scale the results by a factor of 1,000.
func (s *githubSummary) PerformanceQuery(title, pod, query, yLabel string, scaler float64) (string, error) {
	v1api := v1.NewAPI(s.client)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fullQuery := fmt.Sprintf(query, pod)
	result, warnings, err := v1api.Query(ctx, fullQuery, time.Now())
	if err != nil {
		return "", err
	} else if len(warnings) > 0 {
		fmt.Printf("warnings returned from performance query; query=%s, warnings=%v", fullQuery, warnings)
	} else if result.Type() != model.ValMatrix {
		return "", fmt.Errorf("incompatible result type; need: %s, got: %s", model.ValMatrix, result.Type().String())
	}

	matrix, ok := result.(model.Matrix)
	if !ok {
		return "", fmt.Errorf("typecast for metrics samples failed; aborting")
	} else if len(matrix) > 1 {
		return "", fmt.Errorf("expected 1 set of results; got: %d", len(matrix))
	}
	chart := xychart{
		Title:  title,
		YLabel: yLabel,
		YMax:   math.SmallestNonzeroFloat64,
		YMin:   math.MaxFloat64,
	}
	formattedData := make([]string, 0)
	// matrix does not allow [] access, so we just do one iteration for the single result
	for _, metric := range matrix {
		if len(metric.Values) < 2 {
			// A graph with one data point means something with the collection was wrong
			return "", fmt.Errorf("expected at least two data points; got: %d", len(metric.Values))
		}
		for _, sample := range metric.Values {
			floatSample := float64(sample.Value) * scaler
			formattedData = append(formattedData, fmt.Sprintf("%f", floatSample))
			if floatSample > chart.YMax {
				chart.YMax = floatSample
			}
			if floatSample < chart.YMin {
				chart.YMin = floatSample
			}
		}
	}
	// Add some padding
	chart.YMax = (chart.YMax + (math.Abs(chart.YMax) * 0.05))
	chart.YMin = (chart.YMin - (math.Abs(chart.YMin) * 0.05))
	// Pretty print the values, ex: [1,2,3,4]
	chart.Data = strings.ReplaceAll(fmt.Sprintf("%v", formattedData), " ", ",")

	return executeTemplate(chartTemplate, chart)
}

// Alerts queries the prometheus server for alerts and generates markdown output for anything found.
// If no alerts are found, the alerts section will contain only "None." in the final output.
func (s *githubSummary) Alerts() (string, error) {
	v1api := v1.NewAPI(s.client)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := v1api.Alerts(ctx)
	if err != nil {
		return "", err
	}

	firingAlerts := make([]summaryAlert, 0)
	pendingAlerts := make([]summaryAlert, 0)
	if len(result.Alerts) > 0 {
		for _, a := range result.Alerts {
			aConv := summaryAlert{
				Alert:       a,
				Name:        string(a.Labels["alertname"]),
				Description: string(a.Annotations["description"]),
			}
			switch a.State {
			case v1.AlertStateFiring:
				firingAlerts = append(firingAlerts, aConv)
				s.alertsFiring = true
			case v1.AlertStatePending:
				pendingAlerts = append(pendingAlerts, aConv)
				// Ignore AlertStateInactive; the alerts endpoint doesn't return them
			}
		}
	} else {
		return "None.", nil
	}

	return executeTemplate(alertsTemplate, summaryAlerts{
		FiringAlerts:  firingAlerts,
		PendingAlerts: pendingAlerts,
	})
}

func executeTemplate(templateFile string, obj any) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}
	tmpl, err := template.New(templateFile).ParseGlob(filepath.Join(wd, "../../internal/shared/util/testutils/templates", templateFile))
	if err != nil {
		return "", err
	}
	buffer := new(strings.Builder)
	err = tmpl.Execute(buffer, obj)
	if err != nil {
		return "", err
	}
	return buffer.String(), nil
}

// PrintSummary executes the main summary template, generating the full test report.
// The markdown is template-driven; the summary methods are called from within the
// template. This allows us to add or change queries (hopefully) without needing to
// touch code. The summary will be output to a file supplied by the env target.
func PrintSummary(path string) error {
	if path == "" {
		fmt.Printf("No summary output path specified; skipping")
		return nil
	}

	client, err := api.NewClient(api.Config{
		Address: defaultPromUrl,
	})
	if err != nil {
		fmt.Printf("warning: failed to initialize promQL client: %v", err)
		return nil
	}

	summary := NewSummary(client, "operator-controller", "catalogd")
	summaryMarkdown, err := executeTemplate(summaryTemplate, &summary)
	if err != nil {
		fmt.Printf("warning: failed to generate e2e test summary: %v", err)
		return nil
	}
	err = os.WriteFile(path, []byte(summaryMarkdown), 0o600)
	if err != nil {
		fmt.Printf("warning: failed to write e2e test summary output to %s: %v", path, err)
		return nil
	}
	fmt.Printf("Test summary output to %s successful\n", path)
	if summary.alertsFiring {
		return fmt.Errorf("performance alerts encountered during test run; please check e2e test summary for details")
	}
	return nil
}
