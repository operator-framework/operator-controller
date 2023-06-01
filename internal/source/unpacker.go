package source

import (
	"context"
	"fmt"
	"io/fs"

	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/cluster"

	catalogdv1alpha1 "github.com/operator-framework/catalogd/api/core/v1alpha1"
)

// TODO: This package is almost entirely copy/pasted from rukpak. We should look
//   into whether it is possible to share this code.
//
// TODO: None of the rukpak CRD validations (both static and from the rukpak
//    webhooks) related to the source are present here. Which of them do we need?

// Unpacker unpacks catalog content, either synchronously or asynchronously and
// returns a Result, which conveys information about the progress of unpacking
// the catalog content.
//
// If a Source unpacks content asynchronously, it should register one or more
// watches with a controller to ensure that Bundles referencing this source
// can be reconciled as progress updates are available.
//
// For asynchronous Sources, multiple calls to Unpack should be made until the
// returned result includes state StateUnpacked.
//
// NOTE: A source is meant to be agnostic to specific catalog formats and
// specifications. A source should treat a catalog root directory as an opaque
// file tree and delegate catalog format concerns to catalog parsers.
type Unpacker interface {
	Unpack(context.Context, *catalogdv1alpha1.Catalog) (*Result, error)
}

// Result conveys progress information about unpacking catalog content.
type Result struct {
	// Bundle contains the full filesystem of a catalog's root directory.
	FS fs.FS

	// ResolvedSource is a reproducible view of a Bundle's Source.
	// When possible, source implementations should return a ResolvedSource
	// that pins the Source such that future fetches of the catalog content can
	// be guaranteed to fetch the exact same catalog content as the original
	// unpack.
	//
	// For example, resolved image sources should reference a container image
	// digest rather than an image tag, and git sources should reference a
	// commit hash rather than a branch or tag.
	ResolvedSource *catalogdv1alpha1.CatalogSource

	// State is the current state of unpacking the catalog content.
	State State

	// Message is contextual information about the progress of unpacking the
	// catalog content.
	Message string
}

type State string

const (
	// StatePending conveys that a request for unpacking a catalog has been
	// acknowledged, but not yet started.
	StatePending State = "Pending"

	// StateUnpacking conveys that the source is currently unpacking a catalog.
	// This state should be used when the catalog contents are being downloaded
	// and processed.
	StateUnpacking State = "Unpacking"

	// StateUnpacked conveys that the catalog has been successfully unpacked.
	StateUnpacked State = "Unpacked"
)

type unpacker struct {
	sources map[catalogdv1alpha1.SourceType]Unpacker
}

// NewUnpacker returns a new composite Source that unpacks catalogs using the source
// mapping provided by the configured sources.
func NewUnpacker(sources map[catalogdv1alpha1.SourceType]Unpacker) Unpacker {
	return &unpacker{sources: sources}
}

func (s *unpacker) Unpack(ctx context.Context, catalog *catalogdv1alpha1.Catalog) (*Result, error) {
	source, ok := s.sources[catalog.Spec.Source.Type]
	if !ok {
		return nil, fmt.Errorf("source type %q not supported", catalog.Spec.Source.Type)
	}
	return source.Unpack(ctx, catalog)
}

// NewDefaultUnpacker returns a new composite Source that unpacks catalogs using
// a default source mapping with built-in implementations of all of the supported
// source types.
//
// TODO: refactor NewDefaultUnpacker due to growing parameter list
func NewDefaultUnpacker(systemNsCluster cluster.Cluster, namespace, unpackImage string) (Unpacker, error) {
	cfg := systemNsCluster.GetConfig()
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return NewUnpacker(map[catalogdv1alpha1.SourceType]Unpacker{
		catalogdv1alpha1.SourceTypeImage: &Image{
			Client:       systemNsCluster.GetClient(),
			KubeClient:   kubeClient,
			PodNamespace: namespace,
			UnpackImage:  unpackImage,
		},
	}), nil
}
