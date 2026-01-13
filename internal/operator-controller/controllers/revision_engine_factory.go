//go:build !standard

// This file is excluded from standard builds because ClusterExtensionRevision
// is an experimental feature. Standard builds use Helm-based applier only.
// The experimental build includes BoxcutterRuntime which requires these factories
// for serviceAccount-scoped client creation and RevisionEngine instantiation.

package controllers

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"pkg.package-operator.run/boxcutter/machinery"
	machinerytypes "pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/managedcache"
	"pkg.package-operator.run/boxcutter/ownerhandling"
	"pkg.package-operator.run/boxcutter/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/authentication"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
)

// RevisionEngine defines the interface for reconciling and tearing down revisions.
type RevisionEngine interface {
	Teardown(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionTeardownOption) (machinery.RevisionTeardownResult, error)
	Reconcile(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionReconcileOption) (machinery.RevisionResult, error)
}

// RevisionEngineFactory creates a RevisionEngine for a ClusterExtensionRevision.
type RevisionEngineFactory interface {
	CreateRevisionEngine(ctx context.Context, rev *ocv1.ClusterExtensionRevision) (RevisionEngine, error)
}

// defaultRevisionEngineFactory creates boxcutter RevisionEngines with serviceAccount-scoped clients.
type defaultRevisionEngineFactory struct {
	Scheme           *runtime.Scheme
	TrackingCache    managedcache.TrackingCache
	DiscoveryClient  discovery.CachedDiscoveryInterface
	RESTMapper       meta.RESTMapper
	FieldOwnerPrefix string
	BaseConfig       *rest.Config
	TokenGetter      *authentication.TokenGetter
}

// CreateRevisionEngine constructs a boxcutter RevisionEngine for the given ClusterExtensionRevision.
// It reads the ServiceAccount from annotations and creates a scoped client.
func (f *defaultRevisionEngineFactory) CreateRevisionEngine(_ context.Context, rev *ocv1.ClusterExtensionRevision) (RevisionEngine, error) {
	saNamespace, saName, err := f.getServiceAccount(rev)
	if err != nil {
		return nil, err
	}

	scopedClient, err := f.createScopedClient(saNamespace, saName)
	if err != nil {
		return nil, err
	}

	return machinery.NewRevisionEngine(
		machinery.NewPhaseEngine(
			machinery.NewObjectEngine(
				f.Scheme, f.TrackingCache, scopedClient,
				ownerhandling.NewNative(f.Scheme),
				machinery.NewComparator(ownerhandling.NewNative(f.Scheme), f.DiscoveryClient, f.Scheme, f.FieldOwnerPrefix),
				f.FieldOwnerPrefix, f.FieldOwnerPrefix,
			),
			validation.NewClusterPhaseValidator(f.RESTMapper, scopedClient),
		),
		validation.NewRevisionValidator(), scopedClient,
	), nil
}

func (f *defaultRevisionEngineFactory) getServiceAccount(rev *ocv1.ClusterExtensionRevision) (string, string, error) {
	annotations := rev.GetAnnotations()
	if annotations == nil {
		return "", "", fmt.Errorf("revision %q is missing required annotations", rev.Name)
	}

	saName := strings.TrimSpace(annotations[labels.ServiceAccountNameKey])
	saNamespace := strings.TrimSpace(annotations[labels.ServiceAccountNamespaceKey])

	if len(saName) == 0 {
		return "", "", fmt.Errorf("revision %q is missing ServiceAccount name annotation", rev.Name)
	}
	if len(saNamespace) == 0 {
		return "", "", fmt.Errorf("revision %q is missing ServiceAccount namespace annotation", rev.Name)
	}

	return saNamespace, saName, nil
}

func (f *defaultRevisionEngineFactory) createScopedClient(namespace, serviceAccountName string) (client.Client, error) {
	saConfig := rest.AnonymousClientConfig(f.BaseConfig)
	saConfig.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return &authentication.TokenInjectingRoundTripper{
			Tripper:     rt,
			TokenGetter: f.TokenGetter,
			Key: types.NamespacedName{
				Name:      serviceAccountName,
				Namespace: namespace,
			},
		}
	})

	scopedClient, err := client.New(saConfig, client.Options{
		Scheme: f.Scheme,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create client for ServiceAccount %s/%s: %w", namespace, serviceAccountName, err)
	}

	return scopedClient, nil
}

// NewDefaultRevisionEngineFactory creates a new defaultRevisionEngineFactory.
func NewDefaultRevisionEngineFactory(
	scheme *runtime.Scheme,
	trackingCache managedcache.TrackingCache,
	discoveryClient discovery.CachedDiscoveryInterface,
	restMapper meta.RESTMapper,
	fieldOwnerPrefix string,
	baseConfig *rest.Config,
	tokenGetter *authentication.TokenGetter,
) (RevisionEngineFactory, error) {
	if baseConfig == nil {
		return nil, fmt.Errorf("baseConfig is required but not provided")
	}
	if tokenGetter == nil {
		return nil, fmt.Errorf("tokenGetter is required but not provided")
	}
	return &defaultRevisionEngineFactory{
		Scheme:           scheme,
		TrackingCache:    trackingCache,
		DiscoveryClient:  discoveryClient,
		RESTMapper:       restMapper,
		FieldOwnerPrefix: fieldOwnerPrefix,
		BaseConfig:       baseConfig,
		TokenGetter:      tokenGetter,
	}, nil
}
