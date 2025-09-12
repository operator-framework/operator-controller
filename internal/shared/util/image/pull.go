package image

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"os"
	"time"

	"github.com/containers/image/v5/copy"
	"github.com/containers/image/v5/docker"
	"github.com/containers/image/v5/docker/reference"
	"github.com/containers/image/v5/image"
	"github.com/containers/image/v5/manifest"
	"github.com/containers/image/v5/oci/layout"
	"github.com/containers/image/v5/pkg/blobinfocache/none"
	"github.com/containers/image/v5/pkg/compression"
	"github.com/containers/image/v5/pkg/sysregistriesv2"
	"github.com/containers/image/v5/signature"
	"github.com/containers/image/v5/types"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
	"github.com/operator-framework/operator-controller/internal/shared/util/http"
)

type Puller interface {
	Pull(context.Context, string, string, Cache) (fs.FS, reference.Canonical, time.Time, error)
}

var insecurePolicy = []byte(`{"default":[{"type":"insecureAcceptAnything"}]}`)

type ContainersImagePuller struct {
	SourceCtxFunc func(context.Context) (*types.SystemContext, error)
}

func (p *ContainersImagePuller) Pull(ctx context.Context, ownerID string, ref string, cache Cache) (fs.FS, reference.Canonical, time.Time, error) {
	srcCtx, err := p.SourceCtxFunc(ctx)
	if err != nil {
		return nil, nil, time.Time{}, err
	}

	dockerRef, err := reference.ParseNamed(ref)
	if err != nil {
		return nil, nil, time.Time{}, reconcile.TerminalError(fmt.Errorf("error parsing image reference %q: %w", ref, err))
	}

	l := log.FromContext(ctx, "ref", dockerRef.String())
	ctx = log.IntoContext(ctx, l)

	fsys, canonicalRef, modTime, err := p.pull(ctx, ownerID, dockerRef, cache, srcCtx)
	if err != nil {
		// Log any CertificateVerificationErrors, and log Docker Certificates if necessary
		if http.LogCertificateVerificationError(err, l) {
			http.LogDockerCertificates(srcCtx.DockerCertPath, l)
		}
		return nil, nil, time.Time{}, err
	}
	return fsys, canonicalRef, modTime, nil
}

func (p *ContainersImagePuller) pull(ctx context.Context, ownerID string, dockerRef reference.Named, cache Cache, srcCtx *types.SystemContext) (fs.FS, reference.Canonical, time.Time, error) {
	l := log.FromContext(ctx)

	dockerImgRef, err := docker.NewReference(dockerRef)
	if err != nil {
		return nil, nil, time.Time{}, reconcile.TerminalError(fmt.Errorf("error creating reference: %w", err))
	}

	// Reload registries cache in case of configuration update
	sysregistriesv2.InvalidateCache()

	//////////////////////////////////////////////////////
	//
	// Resolve a canonical reference for the image.
	//
	//////////////////////////////////////////////////////
	canonicalRef, err := resolveCanonicalRef(ctx, dockerImgRef, srcCtx)
	if err != nil {
		return nil, nil, time.Time{}, err
	}

	l = l.WithValues("digest", canonicalRef.Digest().String())
	ctx = log.IntoContext(ctx, l)

	///////////////////////////////////////////////////////
	//
	// Check if the cache has already applied the
	// canonical keep. If so, we're done.
	//
	///////////////////////////////////////////////////////
	fsys, modTime, err := cache.Fetch(ctx, ownerID, canonicalRef)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("error checking cache for existing content: %w", err)
	}
	if fsys != nil {
		return fsys, canonicalRef, modTime, nil
	}

	//////////////////////////////////////////////////////
	//
	// Create an OCI layout reference for the destination,
	// where we will temporarily store the image in order
	// to unpack it.
	//
	// We use the OCI layout as a temporary storage because
	// copy.Image can concurrently pull all the layers.
	//
	//////////////////////////////////////////////////////
	layoutDir, err := os.MkdirTemp("", fmt.Sprintf("oci-layout-%s-", ownerID))
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("error creating temporary directory: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(layoutDir); err != nil {
			l.Error(err, "error removing temporary OCI layout directory")
		}
	}()

	layoutImgRef, err := layout.NewReference(layoutDir, canonicalRef.String())
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("error creating reference: %w", err)
	}

	//////////////////////////////////////////////////////
	//
	// Load an image signature policy and build
	// a policy context for the image pull.
	//
	//////////////////////////////////////////////////////
	policyContext, err := loadPolicyContext(srcCtx, l)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("error loading policy context: %w", err)
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
	if _, err := copy.Image(ctx, policyContext, layoutImgRef, dockerImgRef, &copy.Options{
		SourceCtx: srcCtx,
		// We use the OCI layout as a temporary storage and
		// pushing signatures for OCI images is not supported
		// so we remove the source signatures when copying.
		// Signature validation will still be performed
		// accordingly to a provided policy context.
		RemoveSignatures: true,
	}); err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("error copying image: %w", err)
	}
	l.Info("pulled image")

	//////////////////////////////////////////////////////
	//
	// Mount the image we just pulled
	//
	//////////////////////////////////////////////////////
	fsys, modTime, err = p.applyImage(ctx, ownerID, dockerRef, canonicalRef, layoutImgRef, cache, srcCtx)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("error applying image: %w", err)
	}

	/////////////////////////////////////////////////////////////
	//
	// Clean up any images from the cache that we no longer need.
	//
	/////////////////////////////////////////////////////////////
	if err := cache.GarbageCollect(ctx, ownerID, canonicalRef); err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("error deleting old images: %w", err)
	}
	return fsys, canonicalRef, modTime, nil
}

func resolveCanonicalRef(ctx context.Context, imgRef types.ImageReference, srcCtx *types.SystemContext) (reference.Canonical, error) {
	if canonicalRef, ok := imgRef.DockerReference().(reference.Canonical); ok {
		return canonicalRef, nil
	}

	imgSrc, err := imgRef.NewImageSource(ctx, srcCtx)
	if err != nil {
		return nil, fmt.Errorf("error creating image source: %w", err)
	}
	defer imgSrc.Close()

	manifestBlob, _, err := imgSrc.GetManifest(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("error getting manifest: %w", err)
	}
	imgDigest, err := manifest.Digest(manifestBlob)
	if err != nil {
		return nil, fmt.Errorf("error getting digest of manifest: %w", err)
	}
	canonicalRef, err := reference.WithDigest(reference.TrimNamed(imgRef.DockerReference()), imgDigest)
	if err != nil {
		return nil, fmt.Errorf("error creating canonical reference: %w", err)
	}
	return canonicalRef, nil
}

func (p *ContainersImagePuller) applyImage(ctx context.Context, ownerID string, srcRef reference.Named, canonicalRef reference.Canonical, srcImgRef types.ImageReference, cache Cache, sourceContext *types.SystemContext) (fs.FS, time.Time, error) {
	imgSrc, err := srcImgRef.NewImageSource(ctx, sourceContext)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("error creating image source: %w", err)
	}
	img, err := image.FromSource(ctx, sourceContext, imgSrc)
	if err != nil {
		return nil, time.Time{}, errors.Join(
			fmt.Errorf("error reading image: %w", err),
			imgSrc.Close(),
		)
	}
	defer func() {
		if err := img.Close(); err != nil {
			panic(err)
		}
	}()

	if features.OperatorControllerFeatureGate.Enabled(features.HelmChartSupport) {
		if hasChart(img) {
			return pullChart(ctx, ownerID, srcRef, canonicalRef, imgSrc, cache)
		}
	}

	ociImg, err := img.OCIConfig(ctx)
	if err != nil {
		return nil, time.Time{}, err
	}

	layerIter := iter.Seq[LayerData](func(yield func(LayerData) bool) {
		for i, layerInfo := range img.LayerInfos() {
			ld := LayerData{Index: i, MediaType: layerInfo.MediaType}
			layerReader, _, err := imgSrc.GetBlob(ctx, layerInfo, none.NoCache)
			if err != nil {
				ld.Err = fmt.Errorf("error getting layer blob reader: %w", err)
				if !yield(ld) {
					return
				}
			}
			defer layerReader.Close()

			decompressed, _, err := compression.AutoDecompress(layerReader)
			if err != nil {
				ld.Err = fmt.Errorf("error decompressing layer: %w", err)
				if !yield(ld) {
					return
				}
			}
			defer decompressed.Close()

			ld.Reader = decompressed
			if !yield(ld) {
				return
			}
		}
	})

	return cache.Store(ctx, ownerID, srcRef, canonicalRef, *ociImg, layerIter)
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
		return nil, fmt.Errorf("error loading signature policy: %w", err)
	}
	return signature.NewPolicyContext(policy)
}
