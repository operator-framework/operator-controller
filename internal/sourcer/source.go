package sourcer

import (
	"context"

	"github.com/blang/semver/v4"
	registryClient "github.com/operator-framework/operator-registry/pkg/client"

	v1alpha1 "github.com/timflannagan/platform-operators/api/v1alpha1"
)

type Sourcer interface {
	Source(context.Context, *v1alpha1.PlatformOperator) (*Bundle, error)
}

type Bundle struct {
	Version  string
	Image    string
	Replaces string
	Skips    []string
}

type catalogSource struct {
	client registryClient.Interface
}

func NewCatalogSourceHandler(c registryClient.Interface) Sourcer {
	return &catalogSource{
		client: c,
	}
}

func (cs catalogSource) Source(ctx context.Context, po *v1alpha1.PlatformOperator) (*Bundle, error) {
	bundles, err := getBundles(ctx, cs.client, po)
	if err != nil {
		return nil, err
	}
	filteredBundle, err := filterBundles(bundles, func(b1, b2 *Bundle) bool {
		currV, err := semver.Parse(b1.Version)
		if err != nil {
			return false
		}
		desiredV, err := semver.Parse(b2.Version)
		if err != nil {
			return false
		}
		return currV.Compare(desiredV) == 1
	})
	if err != nil {
		return nil, err
	}
	return filteredBundle, nil
}

func getBundles(ctx context.Context, client registryClient.Interface, po *v1alpha1.PlatformOperator) ([]Bundle, error) {
	it, err := client.ListBundles(ctx)
	if err != nil {
		return nil, err
	}

	var (
		cb []Bundle
	)
	for b := it.Next(); b != nil; b = it.Next() {
		if b.PackageName != po.Spec.Package || b.ChannelName != po.Spec.Channel {
			continue
		}
		cb = append(cb, Bundle{
			Version:  b.GetVersion(),
			Image:    b.GetBundlePath(),
			Skips:    b.GetSkips(),
			Replaces: b.GetReplaces(),
		})
	}
	return cb, nil
}

func filterBundles(bundles []Bundle, f func(b1, b2 *Bundle) bool) (*Bundle, error) {
	// TODO(tflannag): Respect skips/replaces semantics?
	var (
		desiredBundle *Bundle
	)
	for _, bundle := range bundles {
		if f(&bundle, desiredBundle) {
			desiredBundle = &bundle
		}
	}
	return desiredBundle, nil
}
