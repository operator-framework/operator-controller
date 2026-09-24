//go:build !standard

// This file is excluded from standard builds because ClusterObjectSet
// is an experimental feature. Standard builds use Helm-based applier only.
// The experimental build includes BoxcutterRuntime which requires these factories
// for serviceAccount-scoped client creation and RevisionEngine instantiation.

package revision

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"pkg.package-operator.run/boxcutter"
	"pkg.package-operator.run/boxcutter/machinery"
	machinerytypes "pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/managedcache"
	"pkg.package-operator.run/boxcutter/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// Engine defines the interface for reconciling and tearing down revisions.
type Engine interface {
	Teardown(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionTeardownOption) (machinery.RevisionTeardownResult, error)
	Reconcile(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionReconcileOption) (machinery.RevisionResult, error)
}

// PhaseEngine defines the interface for reconciling and tearing down revision phases
type PhaseEngine interface {
	Reconcile(ctx context.Context, revision int64, phase machinerytypes.Phase, opts ...machinerytypes.PhaseReconcileOption) (machinery.PhaseResult, error)
	Teardown(ctx context.Context, revision int64, phase machinerytypes.Phase, opts ...machinerytypes.PhaseTeardownOption) (machinery.PhaseTeardownResult, error)
}

// EngineFactory creates an Engine for a ClusterObjectSet.
type EngineFactory interface {
	New(ctx context.Context, rev *ocv1.ClusterObjectSet) (Engine, error)
}

// defaultRevisionEngineFactory creates boxcutter RevisionEngines.
type defaultRevisionEngineFactory struct {
	Scheme           *runtime.Scheme
	TrackingCache    managedcache.TrackingCache
	DiscoveryClient  discovery.CachedDiscoveryInterface
	RESTMapper       meta.RESTMapper
	FieldOwnerPrefix string
	Client           client.Client
}

// N constructs a boxcutter Engine for the given ClusterObjectSet.
func (f *defaultRevisionEngineFactory) New(_ context.Context, rev *ocv1.ClusterObjectSet) (Engine, error) {
	return New(rev.Status.ObservedPhases, boxcutter.RevisionEngineOptions{
		Scheme:           f.Scheme,
		FieldOwner:       f.FieldOwnerPrefix,
		SystemPrefix:     f.FieldOwnerPrefix,
		DiscoveryClient:  f.DiscoveryClient,
		RestMapper:       f.RESTMapper,
		Writer:           f.Client,
		Reader:           f.TrackingCache,
		ManagedBy:        f.FieldOwnerPrefix,
		PhaseValidator:   validation.NewClusterPhaseValidator(f.RESTMapper, f.Client),
		UnfilteredReader: f.Client,
	})
}

// NewDefaultRevisionEngineFactory creates a new defaultRevisionEngineFactory.
func NewDefaultRevisionEngineFactory(
	scheme *runtime.Scheme,
	trackingCache managedcache.TrackingCache,
	discoveryClient discovery.CachedDiscoveryInterface,
	restMapper meta.RESTMapper,
	fieldOwnerPrefix string,
	baseConfig *rest.Config,
) (EngineFactory, error) {
	if baseConfig == nil {
		return nil, fmt.Errorf("baseConfig is required but not provided")
	}
	c, err := client.New(baseConfig, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}
	return &defaultRevisionEngineFactory{
		Scheme:           scheme,
		TrackingCache:    trackingCache,
		DiscoveryClient:  discoveryClient,
		RESTMapper:       restMapper,
		FieldOwnerPrefix: fieldOwnerPrefix,
		Client:           c,
	}, nil
}
