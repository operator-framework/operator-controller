package variable_sources

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/blang/semver/v4"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-registry/alpha/property"
)

const PropertyBundlePath = "olm.bundle.path"

type ChannelProperties struct {
	property.Channel
	Replaces  string   `json:"replaces,omitempty"`
	Skips     []string `json:"skips,omitempty"`
	SkipRange string   `json:"skipRange,omitempty"`
}

type PackageRequired struct {
	property.PackageRequired
	SemverRange *semver.Range `json:"-"`
}

type BundleEntity struct {
	*input.Entity

	// these properties are lazy loaded as they are requested
	bundlePackage     *property.Package
	providedGVKs      []property.GVK
	requiredGVKs      []property.GVKRequired
	requiredPackages  []PackageRequired
	channelProperties *ChannelProperties
	semVersion        *semver.Version
	bundlePath        string
	mu                sync.RWMutex
}

func NewBundleEntity(entity *input.Entity) *BundleEntity {
	return &BundleEntity{
		Entity: entity,
		mu:     sync.RWMutex{},
	}
}

func (b *BundleEntity) PackageName() (string, error) {
	if err := b.loadPackage(); err != nil {
		return "", err
	}
	return b.bundlePackage.PackageName, nil
}

func (b *BundleEntity) Version() (*semver.Version, error) {
	if err := b.loadPackage(); err != nil {
		return nil, err
	}
	return b.semVersion, nil
}

func (b *BundleEntity) ProvidedGVKs() ([]property.GVK, error) {
	if err := b.loadProvidedGVKs(); err != nil {
		return nil, err
	}
	return b.providedGVKs, nil
}

func (b *BundleEntity) RequiredGVKs() ([]property.GVKRequired, error) {
	if err := b.loadRequiredGVKs(); err != nil {
		return nil, err
	}
	return b.requiredGVKs, nil
}

func (b *BundleEntity) RequiredPackages() ([]PackageRequired, error) {
	if err := b.loadRequiredPackages(); err != nil {
		return nil, err
	}
	return b.requiredPackages, nil
}

func (b *BundleEntity) ChannelName() (string, error) {
	if err := b.loadChannelProperties(); err != nil {
		return "", err
	}
	return b.channelProperties.ChannelName, nil
}

func (b *BundleEntity) ChannelProperties() (*ChannelProperties, error) {
	if err := b.loadChannelProperties(); err != nil {
		return nil, err
	}
	return b.channelProperties, nil
}

func (b *BundleEntity) BundlePath() (string, error) {
	if err := b.loadBundlePath(); err != nil {
		return "", err
	}
	return b.bundlePath, nil
}

func (b *BundleEntity) loadPackage() error {
	if b.bundlePackage == nil {
		bundlePackage, err := loadFromEntity[property.Package](b.Entity, property.TypePackage)
		if err != nil {
			return fmt.Errorf("error determining package for entity '%s': %s", b.ID, err)
		}
		b.bundlePackage = &bundlePackage
		if b.semVersion == nil {
			semVer, err := semver.Parse(b.bundlePackage.Version)
			if err != nil {
				return fmt.Errorf("could not parse semver (%s) for entity '%s': %s", b.bundlePackage.Version, b.ID, err)
			}
			b.semVersion = &semVer
		}
	}
	return nil
}

func (b *BundleEntity) loadProvidedGVKs() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.providedGVKs == nil {
		providedGVKs, err := loadFromEntity[[]property.GVK](b.Entity, property.TypeGVK)
		if err != nil {
			return fmt.Errorf("error determining bundle provided gvks for entity '%s': %s", b.ID, err)
		}
		b.providedGVKs = providedGVKs
	}
	return nil
}

func (b *BundleEntity) loadRequiredGVKs() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.requiredGVKs == nil {
		requiredGVKs, err := loadFromEntity[[]property.GVKRequired](b.Entity, property.TypeGVKRequired)
		if err != nil {
			return fmt.Errorf("error determining bundle required gvks for entity '%s': %s", b.ID, err)
		}
		b.requiredGVKs = requiredGVKs
	}
	return nil
}

func (b *BundleEntity) loadRequiredPackages() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.requiredPackages == nil {
		requiredPackages, err := loadFromEntity[[]PackageRequired](b.Entity, property.TypePackageRequired)
		if err != nil {
			return fmt.Errorf("error determining bundle required packages for entity '%s': %s", b.ID, err)
		}
		for _, requiredPackage := range requiredPackages {
			semverRange, err := semver.ParseRange(requiredPackage.VersionRange)
			if err != nil {
				return fmt.Errorf("error determining bundle required package semver range for entity '%s': '%s'", b.ID, err)
			}
			requiredPackage.SemverRange = &semverRange
		}
		b.requiredPackages = requiredPackages
	}
	return nil
}

func (b *BundleEntity) loadChannelProperties() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.channelProperties == nil {
		channel, err := loadFromEntity[ChannelProperties](b.Entity, property.TypeChannel)
		if err != nil {
			return fmt.Errorf("error determining bundle channel properties for entity '%s': %s", b.ID, err)
		}
		b.channelProperties = &channel
	}
	return nil
}

func (b *BundleEntity) loadBundlePath() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bundlePath == "" {
		bundlePath, err := loadFromEntity[string](b.Entity, PropertyBundlePath)
		if err != nil {
			return fmt.Errorf("error determining bundle path for entity '%s': %s", b.ID, err)
		}
		b.bundlePath = bundlePath
	}
	return nil
}

func loadFromEntity[T interface{}](entity *input.Entity, propertyName string) (T, error) {
	deserializedProperty := *new(T)
	propertyValue, ok := entity.Properties[propertyName]
	if !ok {
		return deserializedProperty, fmt.Errorf("property '%s' not found", propertyName)
	}

	if err := json.Unmarshal([]byte(propertyValue), &deserializedProperty); err != nil {
		return deserializedProperty, fmt.Errorf("property '%s' ('%s') could not be parsed: %s", propertyName, propertyValue, err)
	}
	return deserializedProperty, nil
}
