package config

import (
	"os"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	// Clear environment
	os.Clearenv()

	cfg := DefaultConfig()

	if cfg.Interval != 10*time.Second {
		t.Errorf("Expected interval 10s, got %v", cfg.Interval)
	}

	if cfg.CPUDuration != 10*time.Second {
		t.Errorf("Expected CPU duration 10s, got %v", cfg.CPUDuration)
	}

	if cfg.Mode != "both" {
		t.Errorf("Expected mode both, got %s", cfg.Mode)
	}

	if cfg.OutputDir != "./test-profiles" {
		t.Errorf("Expected output dir ./test-profiles, got %s", cfg.OutputDir)
	}

	if cfg.TestTarget != "test-experimental-e2e" {
		t.Errorf("Expected test target test-experimental-e2e, got %s", cfg.TestTarget)
	}

	if len(cfg.Components) != 2 {
		t.Errorf("Expected 2 default components, got %d", len(cfg.Components))
	}

	// Check default components
	if cfg.Components[0].Name != "operator-controller" {
		t.Errorf("Expected first component to be operator-controller, got %s", cfg.Components[0].Name)
	}
	if cfg.Components[0].Namespace != "olmv1-system" {
		t.Errorf("Expected first component namespace to be olmv1-system, got %s", cfg.Components[0].Namespace)
	}
	if cfg.Components[1].Name != "catalogd" {
		t.Errorf("Expected second component to be catalogd, got %s", cfg.Components[1].Name)
	}
	if cfg.Components[1].Namespace != "olmv1-system" {
		t.Errorf("Expected second component namespace to be olmv1-system, got %s", cfg.Components[1].Namespace)
	}
}

func TestConfigFromEnvironment(t *testing.T) {
	os.Clearenv()
	os.Setenv("TEST_PROFILE_INTERVAL", "5")
	os.Setenv("TEST_PROFILE_CPU_DURATION", "15")
	os.Setenv("TEST_PROFILE_MODE", "heap")
	os.Setenv("TEST_PROFILE_DIR", "./custom-dir")
	os.Setenv("TEST_PROFILE_COMPONENTS", "app1:ns1:deploy1:8080")
	os.Setenv("TEST_PROFILE_TEST_TARGET", "test-e2e")

	cfg := DefaultConfig()

	if cfg.Interval != 5*time.Second {
		t.Errorf("Expected interval 5s, got %v", cfg.Interval)
	}

	if cfg.CPUDuration != 15*time.Second {
		t.Errorf("Expected CPU duration 15s, got %v", cfg.CPUDuration)
	}

	if cfg.Mode != "heap" {
		t.Errorf("Expected mode heap, got %s", cfg.Mode)
	}

	if cfg.OutputDir != "./custom-dir" {
		t.Errorf("Expected output dir ./custom-dir, got %s", cfg.OutputDir)
	}

	if cfg.TestTarget != "test-e2e" {
		t.Errorf("Expected test target test-e2e, got %s", cfg.TestTarget)
	}

	os.Clearenv()
}

func TestConfigValidation(t *testing.T) {
	validComponent := ComponentConfig{
		Name:       "test",
		Namespace:  "test-ns",
		Deployment: "test-deploy",
		Port:       8080,
	}

	tests := []struct {
		name      string
		config    *Config
		expectErr bool
	}{
		{
			name: "valid config",
			config: &Config{
				Name:        "test",
				Mode:        "both",
				Interval:    10 * time.Second,
				CPUDuration: 10 * time.Second,
				Components:  []ComponentConfig{validComponent},
			},
			expectErr: false,
		},
		{
			name: "empty name",
			config: &Config{
				Name:        "",
				Mode:        "both",
				Interval:    10 * time.Second,
				CPUDuration: 10 * time.Second,
				Components:  []ComponentConfig{validComponent},
			},
			expectErr: true,
		},
		{
			name: "invalid mode",
			config: &Config{
				Name:        "test",
				Mode:        "invalid",
				Interval:    10 * time.Second,
				CPUDuration: 10 * time.Second,
				Components:  []ComponentConfig{validComponent},
			},
			expectErr: true,
		},
		{
			name: "negative interval",
			config: &Config{
				Name:        "test",
				Mode:        "both",
				Interval:    -1 * time.Second,
				CPUDuration: 10 * time.Second,
				Components:  []ComponentConfig{validComponent},
			},
			expectErr: true,
		},
		{
			name: "zero CPU duration",
			config: &Config{
				Name:        "test",
				Mode:        "both",
				Interval:    10 * time.Second,
				CPUDuration: 0,
				Components:  []ComponentConfig{validComponent},
			},
			expectErr: true,
		},
		{
			name: "heap mode",
			config: &Config{
				Name:        "test",
				Mode:        "heap",
				Interval:    10 * time.Second,
				CPUDuration: 10 * time.Second,
				Components:  []ComponentConfig{validComponent},
			},
			expectErr: false,
		},
		{
			name: "cpu mode",
			config: &Config{
				Name:        "test",
				Mode:        "cpu",
				Interval:    10 * time.Second,
				CPUDuration: 10 * time.Second,
				Components:  []ComponentConfig{validComponent},
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectErr && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

func TestProfileDir(t *testing.T) {
	cfg := &Config{
		Name:      "test-run",
		OutputDir: "/tmp/profiles",
	}

	expected := "/tmp/profiles/test-run"
	if cfg.ProfileDir() != expected {
		t.Errorf("Expected profile dir %s, got %s", expected, cfg.ProfileDir())
	}
}

func TestComponentDir(t *testing.T) {
	cfg := &Config{
		Name:      "test-run",
		OutputDir: "/tmp/profiles",
	}

	expected := "/tmp/profiles/test-run/operator-controller"
	if cfg.ComponentDir("operator-controller") != expected {
		t.Errorf("Expected component dir %s, got %s", expected, cfg.ComponentDir("operator-controller"))
	}
}

func TestPIDFile(t *testing.T) {
	cfg := &Config{
		Name:      "test-run",
		OutputDir: "/tmp/profiles",
	}

	expected := "/tmp/profiles/test-run/.profiler.pid"
	if cfg.PIDFile() != expected {
		t.Errorf("Expected PID file %s, got %s", expected, cfg.PIDFile())
	}
}

func TestCollectHeap(t *testing.T) {
	tests := []struct {
		mode     string
		expected bool
	}{
		{"both", true},
		{"heap", true},
		{"cpu", false},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			cfg := &Config{Mode: tt.mode}
			if cfg.CollectHeap() != tt.expected {
				t.Errorf("Expected CollectHeap() = %v for mode %s", tt.expected, tt.mode)
			}
		})
	}
}

func TestCollectCPU(t *testing.T) {
	tests := []struct {
		mode     string
		expected bool
	}{
		{"both", true},
		{"cpu", true},
		{"heap", false},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			cfg := &Config{Mode: tt.mode}
			if cfg.CollectCPU() != tt.expected {
				t.Errorf("Expected CollectCPU() = %v for mode %s", tt.expected, tt.mode)
			}
		})
	}
}

func TestGetEnv(t *testing.T) {
	os.Clearenv()

	// Test default value
	val := getEnv("NONEXISTENT_VAR", "default")
	if val != "default" {
		t.Errorf("Expected 'default', got %s", val)
	}

	// Test existing value
	os.Setenv("TEST_VAR", "custom")
	val = getEnv("TEST_VAR", "default")
	if val != "custom" {
		t.Errorf("Expected 'custom', got %s", val)
	}

	os.Clearenv()
}

func TestGetDurationEnv(t *testing.T) {
	os.Clearenv()

	// Test default value
	dur := getDurationEnv("NONEXISTENT_VAR", 5*time.Second)
	if dur != 5*time.Second {
		t.Errorf("Expected 5s, got %v", dur)
	}

	// Test valid integer
	os.Setenv("TEST_DURATION", "10")
	dur = getDurationEnv("TEST_DURATION", 5*time.Second)
	if dur != 10*time.Second {
		t.Errorf("Expected 10s, got %v", dur)
	}

	// Test invalid value (should use default)
	os.Setenv("TEST_DURATION", "invalid")
	dur = getDurationEnv("TEST_DURATION", 5*time.Second)
	if dur != 5*time.Second {
		t.Errorf("Expected default 5s for invalid value, got %v", dur)
	}

	os.Clearenv()
}

func TestParseComponents(t *testing.T) {
	tests := []struct {
		name        string
		envValue    string
		expectCount int
		expectError bool
	}{
		{
			name:        "empty string",
			envValue:    "",
			expectCount: 0,
			expectError: false,
		},
		{
			name:        "single component",
			envValue:    "app1:ns1:deploy1:8080",
			expectCount: 1,
			expectError: false,
		},
		{
			name:        "multiple components",
			envValue:    "app1:ns1:deploy1:8080;app2:ns2:deploy2:9090",
			expectCount: 2,
			expectError: false,
		},
		{
			name:        "three components",
			envValue:    "app1:ns1:deploy1:8080;app2:ns2:deploy2:9090;app3:ns3:deploy3:6060",
			expectCount: 3,
			expectError: false,
		},
		{
			name:        "invalid format - missing field",
			envValue:    "app1:ns1:deploy1",
			expectCount: 0,
			expectError: true,
		},
		{
			name:        "invalid format - too many fields",
			envValue:    "app1:ns1:deploy1:8080:extra",
			expectCount: 0,
			expectError: true,
		},
		{
			name:        "invalid port",
			envValue:    "app1:ns1:deploy1:invalid",
			expectCount: 0,
			expectError: true,
		},
		{
			name:        "whitespace handling",
			envValue:    " app1:ns1:deploy1:8080 ; app2:ns2:deploy2:9090 ",
			expectCount: 2,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Clearenv()
			if tt.envValue != "" {
				os.Setenv("TEST_PROFILE_COMPONENTS", tt.envValue)
			}

			components, err := parseComponents()

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
				if len(components) != tt.expectCount {
					t.Errorf("Expected %d components, got %d", tt.expectCount, len(components))
				}
			}
		})
	}
}

func TestParseComponentsValidation(t *testing.T) {
	os.Clearenv()
	os.Setenv("TEST_PROFILE_COMPONENTS", "myapp:mynamespace:mydeployment:8080")

	components, err := parseComponents()
	if err != nil {
		t.Fatalf("Failed to parse components: %v", err)
	}

	if len(components) != 1 {
		t.Fatalf("Expected 1 component, got %d", len(components))
	}

	comp := components[0]
	if comp.Name != "myapp" {
		t.Errorf("Expected name 'myapp', got '%s'", comp.Name)
	}
	if comp.Namespace != "mynamespace" {
		t.Errorf("Expected namespace 'mynamespace', got '%s'", comp.Namespace)
	}
	if comp.Deployment != "mydeployment" {
		t.Errorf("Expected deployment 'mydeployment', got '%s'", comp.Deployment)
	}
	if comp.Port != 8080 {
		t.Errorf("Expected port 8080, got %d", comp.Port)
	}
}

func TestConfigValidationWithComponents(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid single component",
			config: &Config{
				Name:        "test",
				Mode:        "both",
				Interval:    10 * time.Second,
				CPUDuration: 10 * time.Second,
				Components: []ComponentConfig{
					{
						Name:       "app1",
						Namespace:  "ns1",
						Deployment: "deploy1",
						Port:       8080,
					},
				},
			},
			expectError: false,
		},
		{
			name: "no components",
			config: &Config{
				Name:        "test",
				Mode:        "both",
				Interval:    10 * time.Second,
				CPUDuration: 10 * time.Second,
				Components:  []ComponentConfig{},
			},
			expectError: true,
			errorMsg:    "at least one component must be configured",
		},
		{
			name: "component missing name",
			config: &Config{
				Name:        "test",
				Mode:        "both",
				Interval:    10 * time.Second,
				CPUDuration: 10 * time.Second,
				Components: []ComponentConfig{
					{
						Name:       "",
						Namespace:  "ns1",
						Deployment: "deploy1",
						Port:       8080,
					},
				},
			},
			expectError: true,
			errorMsg:    "name is required",
		},
		{
			name: "component missing namespace",
			config: &Config{
				Name:        "test",
				Mode:        "both",
				Interval:    10 * time.Second,
				CPUDuration: 10 * time.Second,
				Components: []ComponentConfig{
					{
						Name:       "app1",
						Namespace:  "",
						Deployment: "deploy1",
						Port:       8080,
					},
				},
			},
			expectError: true,
			errorMsg:    "namespace is required",
		},
		{
			name: "invalid port",
			config: &Config{
				Name:        "test",
				Mode:        "both",
				Interval:    10 * time.Second,
				CPUDuration: 10 * time.Second,
				Components: []ComponentConfig{
					{
						Name:       "app1",
						Namespace:  "ns1",
						Deployment: "deploy1",
						Port:       70000,
					},
				},
			},
			expectError: true,
			errorMsg:    "port must be between 1 and 65535",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error containing '%s', got nil", tt.errorMsg)
				} else if !contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
