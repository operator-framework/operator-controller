package applier

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"

	orbv1alpha1 "github.com/joelanford/orb-operator/api/v1alpha1"
	orbac "github.com/joelanford/orb-operator/applyconfigurations/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	orb "github.com/operator-framework/operator-controller/internal/operator-controller/applier/orb"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
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

	cod, slices, err := orb.ExternalizeCOD(cod)
	if err != nil {
		return false, "", fmt.Errorf("externalizing COD: %w", err)
	}
	if len(slices) > 0 {
		l.Info("externalized COD into ClusterObjectSlices", "sliceCount", len(slices))
	} else {
		l.Info("COD fits inline, no externalization needed")
	}

	// Apply the ClusterObjectSlices before the COD, since the COD's objectRefs
	// point to them.
	for _, slice := range slices {
		unchanged, err := o.alreadyApplied(ctx, slice)
		if err != nil {
			return false, "", fmt.Errorf("checking existing ClusterObjectSlice: %w", err)
		}
		if unchanged {
			continue
		}
		if err := o.Client.Apply(ctx, slice, client.FieldOwner(o.FieldOwner), client.ForceOwnership); err != nil {
			return false, "", fmt.Errorf("applying ClusterObjectSlice: %w", err)
		}
	}

	unchanged, err := o.alreadyApplied(ctx, cod)
	if err != nil {
		return false, "", fmt.Errorf("checking existing ClusterObjectDeployment: %w", err)
	}
	if !unchanged {
		if err := o.Client.Apply(ctx, cod, client.FieldOwner(o.FieldOwner), client.ForceOwnership); err != nil {
			return false, "", fmt.Errorf("applying ClusterObjectDeployment: %w", err)
		}
	}

	// Garbage collect ClusterObjectSlices left over from previous reconciles.
	// This is non-fatal: any error is logged and retried on the next reconcile.
	if err := o.garbageCollectOrphanedSlices(ctx, ext, slices); err != nil {
		l.Info("failed to garbage collect orphaned ClusterObjectSlices", "error", err)
	}

	return true, "", nil
}

// alreadyApplied reports whether an object matching the given apply
// configuration already exists in the cache with every desired field already
// reflected in the live object (per semantic DeepDerivative). When true, the
// server-side apply can be skipped: it would be a no-op write. Returns false
// when the object does not exist yet.
func (o *OrbOperator) alreadyApplied(ctx context.Context, ac runtime.ApplyConfiguration) (bool, error) {
	raw, err := json.Marshal(ac)
	if err != nil {
		return false, fmt.Errorf("marshaling apply configuration: %w", err)
	}
	desired := &unstructured.Unstructured{}
	if err := desired.UnmarshalJSON(raw); err != nil {
		return false, fmt.Errorf("unmarshaling apply configuration: %w", err)
	}

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(desired.GroupVersionKind())
	if err := o.Client.Get(ctx, client.ObjectKeyFromObject(desired), existing); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting existing %s %q: %w", desired.GetKind(), desired.GetName(), err)
	}

	return equality.Semantic.DeepDerivative(desired.Object, existing.Object), nil
}

// garbageCollectOrphanedSlices deletes ClusterObjectSlices owned by ext that are
// not part of the current slice set. When slices is empty (no externalization
// occurred), all owned ClusterObjectSlices are deleted.
func (o *OrbOperator) garbageCollectOrphanedSlices(ctx context.Context, ext *ocv1.ClusterExtension, slices []*orbac.ClusterObjectSliceApplyConfiguration) error {
	current := make(map[string]struct{}, len(slices))
	for _, s := range slices {
		if n := s.GetName(); n != nil {
			current[*n] = struct{}{}
		}
	}

	var list orbv1alpha1.ClusterObjectSliceList
	if err := o.Client.List(ctx, &list, client.MatchingLabels{labels.OwnerNameKey: ext.Name}); err != nil {
		return fmt.Errorf("listing ClusterObjectSlices: %w", err)
	}

	var errs []error
	for i := range list.Items {
		cosl := &list.Items[i]
		if _, ok := current[cosl.Name]; ok {
			continue
		}
		// The owner-name label narrows the list but is not proof of ownership.
		// Only collect slices actually controlled by this ClusterExtension so a
		// foreign slice sharing the label is never deleted (single-owner semantics).
		if !metav1.IsControlledBy(cosl, ext) {
			continue
		}
		if err := o.Client.Delete(ctx, cosl); err != nil && !apierrors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("deleting orphaned ClusterObjectSlice %q: %w", cosl.Name, err))
		}
	}
	return errors.Join(errs...)
}
