package filter_test

import (
	"encoding/json"
	"testing"

	mmsemver "github.com/Masterminds/semver/v3"
	bsemver "github.com/blang/semver/v4"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"
	"github.com/stretchr/testify/assert"

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

func TestWithName(t *testing.T) {
	b1 := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{Name: "package1.v1"}}
	b2 := &catalogmetadata.Bundle{Bundle: declcfg.Bundle{Name: "package2.v1"}}
	b3 := &catalogmetadata.Bundle{}

	f := filter.WithName("package1.v1")

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

func TestReplaces(t *testing.T) {
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
			},
		},
	}

	b1 := &catalogmetadata.Bundle{
		Bundle:     declcfg.Bundle{Name: "package1.v0.0.2"},
		InChannels: []*catalogmetadata.Channel{fakeChannel},
	}
	b2 := &catalogmetadata.Bundle{
		Bundle:     declcfg.Bundle{Name: "package1.v0.0.3"},
		InChannels: []*catalogmetadata.Channel{fakeChannel},
	}
	b3 := &catalogmetadata.Bundle{}

	f := filter.Replaces("package1.v0.0.1")

	assert.True(t, f(b1))
	assert.False(t, f(b2))
	assert.False(t, f(b3))
}
