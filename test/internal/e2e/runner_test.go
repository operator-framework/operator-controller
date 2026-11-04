package e2e_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-controller/test/e2e/shared"
)

func TestRunner(t *testing.T) {
	// The runner parses process-wide command-line flags, so these cases are serial.
	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })
	t.Setenv("E2E_SUMMARY_OUTPUT", "")

	featureDir := t.TempDir()
	featurePath := filepath.Join(featureDir, "install.feature")
	require.NoError(t, os.WriteFile(featurePath, []byte(`Feature: Install
  Scenario: Install latest
    Given a passing step

  @Serial
  Scenario Outline: Install version
    Given version "<version>"
    Examples:
      | version |
      | 1.0.0   |
      | 2.0.0   |

  Rule: Recovery
    Scenario: Install after failure
      Given a passing step

    Scenario: Recover existing
      Given a passing step

  @Undefined
  Scenario: Undefined step
    Given an undefined step
`), 0o600))

	for _, tt := range []struct {
		name      string
		paths     []string
		prefix    string
		tags      string
		want      []string
		wantError bool
	}{
		{
			name: "default feature directory",
			want: []string{"Install latest", "Install version", "Install version", "Install after failure", "Recover existing"},
		},
		{
			name:  "explicit line selector",
			paths: []string{featurePath + ":2"},
			want:  []string{"Install latest"},
		},
		{
			name:   "case insensitive prefix includes outlines and rules",
			paths:  []string{featurePath},
			prefix: " INSTALL ",
			want:   []string{"Install latest", "Install version", "Install version", "Install after failure"},
		},
		{
			name:   "single scenario prefix",
			paths:  []string{featurePath},
			prefix: "Install latest",
			want:   []string{"Install latest"},
		},
		{
			name: "serial tag selection",
			tags: "@Serial",
			want: []string{"Install version", "Install version"},
		},
		{
			name: "parallel tag selection",
			tags: "~@Serial",
			want: []string{"Install latest", "Install after failure", "Recover existing"},
		},
		{
			name:      "missing feature path",
			prefix:    "Install",
			wantError: true,
		},
		{
			name:      "multiple feature paths",
			paths:     []string{featurePath, featurePath},
			prefix:    "Install",
			wantError: true,
		},
		{
			name:      "no matching scenario",
			paths:     []string{featurePath},
			prefix:    "Uninstall",
			wantError: true,
		},
		{
			name:      "empty prefix",
			paths:     []string{featurePath},
			prefix:    " ",
			wantError: true,
		},
		{
			name:      "missing file",
			paths:     []string{filepath.Join(featureDir, "missing.feature")},
			prefix:    "Install",
			wantError: true,
		},
		{
			name:      "strict mode rejects undefined steps",
			tags:      "@Undefined",
			want:      []string{"Undefined step"},
			wantError: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tags := "~@Undefined"
			if tt.tags != "" {
				tags += " && " + tt.tags
			}
			if tt.tags == "@Undefined" {
				tags = tt.tags
			}
			os.Args = append([]string{
				"e2e.test", "--godog.format=progress", "--godog.concurrency=1",
				"--godog.tags=" + tags, "--e2e.scenario=" + tt.prefix,
			}, tt.paths...)

			var scenarios []string
			beforeSuite := false
			status := shared.Run(featureDir, func(tc *godog.TestSuiteContext) {
				tc.BeforeSuite(func() { beforeSuite = true })
			}, func(sc *godog.ScenarioContext) {
				sc.Step(`^a passing step$`, func() {})
				sc.Step(`^version "([^"]+)"$`, func(string) {})
				sc.Before(func(ctx context.Context, scenario *godog.Scenario) (context.Context, error) {
					scenarios = append(scenarios, scenario.Name)
					return ctx, nil
				})
			})
			if tt.wantError {
				require.NotZero(t, status)
			} else {
				require.Zero(t, status)
				require.True(t, beforeSuite)
			}
			require.Equal(t, tt.want, scenarios)
		})
	}
}
