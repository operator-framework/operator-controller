package source

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/containers/image/v5/copy"
	"github.com/containers/image/v5/docker"
	"github.com/containers/image/v5/docker/reference"
	"github.com/containers/image/v5/manifest"
	"github.com/containers/image/v5/oci/layout"
	"github.com/containers/image/v5/pkg/sysregistriesv2"
	"github.com/containers/image/v5/signature"
	"github.com/containers/image/v5/types"
	"github.com/go-logr/logr"
	"github.com/opencontainers/go-digest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	catalogdv1 "github.com/operator-framework/operator-controller/catalogd/api/v1"
	"github.com/operator-framework/operator-controller/internal/httputil"
	fsutil "github.com/operator-framework/operator-controller/internal/util/fs"
	imageutil "github.com/operator-framework/operator-controller/internal/util/image"
)

const ConfigDirLabel = "operators.operatorframework.io.index.configs.v1"

var insecurePolicy = []byte(`{"default":[{"type":"insecureAcceptAnything"}]}`)

type ContainersImageRegistry struct {
	BaseCachePath     string
	SourceContextFunc func(logger logr.Logger) (*types.SystemContext, error)
}

func (i *ContainersImageRegistry) Unpack(ctx context.Context, catalog *catalogdv1.ClusterCatalog) (*Result, error) {
	l := log.FromContext(ctx)

	if catalog.Spec.Source.Type != catalogdv1.SourceTypeImage {
		panic(fmt.Sprintf("programmer error: source type %q is unable to handle specified catalog source type %q", catalogdv1.SourceTypeImage, catalog.Spec.Source.Type))
	}

	if catalog.Spec.Source.Image == nil {
		return nil, reconcile.TerminalError(fmt.Errorf("error parsing catalog, catalog %s has a nil image source", catalog.Name))
	}

	// Reload registries cache in case of configuration update
	sysregistriesv2.InvalidateCache()

	srcCtx, err := i.SourceContextFunc(l)
	if err != nil {
		return nil, err
	}

	res, err := i.unpack(ctx, catalog, srcCtx, l)
	if err != nil {
		// Log any CertificateVerificationErrors, and log Docker Certificates if necessary
		if httputil.LogCertificateVerificationError(err, l) {
			httputil.LogDockerCertificates(srcCtx.DockerCertPath, l)
		}
	}
	return res, err
}

func (i *ContainersImageRegistry) unpack(ctx context.Context, catalog *catalogdv1.ClusterCatalog, srcCtx *types.SystemContext, l logr.Logger) (*Result, error) {
	//////////////////////////////////////////////////////
	//
	// Resolve a canonical reference for the image.
	//
	//////////////////////////////////////////////////////
	imgRef, canonicalRef, specIsCanonical, err := resolveReferences(ctx, catalog.Spec.Source.Image.Ref, srcCtx)
	if err != nil {
		return nil, err
	}

	//////////////////////////////////////////////////////
	//
	// Check if the image is already unpacked. If it is,
	// return the unpacked directory.
	//
	//////////////////////////////////////////////////////
	unpackPath := i.unpackPath(catalog.Name, canonicalRef.Digest())
	if unpackTime, err := fsutil.GetDirectoryModTime(unpackPath); err == nil {
		l.Info("image already unpacked", "ref", imgRef.String(), "digest", canonicalRef.Digest().String())
		return successResult(unpackPath, canonicalRef, unpackTime), nil
	} else if errors.Is(err, fsutil.ErrNotDirectory) {
		if err := fsutil.DeleteReadOnlyRecursive(unpackPath); err != nil {
			return nil, err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("error checking image already unpacked: %w", err)
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

	layoutDir, err := os.MkdirTemp("", fmt.Sprintf("oci-layout-%s", catalog.Name))
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
	if err := i.unpackImage(ctx, unpackPath, layoutRef, specIsCanonical, srcCtx); err != nil {
		if cleanupErr := fsutil.DeleteReadOnlyRecursive(unpackPath); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
		return nil, fmt.Errorf("error unpacking image: %w", err)
	}

	//////////////////////////////////////////////////////
	//
	// Delete other images. They are no longer needed.
	//
	//////////////////////////////////////////////////////
	if err := i.deleteOtherImages(catalog.Name, canonicalRef.Digest()); err != nil {
		return nil, fmt.Errorf("error deleting old images: %w", err)
	}

	return successResult(unpackPath, canonicalRef, time.Now()), nil
}

func successResult(unpackPath string, canonicalRef reference.Canonical, lastUnpacked time.Time) *Result {
	return &Result{
		FS: os.DirFS(unpackPath),
		ResolvedSource: &catalogdv1.ResolvedCatalogSource{
			Type: catalogdv1.SourceTypeImage,
			Image: &catalogdv1.ResolvedImageSource{
				Ref: canonicalRef.String(),
			},
		},
		State:   StateUnpacked,
		Message: fmt.Sprintf("unpacked %q successfully", canonicalRef),

		// We truncate both the unpack time and last successful poll attempt
		// to the second because metav1.Time is serialized
		// as RFC 3339 which only has second-level precision. When we
		// use this result in a comparison with what we deserialized
		// from the Kubernetes API server, we need it to match.
		UnpackTime:                lastUnpacked.Truncate(time.Second),
		LastSuccessfulPollAttempt: metav1.NewTime(time.Now().Truncate(time.Second)),
	}
}

func (i *ContainersImageRegistry) Cleanup(_ context.Context, catalog *catalogdv1.ClusterCatalog) error {
	if err := fsutil.DeleteReadOnlyRecursive(i.catalogPath(catalog.Name)); err != nil {
		return fmt.Errorf("error deleting catalog cache: %w", err)
	}
	return nil
}

func (i *ContainersImageRegistry) catalogPath(catalogName string) string {
	return filepath.Join(i.BaseCachePath, catalogName)
}

func (i *ContainersImageRegistry) unpackPath(catalogName string, digest digest.Digest) string {
	return filepath.Join(i.catalogPath(catalogName), digest.String())
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

func (i *ContainersImageRegistry) unpackImage(ctx context.Context, unpackPath string, imageReference types.ImageReference, specIsCanonical bool, sourceContext *types.SystemContext) error {
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

	cfg, err := img.OCIConfig(ctx)
	if err != nil {
		return fmt.Errorf("error parsing image config: %w", err)
	}

	dirToUnpack, ok := cfg.Config.Labels[ConfigDirLabel]
	if !ok {
		// If the spec is a tagged ref, retries could end up resolving a new digest, where the label
		// might show up. If the spec is canonical, no amount of retries will make the label appear.
		// Therefore, we treat the error as terminal if the reference from the spec is canonical.
		return wrapTerminal(fmt.Errorf("catalog image is missing the required label %q", ConfigDirLabel), specIsCanonical)
	}

	applyFilter := imageutil.AllFilters(
		imageutil.OnlyPath(dirToUnpack),
		imageutil.ForceOwnershipRWX(),
	)
	return imageutil.ApplyLayersToDisk(ctx, unpackPath, img, layoutSrc, applyFilter)
}

func (i *ContainersImageRegistry) deleteOtherImages(catalogName string, digestToKeep digest.Digest) error {
	catalogPath := i.catalogPath(catalogName)
	imgDirs, err := os.ReadDir(catalogPath)
	if err != nil {
		return fmt.Errorf("error reading image directories: %w", err)
	}
	for _, imgDir := range imgDirs {
		if imgDir.Name() == digestToKeep.String() {
			continue
		}
		imgDirPath := filepath.Join(catalogPath, imgDir.Name())
		if err := fsutil.DeleteReadOnlyRecursive(imgDirPath); err != nil {
			return fmt.Errorf("error removing image directory: %w", err)
		}
	}
	return nil
}

func wrapTerminal(err error, isTerminal bool) error {
	if !isTerminal {
		return err
	}
	return reconcile.TerminalError(err)
}
