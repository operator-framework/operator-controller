package filter_test

import (
	"encoding/json"
	"testing"

	mmsemver "github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	"github.com/operator-framework/operator-controller/internal/operator-controller/catalogmetadata/filter"
)

func TestInMastermindsSemverRange(t *testing.T) {
	b1 := declcfg.Bundle{
		Properties: []property.Property{
			{
				Type:  property.TypePackage,
				Value: json.RawMessage(`{"packageName": "package1", "version": "1.0.0"}`),
			},
		},
	}
	b2 := declcfg.Bundle{
		Properties: []property.Property{
			{
				Type:  property.TypePackage,
				Value: json.RawMessage(`{"packageName": "package1", "version": "0.0.1"}`),
			},
		},
	}
	b3 := declcfg.Bundle{
		Properties: []property.Property{
			{
				Type:  property.TypePackage,
				Value: json.RawMessage(`{"packageName": "package1", "version": "broken"}`),
			},
		},
	}

	vRange, err := mmsemver.NewConstraint(">=1.0.0")
	require.NoError(t, err)

	f := filter.InMastermindsSemverRange(vRange)

	assert.True(t, f(b1))
	assert.False(t, f(b2))
	assert.False(t, f(b3))
}

func TestInAnyChannel(t *testing.T) {
	alpha := declcfg.Channel{Name: "alpha", Entries: []declcfg.ChannelEntry{{Name: "b1"}, {Name: "b2"}}}
	stable := declcfg.Channel{Name: "stable", Entries: []declcfg.ChannelEntry{{Name: "b1"}}}

	b1 := declcfg.Bundle{Name: "b1"}
	b2 := declcfg.Bundle{Name: "b2"}
	b3 := declcfg.Bundle{Name: "b3"}

	fAlpha := filter.InAnyChannel(alpha)
	assert.True(t, fAlpha(b1))
	assert.True(t, fAlpha(b2))
	assert.False(t, fAlpha(b3))

	fStable := filter.InAnyChannel(stable)
	assert.True(t, fStable(b1))
	assert.False(t, fStable(b2))
	assert.False(t, fStable(b3))
}
