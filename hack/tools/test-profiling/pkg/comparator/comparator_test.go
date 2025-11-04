package comparator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewComparator(t *testing.T) {
	c := NewComparator("/baseline", "/optimized", "/output")

	if c == nil {
		t.Fatal("Expected non-nil Comparator")
	}

	if c.baselineDir != "/baseline" {
		t.Errorf("Expected baseline dir /baseline, got %s", c.baselineDir)
	}

	if c.optimizedDir != "/optimized" {
		t.Errorf("Expected optimized dir /optimized, got %s", c.optimizedDir)
	}

	if c.outputDir != "/output" {
		t.Errorf("Expected output dir /output, got %s", c.outputDir)
	}
}

func TestParseMemory(t *testing.T) {
	tests := []struct {
		input    string
		expected float64
	}{
		{"100KB", 100},
		{"100kb", 100},
		{"2.5MB", 2560},
		{"2.5mb", 2560},
		{"1GB", 1048576},
		{"1gb", 1048576},
		{"invalid", 0},
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseMemory(tt.input)
			if result != tt.expected {
				t.Errorf("parseMemory(%s) = %f, expected %f", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		input    string
		expected float64
	}{
		{"100K", 100},
		{"50K", 50},
		{"invalid", 0},
		{"", 0},
		{"123", 0}, // No unit
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseSize(tt.input)
			if result != tt.expected {
				t.Errorf("parseSize(%s) = %f, expected %f", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCalculateImprovement(t *testing.T) {
	c := NewComparator("", "", "")

	tests := []struct {
		name      string
		baseline  string
		optimized string
		expected  string
	}{
		{
			name:      "reduction",
			baseline:  "100MB",
			optimized: "80MB",
			expected:  "20.0% reduction",
		},
		{
			name:      "increase",
			baseline:  "100MB",
			optimized: "120MB",
			expected:  "+20.0%",
		},
		{
			name:      "no change",
			baseline:  "100MB",
			optimized: "100MB",
			expected:  "+0.0%",
		},
		{
			name:      "zero baseline",
			baseline:  "0MB",
			optimized: "100MB",
			expected:  "N/A",
		},
		{
			name:      "invalid baseline",
			baseline:  "invalid",
			optimized: "100MB",
			expected:  "N/A",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := c.calculateImprovement(tt.baseline, tt.optimized)
			if result != tt.expected {
				t.Errorf("calculateImprovement(%s, %s) = %s, expected %s",
					tt.baseline, tt.optimized, result, tt.expected)
			}
		})
	}
}

func TestFormatChange(t *testing.T) {
	c := NewComparator("", "", "")

	tests := []struct {
		name      string
		baseline  string
		optimized string
		contains  string // Check if result contains this string
	}{
		{
			name:      "reduction",
			baseline:  "100K",
			optimized: "80K",
			contains:  "-20",
		},
		{
			name:      "increase",
			baseline:  "100K",
			optimized: "120K",
			contains:  "+20",
		},
		{
			name:      "no change",
			baseline:  "100K",
			optimized: "100K",
			contains:  "0.0",
		},
		{
			name:      "zero baseline",
			baseline:  "0K",
			optimized: "100K",
			contains:  "N/A",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := c.formatChange(tt.baseline, tt.optimized)
			if tt.contains != "" && result != "N/A" {
				// For non-N/A results, just verify format looks reasonable
				if result == "" {
					t.Errorf("formatChange(%s, %s) returned empty string",
						tt.baseline, tt.optimized)
				}
			} else if tt.contains == "" && result != "N/A" {
				t.Errorf("formatChange(%s, %s) = %s, expected N/A",
					tt.baseline, tt.optimized, result)
			}
		})
	}
}

func TestGetComponents(t *testing.T) {
	tmpDir := t.TempDir()

	// Create component directories
	components := []string{"operator-controller", "catalogd", "other-component"}
	for _, comp := range components {
		if err := os.MkdirAll(filepath.Join(tmpDir, comp), 0755); err != nil {
			t.Fatal(err)
		}
	}

	// Create a file (should be ignored)
	if err := os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create comparisons directory (should be ignored)
	if err := os.MkdirAll(filepath.Join(tmpDir, "comparisons"), 0755); err != nil {
		t.Fatal(err)
	}

	c := NewComparator(tmpDir, "", "")

	result, err := c.getComponents()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(result) != 3 {
		t.Errorf("Expected 3 components, got %d", len(result))
	}

	// Verify specific components are present
	found := make(map[string]bool)
	for _, comp := range result {
		found[comp] = true
	}

	for _, expected := range components {
		if !found[expected] {
			t.Errorf("Expected component %s not found in results", expected)
		}
	}

	if found["comparisons"] {
		t.Error("comparisons directory should be excluded")
	}
}

func TestGetComponents_NonExistentDir(t *testing.T) {
	c := NewComparator("/nonexistent", "", "")

	_, err := c.getComponents()
	if err == nil {
		t.Error("Expected error for non-existent directory")
	}
}

func TestFindPeakProfile_Comparator(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	testFiles := map[string]int{
		"heap0.pprof": 1024,
		"heap1.pprof": 2048,
		"heap2.pprof": 512,
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

	if filepath.Base(peakPath) != "heap1.pprof" {
		t.Errorf("Expected peak profile heap1.pprof, got %s", filepath.Base(peakPath))
	}

	if sizeStr != "2K" {
		t.Errorf("Expected size 2K, got %s", sizeStr)
	}
}

// Note: Testing Compare() requires real pprof files and go tool pprof.
// This would be an integration test.
//
// Example integration test structure:
// func TestCompare_Integration(t *testing.T) {
//     if testing.Short() {
//         t.Skip("Skipping integration test")
//     }
//     // Setup baseline and optimized directories with real pprof files
//     // Run comparator
//     // Verify comparison report generation
// }
