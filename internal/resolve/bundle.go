package resolve

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	bsemver "github.com/blang/semver/v4"
	"github.com/containers/image/v5/docker/reference"

	"github.com/operator-framework/operator-registry/alpha/action"
	"github.com/operator-framework/operator-registry/alpha/declcfg"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/bundleutil"
	"github.com/operator-framework/operator-controller/internal/rukpak/source"
)

type BundleResolver struct {
	Unpacker                source.Unpacker
	BrittleUnpackerCacheDir string
}

func (r *BundleResolver) Resolve(ctx context.Context, ext *ocv1.ClusterExtension, _ *ocv1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
	res, err := r.Unpacker.Unpack(ctx, &source.BundleSource{
		Name: ext.Name,
		Type: source.SourceTypeImage,
		Image: &source.ImageSource{
			Ref: ext.Spec.Source.Bundle.Ref,
		},
	})
	if err != nil {
		return nil, nil, nil, err
	}
	if res.State != source.StateUnpacked {
		return nil, nil, nil, fmt.Errorf("bundle not unpacked: %v", res.Message)
	}

	ref, err := reference.ParseNamed(res.ResolvedSource.Image.Ref)
	if err != nil {
		return nil, nil, nil, err
	}
	canonicalRef, ok := ref.(reference.Canonical)
	if !ok {
		return nil, nil, nil, errors.New("expected canonical reference")
	}
	bundlePath := filepath.Join(r.BrittleUnpackerCacheDir, ext.Name, canonicalRef.Digest().String())

	// TODO: This is a temporary workaround to get the bundle from the filesystem
	//    until the operator-registry library is updated to support reading from a
	//    filesystem. This will be removed once the library is updated.

	render := action.Render{
		Refs:           []string{bundlePath},
		AllowedRefMask: action.RefBundleDir,
	}
	fbc, err := render.Run(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(fbc.Bundles) != 1 {
		return nil, nil, nil, errors.New("expected exactly one bundle")
	}
	bundle := fbc.Bundles[0]
	bundle.Image = canonicalRef.String()
	v, err := bundleutil.GetVersion(bundle)
	if err != nil {
		return nil, nil, nil, err
	}
	return &bundle, v, nil, nil
}
