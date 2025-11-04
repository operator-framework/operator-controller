package collector

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/operator-framework/operator-controller/hack/tools/test-profiling/pkg/config"
	"github.com/operator-framework/operator-controller/hack/tools/test-profiling/pkg/kubernetes"
)

// Collector handles profile collection
type Collector struct {
	config      *config.Config
	forwarders  map[string]*kubernetes.PortForwarder
	localPorts  map[string]int // component name -> assigned local port
	stopChan    chan struct{}
	heapCounter map[string]int
	cpuCounter  map[string]int
	startTime   time.Time
	mu          sync.Mutex
}

// NewCollector creates a new profile collector
func NewCollector(cfg *config.Config) *Collector {
	return &Collector{
		config:      cfg,
		forwarders:  make(map[string]*kubernetes.PortForwarder),
		localPorts:  make(map[string]int),
		stopChan:    make(chan struct{}),
		heapCounter: make(map[string]int),
		cpuCounter:  make(map[string]int),
	}
}

// Start starts the profile collection daemon
func (c *Collector) Start(ctx context.Context) error {
	// Setup directories
	if err := c.setupDirectories(); err != nil {
		return err
	}

	// Write PID file early so stop command works even if waiting for cluster
	if err := c.writePIDFile(); err != nil {
		return err
	}

	// Wait for namespaces (collect unique namespaces)
	namespaces := make(map[string]bool)
	for _, comp := range c.config.Components {
		namespaces[comp.Namespace] = true
	}

	// Wait for each unique namespace (15 minute timeout - cluster may need to start up)
	for ns := range namespaces {
		fmt.Printf("⏳ Waiting for namespace %s (timeout: 15m)...\n", ns)
		if err := kubernetes.WaitForNamespace(ctx, ns, 15*time.Minute); err != nil {
			c.cleanup()
			return fmt.Errorf("❌ Timeout waiting for namespace %s: %w\n"+
				"   The cluster may not be running or the namespace doesn't exist.\n"+
				"   If running e2e tests, ensure the cluster is created first.", ns, err)
		}
		fmt.Printf("✅ Namespace %s is ready\n", ns)
	}

	// Wait for deployments and setup port-forwarding
	for _, comp := range c.config.Components {
		fmt.Printf("⏳ Waiting for %s deployment in %s (timeout: 10m)...\n", comp.Name, comp.Namespace)
		if err := kubernetes.WaitForDeployment(ctx, comp.Namespace, comp.Deployment, 10*time.Minute); err != nil {
			return fmt.Errorf("❌ Timeout waiting for deployment %s in namespace %s: %w\n"+
				"   The deployment may not exist or is not becoming ready.", comp.Deployment, comp.Namespace, err)
		}
		fmt.Printf("✅ Deployment %s is ready\n", comp.Name)

		fmt.Printf("🔌 Setting up port-forward for %s...\n", comp.Name)

		// Use 0 for local port to get a dynamically assigned port
		pf := kubernetes.NewPortForwarder(comp.Namespace, comp.Deployment, comp.Port, 0)
		if err := pf.Start(ctx); err != nil {
			c.cleanup()
			return err
		}
		c.forwarders[comp.Name] = pf
		c.localPorts[comp.Name] = pf.GetLocalPort()

		fmt.Printf("✅ Port-forward ready: localhost:%d -> %s:%d\n",
			c.localPorts[comp.Name], comp.Name, comp.Port)
	}

	c.startTime = time.Now()
	fmt.Printf("✅ Profiler started at %s\n", c.startTime.Format(time.RFC3339))
	fmt.Printf("📊 Collecting profiles every %v\n", c.config.Interval)
	if c.config.CollectCPU() {
		fmt.Printf("⏱️  CPU profile duration: %v\n", c.config.CPUDuration)
	}
	fmt.Printf("📁 Output directory: %s\n", c.config.ProfileDir())

	// Start separate collection loops for heap and CPU
	if c.config.CollectHeap() {
		go c.collectHeapLoop(ctx)
	}
	if c.config.CollectCPU() {
		go c.collectCPULoop(ctx)
	}

	return nil
}

// Stop stops the collector
func (c *Collector) Stop() error {
	close(c.stopChan)
	c.cleanup()

	// Remove PID file
	_ = os.Remove(c.config.PIDFile())

	duration := time.Since(c.startTime)
	fmt.Printf("\n✅ Profiling stopped after %v\n", duration.Round(time.Second))

	return nil
}

// CollectOnce collects a single snapshot
func (c *Collector) CollectOnce(ctx context.Context) error {
	// Setup port-forwarding for all components
	for _, comp := range c.config.Components {
		pf := kubernetes.NewPortForwarder(comp.Namespace, comp.Deployment, comp.Port, 0)
		if err := pf.Start(ctx); err != nil {
			c.cleanup()
			return err
		}
		c.forwarders[comp.Name] = pf
		c.localPorts[comp.Name] = pf.GetLocalPort()
		defer pf.Stop()
	}

	// Setup directories
	if err := c.setupDirectories(); err != nil {
		return err
	}

	// Collect from all components
	for _, comp := range c.config.Components {
		localPort := c.localPorts[comp.Name]
		if c.config.CollectHeap() {
			if err := c.collectHeapProfile(comp.Name, localPort); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to collect heap profile for %s: %v\n", comp.Name, err)
			}
		}
		if c.config.CollectCPU() {
			if err := c.collectCPUProfile(comp.Name, localPort); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to collect CPU profile for %s: %v\n", comp.Name, err)
			}
		}
	}

	return nil
}

// collectHeapLoop runs the heap collection loop with precise timing
func (c *Collector) collectHeapLoop(ctx context.Context) {
	ticker := time.NewTicker(c.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopChan:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Collect heap profiles for all components
			for _, comp := range c.config.Components {
				localPort := c.localPorts[comp.Name]
				if err := c.collectHeapProfile(comp.Name, localPort); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to collect heap profile for %s: %v\n", comp.Name, err)
				}
			}
		}
	}
}

// collectCPULoop runs the CPU collection loop with differential delays
func (c *Collector) collectCPULoop(ctx context.Context) {
	for {
		select {
		case <-c.stopChan:
			return
		case <-ctx.Done():
			return
		default:
			// Record start time for differential delay calculation
			startTime := time.Now()

			// Collect CPU profiles for all components
			for _, comp := range c.config.Components {
				localPort := c.localPorts[comp.Name]
				if err := c.collectCPUProfile(comp.Name, localPort); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to collect CPU profile for %s: %v\n", comp.Name, err)
				}
			}

			// Calculate how long the collection took
			elapsed := time.Since(startTime)

			// Calculate delay until next collection
			// If elapsed < interval, wait for (interval - elapsed)
			// This ensures collections happen precisely at the configured interval
			if elapsed < c.config.Interval {
				delay := c.config.Interval - elapsed
				select {
				case <-c.stopChan:
					return
				case <-ctx.Done():
					return
				case <-time.After(delay):
					// Continue to next collection
				}
			}
			// If elapsed >= interval, start next collection immediately
		}
	}
}

// collectHeapProfile collects a heap profile
func (c *Collector) collectHeapProfile(component string, port int) error {
	c.mu.Lock()
	counter := c.heapCounter[component]
	c.mu.Unlock()

	filename := fmt.Sprintf("heap%d.pprof", counter)
	outputPath := filepath.Join(c.config.ComponentDir(component), filename)

	url := fmt.Sprintf("http://localhost:%d/debug/pprof/heap", port)
	if err := c.downloadProfile(url, outputPath); err != nil {
		return err
	}

	c.mu.Lock()
	c.heapCounter[component]++
	c.mu.Unlock()

	fmt.Printf("📸 [%s] Collected heap profile: %s\n", component, filename)
	return nil
}

// collectCPUProfile collects a CPU profile
func (c *Collector) collectCPUProfile(component string, port int) error {
	c.mu.Lock()
	counter := c.cpuCounter[component]
	c.mu.Unlock()

	filename := fmt.Sprintf("cpu%d.pprof", counter)
	outputPath := filepath.Join(c.config.ComponentDir(component), filename)

	url := fmt.Sprintf("http://localhost:%d/debug/pprof/profile?seconds=%d", port, int(c.config.CPUDuration.Seconds()))
	if err := c.downloadProfile(url, outputPath); err != nil {
		return err
	}

	c.mu.Lock()
	c.cpuCounter[component]++
	c.mu.Unlock()

	fmt.Printf("📸 [%s] Collected CPU profile: %s\n", component, filename)
	return nil
}

// downloadProfile downloads a profile from a URL
func (c *Collector) downloadProfile(url, outputPath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download profile: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	if _, err := io.Copy(outFile, resp.Body); err != nil {
		return fmt.Errorf("failed to write profile: %w", err)
	}

	return nil
}

// setupDirectories creates the output directory structure
func (c *Collector) setupDirectories() error {
	for _, comp := range c.config.Components {
		dir := c.config.ComponentDir(comp.Name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}
	return nil
}

// writePIDFile writes the PID file
func (c *Collector) writePIDFile() error {
	pidFile := c.config.PIDFile()
	pid := fmt.Sprintf("%d", os.Getpid())
	if err := os.WriteFile(pidFile, []byte(pid), 0644); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	return nil
}

// cleanup stops all port forwarders
func (c *Collector) cleanup() {
	for name, pf := range c.forwarders {
		fmt.Printf("🔌 Stopping port-forward for %s...\n", name)
		if pf != nil {
			pf.Stop()
		}
	}
	c.forwarders = make(map[string]*kubernetes.PortForwarder)
}
