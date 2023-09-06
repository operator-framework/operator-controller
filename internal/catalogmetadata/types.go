package catalogmetadata

import (
	"encoding/json"
	"fmt"
	"sync"

	bsemver "github.com/blang/semver/v4"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"
)

type Schemas interface {
	Package | Bundle | Channel
}

type Package struct {
	declcfg.Package
}

type Channel struct {
	declcfg.Channel
}

type GVK property.GVK

func (g GVK) String() string {
	return fmt.Sprintf(`group:"%s" version:"%s" kind:"%s"`, g.Group, g.Version, g.Kind)
}

type GVKRequired property.GVKRequired

func (g GVKRequired) String() string {
	return fmt.Sprintf(`group:"%s" version:"%s" kind:"%s"`, g.Group, g.Version, g.Kind)
}

func (g GVKRequired) AsGVK() GVK {
	return GVK(g)
}

type Bundle struct {
	declcfg.Bundle
	InChannels []*Channel

	mu sync.RWMutex
	// these properties are lazy loaded as they are requested
	propertiesMap map[string]property.Property
	bundlePackage *property.Package
	semVersion    *bsemver.Version
	providedGVKs  []GVK
	requiredGVKs  []GVKRequired
}

func (b *Bundle) Version() (*bsemver.Version, error) {
	if err := b.loadPackage(); err != nil {
		return nil, err
	}
	return b.semVersion, nil
}

func (b *Bundle) ProvidedGVKs() ([]GVK, error) {
	if err := b.loadProvidedGVKs(); err != nil {
		return nil, err
	}
	return b.providedGVKs, nil
}

func (b *Bundle) RequiredGVKs() ([]GVKRequired, error) {
	if err := b.loadRequiredGVKs(); err != nil {
		return nil, err
	}
	return b.requiredGVKs, nil
}

func (b *Bundle) loadPackage() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bundlePackage == nil {
		bundlePackage, err := loadFromProps[property.Package](b, property.TypePackage, true)
		if err != nil {
			return err
		}
		b.bundlePackage = &bundlePackage
	}
	if b.semVersion == nil {
		semVer, err := bsemver.Parse(b.bundlePackage.Version)
		if err != nil {
			return fmt.Errorf("could not parse semver %q for bundle '%s': %s", b.bundlePackage.Version, b.Name, err)
		}
		b.semVersion = &semVer
	}
	return nil
}

func (b *Bundle) loadProvidedGVKs() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.providedGVKs == nil {
		providedGVKs, err := loadFromProps[[]GVK](b, property.TypeGVK, false)
		if err != nil {
			return fmt.Errorf("error determining provided GVKs for bundle %q: %s", b.Name, err)
		}
		b.providedGVKs = providedGVKs
	}
	return nil
}

func (b *Bundle) loadRequiredGVKs() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.requiredGVKs == nil {
		requiredGVKs, err := loadFromProps[[]GVKRequired](b, property.TypeGVKRequired, false)
		if err != nil {
			return fmt.Errorf("error determining required GVKs for bundle %q: %s", b.Name, err)
		}
		b.requiredGVKs = requiredGVKs
	}
	return nil
}

func (b *Bundle) propertyByType(propType string) *property.Property {
	if b.propertiesMap == nil {
		b.propertiesMap = make(map[string]property.Property)
		for _, prop := range b.Properties {
			b.propertiesMap[prop.Type] = prop
		}
	}

	prop, ok := b.propertiesMap[propType]
	if !ok {
		return nil
	}
	return &prop
}

func loadFromProps[T any](bundle *Bundle, propType string, required bool) (T, error) {
	parsedProp := *new(T)
	prop := bundle.propertyByType(propType)
	if prop != nil {
		if err := json.Unmarshal(prop.Value, &parsedProp); err != nil {
			return parsedProp, fmt.Errorf("property %q with value %q could not be parsed: %s", propType, prop.Value, err)
		}
	} else if required {
		return parsedProp, fmt.Errorf("bundle property with type %q not found", propType)
	}

	return parsedProp, nil
}
