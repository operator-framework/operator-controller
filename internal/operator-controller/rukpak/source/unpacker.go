package source

import (
	"context"
	"io/fs"
)

// SourceTypeImage is the identifier for image-type bundle sources
const SourceTypeImage SourceType = "image"

type ImageSource struct {
	// Ref contains the reference to a container image containing Bundle contents.
	Ref string
}

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
	Unpack(context.Context, *BundleSource) (*Result, error)
	Cleanup(context.Context, *BundleSource) error
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
	ResolvedSource *BundleSource

	// State is the current state of unpacking the bundle content.
	State State

	// Message is contextual information about the progress of unpacking the
	// bundle content.
	Message string
}

type State string

const (
	// StateUnpacked conveys that the bundle has been successfully unpacked.
	StateUnpacked State = "Unpacked"
)

type SourceType string

type BundleSource struct {
	Name string
	// Type defines the kind of Bundle content being sourced.
	Type SourceType
	// Image is the bundle image that backs the content of this bundle.
	Image *ImageSource
}
