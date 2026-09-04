package resolve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/bundleutil"
	bundlesource "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
	imageutil "github.com/operator-framework/operator-controller/internal/shared/util/image"
)

// OCIImageResolver resolves a bundle directly from an OCI image. The image is
// unpacked through the shared image cache before its content is inspected.
type OCIImageResolver struct {
	Puller    imageutil.Puller
	Cache     imageutil.Cache
	Detectors []BundleContentDetector
}

// BundleContentDetector identifies and loads a supported bundle format from
// already-unpacked image content.
type BundleContentDetector interface {
	Detect(fs.FS, string) (*declcfg.Bundle, error)
}

// RegistryV1ContentDetector loads registry+v1 bundles from their filesystem layout.
type RegistryV1ContentDetector struct{}

func (RegistryV1ContentDetector) Detect(bundleFS fs.FS, image string) (*declcfg.Bundle, error) {
	return bundleFromFS(bundleFS, image)
}

// Resolve loads a registry+v1 bundle from the direct OCIImage source. Direct
// sources intentionally do not consult catalogs or perform dependency resolution.
func (r *OCIImageResolver) Resolve(ctx context.Context, ext *ocv1.ClusterExtension, _ *ocv1.BundleMetadata) (*declcfg.Bundle, *declcfg.VersionRelease, *declcfg.Deprecation, error) {
	if ext.Spec.Source.OCIImage == nil {
		return nil, nil, nil, reconcile.TerminalError(fmt.Errorf("OCIImage source is missing ociImage.ref"))
	}
	if r.Puller == nil || r.Cache == nil {
		return nil, nil, nil, fmt.Errorf("direct OCIImage resolver is not configured")
	}

	imageFS, canonicalRef, _, err := r.Puller.Pull(ctx, ext.Name, ext.Spec.Source.OCIImage.Ref, r.Cache)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to pull direct bundle image: %w", err)
	}
	if canonicalRef == nil {
		return nil, nil, nil, fmt.Errorf("direct bundle image pull returned no canonical reference")
	}

	bundle, err := r.detect(imageFS, canonicalRef.String())
	if err != nil {
		return nil, nil, nil, reconcile.TerminalError(fmt.Errorf("invalid direct bundle image: %w", err))
	}
	versionRelease, err := bundleutil.GetVersionAndRelease(*bundle)
	if err != nil {
		return nil, nil, nil, reconcile.TerminalError(err)
	}
	return bundle, versionRelease, nil, nil
}

func (r *OCIImageResolver) detect(bundleFS fs.FS, image string) (*declcfg.Bundle, error) {
	detectors := r.Detectors
	if len(detectors) == 0 {
		detectors = []BundleContentDetector{RegistryV1ContentDetector{}}
	}
	var errs []error
	for _, detector := range detectors {
		bundle, err := detector.Detect(bundleFS, image)
		if err == nil {
			return bundle, nil
		}
		errs = append(errs, err)
	}
	return nil, errors.Join(errs...)
}

func bundleFromFS(bundleFS fs.FS, image string) (*declcfg.Bundle, error) {
	registryBundle, err := bundlesource.FromFS(bundleFS).GetBundle()
	if err != nil {
		return nil, err
	}

	bundle := &declcfg.Bundle{
		Name:    registryBundle.CSV.Name,
		Package: registryBundle.PackageName,
		Image:   image,
	}
	propertiesJSON := registryBundle.CSV.Annotations[bundlesource.PropertyOLMProperties]
	if propertiesJSON == "" {
		return nil, fmt.Errorf("bundle %q has no %q package property", bundle.Name, bundlesource.PropertyOLMProperties)
	}
	if err := json.Unmarshal([]byte(propertiesJSON), &bundle.Properties); err != nil {
		return nil, fmt.Errorf("failed to parse bundle properties: %w", err)
	}
	if !hasPackageProperty(bundle.Properties) {
		return nil, fmt.Errorf("bundle %q has no package property", bundle.Name)
	}
	return bundle, nil
}

func hasPackageProperty(properties []property.Property) bool {
	for _, p := range properties {
		if p.Type == property.TypePackage {
			return true
		}
	}
	return false
}
