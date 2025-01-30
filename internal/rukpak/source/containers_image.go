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
	"github.com/containers/image/v5/pkg/sysregistriesv2"
	"github.com/containers/image/v5/signature"
	"github.com/containers/image/v5/types"
	"github.com/go-logr/logr"
	"github.com/opencontainers/go-digest"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/operator-framework/operator-controller/internal/util"
)

var insecurePolicy = []byte(`{"default":[{"type":"insecureAcceptAnything"}]}`)

type ContainersImageRegistry struct {
	BaseCachePath     string
	SourceContextFunc func(logger logr.Logger) (*types.SystemContext, error)
}

func (i *ContainersImageRegistry) Unpack(ctx context.Context, bundle *BundleSource) (*Result, error) {
	l := log.FromContext(ctx)

	if bundle.Type != SourceTypeImage {
		panic(fmt.Sprintf("programmer error: source type %q is unable to handle specified bundle source type %q", SourceTypeImage, bundle.Type))
	}

	if bundle.Image == nil {
		return nil, reconcile.TerminalError(fmt.Errorf("error parsing bundle, bundle %s has a nil image source", bundle.Name))
	}

	// Reload registries cache in case of configuration update
	sysregistriesv2.InvalidateCache()

	srcCtx, err := i.SourceContextFunc(l)
	if err != nil {
		return nil, err
	}

	//////////////////////////////////////////////////////
	//
	// Resolve a canonical reference for the image.
	//
	//////////////////////////////////////////////////////
	imgRef, canonicalRef, _, err := resolveReferences(ctx, bundle.Image.Ref, srcCtx)
	if err != nil {
		return nil, err
	}

	//////////////////////////////////////////////////////
	//
	// Check if the image is already unpacked. If it is,
	// return the unpacked directory.
	//
	//////////////////////////////////////////////////////
	unpackPath := i.unpackPath(bundle.Name, canonicalRef.Digest())
	if isUnpacked, _, err := IsImageUnpacked(unpackPath); isUnpacked && err == nil {
		l.Info("image already unpacked", "ref", imgRef.String(), "digest", canonicalRef.Digest().String())
		return successResult(bundle.Name, unpackPath, canonicalRef), nil
	} else if err != nil {
		return nil, fmt.Errorf("error checking bundle already unpacked: %w", err)
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
	dockerRef, err := docker.NewReference(imgRef)
	if err != nil {
		return nil, fmt.Errorf("error creating source reference: %w", err)
	}

	layoutDir, err := os.MkdirTemp("", fmt.Sprintf("oci-layout-%s", bundle.Name))
	if err != nil {
		return nil, fmt.Errorf("error creating temporary directory: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(layoutDir); err != nil {
			l.Error(err, "error removing temporary OCI layout directory")
		}
	}()

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
	policyContext, err := loadPolicyContext(srcCtx, l)
	if err != nil {
		return nil, fmt.Errorf("error loading policy context: %w", err)
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
		SourceCtx: srcCtx,
		// We use the OCI layout as a temporary storage and
		// pushing signatures for OCI images is not supported
		// so we remove the source signatures when copying.
		// Signature validation will still be performed
		// accordingly to a provided policy context.
		RemoveSignatures: true,
	}); err != nil {
		return nil, fmt.Errorf("error copying image: %w", err)
	}
	l.Info("pulled image", "ref", imgRef.String(), "digest", canonicalRef.Digest().String())

	//////////////////////////////////////////////////////
	//
	// Mount the image we just pulled
	//
	//////////////////////////////////////////////////////
	if err := i.unpackImage(ctx, unpackPath, layoutRef, srcCtx); err != nil {
		if cleanupErr := DeleteReadOnlyRecursive(unpackPath); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
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
	return DeleteReadOnlyRecursive(i.bundlePath(bundle.Name))
}

func (i *ContainersImageRegistry) bundlePath(bundleName string) string {
	return filepath.Join(i.BaseCachePath, bundleName)
}

func (i *ContainersImageRegistry) unpackPath(bundleName string, digest digest.Digest) string {
	return filepath.Join(i.bundlePath(bundleName), digest.String())
}

func resolveReferences(ctx context.Context, ref string, sourceContext *types.SystemContext) (reference.Named, reference.Canonical, bool, error) {
	imgRef, err := reference.ParseNamed(ref)
	if err != nil {
		return nil, nil, false, reconcile.TerminalError(fmt.Errorf("error parsing image reference %q: %w", ref, err))
	}

	canonicalRef, isCanonical, err := resolveCanonicalRef(ctx, imgRef, sourceContext)
	if err != nil {
		return nil, nil, false, fmt.Errorf("error resolving canonical reference: %w", err)
	}
	return imgRef, canonicalRef, isCanonical, nil
}

func resolveCanonicalRef(ctx context.Context, imgRef reference.Named, imageCtx *types.SystemContext) (reference.Canonical, bool, error) {
	if canonicalRef, ok := imgRef.(reference.Canonical); ok {
		return canonicalRef, true, nil
	}

	srcRef, err := docker.NewReference(imgRef)
	if err != nil {
		return nil, false, reconcile.TerminalError(fmt.Errorf("error creating reference: %w", err))
	}

	imgSrc, err := srcRef.NewImageSource(ctx, imageCtx)
	if err != nil {
		return nil, false, fmt.Errorf("error creating image source: %w", err)
	}
	defer imgSrc.Close()

	imgManifestData, _, err := imgSrc.GetManifest(ctx, nil)
	if err != nil {
		return nil, false, fmt.Errorf("error getting manifest: %w", err)
	}
	imgDigest, err := manifest.Digest(imgManifestData)
	if err != nil {
		return nil, false, fmt.Errorf("error getting digest of manifest: %w", err)
	}
	canonicalRef, err := reference.WithDigest(reference.TrimNamed(imgRef), imgDigest)
	if err != nil {
		return nil, false, fmt.Errorf("error creating canonical reference: %w", err)
	}
	return canonicalRef, false, nil
}

func loadPolicyContext(sourceContext *types.SystemContext, l logr.Logger) (*signature.PolicyContext, error) {
	policy, err := signature.DefaultPolicy(sourceContext)
	// TODO: there are security implications to silently moving to an insecure policy
	// tracking issue: https://github.com/operator-framework/operator-controller/issues/1622
	if err != nil {
		l.Info("no default policy found, using insecure policy")
		policy, err = signature.NewPolicyFromBytes(insecurePolicy)
	}
	if err != nil {
		return nil, fmt.Errorf("error loading default policy: %w", err)
	}
	return signature.NewPolicyContext(policy)
}

func (i *ContainersImageRegistry) unpackImage(ctx context.Context, unpackPath string, imageReference types.ImageReference, sourceContext *types.SystemContext) error {
	img, err := imageReference.NewImage(ctx, sourceContext)
	if err != nil {
		return fmt.Errorf("error reading image: %w", err)
	}
	defer func() {
		if err := img.Close(); err != nil {
			panic(err)
		}
	}()

	layoutSrc, err := imageReference.NewImageSource(ctx, sourceContext)
	if err != nil {
		return fmt.Errorf("error creating image source: %w", err)
	}
	defer func() {
		if err := layoutSrc.Close(); err != nil {
			panic(err)
		}
	}()

	if err := util.EnsureEmptyDirectory(unpackPath, 0700); err != nil {
		return fmt.Errorf("error ensuring empty unpack directory: %w", err)
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
			return errors.Join(err, DeleteReadOnlyRecursive(unpackPath))
		}
	}
	if err := SetReadOnlyRecursive(unpackPath); err != nil {
		return fmt.Errorf("error making unpack directory read-only: %w", err)
	}
	return nil
}

func applyLayer(ctx context.Context, unpackPath string, layer io.ReadCloser) error {
	decompressed, _, err := compression.AutoDecompress(layer)
	if err != nil {
		return fmt.Errorf("auto-decompress failed: %w", err)
	}
	defer decompressed.Close()

	_, err = archive.Apply(ctx, unpackPath, decompressed, archive.WithFilter(applyLayerFilter()))
	return err
}

func applyLayerFilter() archive.Filter {
	return func(h *tar.Header) (bool, error) {
		h.Uid = os.Getuid()
		h.Gid = os.Getgid()
		h.Mode |= 0700
		return true, nil
	}
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
		if err := DeleteReadOnlyRecursive(imgDirPath); err != nil {
			return fmt.Errorf("error removing image directory: %w", err)
		}
	}
	return nil
}
