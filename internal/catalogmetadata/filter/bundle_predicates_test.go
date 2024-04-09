package filter_test

import (
	"encoding/json"
	"fmt"
	"testing"

	mmsemver "github.com/Masterminds/semver/v3"
	bsemver "github.com/blang/semver/v4"
	"github.com/stretchr/testify/assert"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata/filter"
)

func TestWithPackageName(t *testing.T) {
	b1 := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{Package: "package1"}}
	b2 := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{Package: "package2"}}
	b3 := &catalogmetadata.Bundle{}

	f := filter.WithPackageName("package1")

	assert.True(t, f(b1))
	assert.False(t, f(b2))
	assert.False(t, f(b3))
}

func TestInMastermindsSemverRange(t *testing.T) {
	b1 := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
		Properties: []property.Property{
			{
				Type:  property.TypePackage,
				Value: json.RawMessage(`{"packageName": "package1", "version": "1.0.0"}`),
			},
		},
	}}
	b2 := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
		Properties: []property.Property{
			{
				Type:  property.TypePackage,
				Value: json.RawMessage(`{"packageName": "package1", "version": "0.0.1"}`),
			},
		},
	}}
	b3 := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
		Properties: []property.Property{
			{
				Type:  property.TypePackage,
				Value: json.RawMessage(`{"packageName": "package1", "version": "broken"}`),
			},
		},
	}}

	vRange, err := mmsemver.NewConstraint(">=1.0.0")
	assert.NoError(t, err)

	f := filter.InMastermindsSemverRange(vRange)

	assert.True(t, f(b1))
	assert.False(t, f(b2))
	assert.False(t, f(b3))
}

func TestHigherBundleVersion(t *testing.T) {
	testCases := []struct {
		name             string
		requestedVersion string
		comparedVersion  string
		wantResult       bool
	}{
		{
			name:             "includes equal version",
			requestedVersion: "1.0.0",
			comparedVersion:  "1.0.0",
			wantResult:       true,
		},
		{
			name:             "filters out older version",
			requestedVersion: "1.0.0",
			comparedVersion:  "0.0.1",
			wantResult:       false,
		},
		{
			name:             "includes higher version",
			requestedVersion: "1.0.0",
			comparedVersion:  "2.0.0",
			wantResult:       true,
		},
		{
			name:             "filters out broken version",
			requestedVersion: "1.0.0",
			comparedVersion:  "broken",
			wantResult:       false,
		},
		{
			name:             "filter returns false with nil version",
			requestedVersion: "",
			comparedVersion:  "1.0.0",
			wantResult:       false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			bundle := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
				Properties: []property.Property{
					{
						Type:  property.TypePackage,
						Value: json.RawMessage(fmt.Sprintf(`{"packageName": "package1", "version": "%s"}`, tc.comparedVersion)),
					},
				},
			}}
			var version *bsemver.Version
			if tc.requestedVersion != "" {
				parsedVersion := bsemver.MustParse(tc.requestedVersion)
				// to test with nil requested version
				version = &parsedVersion
			}
			f := filter.HigherBundleVersion(version)
			assert.Equal(t, tc.wantResult, f(bundle))
		})
	}
}

func TestInBlangSemverRange(t *testing.T) {
	b1 := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
		Properties: []property.Property{
			{
				Type:  property.TypePackage,
				Value: json.RawMessage(`{"packageName": "package1", "version": "1.0.0"}`),
			},
		},
	}}
	b2 := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
		Properties: []property.Property{
			{
				Type:  property.TypePackage,
				Value: json.RawMessage(`{"packageName": "package1", "version": "0.0.1"}`),
			},
		},
	}}
	b3 := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{
		Properties: []property.Property{
			{
				Type:  property.TypePackage,
				Value: json.RawMessage(`{"packageName": "package1", "version": "broken"}`),
			},
		},
	}}

	vRange := bsemver.MustParseRange(">=1.0.0")

	f := filter.InBlangSemverRange(vRange)

	assert.True(t, f(b1))
	assert.False(t, f(b2))
	assert.False(t, f(b3))
}

func TestInChannel(t *testing.T) {
	b1 := &catalogmetadata.Bundle{InChannels: []*catalogmetadata.Channel{
		{Channel: declcfg.Channel{Name: "alpha"}},
		{Channel: declcfg.Channel{Name: "stable"}},
	}}
	b2 := &catalogmetadata.Bundle{InChannels: []*catalogmetadata.Channel{
		{Channel: declcfg.Channel{Name: "alpha"}},
	}}
	b3 := &catalogmetadata.Bundle{}

	f := filter.InChannel("stable")

	assert.True(t, f(b1))
	assert.False(t, f(b2))
	assert.False(t, f(b3))
}

func TestWithBundleImage(t *testing.T) {
	b1 := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{Image: "fake-image-uri-1"}}
	b2 := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{Image: "fake-image-uri-2"}}
	b3 := &catalogmetadata.Bundle{}

	f := filter.WithBundleImage("fake-image-uri-1")

	assert.True(t, f(b1))
	assert.False(t, f(b2))
	assert.False(t, f(b3))
}

func TestLegacySuccessor(t *testing.T) {
	fakeChannel := &catalogmetadata.Channel{
		Channel: declcfg.Channel{
			Entries: []declcfg.ChannelEntry{
				{
					Name:     "package1.v0.0.2",
					Replaces: "package1.v0.0.1",
				},
				{
					Name:     "package1.v0.0.3",
					Replaces: "package1.v0.0.2",
				},
				{
					Name:  "package1.v0.0.4",
					Skips: []string{"package1.v0.0.1"},
				},
				{
					Name:      "package1.v0.0.5",
					SkipRange: "<=0.0.1",
				},
			},
		},
	}
	installedBundle := &catalogmetadata.Bundle{
		Bundle: declcfg.Bundle{
			Name: "package1.v0.0.1",
			Properties: []property.Property{
				{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "package1", "version": "0.0.1"}`)},
			},
		},
	}

	b2 := &catalogmetadata.Bundle{
		Bundle:     declcfg.Bundle{Name: "package1.v0.0.2"},
		InChannels: []*catalogmetadata.Channel{fakeChannel},
	}
	b3 := &catalogmetadata.Bundle{
		Bundle:     declcfg.Bundle{Name: "package1.v0.0.3"},
		InChannels: []*catalogmetadata.Channel{fakeChannel},
	}
	b4 := &catalogmetadata.Bundle{
		Bundle:     declcfg.Bundle{Name: "package1.v0.0.4"},
		InChannels: []*catalogmetadata.Channel{fakeChannel},
	}
	b5 := &catalogmetadata.Bundle{
		Bundle:     declcfg.Bundle{Name: "package1.v0.0.5"},
		InChannels: []*catalogmetadata.Channel{fakeChannel},
	}
	emptyBundle := &catalogmetadata.Bundle{}

	f := filter.LegacySuccessor(installedBundle)

	assert.True(t, f(b2))
	assert.False(t, f(b3))
	assert.True(t, f(b4))
	assert.True(t, f(b5))
	assert.False(t, f(emptyBundle))
}

func TestWithDeprecation(t *testing.T) {
	b1 := &catalogmetadata.Bundle{
		Deprecations: []declcfg.DeprecationEntry{
			{
				Reference: declcfg.PackageScopedReference{},
			},
		},
	}

	b2 := &catalogmetadata.Bundle{}

	f := filter.WithDeprecation(true)
	assert.True(t, f(b1))
	assert.False(t, f(b2))
}
