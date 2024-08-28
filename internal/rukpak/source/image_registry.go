package source

import (
	"archive/tar"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/containerd/containerd/archive"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	apimacherrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/operator-framework/operator-controller/internal/httputil"
)

// SourceTypeImage is the identifier for image-type bundle sources
const SourceTypeImage SourceType = "image"

type ImageSource struct {
	// Ref contains the reference to a container image containing Bundle contents.
	Ref string
	// InsecureSkipTLSVerify indicates that TLS certificate validation should be skipped.
	// If this option is specified, the HTTPS protocol will still be used to
	// fetch the specified image reference.
	// This should not be used in a production environment.
	InsecureSkipTLSVerify bool
}

// Unrecoverable represents an error that can not be recovered
// from without user intervention. When this error is returned
// the request should not be requeued.
type Unrecoverable struct {
	error
}

func NewUnrecoverable(err error) *Unrecoverable {
	return &Unrecoverable{err}
}

// TODO: Make asynchronous

type ImageRegistry struct {
	BaseCachePath   string
	CertPoolWatcher *httputil.CertPoolWatcher
}

func (i *ImageRegistry) Unpack(ctx context.Context, bundle *BundleSource) (*Result, error) {
	l := log.FromContext(ctx)
	if bundle.Type != SourceTypeImage {
		panic(fmt.Sprintf("programmer error: source type %q is unable to handle specified bundle source type %q", SourceTypeImage, bundle.Type))
	}

	if bundle.Image == nil {
		return nil, NewUnrecoverable(fmt.Errorf("error parsing bundle, bundle %s has a nil image source", bundle.Name))
	}

	imgRef, err := name.ParseReference(bundle.Image.Ref)
	if err != nil {
		return nil, NewUnrecoverable(fmt.Errorf("error parsing image reference: %w", err))
	}

	transport := remote.DefaultTransport.(*http.Transport).Clone()
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: false,
			MinVersion:         tls.VersionTLS12,
		} // nolint:gosec
	}
	if bundle.Image.InsecureSkipTLSVerify {
		transport.TLSClientConfig.InsecureSkipVerify = true // nolint:gosec
	}
	if i.CertPoolWatcher != nil {
		pool, _, err := i.CertPoolWatcher.Get()
		if err != nil {
			return nil, err
		}
		transport.TLSClientConfig.RootCAs = pool
	}

	remoteOpts := []remote.Option{}
	remoteOpts = append(remoteOpts, remote.WithTransport(transport))

	digest, isDigest := imgRef.(name.Digest)
	if isDigest {
		hexVal := strings.TrimPrefix(digest.DigestStr(), "sha256:")
		unpackPath := filepath.Join(i.BaseCachePath, bundle.Name, hexVal)
		if stat, err := os.Stat(unpackPath); err == nil && stat.IsDir() {
			l.V(1).Info("found image in filesystem cache", "digest", hexVal)
			return unpackedResult(os.DirFS(unpackPath), bundle, digest.String()), nil
		}
	}

	// always fetch the hash
	imgDesc, err := remote.Head(imgRef, remoteOpts...)
	if err != nil {
		return nil, fmt.Errorf("error fetching image descriptor: %w", err)
	}
	l.V(1).Info("resolved image descriptor", "digest", imgDesc.Digest.String())

	unpackPath := filepath.Join(i.BaseCachePath, bundle.Name, imgDesc.Digest.Hex)
	if _, err = os.Stat(unpackPath); errors.Is(err, os.ErrNotExist) { //nolint: nestif
		// Ensure any previous unpacked bundle is cleaned up before unpacking the new catalog.
		if err := i.Cleanup(ctx, bundle); err != nil {
			return nil, fmt.Errorf("error cleaning up bundle cache: %w", err)
		}

		if err = os.MkdirAll(unpackPath, 0700); err != nil {
			return nil, fmt.Errorf("error creating unpack path: %w", err)
		}

		if err = unpackImage(ctx, imgRef, unpackPath, remoteOpts...); err != nil {
			cleanupErr := os.RemoveAll(unpackPath)
			if cleanupErr != nil {
				err = apimacherrors.NewAggregate(
					[]error{
						err,
						fmt.Errorf("error cleaning up unpack path after unpack failed: %w", cleanupErr),
					},
				)
			}
			return nil, wrapUnrecoverable(fmt.Errorf("error unpacking image: %w", err), isDigest)
		}
	} else if err != nil {
		return nil, fmt.Errorf("error checking if image is in filesystem cache: %w", err)
	}

	resolvedRef := fmt.Sprintf("%s@sha256:%s", imgRef.Context().Name(), imgDesc.Digest.Hex)
	return unpackedResult(os.DirFS(unpackPath), bundle, resolvedRef), nil
}

func wrapUnrecoverable(err error, isUnrecoverable bool) error {
	if isUnrecoverable {
		return NewUnrecoverable(err)
	}
	return err
}

func (i *ImageRegistry) Cleanup(_ context.Context, bundle *BundleSource) error {
	return os.RemoveAll(filepath.Join(i.BaseCachePath, bundle.Name))
}

func unpackedResult(fsys fs.FS, bundle *BundleSource, ref string) *Result {
	return &Result{
		Bundle: fsys,
		ResolvedSource: &BundleSource{
			Type: SourceTypeImage,
			Image: &ImageSource{
				Ref:                   ref,
				InsecureSkipTLSVerify: bundle.Image.InsecureSkipTLSVerify,
			},
		},
		State: StateUnpacked,
	}
}

// unpackImage unpacks a bundle image reference to the provided unpackPath,
// returning an error if any errors are encountered along the way.
func unpackImage(ctx context.Context, imgRef name.Reference, unpackPath string, remoteOpts ...remote.Option) error {
	img, err := remote.Image(imgRef, remoteOpts...)
	if err != nil {
		return fmt.Errorf("error fetching remote image %q: %w", imgRef.Name(), err)
	}

	layers, err := img.Layers()
	if err != nil {
		return fmt.Errorf("error getting image layers: %w", err)
	}

	for _, layer := range layers {
		layerRc, err := layer.Uncompressed()
		if err != nil {
			return fmt.Errorf("error getting uncompressed layer data: %w", err)
		}

		// This filter ensures that the files created have the proper UID and GID
		// for the filesystem they will be stored on to ensure no permission errors occur when attempting to create the
		// files.
		_, err = archive.Apply(ctx, unpackPath, layerRc, archive.WithFilter(func(th *tar.Header) (bool, error) {
			th.Uid = os.Getuid()
			th.Gid = os.Getgid()
			return true, nil
		}))
		if err != nil {
			return fmt.Errorf("error applying layer to archive: %w", err)
		}
	}

	return nil
}
