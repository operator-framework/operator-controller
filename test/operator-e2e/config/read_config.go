package config

import (
	"errors"
	"fmt"
	"os"

	"sigs.k8s.io/yaml"
)

const (
	schemaName = "catalog-config"
)

type ChannelEntry struct {
	EntryVersion string   `json:"entryVersion"`
	Replaces     string   `json:"replaces,omitempty"`
	Skips        []string `json:"skips,omitempty"`
	SkipRange    string   `json:"skipRange,omitempty"`
}

type ChannelData struct {
	ChannelName    string         `json:"channelName"`
	ChannelEntries []ChannelEntry `json:"channelEntries"`
}

type BundleData struct {
	BundleImage   string `json:"bundleImage"`
	BundleVersion string `json:"bundleVersion"`
}

type Config struct {
	Schema      string        `json:"schema"`
	PackageName string        `json:"packageName"`
	ChannelData []ChannelData `json:"channelData"`
	BundleData  []BundleData  `json:"bundleData"`
}

func ReadFile(f string) (*Config, error) {
	b, err := readFile(f)
	if err != nil {
		return nil, err
	}

	var c Config
	err = yaml.Unmarshal(b, &c)
	if err != nil {
		return nil, err
	}
	if c.Schema != schemaName {
		return nil, fmt.Errorf("invalid schema: %q should be %q: %v", c.Schema, schemaName, err)
	}
	if c.PackageName == "" {
		return nil, errors.New("missing required package name")
	}
	if len(c.ChannelData) == 0 {
		return nil, errors.New("missing required channel information")
	}
	if len(c.BundleData) == 0 {
		return nil, errors.New("missing required bundle information")
	}

	return &c, nil
}

// overrideable func for mocking os.ReadFile
var readFile = func(file string) ([]byte, error) {
	return os.ReadFile(file)
}
