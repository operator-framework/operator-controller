package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config holds all configuration for profiling
type Config struct {
	// Collection interval
	Interval time.Duration

	// CPU profiling duration
	CPUDuration time.Duration

	// Profile mode: both, heap, cpu
	Mode string

	// Output directory
	OutputDir string

	// Test target (make target)
	TestTarget string

	// Profile name
	Name string

	// Components to profile
	Components []ComponentConfig
}

// ComponentConfig defines a component to profile
type ComponentConfig struct {
	Name       string
	Namespace  string
	Deployment string
	Port       int
}

// DefaultConfig returns configuration with defaults from environment
func DefaultConfig() *Config {
	// Parse components from environment or use defaults
	components, err := parseComponents()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to parse TEST_PROFILE_COMPONENTS: %v, using defaults\n", err)
		components = nil
	}

	if len(components) == 0 {
		// Use default components (operator-controller and catalogd in olmv1-system)
		components = []ComponentConfig{
			{
				Name:       "operator-controller",
				Namespace:  "olmv1-system",
				Deployment: "operator-controller-controller-manager",
				Port:       6060,
			},
			{
				Name:       "catalogd",
				Namespace:  "olmv1-system",
				Deployment: "catalogd-controller-manager",
				Port:       6060,
			},
		}
	}

	return &Config{
		Interval:    getDurationEnv("TEST_PROFILE_INTERVAL", 10*time.Second),
		CPUDuration: getDurationEnv("TEST_PROFILE_CPU_DURATION", 10*time.Second),
		Mode:        getEnv("TEST_PROFILE_MODE", "both"),
		OutputDir:   getEnv("TEST_PROFILE_DIR", "./test-profiles"),
		TestTarget:  getEnv("TEST_PROFILE_TEST_TARGET", "test-experimental-e2e"),
		Components:  components,
	}
}

// ProfileDir returns the full path to the profile directory
func (c *Config) ProfileDir() string {
	return filepath.Join(c.OutputDir, c.Name)
}

// ComponentDir returns the directory for a specific component
func (c *Config) ComponentDir(component string) string {
	return filepath.Join(c.ProfileDir(), component)
}

// PIDFile returns the path to the PID file
func (c *Config) PIDFile() string {
	return filepath.Join(c.ProfileDir(), ".profiler.pid")
}

// CollectHeap returns whether to collect heap profiles
func (c *Config) CollectHeap() bool {
	return c.Mode == "both" || c.Mode == "heap"
}

// CollectCPU returns whether to collect CPU profiles
func (c *Config) CollectCPU() bool {
	return c.Mode == "both" || c.Mode == "cpu"
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("profile name is required")
	}
	if c.Mode != "both" && c.Mode != "heap" && c.Mode != "cpu" {
		return fmt.Errorf("mode must be 'both', 'heap', or 'cpu', got: %s", c.Mode)
	}
	if c.Interval <= 0 {
		return fmt.Errorf("interval must be positive")
	}
	if c.CPUDuration <= 0 {
		return fmt.Errorf("CPU duration must be positive")
	}
	if len(c.Components) == 0 {
		return fmt.Errorf("at least one component must be configured")
	}
	for i, comp := range c.Components {
		if comp.Name == "" {
			return fmt.Errorf("component %d: name is required", i)
		}
		if comp.Namespace == "" {
			return fmt.Errorf("component %s: namespace is required", comp.Name)
		}
		if comp.Deployment == "" {
			return fmt.Errorf("component %s: deployment is required", comp.Name)
		}
		if comp.Port <= 0 || comp.Port > 65535 {
			return fmt.Errorf("component %s: port must be between 1 and 65535", comp.Name)
		}
	}
	return nil
}

// getEnv gets an environment variable or returns a default
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getDurationEnv gets a duration from environment (in seconds) or returns default
func getDurationEnv(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if seconds, err := strconv.Atoi(value); err == nil {
			return time.Duration(seconds) * time.Second
		}
	}
	return defaultValue
}

// parseComponents parses TEST_PROFILE_COMPONENTS environment variable
// Format: "name:namespace:deployment:port;name2:namespace2:deployment2:port2"
// Example: "operator-controller:olmv1:operator-controller-controller-manager:6060;catalogd:catalogd-system:catalogd-controller-manager:6060"
func parseComponents() ([]ComponentConfig, error) {
	componentsStr := os.Getenv("TEST_PROFILE_COMPONENTS")
	if componentsStr == "" {
		return nil, nil
	}

	var components []ComponentConfig
	parts := strings.Split(componentsStr, ";")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		fields := strings.Split(part, ":")
		if len(fields) != 4 {
			return nil, fmt.Errorf("invalid component format '%s': expected name:namespace:deployment:port", part)
		}

		port, err := strconv.Atoi(fields[3])
		if err != nil {
			return nil, fmt.Errorf("invalid port '%s' in component '%s': %w", fields[3], fields[0], err)
		}

		components = append(components, ComponentConfig{
			Name:       fields[0],
			Namespace:  fields[1],
			Deployment: fields[2],
			Port:       port,
		})
	}

	return components, nil
}
