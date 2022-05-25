package sourcer

import (
	"github.com/blang/semver/v4"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
)

type filterSourceFn func(cs operatorsv1alpha1.CatalogSource) bool

type sources []operatorsv1alpha1.CatalogSource

func (s sources) Filter(f filterSourceFn) sources {
	var (
		filtered []operatorsv1alpha1.CatalogSource
	)
	for _, source := range s {
		if f(source) {
			filtered = append(filtered, source)
		}
	}
	return filtered
}

func byConnectionReadiness(cs operatorsv1alpha1.CatalogSource) bool {
	if cs.Status.GRPCConnectionState == nil {
		return false
	}
	if cs.Status.GRPCConnectionState.Address == "" {
		return false
	}
	return cs.Status.GRPCConnectionState.LastObservedState == "READY"
}

type bundles []Bundle

type bundleFilterFunc func(b1, b2 *Bundle) bool

// TODO(tflannag): Support variadic filtering functions
func (bundles bundles) Filter(f bundleFilterFunc) (*Bundle, error) {
	var (
		desiredBundle *Bundle
	)
	for _, bundle := range bundles {
		bundle := bundle

		if desiredBundle == nil {
			desiredBundle = &bundle
		}
		if f(&bundle, desiredBundle) {
			desiredBundle = &bundle
		}
	}
	return desiredBundle, nil
}

func (bundles bundles) Latest() (*Bundle, error) {
	return bundles.Filter(byHighestSemver)
}

func byHighestSemver(currBundle, desiredBundle *Bundle) bool {
	currV, err := semver.Parse(currBundle.Version)
	if err != nil {
		return false
	}
	desiredV, err := semver.Parse(desiredBundle.Version)
	if err != nil {
		return false
	}
	return currV.Compare(desiredV) == 1
}
