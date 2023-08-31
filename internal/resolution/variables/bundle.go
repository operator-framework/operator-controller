package variables

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/constraint"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-registry/alpha/property"

	bsemver "github.com/blang/semver/v4"
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

type GVKRequired property.GVKRequired

func (g GVKRequired) String() string {
	return fmt.Sprintf(`group:"%s" version:"%s" kind:"%s"`, g.Group, g.Version, g.Kind)
}

func (g GVKRequired) AsGVK() GVK {
	return GVK(g)
}

var _ deppy.Variable = &BundleVariable{}

type BundleVariable struct {
	*input.SimpleVariable
	ID           deppy.Identifier
	Properties   map[string]string
	dependencies []*BundleVariable

	// these properties are lazy loaded as they are requested
	bundlePackage    *property.Package
	providedGVKs     []GVK
	requiredGVKs     []GVKRequired
	requiredPackages []PackageRequired
	channel          *property.Channel
	channelEntry     *ChannelEntry
	semVersion       *bsemver.Version
	bundlePath       string
	mediaType        string
	mu               sync.RWMutex
}

type BundleList []BundleVariable
type BundleListMap map[string]BundleList

func (b *BundleVariable) Dependencies() []*BundleVariable {
	return b.dependencies
}

func NewBundleVariable(variable deppy.Variable, dependencyBundleVariables []*BundleVariable, properties map[string]string) *BundleVariable {
	dependencyIDs := make([]deppy.Identifier, 0, len(dependencyBundleVariables))
	for _, bundle := range dependencyBundleVariables {
		dependencyIDs = append(dependencyIDs, bundle.ID)
	}
	var constraints []deppy.Constraint
	if len(dependencyIDs) > 0 {
		constraints = append(constraints, constraint.Dependency(dependencyIDs...))
	}
	return &BundleVariable{
		SimpleVariable: input.NewSimpleVariable(variable.Identifier(), constraints...),
		dependencies:   dependencyBundleVariables,
		mu:             sync.RWMutex{},
		Properties:     properties,
	}
}

var _ deppy.Variable = &BundleUniquenessVariable{}

type BundleUniquenessVariable struct {
	*input.SimpleVariable
}

// NewBundleUniquenessVariable creates a new variable that instructs the resolver to choose at most a single bundle
// from the input 'atMostID'. Examples:
// 1. restrict the solution to at most a single bundle per package
// 2. restrict the solution to at most a single bundler per provided gvk
// this guarantees that no two operators provide the same gvk and no two version of the same operator are running at the same time
func NewBundleUniquenessVariable(id deppy.Identifier, atMostIDs ...deppy.Identifier) *BundleUniquenessVariable {
	return &BundleUniquenessVariable{
		SimpleVariable: input.NewSimpleVariable(id, constraint.AtMost(1, atMostIDs...)),
	}
}

func (b *BundleVariable) PackageName() (string, error) {
	if err := b.loadPackage(); err != nil {
		return "", err
	}
	return b.bundlePackage.PackageName, nil
}

func (b *BundleVariable) Version() (*bsemver.Version, error) {
	if err := b.loadPackage(); err != nil {
		return nil, err
	}
	return b.semVersion, nil
}

func (b *BundleVariable) ProvidedGVKs() ([]GVK, error) {
	if err := b.loadProvidedGVKs(); err != nil {
		return nil, err
	}
	return b.providedGVKs, nil
}

func (b *BundleVariable) RequiredGVKs() ([]GVKRequired, error) {
	if err := b.loadRequiredGVKs(); err != nil {
		return nil, err
	}
	return b.requiredGVKs, nil
}

func (b *BundleVariable) RequiredPackages() ([]PackageRequired, error) {
	if err := b.loadRequiredPackages(); err != nil {
		return nil, err
	}
	return b.requiredPackages, nil
}

func (b *BundleVariable) ChannelName() (string, error) {
	if err := b.loadChannelProperties(); err != nil {
		return "", err
	}
	return b.channel.ChannelName, nil
}

func (b *BundleVariable) Channel() (*property.Channel, error) {
	if err := b.loadChannelProperties(); err != nil {
		return nil, err
	}
	return b.channel, nil
}

func (b *BundleVariable) BundleChannelEntry() (*ChannelEntry, error) {
	if err := b.loadBundleChannelEntry(); err != nil {
		return nil, err
	}
	return b.channelEntry, nil
}

func (b *BundleVariable) loadBundleChannelEntry() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.channelEntry == nil {
		channelEntry, err := loadFromVariable[*ChannelEntry](b, PropertyBundleChannelEntry, optional)
		if err != nil || channelEntry == nil {
			return fmt.Errorf("error determining replaces for variable '%s': %w", b.ID, err)
		}
		b.channelEntry = channelEntry
	}
	return nil
}

func (b *BundleVariable) BundlePath() (string, error) {
	if err := b.loadBundlePath(); err != nil {
		return "", err
	}
	return b.bundlePath, nil
}

func (b *BundleVariable) MediaType() (string, error) {
	if err := b.loadMediaType(); err != nil {
		return "", err
	}

	return b.mediaType, nil
}

func (b *BundleVariable) loadMediaType() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.mediaType == "" {
		mediaType, err := loadFromVariable[string](b, PropertyBundleMediaType, optional)
		if err != nil {
			return fmt.Errorf("error determining bundle mediatype for entity '%s': %w", b.ID, err)
		}
		b.mediaType = mediaType
	}
	return nil
}

func (b *BundleVariable) loadPackage() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bundlePackage == nil {
		bundlePackage, err := loadFromVariable[property.Package](b, property.TypePackage, required)
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

func (b *BundleVariable) loadProvidedGVKs() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.providedGVKs == nil {
		providedGVKs, err := loadFromVariable[[]GVK](b, property.TypeGVK, optional)
		if err != nil {
			return fmt.Errorf("error determining bundle provided gvks for entity '%s': %w", b.ID, err)
		}
		b.providedGVKs = providedGVKs
	}
	return nil
}

func (b *BundleVariable) loadRequiredGVKs() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.requiredGVKs == nil {
		requiredGVKs, err := loadFromVariable[[]GVKRequired](b, property.TypeGVKRequired, optional)
		if err != nil {
			return fmt.Errorf("error determining bundle required gvks for entity '%s': %w", b.ID, err)
		}
		b.requiredGVKs = requiredGVKs
	}
	return nil
}

func (b *BundleVariable) loadRequiredPackages() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.requiredPackages == nil {
		requiredPackages, err := loadFromVariable[[]PackageRequired](b, property.TypePackageRequired, optional)
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

func (b *BundleVariable) loadChannelProperties() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.channel == nil {
		channel, err := loadFromVariable[property.Channel](b, property.TypeChannel, required)
		if err != nil {
			return fmt.Errorf("error determining bundle channel properties for entity '%s': %w", b.ID, err)
		}
		b.channel = &channel
	}
	return nil
}

func (b *BundleVariable) loadBundlePath() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bundlePath == "" {
		bundlePath, err := loadFromVariable[string](b, PropertyBundlePath, required)
		if err != nil {
			return fmt.Errorf("error determining bundle path for entity '%s': %w", b.ID, err)
		}
		b.bundlePath = bundlePath
	}
	return nil
}

func loadFromVariable[T interface{}](variable *BundleVariable, propertyName string, required propertyRequirement) (T, error) {
	deserializedProperty := *new(T)
	propertyValue, ok := variable.Properties[propertyName]
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
