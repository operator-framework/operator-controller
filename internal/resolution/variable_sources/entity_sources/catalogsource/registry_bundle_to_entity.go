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
	TypeDefaultChannel = "olm.package.defaultChannel"
	TypeBundleSource   = "olm.bundle.path"
	TypeLabel          = "olm.label"
	TypeLabelRequired  = "olm.label.required"
)

func EntityFromBundle(catsrcID string, pkg *catalogsourceapi.Package, bundle *catalogsourceapi.Bundle) (*input.Entity, error) {
	properties := map[string]string{}
	var errs []error

	// Multivalue properties
	propsList := map[string]map[string]struct{}{}

	setPropertyValue := func(key, value string) {
		if _, ok := propsList[key]; !ok {
			propsList[key] = map[string]struct{}{}
		}
		if _, ok := propsList[key][value]; !ok {
			propsList[key][value] = struct{}{}
		}
	}

	for _, prvAPI := range bundle.ProvidedApis {
		apiValue, err := JSONMarshal(prvAPI)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		setPropertyValue(property.TypeGVK, string(apiValue))
	}

	for _, reqAPI := range bundle.RequiredApis {
		apiValue, err := JSONMarshal(reqAPI)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		setPropertyValue(property.TypeGVKRequired, string(apiValue))
	}

	for _, reqAPI := range bundle.Dependencies {
		switch reqAPI.Type {
		case property.TypeGVK:
			setPropertyValue(property.TypeGVKRequired, reqAPI.Value)
		case property.TypePackage:
			setPropertyValue(property.TypePackageRequired, reqAPI.Value)
		case TypeLabel: // legacy property
			setPropertyValue(TypeLabelRequired, reqAPI.Value)
		default:
			setPropertyValue(reqAPI.Type, reqAPI.Value)
		}
	}

	ignoredProperties := map[string]struct{}{
		property.TypeBundleObject: {},
	}

	for _, p := range bundle.Properties {
		if _, ok := ignoredProperties[p.Type]; ok {
			continue
		}
		setPropertyValue(p.Type, p.Value)
	}

	for pType, pValues := range propsList {
		var prop []interface{}
		for pValue := range pValues {
			var v interface{}
			err := JSONUnmarshal([]byte(pValue), &v)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			prop = append(prop, v)
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
		pValue, err := JSONMarshal(prop)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		properties[pType] = string(pValue)
	}

	// Singleton properties.
	// `olm.package`, `olm.channel`, `olm.defaultChannel`
	pkgValue, err := JSONMarshal(property.Package{
		PackageName: bundle.PackageName,
		Version:     bundle.Version,
	})
	if err != nil {
		errs = append(errs, err)
	} else {
		properties[property.TypePackage] = string(pkgValue)
	}

	upValue, err := JSONMarshal(UpgradeEdge{
		Channel: property.Channel{
			ChannelName: bundle.ChannelName,
		},
		Replaces:  bundle.Replaces,
		Skips:     bundle.Skips,
		SkipRange: bundle.SkipRange,
		//		Version:   bundle.Version,
	})
	if err != nil {
		errs = append(errs, err)
	} else {
		properties[property.TypeChannel] = string(upValue)
	}

	properties[TypeDefaultChannel] = pkg.DefaultChannelName
	properties[TypeBundleSource] = bundle.BundlePath

	if len(errs) > 0 {
		return nil, fmt.Errorf("failed to parse properties for bundle %s/%s in %s: %v", bundle.GetPackageName(), bundle.GetVersion(), catsrcID, errors.NewAggregate(errs))
	}

	// Since multiple instances of bundle may exist for different channels, entityID must include reference to channel
	entityIDFromBundle := func(catsrcID string, bundle *catalogsourceapi.Bundle) deppy.Identifier {
		return deppy.Identifier(fmt.Sprintf("%s/%s/%s/%s", catsrcID, bundle.PackageName, bundle.ChannelName, bundle.Version))
	}

	return input.NewEntity(entityIDFromBundle(catsrcID, bundle), properties), nil
}

func JSONMarshal(p interface{}) ([]byte, error) {
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

func JSONUnmarshal(p []byte, out interface{}) error {
	buf := bytes.NewReader(p)
	dec := json.NewDecoder(buf)
	return dec.Decode(out)
}
