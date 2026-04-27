package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
)

// Catalog is a builder for constructing per-scenario FBC catalogs
// with parameterized package names and OCI image references.
type Catalog struct {
	name       string // user-chosen catalog name (e.g. "test", "extra")
	scenarioID string
	packages   []packageSpec
}

type packageSpec struct {
	name     string
	bundles  []bundleDef
	channels []channelDef
}

type bundleDef struct {
	version string
	opts    []BundleOption
}

type channelDef struct {
	name    string
	entries []entryDef
}

type entryDef struct {
	version  string
	replaces string
}

// PackageOption configures a package within a catalog.
type PackageOption func(*packageSpec)

// ChannelOption configures a channel entry.
type ChannelOption func(*entryDef)

// NewCatalog creates a new per-scenario catalog builder.
func NewCatalog(name, scenarioID string, opts ...PackageOption) *Catalog {
	c := &Catalog{name: name, scenarioID: scenarioID}
	for _, o := range opts {
		pkg := packageSpec{}
		o(&pkg)
		c.packages = append(c.packages, pkg)
	}
	return c
}

// WithPackage defines a package in the catalog.
func WithPackage(name string, opts ...PackageOption) PackageOption {
	return func(p *packageSpec) {
		p.name = name
		for _, o := range opts {
			o(p)
		}
	}
}

// Bundle defines a bundle version with its content options.
func Bundle(version string, opts ...BundleOption) PackageOption {
	return func(p *packageSpec) {
		p.bundles = append(p.bundles, bundleDef{version: version, opts: opts})
	}
}

// Channel defines a channel with its entries.
func Channel(name string, entries ...ChannelOption) PackageOption {
	return func(p *packageSpec) {
		ch := channelDef{name: name}
		for _, e := range entries {
			entry := entryDef{}
			e(&entry)
			ch.entries = append(ch.entries, entry)
		}
		p.channels = append(p.channels, ch)
	}
}

// Entry defines a channel entry for a specific version.
func Entry(version string, opts ...ChannelOption) ChannelOption {
	return func(e *entryDef) {
		e.version = version
		for _, o := range opts {
			o(e)
		}
	}
}

// Replaces declares the version this entry replaces.
func Replaces(version string) ChannelOption {
	return func(e *entryDef) {
		e.replaces = version
	}
}

// BuildResult contains the outputs of a successful catalog build.
type BuildResult struct {
	// CatalogImageRef is the in-cluster image reference for the ClusterCatalog source.
	CatalogImageRef string
	// CatalogName is the name to use for the ClusterCatalog resource.
	CatalogName string
	// PackageNames maps original package names to their scenario-parameterized names.
	PackageNames map[string]string
}

// Build generates all bundle images and the catalog image, pushes them to the registry,
// and returns the image refs needed to create a ClusterCatalog.
//
// tag is the image tag for the catalog image (e.g. "v1", "latest").
// localRegistry is the registry address accessible from the test process (obtained via port-forward).
// clusterRegistry is the registry address accessible from inside the cluster
// (e.g. docker-registry.operator-controller-e2e.svc.cluster.local:5000).
func (c *Catalog) Build(ctx context.Context, tag, localRegistry, clusterRegistry string) (*BuildResult, error) {
	packageNames := make(map[string]string)
	var fbcDocs []string

	for _, pkg := range c.packages {
		paramPkgName := fmt.Sprintf("%s-%s", pkg.name, c.scenarioID)
		packageNames[pkg.name] = paramPkgName

		bundleImageRefs := make(map[string]string) // version -> cluster image ref

		// Build and push each bundle image
		for _, bd := range pkg.bundles {
			spec, err := buildBundle(c.scenarioID, paramPkgName, bd.version, bd.opts)
			if err != nil {
				return nil, fmt.Errorf("failed to build bundle %s:%s: %w", paramPkgName, bd.version, err)
			}

			img, err := crane.Image(spec.files)
			if err != nil {
				return nil, fmt.Errorf("failed to create bundle image for %s:%s: %w", paramPkgName, bd.version, err)
			}

			labels := map[string]string{
				"operators.operatorframework.io.bundle.mediatype.v1": "registry+v1",
				"operators.operatorframework.io.bundle.manifests.v1": "manifests/",
				"operators.operatorframework.io.bundle.metadata.v1":  "metadata/",
				"operators.operatorframework.io.bundle.package.v1":   paramPkgName,
				"operators.operatorframework.io.bundle.channels.v1":  "default",
			}
			img, err = mutate.Config(img, v1.Config{Labels: labels})
			if err != nil {
				return nil, fmt.Errorf("failed to set bundle labels for %s:%s: %w", paramPkgName, bd.version, err)
			}

			bundleTag := fmt.Sprintf("%s/bundles/%s:v%s", localRegistry, paramPkgName, bd.version)
			if err := crane.Push(img, bundleTag, crane.Insecure, crane.WithContext(ctx)); err != nil {
				return nil, fmt.Errorf("failed to push bundle image %s: %w", bundleTag, err)
			}
			bundleClusterRegistry := clusterRegistry
			if spec.clusterRegistryOverride != "" {
				bundleClusterRegistry = spec.clusterRegistryOverride
			}
			bundleImageRefs[bd.version] = fmt.Sprintf("%s/bundles/%s:v%s", bundleClusterRegistry, paramPkgName, bd.version)
		}

		// Generate FBC for this package
		if len(pkg.channels) == 0 {
			return nil, fmt.Errorf("package %q must define at least one channel", pkg.name)
		}
		fbcDocs = append(fbcDocs, fmt.Sprintf(`schema: olm.package
name: %s`, paramPkgName))

		for _, ch := range pkg.channels {
			var entries []string
			for _, e := range ch.entries {
				entry := fmt.Sprintf("  - name: %s.%s", paramPkgName, e.version)
				if e.replaces != "" {
					entry += fmt.Sprintf("\n    replaces: %s.%s", paramPkgName, e.replaces)
				}
				entries = append(entries, entry)
			}
			fbcDocs = append(fbcDocs, fmt.Sprintf(`schema: olm.channel
name: %s
package: %s
entries:
%s`, ch.name, paramPkgName, strings.Join(entries, "\n")))
		}

		for _, bd := range pkg.bundles {
			imageRef := bundleImageRefs[bd.version]
			fbcDocs = append(fbcDocs, fmt.Sprintf(`schema: olm.bundle
name: %s.%s
package: %s
image: %s
properties:
  - type: olm.package
    value:
      packageName: %s
      version: %s`, paramPkgName, bd.version, paramPkgName, imageRef, paramPkgName, bd.version))
		}
	}

	// Build catalog image
	fbcContent := strings.Join(fbcDocs, "\n---\n")
	catalogFiles := map[string][]byte{
		"configs/catalog.yaml": []byte(fbcContent),
	}
	catalogImg, err := crane.Image(catalogFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to create catalog image: %w", err)
	}
	catalogImg, err = mutate.Config(catalogImg, v1.Config{
		Labels: map[string]string{
			"operators.operatorframework.io.index.configs.v1": "/configs",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to set catalog labels: %w", err)
	}

	catalogTag := fmt.Sprintf("%s/e2e/%s-catalog-%s:%s", localRegistry, c.name, c.scenarioID, tag)
	if err := crane.Push(catalogImg, catalogTag, crane.Insecure, crane.WithContext(ctx)); err != nil {
		return nil, fmt.Errorf("failed to push catalog image %s: %w", catalogTag, err)
	}

	return &BuildResult{
		CatalogImageRef: fmt.Sprintf("%s/e2e/%s-catalog-%s:%s", clusterRegistry, c.name, c.scenarioID, tag),
		CatalogName:     fmt.Sprintf("%s-catalog-%s", c.name, c.scenarioID),
		PackageNames:    packageNames,
	}, nil
}
