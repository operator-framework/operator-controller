package resolve

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"reflect"

	bsemver "github.com/blang/semver/v4"

	"github.com/operator-framework/operator-registry/alpha/action"
	"github.com/operator-framework/operator-registry/alpha/declcfg"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/bundleutil"
	"github.com/operator-framework/operator-controller/internal/shared/util/image"
)

type BundleResolver struct {
	ImagePuller image.Puller
	ImageCache  image.Cache
}

func (r *BundleResolver) Resolve(ctx context.Context, ext *ocv1.ClusterExtension, _ *ocv1.BundleMetadata) (*declcfg.Bundle, *bsemver.Version, *declcfg.Deprecation, error) {
	bundleFS, canonicalRef, _, err := r.ImagePuller.Pull(ctx, ext.Name, ext.Spec.Source.Bundle.Ref, r.ImageCache)
	if err != nil {
		return nil, nil, nil, err
	}

	// TODO: This is a temporary workaround to get the bundle from the filesystem
	//    until the operator-registry library is updated to support reading from a
	//    fs.FS. This will be removed once the library is updated.
	bundlePath, err := getDirFSPath(bundleFS)
	if err != nil {
		panic(fmt.Errorf("expected to be able to recover bundle path from bundleFS: %v", err))
	}

	// Render the bundle
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

// A function to recover the underlying path string from os.DirFS
func getDirFSPath(f fs.FS) (string, error) {
	v := reflect.ValueOf(f)

	// Check if the underlying type is a string (its kind)
	if v.Kind() != reflect.String {
		return "", fmt.Errorf("underlying type is not a string, it is %s", v.Kind())
	}

	// The type itself (os.dirFS) is unexported, but its Kind is a string.
	// We can convert the reflect.Value back to a regular string using .Interface()
	// after converting it to a basic string type.
	path, ok := v.Convert(reflect.TypeOf("")).Interface().(string)
	if !ok {
		return "", fmt.Errorf("could not convert reflected value to string")
	}

	return path, nil
}
