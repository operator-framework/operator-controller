package sourcer

import (
	"context"
	"fmt"
	"regexp"

	"github.com/blang/semver/v4"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/operator-framework/operator-registry/pkg/api"
	registryClient "github.com/operator-framework/operator-registry/pkg/client"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

type catalogSource struct {
	client.Client
}

func NewCatalogSourceHandler(c client.Client) Sourcer {
	return &catalogSource{
		Client: c,
	}
}

func (cs catalogSource) Source(ctx context.Context, o *operatorv1alpha1.Operator) (*Bundle, error) {
	sources, err := getSources(ctx, cs.Client, o)
	if err != nil {
		return nil, err
	}
	candidates, err := sources.Filter(byConnectionReadiness).GetCandidates(ctx, o)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("failed to find any bundles for the desired %s package name", o.Spec.Package.Name)
	}

	latestBundle, err := candidates.Latest()
	if err != nil {
		return nil, err
	}
	return latestBundle, nil
}

func (s sources) GetCandidates(ctx context.Context, o *operatorv1alpha1.Operator) (bundles, error) {
	matchesDesiredVersion := getVersionFilter(o.Spec.Package.Version)

	// TODO(tflannag): Revisit this implementation as it's expensive.
	var (
		candidates bundles
		errors     []error
	)
	for _, cs := range s {
		// TODO(tflannag): Determine how to handle failure modes when creating a new
		// registry client, and when listing bundles from a catalog. We can still successfully
		// find a candidate from a catalog if 1/3 catalogs are down in the cluster.
		rc, err := registryClient.NewClient(cs.Status.GRPCConnectionState.Address)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to register client from the %s/%s grpc connection: %w", cs.GetName(), cs.GetNamespace(), err))
			continue
		}
		it, err := rc.ListBundles(ctx)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to list bundles from the %s/%s catalog: %w", cs.GetName(), cs.GetNamespace(), err))
			continue
		}

		for b := it.Next(); b != nil; b = it.Next() {
			if b.PackageName != o.Spec.Package.Name {
				continue
			}
			if !matchesDesiredVersion(b) {
				continue
			}
			candidates = append(candidates, Bundle{
				SourceInfo: types.NamespacedName{
					Name:      cs.GetName(),
					Namespace: cs.GetNamespace(),
				},
				Version:  b.GetVersion(),
				Image:    b.GetBundlePath(),
				Skips:    b.GetSkips(),
				Replaces: b.GetReplaces(),
			})
		}
	}
	return candidates, utilerrors.NewAggregate(errors)
}

func isExplicitSemver(v string) bool {
	spaces, err := regexp.MatchString(" ", v)
	if err != nil {
		return false
	}
	specialChar, err := regexp.MatchString("[<|>|=|\\|]", v)
	if err != nil {
		return false
	}
	return !spaces && !specialChar
}

func getVersionFilter(v string) func(b *api.Bundle) bool {
	// check whether no version requirements were supplied,
	// and default to no version filtering in this case.
	if v == "" {
		return func(b *api.Bundle) bool {
			return true
		}
	}

	// distinguish between an explicit desired version, e.g. "2.0.0",
	// versus a version range being supplied in the spec.Package.Version
	// field.
	if isExplicitSemver(v) {
		version, err := semver.Parse(v)
		if err != nil {
			panic(err)
		}
		return func(b *api.Bundle) bool {
			return b.GetVersion() == version.String()
		}
	}
	expectedRange, err := semver.ParseRange(v)
	if err != nil {
		panic(err)
	}
	return func(b *api.Bundle) bool {
		bundleVersion, err := semver.Parse(b.GetVersion())
		if err != nil {
			panic(err)
		}
		return expectedRange(bundleVersion)
	}
}

func getSources(ctx context.Context, c client.Client, o *operatorv1alpha1.Operator) (sources, error) {
	// check whether no catalog was configured, and attempt to use all the catalogs
	// in the cluster to find candidate bundles to install.
	if o.Spec.Catalog == nil {
		css := &operatorsv1alpha1.CatalogSourceList{}
		if err := c.List(ctx, css); err != nil {
			return nil, err
		}
		if len(css.Items) == 0 {
			return nil, fmt.Errorf("failed to query for any catalog sources in the cluster")
		}
		return sources(css.Items), nil
	}
	catalog := &operatorsv1alpha1.CatalogSource{}
	if err := c.Get(ctx, types.NamespacedName{
		Name:      o.Spec.Catalog.Name,
		Namespace: o.Spec.Catalog.Namespace,
	}, catalog); err != nil {
		return nil, err
	}
	return sources([]operatorsv1alpha1.CatalogSource{*catalog}), nil
}
