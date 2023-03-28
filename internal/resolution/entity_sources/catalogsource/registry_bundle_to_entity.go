package catalogsource

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/operator-framework/operator-registry/alpha/property"
	catalogsourceapi "github.com/operator-framework/operator-registry/pkg/api"
	"k8s.io/apimachinery/pkg/util/errors"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
)

type UpgradeEdge struct {
	// possibly replaced by edge specific variablesources
	// rather than being grouped with bundle properties
	property.Channel
	Replaces  string   `json:"replaces,omitempty"`
	Skips     []string `json:"skips,omitempty"`
	SkipRange string   `json:"skipRange,omitempty"`
	Version   string   `json:"version,omitempty"`
}

type DefaultChannel struct {
	DefaultChannel string `json:"defaultchannel"`
}

const (
	// TODO: reevaluate if defaultChannel is strictly necessary in olmv1
	typeDefaultChannel = "olm.package.defaultChannel"
	typeBundleSource   = "olm.bundle.path"
)

func EntityFromBundle(catsrcID string, pkg *catalogsourceapi.Package, bundle *catalogsourceapi.Bundle) (*input.Entity, error) {
	modelBundle, err := catalogsourceapi.ConvertAPIBundleToModelBundle(bundle)
	if err != nil {
		return nil, err
	}
	properties := map[string]string{}
	var errs []error

	// Multivalue properties
	propsList := map[string]map[string]struct{}{}
	for _, p := range modelBundle.Properties {
		switch p.Type {
		case property.TypeBundleObject:
			// ignore - only need metadata for resolution and bundle path for installation
		case property.TypePackage:
			properties[p.Type] = string(p.Value)
		default:
			var v interface{}
			// the keys in the marshaled object may be out of order.
			// recreate the json object so this doesn't happen.
			pValue := p.Value
			err := json.Unmarshal(p.Value, &v)
			if err == nil {
				// don't force property values to be json
				// but if unmarshalled successfully,
				// marshaling again should not fail.
				pValue, err = jsonMarshal(v)
				if err != nil {
					errs = append(errs, err)
					continue
				}
			}
			if _, ok := propsList[p.Type]; !ok {
				propsList[p.Type] = map[string]struct{}{}
			}
			if _, ok := propsList[p.Type][string(pValue)]; !ok {
				propsList[p.Type][string(pValue)] = struct{}{}
			}
		}
	}

	for pType, pValues := range propsList {
		var prop []interface{}
		for pValue := range pValues {
			var v interface{}
			err := json.Unmarshal([]byte(pValue), &v)
			if err == nil {
				// the property value may not be json.
				// if unable to unmarshal, treat property value as a string
				prop = append(prop, v)
			} else {
				prop = append(prop, pValue)
			}
		}
		if len(prop) == 0 {
			continue
		}
		if len(prop) > 1 {
			sort.Slice(prop, func(i, j int) bool {
				// enforce some ordering for deterministic properties. Possibly a neater way to do this.
				return fmt.Sprintf("%v", prop[i]) < fmt.Sprintf("%v", prop[j])
			})
		}
		pValue, err := jsonMarshal(prop)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		properties[pType] = string(pValue)
	}

	upValue, err := jsonMarshal(UpgradeEdge{
		Channel: property.Channel{
			ChannelName: bundle.ChannelName,
		},
		Replaces:  bundle.Replaces,
		Skips:     bundle.Skips,
		SkipRange: bundle.SkipRange,
	})
	if err != nil {
		errs = append(errs, err)
	} else {
		properties[property.TypeChannel] = string(upValue)
	}

	properties[typeDefaultChannel] = pkg.DefaultChannelName
	properties[typeBundleSource] = bundle.BundlePath

	if len(errs) > 0 {
		return nil, fmt.Errorf("failed to parse properties for bundle %s/%s in %s: %v", bundle.GetPackageName(), bundle.GetVersion(), catsrcID, errors.NewAggregate(errs))
	}

	// Since multiple instances of bundle may exist for different channels, entityID must include reference to channel
	return input.NewEntity(deppy.Identifier(fmt.Sprintf("%s/%s/%s/%s", catsrcID, bundle.PackageName, bundle.ChannelName, bundle.Version)), properties), nil
}

func jsonMarshal(p interface{}) ([]byte, error) {
	buf := &bytes.Buffer{}
	dec := json.NewEncoder(buf)
	dec.SetEscapeHTML(false)
	err := dec.Encode(p)
	if err != nil {
		return nil, err
	}
	out := &bytes.Buffer{}
	if err := json.Compact(out, buf.Bytes()); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
