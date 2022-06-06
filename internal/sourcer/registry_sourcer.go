package sourcer

import (
	"context"
	"fmt"

	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	registryClient "github.com/operator-framework/operator-registry/pkg/client"
	utilerror "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/timflannagan/platform-operators/api/v1alpha1"
)

const (
	channelName = "beta"
)

type catalogSource struct {
	client.Client
}

func NewCatalogSourceHandler(c client.Client) Sourcer {
	return &catalogSource{
		Client: c,
	}
}

func (cs catalogSource) Source(ctx context.Context, po *platformv1alpha1.PlatformOperator) ([]*Bundle, error) {
	css := &operatorsv1alpha1.CatalogSourceList{}
	if err := cs.List(ctx, css); err != nil {
		return nil, err
	}
	if len(css.Items) == 0 {
		return nil, fmt.Errorf("failed to query for any catalog sources in the cluster")
	}
	sources := sources(css.Items)

	candidates, err := sources.Filter(byConnectionReadiness).GetCandidates(ctx, po)
	if err != nil {
		return nil, err
	}

	// TODO: Find a way to better understand where and how a PO is failing. Gets
	//		 complex when one or more are failing.
	if len(candidates) != len(po.Spec.Packages) {
		return nil, fmt.Errorf("failed to find one or more candidate olm.bundles for requested PlatformOperators")
	}
	latestBundles := []*Bundle{}
	for _, versions := range candidates {
		latestBundle, err := versions.Latest()
		if err != nil {
			return nil, err
		}
		latestBundles = append(latestBundles, latestBundle)
	}

	return latestBundles, nil
}

func (s sources) GetCandidates(ctx context.Context, po *platformv1alpha1.PlatformOperator) (bundleVersions, error) {
	var (
		errors     []error
		candidates = bundleVersions{}
	)
	// TODO: Should build a cache for efficiency
	for _, cs := range s {
		// Note(tflannag): Need to account for grpc-based CatalogSource(s) that
		// specify a spec.Address or a spec.Image, so ensure this field exists, and
		// it's not empty before creating a registry client.
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
			if !contains(b.PackageName, po.Spec.Packages) || b.ChannelName != channelName {
				continue
			}
			candidates[b.PackageName] = append(candidates[b.PackageName], Bundle{
				Name:     b.GetPackageName(),
				Version:  b.GetVersion(),
				Image:    b.GetBundlePath(),
				Skips:    b.GetSkips(),
				Replaces: b.GetReplaces(),
			})
		}
	}
	if len(errors) != 0 {
		return nil, utilerror.NewAggregate(errors)
	}
	return candidates, nil
}

func contains(search string, arr []string) bool {
	for _, ele := range arr {
		if search == ele {
			return true
		}
	}
	return false
}
