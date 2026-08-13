package applier

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"

	orbac "github.com/joelanford/orb-operator/applyconfigurations/api/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

type OrbOperator struct {
	Client     client.Client
	Scheme     *runtime.Scheme
	Generator  CODGenerator
	Preflights []Preflight
	FieldOwner string
}

func runPreflights(ctx context.Context, ext *ocv1.ClusterExtension, cod *orbac.ClusterObjectDeploymentApplyConfiguration, preflights []Preflight) error {
	objs, err := extractObjectsFromCOD(cod)
	if err != nil {
		return fmt.Errorf("extracting objects from COD: %w", err)
	}

	var errs []error
	for _, pf := range preflights {
		if shouldSkipPreflight(ctx, pf, ext, StateNeedsUpgrade) {
			continue
		}
		if err := pf.Upgrade(ctx, objs); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func extractObjectsFromCOD(cod *orbac.ClusterObjectDeploymentApplyConfiguration) ([]client.Object, error) {
	if cod == nil || cod.Spec == nil || cod.Spec.Template == nil || cod.Spec.Template.Spec == nil {
		return nil, nil
	}

	var objs []client.Object
	for _, phase := range cod.Spec.Template.Spec.Phases {
		for i, po := range phase.Objects {
			if po.Object == nil || len(po.Object.Raw) == 0 {
				continue
			}
			var obj unstructured.Unstructured
			if err := json.Unmarshal(po.Object.Raw, &obj.Object); err != nil {
				phaseName := "<unnamed>"
				if phase.Name != nil {
					phaseName = *phase.Name
				}
				return nil, fmt.Errorf("phase %q object %d: %w", phaseName, i, err)
			}
			objs = append(objs, &obj)
		}
	}
	return objs, nil
}

func (o *OrbOperator) Apply(ctx context.Context, contentFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (bool, string, error) {
	l := log.FromContext(ctx)

	cod, err := o.Generator.GenerateCOD(ctx, contentFS, ext, objectLabels, revisionAnnotations)
	if err != nil {
		return false, "", fmt.Errorf("generating COD: %w", err)
	}

	if err := runPreflights(ctx, ext, cod, o.Preflights); err != nil {
		l.Info("preflight checks failed", "error", err)
		return false, "", err
	}
	l.Info("preflight checks passed")

	return false, "", nil
}
