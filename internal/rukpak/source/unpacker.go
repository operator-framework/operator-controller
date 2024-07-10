package source

import (
	"context"
	"fmt"
	"io/fs"

	"sigs.k8s.io/controller-runtime/pkg/manager"

	bd "github.com/operator-framework/operator-controller/internal/rukpak/bundledeployment"
)

// Unpacker unpacks bundle content, either synchronously or asynchronously and
// returns a Result, which conveys information about the progress of unpacking
// the bundle content.
//
// If a Source unpacks content asynchronously, it should register one or more
// watches with a controller to ensure that Bundles referencing this source
// can be reconciled as progress updates are available.
//
// For asynchronous Sources, multiple calls to Unpack should be made until the
// returned result includes state StateUnpacked.
//
// NOTE: A source is meant to be agnostic to specific bundle formats and
// specifications. A source should treat a bundle root directory as an opaque
// file tree and delegate bundle format concerns to bundle parsers.
type Unpacker interface {
	Unpack(context.Context, *bd.BundleDeployment) (*Result, error)
	Cleanup(context.Context, *bd.BundleDeployment) error
}

// Result conveys progress information about unpacking bundle content.
type Result struct {
	// Bundle contains the full filesystem of a bundle's root directory.
	Bundle fs.FS

	// ResolvedSource is a reproducible view of a Bundle's Source.
	// When possible, source implementations should return a ResolvedSource
	// that pins the Source such that future fetches of the bundle content can
	// be guaranteed to fetch the exact same bundle content as the original
	// unpack.
	//
	// For example, resolved image sources should reference a container image
	// digest rather than an image tag, and git sources should reference a
	// commit hash rather than a branch or tag.
	ResolvedSource *bd.BundleSource

	// State is the current state of unpacking the bundle content.
	State State

	// Message is contextual information about the progress of unpacking the
	// bundle content.
	Message string
}

type State string

const (
	// StatePending conveys that a request for unpacking a bundle has been
	// acknowledged, but not yet started.
	StatePending State = "Pending"

	// StateUnpacking conveys that the source is currently unpacking a bundle.
	// This state should be used when the bundle contents are being downloaded
	// and processed.
	StateUnpacking State = "Unpacking"

	// StateUnpacked conveys that the bundle has been successfully unpacked.
	StateUnpacked State = "Unpacked"
)

type unpacker struct {
	sources map[bd.SourceType]Unpacker
}

// NewUnpacker returns a new composite Source that unpacks bundles using the source
// mapping provided by the configured sources.
func NewUnpacker(sources map[bd.SourceType]Unpacker) Unpacker {
	return &unpacker{sources: sources}
}

func (s *unpacker) Unpack(ctx context.Context, bundle *bd.BundleDeployment) (*Result, error) {
	source, ok := s.sources[bundle.Spec.Source.Type]
	if !ok {
		return nil, fmt.Errorf("source type %q not supported", bundle.Spec.Source.Type)
	}
	return source.Unpack(ctx, bundle)
}

func (s *unpacker) Cleanup(ctx context.Context, bundle *bd.BundleDeployment) error {
	source, ok := s.sources[bundle.Spec.Source.Type]
	if !ok {
		return fmt.Errorf("source type %q not supported", bundle.Spec.Source.Type)
	}
	return source.Cleanup(ctx, bundle)
}

// NewDefaultUnpacker returns a new composite Source that unpacks bundles using
// a default source mapping with built-in implementations of all of the supported
// source types.
func NewDefaultUnpacker(mgr manager.Manager, namespace, cacheDir string) (Unpacker, error) {
	return NewUnpacker(map[bd.SourceType]Unpacker{
		bd.SourceTypeImage: &ImageRegistry{
			BaseCachePath: cacheDir,
			AuthNamespace: namespace,
		},
	}), nil
}
