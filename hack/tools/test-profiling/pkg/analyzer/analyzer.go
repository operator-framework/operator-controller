package analyzer

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/pprof/profile"
	"github.com/operator-framework/operator-controller/hack/tools/test-profiling/pkg/config"
)

// Analyzer analyzes collected profiles and generates reports
type Analyzer struct {
	config *config.Config
}

// NewAnalyzer creates a new analyzer
func NewAnalyzer(cfg *config.Config) *Analyzer {
	return &Analyzer{config: cfg}
}

// Analyze generates an analysis report for the profile run
func (a *Analyzer) Analyze() error {
	reportPath := filepath.Join(a.config.ProfileDir(), "analysis.md")
	fmt.Printf("📊 Generating analysis report: %s\n", reportPath)

	report, err := os.Create(reportPath)
	if err != nil {
		return fmt.Errorf("failed to create report file: %w", err)
	}
	defer report.Close()

	// Write header
	fmt.Fprintf(report, "# Memory Profile Analysis\n\n")
	fmt.Fprintf(report, "**Test Name:** %s\n", a.config.Name)
	fmt.Fprintf(report, "**Date:** %s\n\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(report, "---\n\n")

	// Executive summary
	if err := a.writeExecutiveSummary(report); err != nil {
		return err
	}

	// Analyze each component
	for _, comp := range a.config.Components {
		compDir := a.config.ComponentDir(comp.Name)
		if _, err := os.Stat(compDir); os.IsNotExist(err) {
			continue
		}

		fmt.Fprintf(report, "## %s Analysis\n\n", comp.Name)

		if err := a.analyzeComponent(report, comp.Name, compDir); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to analyze %s: %v\n", comp.Name, err)
		}

		fmt.Fprintf(report, "---\n\n")
	}

	// Recommendations
	a.writeRecommendations(report)

	fmt.Printf("✅ Analysis complete: %s\n", reportPath)
	return nil
}

// writeExecutiveSummary writes the executive summary section
func (a *Analyzer) writeExecutiveSummary(report *os.File) error {
	fmt.Fprintf(report, "## Executive Summary\n")

	for _, comp := range a.config.Components {
		compDir := a.config.ComponentDir(comp.Name)
		peakMemory, err := a.getPeakMemory(compDir)
		if err == nil && peakMemory != "" {
			fmt.Fprintf(report, "- **%s**: %s\n", comp.Name, peakMemory)
		}
	}

	fmt.Fprintf(report, "\n\n\n")
	return nil
}

// analyzeComponent analyzes a single component
func (a *Analyzer) analyzeComponent(report *os.File, component, compDir string) error {
	// Count profiles
	heapCount, err := countProfiles(compDir, "heap*.pprof")
	if err != nil {
		return err
	}

	cpuCount, err := countProfiles(compDir, "cpu*.pprof")
	if err != nil {
		return err
	}

	fmt.Fprintf(report, "**Profiles Collected:** %d\n", heapCount)

	// Heap analysis
	if heapCount > 0 {
		if err := a.analyzeHeap(report, compDir, heapCount); err != nil {
			return err
		}
	}

	// CPU analysis
	if cpuCount > 0 {
		if err := a.analyzeCPU(report, compDir, cpuCount); err != nil {
			return err
		}
	}

	return nil
}

// analyzeHeap analyzes heap profiles
func (a *Analyzer) analyzeHeap(report *os.File, compDir string, count int) error {
	// Find peak profile
	peakProfile, peakSize, err := findPeakProfile(compDir, "heap*.pprof")
	if err != nil {
		return err
	}

	peakMemory, _ := a.getPeakMemory(compDir)

	fmt.Fprintf(report, "**Peak Profile:** %s (%s)\n", filepath.Base(peakProfile), peakSize)
	fmt.Fprintf(report, "**Peak Memory Usage:** %s\n\n", peakMemory)

	// Memory growth table
	if err := a.writeMemoryGrowthTable(report, compDir); err != nil {
		return err
	}

	// Top allocators
	fmt.Fprintf(report, "### Top Memory Allocators (Peak Profile)\n\n")
	fmt.Fprintf(report, "```\n")
	if err := a.generateTopReport(report, peakProfile, 20); err != nil {
		return err
	}
	fmt.Fprintf(report, "```\n\n")

	// OpenAPI allocations
	fmt.Fprintf(report, "### OpenAPI-Related Allocations\n\n")
	fmt.Fprintf(report, "```\n")
	if err := a.generateFilteredReport(report, peakProfile, "openapi", 20); err != nil {
		fmt.Fprintf(report, "No OpenAPI allocations found\n")
	}
	fmt.Fprintf(report, "```\n\n\n")

	// Growth analysis
	baseProfile := filepath.Join(compDir, "heap0.pprof")
	if _, err := os.Stat(baseProfile); err == nil {
		if err := a.analyzeGrowth(report, compDir, baseProfile, peakProfile); err != nil {
			return err
		}
	}

	return nil
}

// analyzeCPU analyzes CPU profiles
func (a *Analyzer) analyzeCPU(report *os.File, compDir string, count int) error {
	peakProfile, peakSize, err := findPeakProfile(compDir, "cpu*.pprof")
	if err != nil {
		return err
	}

	// Get total CPU time
	cpuTotal, _ := a.getCPUTotal(compDir, peakProfile)

	fmt.Fprintf(report, "\n### CPU Profile Analysis\n\n")
	fmt.Fprintf(report, "**CPU Profiles Collected:** %d\n", count)
	fmt.Fprintf(report, "**Peak CPU Profile:** %s (%s)\n", filepath.Base(peakProfile), peakSize)
	fmt.Fprintf(report, "**Total CPU Time:** %s\n\n", cpuTotal)

	fmt.Fprintf(report, "#### Top CPU Consumers (Peak Profile)\n\n")
	fmt.Fprintf(report, "```\n")
	if err := a.generateTopReport(report, peakProfile, 20); err != nil {
		return err
	}
	fmt.Fprintf(report, "```\n\n")

	fmt.Fprintf(report, "#### CPU-Intensive Functions\n\n")
	fmt.Fprintf(report, "```\n")
	if err := a.generateFilteredReport(report, peakProfile, "Reconcile|sync|watch|cache|list", 20); err != nil {
		fmt.Fprintf(report, "No reconciliation functions found in top CPU consumers\n")
	}
	fmt.Fprintf(report, "```\n\n")

	fmt.Fprintf(report, "#### JSON/Serialization CPU Usage\n\n")
	fmt.Fprintf(report, "```\n")
	if err := a.generateFilteredReport(report, peakProfile, "json|unmarshal|decode|marshal|encode", 15); err != nil {
		fmt.Fprintf(report, "No significant JSON/serialization CPU usage detected\n")
	}
	fmt.Fprintf(report, "```\n\n")

	return nil
}

// analyzeGrowth analyzes memory growth from base to peak
func (a *Analyzer) analyzeGrowth(report *os.File, compDir, baseProfile, peakProfile string) error {
	fmt.Fprintf(report, "### Memory Growth Analysis (Baseline to Peak)\n\n")
	fmt.Fprintf(report, "#### Top Growth Contributors\n\n")
	fmt.Fprintf(report, "```\n")

	// Generate diff report
	if err := a.generateDiffReport(report, baseProfile, peakProfile, 20); err != nil {
		return err
	}
	fmt.Fprintf(report, "```\n\n")

	// OpenAPI growth
	fmt.Fprintf(report, "#### OpenAPI Growth\n\n")
	fmt.Fprintf(report, "```\n")
	if err := a.generateDiffFilteredReport(report, baseProfile, peakProfile, "openapi", 20); err != nil {
		fmt.Fprintf(report, "No OpenAPI growth detected\n")
	}
	fmt.Fprintf(report, "```\n\n")

	// JSON deserialization growth
	fmt.Fprintf(report, "#### JSON Deserialization Growth\n\n")
	fmt.Fprintf(report, "```\n")
	if err := a.generateDiffFilteredReport(report, baseProfile, peakProfile, "json|unmarshal|decode", 20); err != nil {
		fmt.Fprintf(report, "No JSON deserialization growth detected\n")
	}
	fmt.Fprintf(report, "```\n\n")

	// Dynamic client growth
	fmt.Fprintf(report, "#### Dynamic Client Growth\n\n")
	fmt.Fprintf(report, "```\n")
	if err := a.generateDiffFilteredReport(report, baseProfile, peakProfile, "dynamic|client-go", 20); err != nil {
		fmt.Fprintf(report, "No dynamic client growth detected\n")
	}
	fmt.Fprintf(report, "```\n\n")

	return nil
}

// writeMemoryGrowthTable writes the memory growth table
func (a *Analyzer) writeMemoryGrowthTable(report *os.File, compDir string) error {
	fmt.Fprintf(report, "### Memory Growth\n\n")
	fmt.Fprintf(report, "| Snapshot | File Size | Growth from Previous |\n")
	fmt.Fprintf(report, "|----------|-----------|---------------------|\n")

	profiles, err := filepath.Glob(filepath.Join(compDir, "heap*.pprof"))
	if err != nil {
		return err
	}

	// Sort profiles numerically by extracting the number from the filename
	profileNumberRe := regexp.MustCompile(`(heap|cpu)(\d+)\.pprof`)
	sort.Slice(profiles, func(i, j int) bool {
		matchI := profileNumberRe.FindStringSubmatch(filepath.Base(profiles[i]))
		matchJ := profileNumberRe.FindStringSubmatch(filepath.Base(profiles[j]))
		if len(matchI) < 3 || len(matchJ) < 3 {
			return profiles[i] < profiles[j] // fallback to lexical sort
		}
		numI, errI := strconv.Atoi(matchI[2])
		numJ, errJ := strconv.Atoi(matchJ[2])
		if errI != nil || errJ != nil {
			return profiles[i] < profiles[j] // fallback to lexical sort
		}
		return numI < numJ
	})

	var prevSize int64
	for i, profile := range profiles {
		info, err := os.Stat(profile)
		if err != nil {
			continue
		}

		size := info.Size()
		growth := "baseline"
		if i > 0 {
			diff := size - prevSize
			if diff == 0 {
				growth = "0"
			} else if diff > 0 {
				growth = fmt.Sprintf("+%dK", diff/1024)
			} else {
				growth = fmt.Sprintf("%dK", diff/1024)
			}
		}

		fmt.Fprintf(report, "| %s | %dK | %s |\n",
			filepath.Base(profile), size/1024, growth)
		prevSize = size
	}

	fmt.Fprintf(report, "\n")
	return nil
}

// writeRecommendations writes the recommendations section
func (a *Analyzer) writeRecommendations(report *os.File) {
	fmt.Fprintf(report, "---\n\n")
	fmt.Fprintf(report, "## Recommendations\n\n")
	fmt.Fprintf(report, "Based on the analysis above, consider:\n\n")
	fmt.Fprintf(report, "1. **OpenAPI Schema Caching**: If OpenAPI allocations are significant, implement caching\n")
	fmt.Fprintf(report, "2. **Informer Optimization**: Review and deduplicate informer creation\n")
	fmt.Fprintf(report, "3. **List Operation Limits**: Add pagination or field selectors to reduce list overhead\n")
	fmt.Fprintf(report, "4. **JSON Optimization**: Consider using typed clients instead of unstructured where possible\n\n\n")
}

// generateTopReport generates a top allocation/CPU report
func (a *Analyzer) generateTopReport(w io.Writer, profilePath string, limit int) error {
	cmd := exec.Command("go", "tool", "pprof", "-top", "-lines", "-nodecount="+fmt.Sprintf("%d", limit), profilePath)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to run pprof: %w", err)
	}
	fmt.Fprint(w, string(output))
	return nil
}

// generateFilteredReport generates a report filtered by pattern
func (a *Analyzer) generateFilteredReport(w io.Writer, profilePath, pattern string, limit int) error {
	cmd := exec.Command("go", "tool", "pprof", "-text", "-lines", "-nodecount=100", profilePath)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to run pprof: %w", err)
	}

	// Filter output by pattern
	filtered := grepLines(string(output), pattern)
	lines := strings.Split(filtered, "\n")

	// Include headers from original output
	fullOutput := string(output)
	fullLines := strings.Split(fullOutput, "\n")
	var result []string

	// Add header lines
	for _, line := range fullLines {
		if strings.HasPrefix(strings.TrimSpace(line), "File:") ||
			strings.HasPrefix(strings.TrimSpace(line), "Type:") ||
			strings.HasPrefix(strings.TrimSpace(line), "Time:") ||
			strings.HasPrefix(strings.TrimSpace(line), "Showing") ||
			strings.HasPrefix(strings.TrimSpace(line), "Dropped") ||
			(strings.Contains(line, "flat") && strings.Contains(line, "sum%")) {
			result = append(result, line)
		}
	}

	// Add matched lines
	count := 0
	for _, line := range lines {
		if line != "" && count < limit {
			result = append(result, line)
			count++
		}
	}

	if len(result) <= 6 { // Just headers
		return fmt.Errorf("no matches found")
	}

	fmt.Fprint(w, strings.Join(result, "\n")+"\n")
	return nil
}

// generateDiffReport generates a differential report
func (a *Analyzer) generateDiffReport(w io.Writer, basePath, profilePath string, limit int) error {
	cmd := exec.Command("go", "tool", "pprof", "-top", "-lines", "-nodecount="+fmt.Sprintf("%d", limit), "-base="+basePath, profilePath)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to run pprof: %w", err)
	}
	fmt.Fprint(w, string(output))
	return nil
}

// generateDiffFilteredReport generates a filtered differential report
func (a *Analyzer) generateDiffFilteredReport(w io.Writer, basePath, profilePath, pattern string, limit int) error {
	cmd := exec.Command("go", "tool", "pprof", "-text", "-lines", "-nodecount=100", "-base="+basePath, profilePath)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to run pprof: %w", err)
	}

	// Filter output by pattern
	filtered := grepLines(string(output), pattern)
	lines := strings.Split(filtered, "\n")

	// Include headers from original output
	fullOutput := string(output)
	fullLines := strings.Split(fullOutput, "\n")
	var result []string

	// Add header lines
	for _, line := range fullLines {
		if strings.HasPrefix(strings.TrimSpace(line), "File:") ||
			strings.HasPrefix(strings.TrimSpace(line), "Type:") ||
			strings.HasPrefix(strings.TrimSpace(line), "Time:") ||
			strings.HasPrefix(strings.TrimSpace(line), "Showing") ||
			strings.HasPrefix(strings.TrimSpace(line), "Dropped") ||
			(strings.Contains(line, "flat") && strings.Contains(line, "sum%")) {
			result = append(result, line)
		}
	}

	// Add matched lines
	count := 0
	for _, line := range lines {
		if line != "" && count < limit {
			result = append(result, line)
			count++
		}
	}

	if len(result) <= 6 {
		return fmt.Errorf("no matches found")
	}

	fmt.Fprint(w, strings.Join(result, "\n")+"\n")
	return nil
}

// getPeakMemory extracts peak memory from a profile
func (a *Analyzer) getPeakMemory(compDir string) (string, error) {
	peakProfile, _, err := findPeakProfile(compDir, "heap*.pprof")
	if err != nil {
		return "", err
	}

	f, err := os.Open(peakProfile)
	if err != nil {
		return "", err
	}
	defer f.Close()

	p, err := profile.Parse(f)
	if err != nil {
		return "", err
	}

	// Sum up total memory
	var total int64
	for _, sample := range p.Sample {
		if len(sample.Value) > 1 {
			total += sample.Value[1] // inuse_space
		}
	}

	return formatBytes(total), nil
}

// getCPUTotal extracts total CPU time from a profile
func (a *Analyzer) getCPUTotal(compDir, profilePath string) (string, error) {
	f, err := os.Open(profilePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	p, err := profile.Parse(f)
	if err != nil {
		return "", err
	}

	// Sum up total samples
	var total int64
	for _, sample := range p.Sample {
		if len(sample.Value) > 0 {
			total += sample.Value[0]
		}
	}

	// Convert to seconds based on period
	period := p.Period
	if period == 0 {
		period = 1
	}
	seconds := float64(total) * float64(period) / 1e9

	return fmt.Sprintf("of %.2fs", seconds), nil
}

// formatBytes formats byte count in human-readable format
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("of %.2f%cB", float64(b)/float64(div), "kMGTPE"[exp])
}

// Helper functions

func countProfiles(compDir, pattern string) (int, error) {
	profiles, err := filepath.Glob(filepath.Join(compDir, pattern))
	if err != nil {
		return 0, err
	}
	return len(profiles), nil
}

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

func grepLines(text, pattern string) string {
	re := regexp.MustCompile(fmt.Sprintf("(?i)%s", pattern))
	lines := strings.Split(text, "\n")
	var matched []string

	for _, line := range lines {
		if re.MatchString(line) {
			matched = append(matched, line)
		}
	}

	return strings.Join(matched, "\n")
}
