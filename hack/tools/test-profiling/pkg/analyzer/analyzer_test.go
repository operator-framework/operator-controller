package analyzer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/operator-framework/operator-controller/hack/tools/test-profiling/pkg/config"
)

func TestGrepLines(t *testing.T) {
	text := `line one with openapi
line two normal
line three with OpenAPI client
line four normal
line five with json parser`

	tests := []struct {
		name     string
		pattern  string
		expected int
	}{
		{"case insensitive openapi", "openapi", 2},
		{"case insensitive json", "json", 1},
		{"no matches", "notfound", 0},
		{"multiple patterns", "openapi|json", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := grepLines(text, tt.pattern)
			lines := strings.Split(result, "\n")
			if result == "" {
				lines = []string{}
			}
			count := 0
			for _, line := range lines {
				if line != "" {
					count++
				}
			}
			if count != tt.expected {
				t.Errorf("Expected %d matches, got %d", tt.expected, count)
			}
		})
	}
}

func TestCountProfiles(t *testing.T) {
	// Create temporary directory
	tmpDir := t.TempDir()

	// Create test profile files
	for i := 0; i < 5; i++ {
		f, err := os.Create(filepath.Join(tmpDir, "heap"+string(rune('0'+i))+".pprof"))
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
	}

	// Create non-matching files
	f, err := os.Create(filepath.Join(tmpDir, "cpu0.pprof"))
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	count, err := countProfiles(tmpDir, "heap*.pprof")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if count != 5 {
		t.Errorf("Expected 5 heap profiles, got %d", count)
	}

	count, err = countProfiles(tmpDir, "cpu*.pprof")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if count != 1 {
		t.Errorf("Expected 1 cpu profile, got %d", count)
	}

	// Test non-existent directory (glob returns 0 results, not an error)
	count, err = countProfiles("/nonexistent/dir", "*.pprof")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 profiles for non-existent directory, got %d", count)
	}
}

func TestFindPeakProfile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files with different sizes
	testFiles := map[string]int{
		"heap0.pprof": 100,
		"heap1.pprof": 500,
		"heap2.pprof": 300,
		"heap3.pprof": 1000, // This should be the peak
		"heap4.pprof": 200,
	}

	for name, size := range testFiles {
		path := filepath.Join(tmpDir, name)
		data := make([]byte, size)
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}

	peakPath, sizeStr, err := findPeakProfile(tmpDir, "heap*.pprof")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if filepath.Base(peakPath) != "heap3.pprof" {
		t.Errorf("Expected peak profile heap3.pprof, got %s", filepath.Base(peakPath))
	}

	// Size should be 1000 bytes = 0K (integer division)
	if sizeStr != "0K" {
		t.Errorf("Expected size 0K, got %s", sizeStr)
	}

	// Test with larger file to get non-zero K
	largeFile := filepath.Join(tmpDir, "heap5.pprof")
	data := make([]byte, 5*1024) // 5KB
	if err := os.WriteFile(largeFile, data, 0644); err != nil {
		t.Fatal(err)
	}

	peakPath, sizeStr, err = findPeakProfile(tmpDir, "heap*.pprof")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if filepath.Base(peakPath) != "heap5.pprof" {
		t.Errorf("Expected peak profile heap5.pprof, got %s", filepath.Base(peakPath))
	}

	if sizeStr != "5K" {
		t.Errorf("Expected size 5K, got %s", sizeStr)
	}
}

func TestFindPeakProfile_NoFiles(t *testing.T) {
	tmpDir := t.TempDir()

	_, _, err := findPeakProfile(tmpDir, "heap*.pprof")
	if err == nil {
		t.Error("Expected error when no profiles found")
	}
}

func TestNewAnalyzer(t *testing.T) {
	cfg := &config.Config{
		Name:      "test",
		OutputDir: "/tmp/profiles",
	}

	a := NewAnalyzer(cfg)

	if a == nil {
		t.Fatal("Expected non-nil Analyzer")
	}

	if a.config != cfg {
		t.Error("Expected config to be set")
	}
}

func TestWriteMemoryGrowthTable(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test profile files with different sizes
	profiles := []struct {
		name string
		size int
	}{
		{"heap0.pprof", 10 * 1024}, // 10K
		{"heap1.pprof", 20 * 1024}, // 20K
		{"heap2.pprof", 20 * 1024}, // 20K (no growth)
		{"heap3.pprof", 15 * 1024}, // 15K (shrink)
		{"heap4.pprof", 25 * 1024}, // 25K
	}

	for _, p := range profiles {
		path := filepath.Join(tmpDir, p.name)
		data := make([]byte, p.size)
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := &config.Config{
		Name:      "test",
		OutputDir: tmpDir,
	}
	a := NewAnalyzer(cfg)

	// Create output file
	reportPath := filepath.Join(tmpDir, "test_report.md")
	report, err := os.Create(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	defer report.Close()

	err = a.writeMemoryGrowthTable(report, tmpDir)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Read the report and verify content
	content, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}

	contentStr := string(content)

	// Check for table header
	if !strings.Contains(contentStr, "Memory Growth") {
		t.Error("Expected 'Memory Growth' header")
	}

	if !strings.Contains(contentStr, "Snapshot") {
		t.Error("Expected table column headers")
	}

	// Check for baseline
	if !strings.Contains(contentStr, "baseline") {
		t.Error("Expected 'baseline' for first entry")
	}

	// Check for growth indicators
	if !strings.Contains(contentStr, "+") {
		t.Error("Expected growth indicator '+' in table")
	}
}

// Note: Testing Analyze(), analyzeHeap(), analyzeCPU() requires real pprof files
// and go tool pprof to be available. These would be integration tests.
//
// Example integration test structure:
// func TestAnalyze_Integration(t *testing.T) {
//     if testing.Short() {
//         t.Skip("Skipping integration test")
//     }
//     // Setup test directory with real pprof files
//     // Run analyzer
//     // Verify report generation
// }
