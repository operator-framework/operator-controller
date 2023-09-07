package entities

import (
	"encoding/json"
	"fmt"
	"sync"

	bsemver "github.com/blang/semver/v4"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-registry/alpha/property"
)

const PropertyBundlePath = "olm.bundle.path"
const PropertyBundleChannelEntry = "olm.bundle.channelEntry"
const PropertyBundleMediaType = "olm.bundle.mediatype"

type MediaType string

const (
	MediaTypePlain    = "plain+v0"
	MediaTypeRegistry = "registry+v1"
)

// ----

type propertyRequirement bool

const (
	required propertyRequirement = true
	optional propertyRequirement = false
)

type PackageRequired struct {
	property.PackageRequired
	SemverRange *bsemver.Range `json:"-"`
}

type GVK property.GVK

type ChannelEntry struct {
	Name     string `json:"name"`
	Replaces string `json:"replaces"`
	// Skips and skipRange will probably go here as well
}

func (g GVK) String() string {
	return fmt.Sprintf(`group:"%s" version:"%s" kind:"%s"`, g.Group, g.Version, g.Kind)
}

type BundleEntity struct {
	*input.Entity

	// these properties are lazy loaded as they are requested
	bundlePackage    *property.Package
	providedGVKs     []GVK
	requiredGVKs     []GVK
	requiredPackages []PackageRequired
	channel          *property.Channel
	channelEntry     *ChannelEntry
	semVersion       *bsemver.Version
	bundlePath       string
	mediaType        string
	mu               sync.RWMutex
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

func (b *BundleEntity) Version() (*bsemver.Version, error) {
	if err := b.loadPackage(); err != nil {
		return nil, err
	}
	return b.semVersion, nil
}

func (b *BundleEntity) ProvidedGVKs() ([]GVK, error) {
	if err := b.loadProvidedGVKs(); err != nil {
		return nil, err
	}
	return b.providedGVKs, nil
}

func (b *BundleEntity) RequiredGVKs() ([]GVK, error) {
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
	return b.channel.ChannelName, nil
}

func (b *BundleEntity) Channel() (*property.Channel, error) {
	if err := b.loadChannelProperties(); err != nil {
		return nil, err
	}
	return b.channel, nil
}

func (b *BundleEntity) BundleChannelEntry() (*ChannelEntry, error) {
	if err := b.loadBundleChannelEntry(); err != nil {
		return nil, err
	}
	return b.channelEntry, nil
}

func (b *BundleEntity) loadBundleChannelEntry() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.channelEntry == nil {
		channelEntry, err := loadFromEntity[*ChannelEntry](b.Entity, PropertyBundleChannelEntry, optional)
		if err != nil || channelEntry == nil {
			return fmt.Errorf("error determining replaces for entity '%s': %w", b.ID, err)
		}
		b.channelEntry = channelEntry
	}
	return nil
}

func (b *BundleEntity) BundlePath() (string, error) {
	if err := b.loadBundlePath(); err != nil {
		return "", err
	}
	return b.bundlePath, nil
}

func (b *BundleEntity) MediaType() (string, error) {
	if err := b.loadMediaType(); err != nil {
		return "", err
	}

	return b.mediaType, nil
}

func (b *BundleEntity) loadMediaType() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.mediaType == "" {
		mediaType, err := loadFromEntity[string](b.Entity, PropertyBundleMediaType, optional)
		if err != nil {
			return fmt.Errorf("error determining bundle mediatype for entity '%s': %w", b.ID, err)
		}
		b.mediaType = mediaType
	}
	return nil
}

func (b *BundleEntity) loadPackage() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bundlePackage == nil {
		bundlePackage, err := loadFromEntity[property.Package](b.Entity, property.TypePackage, required)
		if err != nil {
			return fmt.Errorf("error determining package for entity '%s': %w", b.ID, err)
		}
		b.bundlePackage = &bundlePackage
		if b.semVersion == nil {
			semVer, err := bsemver.Parse(b.bundlePackage.Version)
			if err != nil {
				return fmt.Errorf("could not parse semver (%s) for entity '%s': %w", b.bundlePackage.Version, b.ID, err)
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
		providedGVKs, err := loadFromEntity[[]GVK](b.Entity, property.TypeGVK, optional)
		if err != nil {
			return fmt.Errorf("error determining bundle provided gvks for entity '%s': %w", b.ID, err)
		}
		b.providedGVKs = providedGVKs
	}
	return nil
}

func (b *BundleEntity) loadRequiredGVKs() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.requiredGVKs == nil {
		requiredGVKs, err := loadFromEntity[[]GVK](b.Entity, property.TypeGVKRequired, optional)
		if err != nil {
			return fmt.Errorf("error determining bundle required gvks for entity '%s': %w", b.ID, err)
		}
		b.requiredGVKs = requiredGVKs
	}
	return nil
}

func (b *BundleEntity) loadRequiredPackages() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.requiredPackages == nil {
		requiredPackages, err := loadFromEntity[[]PackageRequired](b.Entity, property.TypePackageRequired, optional)
		if err != nil {
			return fmt.Errorf("error determining bundle required packages for entity '%s': %w", b.ID, err)
		}
		for _, requiredPackage := range requiredPackages {
			semverRange, err := bsemver.ParseRange(requiredPackage.VersionRange)
			if err != nil {
				return fmt.Errorf("error determining bundle required package semver range for entity '%s': '%w'", b.ID, err)
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
	if b.channel == nil {
		channel, err := loadFromEntity[property.Channel](b.Entity, property.TypeChannel, required)
		if err != nil {
			return fmt.Errorf("error determining bundle channel properties for entity '%s': %w", b.ID, err)
		}
		b.channel = &channel
	}
	return nil
}

func (b *BundleEntity) loadBundlePath() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bundlePath == "" {
		bundlePath, err := loadFromEntity[string](b.Entity, PropertyBundlePath, required)
		if err != nil {
			return fmt.Errorf("error determining bundle path for entity '%s': %w", b.ID, err)
		}
		b.bundlePath = bundlePath
	}
	return nil
}

func loadFromEntity[T interface{}](entity *input.Entity, propertyName string, required propertyRequirement) (T, error) {
	deserializedProperty := *new(T)
	propertyValue, ok := entity.Properties[propertyName]
	if ok {
		// TODO: In order to avoid invalid properties we should use a decoder that only allows the properties we expect.
		//       ie. decoder.DisallowUnknownFields()
		if err := json.Unmarshal([]byte(propertyValue), &deserializedProperty); err != nil {
			return deserializedProperty, fmt.Errorf("property '%s' ('%s') could not be parsed: %w", propertyName, propertyValue, err)
		}
	} else if required {
		return deserializedProperty, fmt.Errorf("required property '%s' not found", propertyName)
	}
	return deserializedProperty, nil
}
