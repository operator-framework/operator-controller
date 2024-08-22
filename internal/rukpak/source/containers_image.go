package source

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/containerd/containerd/archive"
	"github.com/containers/image/v5/copy"
	"github.com/containers/image/v5/docker"
	"github.com/containers/image/v5/docker/reference"
	"github.com/containers/image/v5/manifest"
	"github.com/containers/image/v5/oci/layout"
	"github.com/containers/image/v5/pkg/blobinfocache/none"
	"github.com/containers/image/v5/pkg/compression"
	"github.com/containers/image/v5/signature"
	"github.com/containers/image/v5/types"
	"github.com/opencontainers/go-digest"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type ContainersImageRegistry struct {
	BaseCachePath string
	SourceContext *types.SystemContext
}

func (i *ContainersImageRegistry) Unpack(ctx context.Context, bundle *BundleSource) (*Result, error) {
	l := log.FromContext(ctx)

	if bundle.Type != SourceTypeImage {
		panic(fmt.Sprintf("programmer error: source type %q is unable to handle specified bundle source type %q", SourceTypeImage, bundle.Type))
	}

	if bundle.Image == nil {
		return nil, NewUnrecoverable(fmt.Errorf("error parsing bundle, bundle %s has a nil image source", bundle.Name))
	}

	//////////////////////////////////////////////////////
	//
	// Resolve a canonical reference for the image.
	//
	//////////////////////////////////////////////////////
	imgRef, err := reference.ParseNamed(bundle.Image.Ref)
	if err != nil {
		return nil, NewUnrecoverable(fmt.Errorf("error parsing image reference %q: %w", bundle.Image.Ref, err))
	}

	canonicalRef, err := resolveCanonicalRef(ctx, imgRef, i.SourceContext)
	if err != nil {
		return nil, fmt.Errorf("error resolving canonical reference: %w", err)
	}

	//////////////////////////////////////////////////////
	//
	// Check if the image is already unpacked. If it is,
	// return the unpacked directory.
	//
	//////////////////////////////////////////////////////
	unpackPath := i.unpackPath(bundle.Name, canonicalRef.Digest())
	if unpackStat, err := os.Stat(unpackPath); err == nil {
		if !unpackStat.IsDir() {
			return nil, fmt.Errorf("unexpected file at unpack path %q: expected a directory", unpackPath)
		}
		l.Info("image already unpacked", "ref", imgRef.String(), "digest", canonicalRef.Digest().String())
		return successResult(bundle.Name, unpackPath, canonicalRef), nil
	}

	//////////////////////////////////////////////////////
	//
	// Create a docker reference for the source and an OCI
	// layout reference for the destination, where we will
	// temporarily store the image in order to unpack it.
	//
	// We use the OCI layout as a temporary storage because
	// copy.Image can concurrently pull all the layers.
	//
	//////////////////////////////////////////////////////
	dockerRef, err := docker.NewReference(canonicalRef)
	if err != nil {
		return nil, fmt.Errorf("error creating source reference: %w", err)
	}

	layoutDir, err := os.MkdirTemp("", fmt.Sprintf("oci-layout-%s", bundle.Name))
	if err != nil {
		return nil, fmt.Errorf("error creating temporary directory: %w", err)
	}
	defer os.RemoveAll(layoutDir)

	layoutRef, err := layout.NewReference(layoutDir, canonicalRef.String())
	if err != nil {
		return nil, fmt.Errorf("error creating reference: %w", err)
	}

	//////////////////////////////////////////////////////
	//
	// Load an image signature policy and build
	// a policy context for the image pull.
	//
	//////////////////////////////////////////////////////
	policy, err := signature.DefaultPolicy(i.SourceContext)
	if os.IsNotExist(err) {
		l.Info("no default policy found, using insecure policy")
		policy, err = signature.NewPolicyFromBytes([]byte(`{"default":[{"type":"insecureAcceptAnything"}]}`))
	}
	if err != nil {
		return nil, fmt.Errorf("error getting policy: %w", err)
	}
	policyContext, err := signature.NewPolicyContext(policy)
	if err != nil {
		return nil, fmt.Errorf("error getting policy context: %w", err)
	}
	defer func() {
		if err := policyContext.Destroy(); err != nil {
			l.Error(err, "error destroying policy context")
		}
	}()

	//////////////////////////////////////////////////////
	//
	// Pull the image from the source to the destination
	//
	//////////////////////////////////////////////////////
	if _, err := copy.Image(ctx, policyContext, layoutRef, dockerRef, &copy.Options{
		SourceCtx: i.SourceContext,
	}); err != nil {
		return nil, fmt.Errorf("error copying image: %w", err)
	}
	l.Info("pulled image", "ref", imgRef.String(), "digest", canonicalRef.Digest().String())

	//////////////////////////////////////////////////////
	//
	// Mount the image we just pulled
	//
	//////////////////////////////////////////////////////
	if err := i.unpackImage(ctx, unpackPath, layoutRef); err != nil {
		return nil, fmt.Errorf("error unpacking image: %w", err)
	}

	//////////////////////////////////////////////////////
	//
	// Delete other images. They are no longer needed.
	//
	//////////////////////////////////////////////////////
	if err := i.deleteOtherImages(bundle.Name, canonicalRef.Digest()); err != nil {
		return nil, fmt.Errorf("error deleting old images: %w", err)
	}

	return successResult(bundle.Name, unpackPath, canonicalRef), nil
}

func successResult(bundleName, unpackPath string, canonicalRef reference.Canonical) *Result {
	return &Result{
		Bundle:         os.DirFS(unpackPath),
		ResolvedSource: &BundleSource{Type: SourceTypeImage, Name: bundleName, Image: &ImageSource{Ref: canonicalRef.String()}},
		State:          StateUnpacked,
		Message:        fmt.Sprintf("unpacked %q successfully", canonicalRef),
	}
}

func (i *ContainersImageRegistry) Cleanup(_ context.Context, bundle *BundleSource) error {
	return os.RemoveAll(i.bundlePath(bundle.Name))
}

func (i *ContainersImageRegistry) bundlePath(bundleName string) string {
	return filepath.Join(i.BaseCachePath, bundleName)
}

func (i *ContainersImageRegistry) unpackPath(bundleName string, digest digest.Digest) string {
	return filepath.Join(i.bundlePath(bundleName), digest.String())
}

func resolveCanonicalRef(ctx context.Context, imgRef reference.Named, imageCtx *types.SystemContext) (reference.Canonical, error) {
	if canonicalRef, ok := imgRef.(reference.Canonical); ok {
		return canonicalRef, nil
	}

	srcRef, err := docker.NewReference(imgRef)
	if err != nil {
		return nil, NewUnrecoverable(fmt.Errorf("error creating reference: %w", err))
	}

	imgSrc, err := srcRef.NewImageSource(ctx, imageCtx)
	if err != nil {
		return nil, fmt.Errorf("error creating image source: %w", err)
	}
	defer imgSrc.Close()

	imgManifestData, _, err := imgSrc.GetManifest(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("error getting manifest: %w", err)
	}
	imgDigest, err := manifest.Digest(imgManifestData)
	if err != nil {
		return nil, fmt.Errorf("error getting digest of manifest: %w", err)
	}
	return reference.WithDigest(reference.TrimNamed(imgRef), imgDigest)
}

func (i *ContainersImageRegistry) unpackImage(ctx context.Context, unpackPath string, imageReference types.ImageReference) error {
	img, err := imageReference.NewImage(ctx, i.SourceContext)
	if err != nil {
		return fmt.Errorf("error reading image: %w", err)
	}
	defer func() {
		if err := img.Close(); err != nil {
			panic(err)
		}
	}()

	layoutSrc, err := imageReference.NewImageSource(ctx, i.SourceContext)
	if err != nil {
		return fmt.Errorf("error creating image source: %w", err)
	}

	if err := os.MkdirAll(unpackPath, 0755); err != nil {
		return fmt.Errorf("error creating unpack directory: %w", err)
	}
	l := log.FromContext(ctx)
	l.Info("unpacking image", "path", unpackPath)
	for i, layerInfo := range img.LayerInfos() {
		if err := func() error {
			layerReader, _, err := layoutSrc.GetBlob(ctx, layerInfo, none.NoCache)
			if err != nil {
				return fmt.Errorf("error getting blob for layer[%d]: %w", i, err)
			}
			defer layerReader.Close()

			if err := applyLayer(ctx, unpackPath, layerReader); err != nil {
				return fmt.Errorf("error applying layer[%d]: %w", i, err)
			}
			l.Info("applied layer", "layer", i)
			return nil
		}(); err != nil {
			return errors.Join(err, os.RemoveAll(unpackPath))
		}
	}
	return nil
}

func applyLayer(ctx context.Context, unpackPath string, layer io.ReadCloser) error {
	decompressed, _, err := compression.AutoDecompress(layer)
	if err != nil {
		return fmt.Errorf("auto-decompress failed: %w", err)
	}
	defer decompressed.Close()

	_, err = archive.Apply(ctx, unpackPath, decompressed, archive.WithFilter(func(h *tar.Header) (bool, error) {
		h.Uid = os.Getuid()
		h.Gid = os.Getgid()
		h.Mode |= 0770
		return true, nil
	}))
	return err
}

func (i *ContainersImageRegistry) deleteOtherImages(bundleName string, digestToKeep digest.Digest) error {
	bundlePath := i.bundlePath(bundleName)
	imgDirs, err := os.ReadDir(bundlePath)
	if err != nil {
		return fmt.Errorf("error reading image directories: %w", err)
	}
	for _, imgDir := range imgDirs {
		if imgDir.Name() == digestToKeep.String() {
			continue
		}
		imgDirPath := filepath.Join(bundlePath, imgDir.Name())
		if err := os.RemoveAll(imgDirPath); err != nil {
			return fmt.Errorf("error removing image directory: %w", err)
		}
	}
	return nil
}
