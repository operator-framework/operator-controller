package resolve

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/bundleutil"
	bundlesource "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
	imageutil "github.com/operator-framework/operator-controller/internal/shared/util/image"
)

// OCIImageResolver resolves a bundle directly from an OCI image. The image is
// unpacked through the shared image cache before its content is inspected.
type OCIImageResolver struct {
	Puller imageutil.Puller
	Cache  imageutil.Cache
}

// Resolve loads a registry+v1 bundle from the direct OCIImage source. Direct
// sources intentionally do not consult catalogs or perform dependency resolution.
func (r *OCIImageResolver) Resolve(ctx context.Context, ext *ocv1.ClusterExtension, _ *ocv1.BundleMetadata) (*declcfg.Bundle, *declcfg.VersionRelease, *declcfg.Deprecation, error) {
	if ext.Spec.Source.OCIImage.Ref == "" {
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

	bundle, err := bundleFromFS(imageFS, canonicalRef.String())
	if err != nil {
		return nil, nil, nil, reconcile.TerminalError(fmt.Errorf("invalid direct bundle image: %w", err))
	}
	versionRelease, err := bundleutil.GetVersionAndRelease(*bundle)
	if err != nil {
		return nil, nil, nil, reconcile.TerminalError(err)
	}
	return bundle, versionRelease, nil, nil
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
	if err := json.Unmarshal([]byte(propertiesJSON), &bundle.Properties); err != nil {
		return nil, fmt.Errorf("failed to parse bundle properties: %w", err)
	}
	return bundle, nil
}
