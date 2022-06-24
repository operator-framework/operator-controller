package sourcer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	utilerror "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	deppyv1alpha1 "github.com/operator-framework/deppy/api/v1alpha1"
	registryproperty "github.com/operator-framework/operator-registry/alpha/property"
	registryClient "github.com/operator-framework/operator-registry/pkg/client"
)

var (
	ErrNoCandidates = errors.New("failed to find any catalog candidates")
)

type catalogSource struct {
	client.Client
}

func NewCatalogSourceHandler(c client.Client) Sourcer {
	return &catalogSource{
		Client: c,
	}
}

func (cs catalogSource) Source(ctx context.Context) ([]Bundle, error) {
	css := &operatorsv1alpha1.CatalogSourceList{}
	if err := cs.List(ctx, css); err != nil {
		return nil, err
	}
	if len(css.Items) == 0 {
		return nil, fmt.Errorf("failed to query for any catalog sources in the cluster")
	}
	sources := sources(css.Items)

	candidates, err := sources.Filter(byConnectionReadiness).GetCandidates(ctx)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, ErrNoCandidates
	}
	return candidates, nil
}

func (s sources) GetCandidates(ctx context.Context) ([]Bundle, error) {
	var (
		errors     []error
		candidates bundles
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
			properties := []deppyv1alpha1.Property{}
			for _, property := range b.GetProperties() {
				var value map[string]string

				switch property.Type {
				case registryproperty.TypePackage:
					var p registryproperty.Package
					if err := json.Unmarshal(json.RawMessage(property.Value), &p); err != nil {
						return nil, fmt.Errorf("failed to parse the %s/%v bundle property: %w", property.Type, property.Value, err)
					}
					value = map[string]string{
						"package": p.PackageName,
						"version": p.Version,
					}
				case registryproperty.TypeGVK:
					var v registryproperty.GVK
					if err := json.Unmarshal(json.RawMessage(property.Value), &v); err != nil {
						return nil, fmt.Errorf("failed to parse the %s/%v bundle property: %w", property.Type, property.Value, err)
					}
					value = map[string]string{
						"group":   v.Group,
						"kind":    v.Kind,
						"version": v.Version,
					}
				default:
					// avoid handling unknown property types
					continue
				}
				properties = append(properties, deppyv1alpha1.Property{Type: property.Type, Value: value})
			}
			candidates = append(candidates, Bundle{
				Name:            b.GetCsvName(),
				PackageName:     b.GetPackageName(),
				ChannelName:     b.GetChannelName(),
				Version:         b.GetVersion(),
				Image:           b.GetBundlePath(),
				Skips:           b.GetSkips(),
				Replaces:        b.GetReplaces(),
				SourceName:      cs.GetName(),
				SourceNamespace: cs.GetNamespace(),
				Properties:      properties,
			})
		}
	}
	if len(errors) != 0 {
		return nil, utilerror.NewAggregate(errors)
	}
	return candidates, nil
}
