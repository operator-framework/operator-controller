//go:build !standard

// This file is excluded from standard builds because ClusterExtensionRevision
// is an experimental feature. Standard builds use Helm-based applier only.

package controllers

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"pkg.package-operator.run/boxcutter/machinery"
	machinerytypes "pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/managedcache"
	"pkg.package-operator.run/boxcutter/ownerhandling"
	"pkg.package-operator.run/boxcutter/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RevisionEngine defines the interface for reconciling and tearing down revisions.
type RevisionEngine interface {
	Teardown(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionTeardownOption) (machinery.RevisionTeardownResult, error)
	Reconcile(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionReconcileOption) (machinery.RevisionResult, error)
}

// NewRevisionEngine creates a boxcutter RevisionEngine using the provided client.
func NewRevisionEngine(
	scheme *runtime.Scheme,
	trackingCache managedcache.TrackingCache,
	discoveryClient discovery.CachedDiscoveryInterface,
	restMapper meta.RESTMapper,
	fieldOwnerPrefix string,
	c client.Client,
) RevisionEngine {
	return machinery.NewRevisionEngine(
		machinery.NewPhaseEngine(
			machinery.NewObjectEngine(
				scheme, trackingCache, c,
				ownerhandling.NewNative(scheme),
				machinery.NewComparator(ownerhandling.NewNative(scheme), discoveryClient, scheme, fieldOwnerPrefix),
				fieldOwnerPrefix, fieldOwnerPrefix,
			),
			validation.NewClusterPhaseValidator(restMapper, c),
		),
		validation.NewRevisionValidator(), c,
	)
}
