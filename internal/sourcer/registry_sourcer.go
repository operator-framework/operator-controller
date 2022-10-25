package sourcer

import (
	"context"
	"fmt"

	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	registryClient "github.com/operator-framework/operator-registry/pkg/client"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformtypes "github.com/timflannagan/platform-operators/api/v1alpha1"
)

type catalogSource struct {
	client.Client
}

func NewCatalogSourceHandler(c client.Client) Sourcer {
	return &catalogSource{
		Client: c,
	}
}

func (cs catalogSource) Source(ctx context.Context, o *platformtypes.Operator) (*Bundle, error) {
	catalog := &operatorsv1alpha1.CatalogSource{}
	if err := cs.Client.Get(ctx, types.NamespacedName{
		Name:      o.Spec.Catalog.Name,
		Namespace: o.Spec.Catalog.Namespace,
	}, catalog); err != nil {
		return nil, err
	}
	sources := sources([]operatorsv1alpha1.CatalogSource{*catalog})

	candidates, err := sources.Filter(byConnectionReadiness).GetCandidates(ctx, o)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("failed to find candidate olm.bundles from the %s package", o.Spec.Package.Name)
	}
	latestBundle, err := candidates.Latest()
	if err != nil {
		return nil, err
	}

	return latestBundle, nil
}

func (s sources) GetCandidates(ctx context.Context, o *platformtypes.Operator) (bundles, error) {
	// TODO(tflannag): This doesn't account for edge case where there are zero sources.
	if len(s) != 1 {
		return nil, fmt.Errorf("validation error: only a single catalog source is supported during phase 0")
	}
	cs := s[0]

	rc, err := registryClient.NewClient(cs.Status.GRPCConnectionState.Address)
	if err != nil {
		return nil, fmt.Errorf("failed to register client from the %s/%s grpc connection: %w", cs.GetName(), cs.GetNamespace(), err)
	}
	it, err := rc.ListBundles(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list bundles from the %s/%s catalog: %w", cs.GetName(), cs.GetNamespace(), err)
	}

	var (
		candidates bundles
	)
	for b := it.Next(); b != nil; b = it.Next() {
		if b.PackageName != o.Spec.Package.Name {
			continue
		}
		candidates = append(candidates, Bundle{
			Version:  b.GetVersion(),
			Image:    b.GetBundlePath(),
			Skips:    b.GetSkips(),
			Replaces: b.GetReplaces(),
		})
	}
	return candidates, nil
}
