package store

import (
	"encoding/json"
	"fmt"

	masterminds "github.com/Masterminds/semver/v3"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	PropertyBundleMediaType = "olm.bundle.mediatype"
	MediaTypeRegistryV1     = "registry+v1"
	MediaTypePlainV0        = "plain+v0"
)

type Catalog struct {
	Name     string
	Packages map[string]*Package
}

type Package struct {
	CatalogName string
	declcfg.Package
	Channels map[string]*Channel
	Bundles  map[string]*Bundle
}

type Channel struct {
	CatalogName string
	declcfg.Channel
	Bundles []*Bundle
}

func NewChannel(catalogName string, ch declcfg.Channel, packageBundles map[string]*Bundle) (*Channel, error) {
	memberNames := sets.New[string]()
	for _, e := range ch.Entries {
		memberNames.Insert(e.Name)
	}

	members := make([]*Bundle, 0, len(memberNames))
	for _, b := range packageBundles {
		if memberNames.Has(b.Name) {
			members = append(members, b)
		}
	}
	return &Channel{
		CatalogName: catalogName,
		Channel:     ch,
		Bundles:     members,
	}, nil
}

type Bundle struct {
	CatalogName string
	declcfg.Bundle
	Version masterminds.Version
}

func NewBundle(catalogName string, b declcfg.Bundle) (*Bundle, error) {
	vers, err := bundleVersion(b)
	if err != nil {
		return nil, err
	}
	return &Bundle{
		CatalogName: catalogName,
		Bundle:      b,
		Version:     *vers,
	}, nil
}

func bundleVersion(b declcfg.Bundle) (*masterminds.Version, error) {
	for _, p := range b.Properties {
		if p.Type != property.TypePackage {
			continue
		}
		var pkg property.Package
		if err := json.Unmarshal(p.Value, &pkg); err != nil {
			return nil, fmt.Errorf("invalid package property: %w", err)
		}
		v, err := masterminds.NewVersion(pkg.Version)
		if err != nil {
			return nil, fmt.Errorf("invalid bundle version: %w", err)
		}
		return v, nil
	}
	return nil, fmt.Errorf("could not get bundle version: no olm.package property found")
}
