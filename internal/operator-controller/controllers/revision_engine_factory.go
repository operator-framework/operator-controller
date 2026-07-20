//go:build !standard

// This file is excluded from standard builds because ClusterObjectSet
// is an experimental feature. Standard builds use Helm-based applier only.
// The experimental build includes BoxcutterRuntime which requires these factories
// for serviceAccount-scoped client creation and RevisionEngine instantiation.

package controllers

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"pkg.package-operator.run/boxcutter/machinery"
	machinerytypes "pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/managedcache"
	"pkg.package-operator.run/boxcutter/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// RevisionEngine defines the interface for reconciling and tearing down revisions.
type RevisionEngine interface {
	Teardown(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionTeardownOption) (machinery.RevisionTeardownResult, error)
	Reconcile(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionReconcileOption) (machinery.RevisionResult, error)
}

// RevisionEngineFactory creates a RevisionEngine for a ClusterObjectSet.
type RevisionEngineFactory interface {
	CreateRevisionEngine(ctx context.Context, rev *ocv1.ClusterObjectSet) (RevisionEngine, error)
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

// CreateRevisionEngine constructs a boxcutter RevisionEngine for the given ClusterObjectSet.
func (f *defaultRevisionEngineFactory) CreateRevisionEngine(_ context.Context, rev *ocv1.ClusterObjectSet) (RevisionEngine, error) {
	return machinery.NewRevisionEngine(
		machinery.NewPhaseEngine(
			machinery.NewObjectEngine(
				f.Scheme, f.TrackingCache, f.Client,
				machinery.NewComparator(f.DiscoveryClient, f.Scheme, f.FieldOwnerPrefix),
				f.FieldOwnerPrefix, f.FieldOwnerPrefix,
				f.FieldOwnerPrefix, // managedBy
				f.Client,
			),
			validation.NewClusterPhaseValidator(f.RESTMapper, f.Client),
		),
		validation.NewRevisionValidator(), f.Client,
	), nil
}

// NewDefaultRevisionEngineFactory creates a new defaultRevisionEngineFactory.
func NewDefaultRevisionEngineFactory(
	scheme *runtime.Scheme,
	trackingCache managedcache.TrackingCache,
	discoveryClient discovery.CachedDiscoveryInterface,
	restMapper meta.RESTMapper,
	fieldOwnerPrefix string,
	baseConfig *rest.Config,
) (RevisionEngineFactory, error) {
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
