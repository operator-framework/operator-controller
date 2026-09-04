package resolve

import (
	"context"
	"io/fs"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.podman.io/image/v5/docker/reference"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
	imageutil "github.com/operator-framework/operator-controller/internal/shared/util/image"
	csvbuilder "github.com/operator-framework/operator-controller/internal/testing/bundle/csv"
	bundlefs "github.com/operator-framework/operator-controller/internal/testing/bundle/fs"
)

func TestOCIImageResolverResolve(t *testing.T) {
	ref := "quay.io/example/operator@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	bundleFS := bundlefs.Builder().
		WithPackageName("example-operator").
		WithCSV(csvbuilder.Builder().WithName("example-operator.v1.2.3").WithAnnotations(map[string]string{
			source.PropertyOLMProperties: `[{"type":"olm.package","value":{"packageName":"example-operator","version":"1.2.3"}}]`,
		}).Build()).
		Build()

	resolver := &OCIImageResolver{Puller: fakePuller{fs: bundleFS, ref: ref}, Cache: fakeCache{}}
	ext := &ocv1.ClusterExtension{Spec: ocv1.ClusterExtensionSpec{Source: ocv1.SourceConfig{
		SourceType: ocv1.SourceTypeOCIImage,
		OCIImage:   &ocv1.OCIImageSource{Ref: ref},
	}}}

	bundle, version, deprecation, err := resolver.Resolve(context.Background(), ext, nil)
	require.NoError(t, err)
	require.Equal(t, "example-operator.v1.2.3", bundle.Name)
	require.Equal(t, "example-operator", bundle.Package)
	require.Equal(t, ref, bundle.Image)
	require.Equal(t, "1.2.3", version.Version.String())
	require.Nil(t, deprecation)
}

func TestOCIImageResolverRejectsInvalidBundle(t *testing.T) {
	ref := "quay.io/example/operator@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	resolver := &OCIImageResolver{Puller: fakePuller{fs: bundlefs.Builder().Build(), ref: ref}, Cache: fakeCache{}}
	ext := &ocv1.ClusterExtension{Spec: ocv1.ClusterExtensionSpec{Source: ocv1.SourceConfig{
		SourceType: ocv1.SourceTypeOCIImage,
		OCIImage:   &ocv1.OCIImageSource{Ref: ref},
	}}}

	_, _, _, err := resolver.Resolve(context.Background(), ext, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, reconcile.TerminalError(nil))
}

type fakePuller struct {
	fs  fs.FS
	ref string
}

func (p fakePuller) Pull(context.Context, string, string, imageutil.Cache) (fs.FS, reference.Canonical, time.Time, error) {
	canonical, err := reference.ParseNormalizedNamed(p.ref)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	return p.fs, canonical.(reference.Canonical), time.Time{}, nil
}

type fakeCache struct{ imageutil.Cache }
