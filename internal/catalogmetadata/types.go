package catalogmetadata

import (
	"encoding/json"
	"fmt"

	bsemver "github.com/blang/semver/v4"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"
)

const (
	MediaTypePlain          = "plain+v0"
	MediaTypeRegistry       = "registry+v1"
	PropertyBundleMediaType = "olm.bundle.mediatype"
)

type Schemas interface {
	Package | Bundle | Channel | Deprecation
}

type Package struct {
	declcfg.Package
	Deprecation *declcfg.DeprecationEntry
	Catalog     string
}

func (p *Package) IsDeprecated() bool {
	return p.Deprecation != nil
}

type Channel struct {
	declcfg.Channel
	Deprecation *declcfg.DeprecationEntry
	Catalog     string
}

func (c *Channel) IsDeprecated() bool {
	return c.Deprecation != nil
}

type Deprecation struct {
	declcfg.Deprecation
	Catalog string
}

type PackageRequired struct {
	property.PackageRequired
	SemverRange bsemver.Range `json:"-"`
}

type Bundle struct {
	declcfg.Bundle
	Deprecation      *declcfg.DeprecationEntry
	Catalog          string
	version          *bsemver.Version
	requiredPackages []PackageRequired
	mediaType        string
}

func (b *Bundle) Version() (*bsemver.Version, error) {
	if b.version != nil {
		return b.version, nil
	}

	pkg, err := loadOneFromProps[property.Package](b, property.TypePackage, true)
	if err != nil {
		return nil, err
	}
	semVer, err := bsemver.Parse(pkg.Version)
	if err != nil {
		return nil, fmt.Errorf("could not parse semver %q for bundle '%s': %s", pkg.Version, b.Name, err)
	}
	b.version = &semVer
	return b.version, nil
}

func (b *Bundle) RequiredPackages() ([]PackageRequired, error) {
	if len(b.requiredPackages) > 0 {
		return b.requiredPackages, nil
	}

	requiredPackages, err := loadFromProps[PackageRequired](b, property.TypePackageRequired, false)
	if err != nil {
		return nil, fmt.Errorf("error determining bundle required packages for bundle %q: %s", b.Name, err)
	}
	for i := range requiredPackages {
		semverRange, err := bsemver.ParseRange(requiredPackages[i].VersionRange)
		if err != nil {
			return nil, fmt.Errorf(
				"error parsing bundle required package semver range for bundle %q (required package %q): %s",
				b.Name,
				requiredPackages[i].PackageName,
				err,
			)
		}
		requiredPackages[i].SemverRange = semverRange
	}
	b.requiredPackages = requiredPackages
	return b.requiredPackages, nil
}

func (b *Bundle) MediaType() (string, error) {
	if b.mediaType != "" {
		return b.mediaType, nil
	}

	mediaType, err := loadOneFromProps[string](b, PropertyBundleMediaType, false)
	if err != nil {
		return "", fmt.Errorf("error determining bundle mediatype for bundle %q: %s", b.Name, err)
	}
	b.mediaType = mediaType
	return b.mediaType, nil
}

func (b *Bundle) propertiesByType(propType string) []*property.Property {
	propertiesMap := make(map[string][]*property.Property)
	for i := range b.Properties {
		prop := b.Properties[i]
		propertiesMap[prop.Type] = append(propertiesMap[prop.Type], &prop)
	}

	return propertiesMap[propType]
}

// IsDeprecated returns true if the bundle
// has been explicitly deprecated. This can occur
// if the bundle itself has been deprecated.
// this function does not take into consideration
// olm.channel or olm.package deprecations associated
// with the bundle as a bundle can be present in multiple
// channels with some channels being deprecated and some not
// Package deprecation does not carry the same meaning as an individual
// bundle deprecation, so package deprecation is not considered.
func (b *Bundle) IsDeprecated() bool {
	return b.Deprecation != nil
}

func loadOneFromProps[T any](bundle *Bundle, propType string, required bool) (T, error) {
	r, err := loadFromProps[T](bundle, propType, required)
	if err != nil {
		return *new(T), err
	}
	if len(r) > 1 {
		return *new(T), fmt.Errorf("expected 1 instance of property with type %q, got %d", propType, len(r))
	}
	if !required && len(r) == 0 {
		return *new(T), nil
	}

	return r[0], nil
}

func loadFromProps[T any](bundle *Bundle, propType string, required bool) ([]T, error) {
	props := bundle.propertiesByType(propType)
	if len(props) != 0 {
		result := []T{}
		for i := range props {
			parsedProp := *new(T)
			if err := json.Unmarshal(props[i].Value, &parsedProp); err != nil {
				return nil, fmt.Errorf("property %q with value %q could not be parsed: %s", propType, props[i].Value, err)
			}
			result = append(result, parsedProp)
		}
		return result, nil
	} else if required {
		return nil, fmt.Errorf("bundle property with type %q not found", propType)
	}

	return nil, nil
}
