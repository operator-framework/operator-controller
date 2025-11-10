package collector

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/operator-framework/operator-controller/hack/tools/test-profiling/pkg/config"
)

func TestNewCollector(t *testing.T) {
	cfg := &config.Config{
		Name:      "test",
		OutputDir: "/tmp/profiles",
		Components: []config.ComponentConfig{
			{Name: "operator-controller", Namespace: "olmv1-system", Deployment: "operator-controller-controller-manager", Port: 6060},
		},
	}

	c := NewCollector(cfg)

	if c == nil {
		t.Fatal("Expected non-nil Collector")
	}

	if c.config != cfg {
		t.Error("Expected config to be set")
	}

	if len(c.forwarders) != 0 {
		t.Error("Expected empty forwarders map")
	}

	if len(c.heapCounter) != 0 {
		t.Error("Expected empty heapCounter map")
	}

	if len(c.cpuCounter) != 0 {
		t.Error("Expected empty cpuCounter map")
	}
}

func TestSetupDirectories(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Name:      "test-run",
		OutputDir: tmpDir,
		Components: []config.ComponentConfig{
			{Name: "operator-controller"},
			{Name: "catalogd"},
		},
	}

	c := NewCollector(cfg)

	err := c.setupDirectories()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify directories were created
	for _, comp := range cfg.Components {
		dir := filepath.Join(tmpDir, "test-run", comp.Name)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			t.Errorf("Expected directory %s to exist", dir)
		}
	}
}

func TestWritePIDFile(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Name:      "test-run",
		OutputDir: tmpDir,
	}

	c := NewCollector(cfg)

	// Create profile directory first
	if err := os.MkdirAll(cfg.ProfileDir(), 0755); err != nil {
		t.Fatal(err)
	}

	err := c.writePIDFile()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	pidFile := cfg.PIDFile()
	if _, err := os.Stat(pidFile); os.IsNotExist(err) {
		t.Error("Expected PID file to exist")
	}

	// Read and verify PID
	content, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}

	if len(content) == 0 {
		t.Error("Expected non-empty PID file")
	}
}

func TestCleanup(t *testing.T) {
	cfg := &config.Config{
		Name:      "test",
		OutputDir: "/tmp/profiles",
	}

	c := NewCollector(cfg)

	// Add some mock forwarders
	c.forwarders["test1"] = nil
	c.forwarders["test2"] = nil

	if len(c.forwarders) != 2 {
		t.Fatalf("Expected 2 forwarders, got %d", len(c.forwarders))
	}

	c.cleanup()

	if len(c.forwarders) != 0 {
		t.Errorf("Expected forwarders to be cleared, got %d", len(c.forwarders))
	}
}

func TestHeapCounter(t *testing.T) {
	cfg := &config.Config{
		Name:      "test",
		OutputDir: "/tmp/profiles",
	}

	c := NewCollector(cfg)

	// Initially should be 0
	if c.heapCounter["operator-controller"] != 0 {
		t.Error("Expected initial heap counter to be 0")
	}

	// Increment
	c.heapCounter["operator-controller"]++
	if c.heapCounter["operator-controller"] != 1 {
		t.Error("Expected heap counter to be 1 after increment")
	}

	// Increment again
	c.heapCounter["operator-controller"]++
	if c.heapCounter["operator-controller"] != 2 {
		t.Error("Expected heap counter to be 2 after second increment")
	}
}

func TestCPUCounter(t *testing.T) {
	cfg := &config.Config{
		Name:      "test",
		OutputDir: "/tmp/profiles",
	}

	c := NewCollector(cfg)

	// Initially should be 0
	if c.cpuCounter["catalogd"] != 0 {
		t.Error("Expected initial CPU counter to be 0")
	}

	// Increment
	c.cpuCounter["catalogd"]++
	if c.cpuCounter["catalogd"] != 1 {
		t.Error("Expected CPU counter to be 1 after increment")
	}
}

func TestLastCPUStart(t *testing.T) {
	cfg := &config.Config{
		Name:      "test",
		OutputDir: "/tmp/profiles",
	}

	c := NewCollector(cfg)

	// Record start time
	now := time.Now()
	c.lastCPUStart["operator-controller"] = now

	// Verify we can retrieve it
	if recorded, ok := c.lastCPUStart["operator-controller"]; !ok {
		t.Error("Expected lastCPUStart to be recorded")
	} else if recorded != now {
		t.Error("Expected recorded time to match")
	}

	// Check time-based logic
	time.Sleep(10 * time.Millisecond)
	elapsed := time.Since(c.lastCPUStart["operator-controller"])
	if elapsed < 10*time.Millisecond {
		t.Error("Expected elapsed time to be at least 10ms")
	}
}

func TestStopChannel(t *testing.T) {
	cfg := &config.Config{
		Name:      "test",
		OutputDir: "/tmp/profiles",
	}

	c := NewCollector(cfg)

	// Verify channel is created
	if c.stopChan == nil {
		t.Error("Expected stopChan to be initialized")
	}

	// Verify we can close it
	close(c.stopChan)

	// Verify it's closed by trying to read
	select {
	case <-c.stopChan:
		// Expected - channel is closed
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected stopChan to be closed")
	}
}

// Note: Testing Start(), Stop(), CollectOnce(), and the actual profile collection
// requires a real Kubernetes cluster with deployments and pprof endpoints.
// These would be integration tests.
//
// Example integration test structure:
// func TestCollector_Integration(t *testing.T) {
//     if testing.Short() {
//         t.Skip("Skipping integration test")
//     }
//     // Setup test cluster with components exposing pprof
//     // Start collector
//     // Verify profiles are collected
//     // Stop collector
//     // Verify cleanup
// }
