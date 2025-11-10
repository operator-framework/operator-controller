package comparator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Comparator compares two profile runs
type Comparator struct {
	baselineDir  string
	optimizedDir string
	outputDir    string
}

// NewComparator creates a new comparator
func NewComparator(baselineDir, optimizedDir, outputDir string) *Comparator {
	return &Comparator{
		baselineDir:  baselineDir,
		optimizedDir: optimizedDir,
		outputDir:    outputDir,
	}
}

// Compare generates a comparison report
func (c *Comparator) Compare(baselineName, optimizedName string) error {
	reportName := fmt.Sprintf("%s-vs-%s.md", baselineName, optimizedName)
	reportPath := filepath.Join(c.outputDir, reportName)

	fmt.Printf("📊 Generating comparison report: %s\n", reportPath)

	if err := os.MkdirAll(c.outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	report, err := os.Create(reportPath)
	if err != nil {
		return fmt.Errorf("failed to create report file: %w", err)
	}
	defer report.Close()

	// Write header
	fmt.Fprintf(report, "# Profile Comparison: %s vs %s\n\n", baselineName, optimizedName)
	fmt.Fprintf(report, "**Generated:** %s\n\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(report, "---\n\n")

	// Compare each component
	components, err := c.getComponents()
	if err != nil {
		return err
	}

	for _, comp := range components {
		fmt.Printf("📊 Comparing %s...\n", comp)
		fmt.Fprintf(report, "## %s\n\n", comp)

		if err := c.compareComponent(report, comp); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to compare %s: %v\n", comp, err)
		}

		fmt.Fprintf(report, "---\n\n")
	}

	// Summary
	c.writeSummary(report, baselineName, optimizedName)

	fmt.Printf("✅ Comparison complete: %s\n", reportPath)
	return nil
}

// compareComponent compares a single component
func (c *Comparator) compareComponent(report *os.File, component string) error {
	baselineComp := filepath.Join(c.baselineDir, component)
	optimizedComp := filepath.Join(c.optimizedDir, component)

	// Check if both directories exist
	if _, err := os.Stat(baselineComp); os.IsNotExist(err) {
		fmt.Fprintf(report, "⚠️ Baseline profiles not found\n\n")
		return nil
	}
	if _, err := os.Stat(optimizedComp); os.IsNotExist(err) {
		fmt.Fprintf(report, "⚠️ Optimized profiles not found\n\n")
		return nil
	}

	// Find peak profiles
	baselinePeak, baselineSize, err := findPeakProfile(baselineComp, "heap*.pprof")
	if err != nil {
		return err
	}

	optimizedPeak, optimizedSize, err := findPeakProfile(optimizedComp, "heap*.pprof")
	if err != nil {
		return err
	}

	// Extract memory usage
	baselineMem, _ := c.getMemoryUsage(baselineComp, baselinePeak)
	optimizedMem, _ := c.getMemoryUsage(optimizedComp, optimizedPeak)

	// Calculate improvement
	improvement := c.calculateImprovement(baselineMem, optimizedMem)

	fmt.Fprintf(report, "### Memory Comparison\n\n")
	fmt.Fprintf(report, "| Metric | Baseline | Optimized | Change |\n")
	fmt.Fprintf(report, "|--------|----------|-----------|--------|\n")
	fmt.Fprintf(report, "| Peak Profile Size | %s | %s | %s |\n",
		baselineSize, optimizedSize, c.formatChange(baselineSize, optimizedSize))
	fmt.Fprintf(report, "| Peak Memory Usage | %s | %s | %s |\n",
		baselineMem, optimizedMem, improvement)
	fmt.Fprintf(report, "\n")

	// Detailed comparison
	fmt.Fprintf(report, "### Top Differences\n\n")
	fmt.Fprintf(report, "```\n")
	if err := c.runDiff(report, baselineComp, baselinePeak, optimizedComp, optimizedPeak); err != nil {
		fmt.Fprintf(report, "Unable to generate diff\n")
	}
	fmt.Fprintf(report, "```\n\n")

	// OpenAPI comparison
	fmt.Fprintf(report, "### OpenAPI Comparison\n\n")
	baselineOpenAPI := c.getOpenAPIUsage(baselineComp, baselinePeak)
	optimizedOpenAPI := c.getOpenAPIUsage(optimizedComp, optimizedPeak)

	if baselineOpenAPI != "" || optimizedOpenAPI != "" {
		fmt.Fprintf(report, "**Baseline OpenAPI Usage:**\n```\n%s\n```\n\n", baselineOpenAPI)
		fmt.Fprintf(report, "**Optimized OpenAPI Usage:**\n```\n%s\n```\n\n", optimizedOpenAPI)
	} else {
		fmt.Fprintf(report, "No significant OpenAPI usage detected in either profile\n\n")
	}

	return nil
}

// getComponents returns the list of components to compare
func (c *Comparator) getComponents() ([]string, error) {
	entries, err := os.ReadDir(c.baselineDir)
	if err != nil {
		return nil, err
	}

	var components []string
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != "comparisons" {
			components = append(components, entry.Name())
		}
	}

	return components, nil
}

// runDiff runs pprof diff between baseline and optimized
func (c *Comparator) runDiff(w *os.File, baselineDir, baselinePeak, optimizedDir, optimizedPeak string) error {
	// Change to baseline directory for the command
	cmd := exec.Command("go", "tool", "pprof",
		"-base="+baselinePeak,
		"-top",
		optimizedPeak)
	cmd.Dir = baselineDir

	output, err := cmd.Output()
	if err != nil {
		return err
	}

	// Limit output
	lines := strings.Split(string(output), "\n")
	if len(lines) > 25 {
		lines = lines[:25]
	}
	fmt.Fprintf(w, "%s\n", strings.Join(lines, "\n"))
	return nil
}

// getMemoryUsage extracts memory usage from a profile
func (c *Comparator) getMemoryUsage(compDir, profile string) (string, error) {
	cmd := exec.Command("go", "tool", "pprof", "-top", profile)
	cmd.Dir = compDir
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	// Extract "Showing nodes accounting for X, Y% of Z total"
	re := regexp.MustCompile(`of ([0-9.]+[A-Za-z]+) total`)
	matches := re.FindStringSubmatch(string(output))
	if len(matches) > 1 {
		return matches[1], nil
	}

	return "unknown", nil
}

// getOpenAPIUsage extracts OpenAPI-related allocations
func (c *Comparator) getOpenAPIUsage(compDir, profile string) string {
	cmd := exec.Command("go", "tool", "pprof", "-text", profile)
	cmd.Dir = compDir
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	// Filter for openapi
	re := regexp.MustCompile("(?i)openapi")
	lines := strings.Split(string(output), "\n")
	var matched []string

	for _, line := range lines {
		if re.MatchString(line) {
			matched = append(matched, line)
		}
	}

	if len(matched) == 0 {
		return ""
	}

	if len(matched) > 10 {
		matched = matched[:10]
	}

	return strings.Join(matched, "\n")
}

// calculateImprovement calculates the improvement percentage
func (c *Comparator) calculateImprovement(baseline, optimized string) string {
	baselineVal := parseMemory(baseline)
	optimizedVal := parseMemory(optimized)

	if baselineVal == 0 {
		return "N/A"
	}

	diff := optimizedVal - baselineVal
	pct := (diff / baselineVal) * 100

	if diff < 0 {
		return fmt.Sprintf("%.1f%% reduction", -pct)
	}
	return fmt.Sprintf("+%.1f%%", pct)
}

// formatChange formats the change between two values
func (c *Comparator) formatChange(baseline, optimized string) string {
	baselineVal := parseSize(baseline)
	optimizedVal := parseSize(optimized)

	if baselineVal == 0 {
		return "N/A"
	}

	diff := optimizedVal - baselineVal
	pct := (diff / baselineVal) * 100

	if diff < 0 {
		return fmt.Sprintf("%.1f%%", pct)
	}
	return fmt.Sprintf("+%.1f%%", pct)
}

// writeSummary writes the summary section
func (c *Comparator) writeSummary(report *os.File, baseline, optimized string) {
	fmt.Fprintf(report, "## Summary\n\n")
	fmt.Fprintf(report, "This comparison shows the differences between:\n")
	fmt.Fprintf(report, "- **Baseline**: %s\n", baseline)
	fmt.Fprintf(report, "- **Optimized**: %s\n\n", optimized)
	fmt.Fprintf(report, "Look for negative percentages (reductions) as improvements.\n")
}

// Helper functions

func findPeakProfile(compDir, pattern string) (string, string, error) {
	profiles, err := filepath.Glob(filepath.Join(compDir, pattern))
	if err != nil {
		return "", "", err
	}

	if len(profiles) == 0 {
		return "", "", fmt.Errorf("no profiles found")
	}

	var largest string
	var largestSize int64

	for _, profile := range profiles {
		info, err := os.Stat(profile)
		if err != nil {
			continue
		}
		if info.Size() > largestSize {
			largest = profile
			largestSize = info.Size()
		}
	}

	sizeStr := fmt.Sprintf("%dK", largestSize/1024)
	return largest, sizeStr, nil
}

func parseMemory(s string) float64 {
	re := regexp.MustCompile(`([0-9.]+)([A-Za-z]+)`)
	matches := re.FindStringSubmatch(s)
	if len(matches) < 3 {
		return 0
	}

	var val float64
	fmt.Sscanf(matches[1], "%f", &val)

	unit := strings.ToLower(matches[2])
	switch unit {
	case "kb":
		return val
	case "mb":
		return val * 1024
	case "gb":
		return val * 1024 * 1024
	}

	return val
}

func parseSize(s string) float64 {
	re := regexp.MustCompile(`([0-9.]+)([A-Z])`)
	matches := re.FindStringSubmatch(s)
	if len(matches) < 3 {
		return 0
	}

	var val float64
	fmt.Sscanf(matches[1], "%f", &val)

	return val
}
