package applier

import (
	"context"
	"errors"
	"fmt"
	"strings"

	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/jsonpath"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/timflannagan/platform-operators/api/v1alpha1"
	"github.com/timflannagan/platform-operators/internal/sourcer"
)

const (
	plainProvisionerID    = "core.rukpak.io/plain"
	registryProvisionerID = "core.rukpak.io/registry"
	bundleMediaType       = "registry+v1"
)

var (
	ErrNotUpgradeable = errors.New("waiting for the PlatformOperator to meet upgrade probe conditions")
	ErrPending        = errors.New("waiting for the BundleInstance to be unpacked and installed")
)

type biApplier struct {
	client.Client
}

func NewBundleInstanceHandler(c client.Client) Applier {
	return &biApplier{
		Client: c,
	}
}

func (a *biApplier) Apply(ctx context.Context, po *v1alpha1.PlatformOperator, b *sourcer.Bundle) error {
	bi := &rukpakv1alpha1.BundleInstance{}
	bi.SetName(po.GetName())
	controllerRef := metav1.NewControllerRef(po, po.GroupVersionKind())
	_, err := a.CreateOrUpdate(ctx, a.Client, po, bi, func() error {
		bi.SetOwnerReferences([]metav1.OwnerReference{*controllerRef})
		bi.Spec = *buildBundleInstance(b)
		return nil
	})
	return err
}

// mutate wraps a MutateFn and applies validation to its result.
func mutate(f controllerutil.MutateFn, key client.ObjectKey, obj client.Object) error {
	if err := f(); err != nil {
		return err
	}
	if newKey := client.ObjectKeyFromObject(obj); key != newKey {
		return fmt.Errorf("MutateFn cannot mutate object name and/or object namespace")
	}
	return nil
}

// CreateOrUpdate creates or updates the given object in the Kubernetes
// cluster. The object's desired state must be reconciled with the existing
// state inside the passed in callback MutateFn.
//
// The MutateFn is called regardless of creating or updating an object.
//
// It returns the executed operation and an error.
func (a *biApplier) CreateOrUpdate(ctx context.Context, c client.Client, po *v1alpha1.PlatformOperator, obj *rukpakv1alpha1.BundleInstance, f controllerutil.MutateFn) (controllerutil.OperationResult, error) {
	key := client.ObjectKeyFromObject(obj)
	if err := c.Get(ctx, key, obj); err != nil {
		if !apierrors.IsNotFound(err) {
			return controllerutil.OperationResultNone, err
		}
		if err := mutate(f, key, obj); err != nil {
			return controllerutil.OperationResultNone, err
		}
		if err := c.Create(ctx, obj); err != nil {
			return controllerutil.OperationResultNone, err
		}
		return controllerutil.OperationResultCreated, nil
	}

	// check whether the BundleInstance exists but is waiting for the
	// desired bundle to be unpacked and/or installed.
	if pending := a.Pending(ctx, obj); pending {
		return controllerutil.OperationResultNone, ErrPending
	}
	// check whether the BundleInstance is in an upgradeable state
	// before updating the resource and causing the BI controller
	// to potentially perform a pivot.
	upgradeable, err := a.Upgradeable(ctx, po)
	if err != nil {
		return controllerutil.OperationResultNone, fmt.Errorf("%v: %w", err, ErrNotUpgradeable)
	}
	if !upgradeable {
		return controllerutil.OperationResultNone, ErrNotUpgradeable
	}

	existing := obj.DeepCopyObject() //nolint
	if err := mutate(f, key, obj); err != nil {
		return controllerutil.OperationResultNone, err
	}
	if equality.Semantic.DeepEqual(existing, obj) {
		return controllerutil.OperationResultNone, nil
	}
	if err := c.Update(ctx, obj); err != nil {
		return controllerutil.OperationResultNone, err
	}
	return controllerutil.OperationResultUpdated, nil
}

func (a *biApplier) Pending(_ context.Context, bi *rukpakv1alpha1.BundleInstance) bool {
	unpackedCond := meta.FindStatusCondition(bi.Status.Conditions, rukpakv1alpha1.TypeHasValidBundle)
	if unpackedCond == nil || unpackedCond.Reason == rukpakv1alpha1.ReasonUnpackPending && unpackedCond.Status == metav1.ConditionTrue {
		return true
	}
	installedCond := meta.FindStatusCondition(bi.Status.Conditions, rukpakv1alpha1.TypeInstalled)
	if installedCond == nil || installedCond.Status != metav1.ConditionTrue {
		return true
	}
	return false
}

func (a *biApplier) Upgradeable(ctx context.Context, po *v1alpha1.PlatformOperator) (bool, error) {
	if len(po.Spec.UpgradeChecks) == 0 {
		return true, nil
	}
	for _, probe := range po.Spec.UpgradeChecks {
		u := newUnstructuredFromProbe(probe, po)
		if err := a.Get(ctx, client.ObjectKeyFromObject(u), u); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return false, err
		}
		// TODO: Clean up this implementation
		path := probe.Path
		if !strings.HasPrefix(path, "{") {
			path = fmt.Sprintf("{%s}", probe.Path)
		}
		if !strings.HasSuffix(path, "}") {
			path = fmt.Sprintf("{%s}", probe.Path)
		}
		jp := jsonpath.New(u.GetName())
		if err := jp.Parse(path); err != nil {
			return false, fmt.Errorf("failed to parse the %q json path expression", path)
		}
		res, err := jp.FindResults(u.Object)
		if err != nil {
			return false, fmt.Errorf("failed to find results from the %q json path expression", path)
		}
		if len(res) == 0 {
			return false, fmt.Errorf("failed to results from the %q json path expression", path)
		}
		if len(res) != 1 && len(res[0]) != 1 {
			return false, fmt.Errorf("unexpected json path results")
		}
		v := res[0][0]
		if !v.CanInterface() {
			return false, fmt.Errorf("failed to access jsonpath results")
		}
		formatted := fmt.Sprintf("%v", v.Interface())
		if formatted != probe.Value {
			return false, fmt.Errorf("expected probe value to match %v, got %v", probe.Value, v)
		}
	}
	return true, nil
}

func newUnstructuredFromProbe(probe v1alpha1.UpgradeCheck, po *v1alpha1.PlatformOperator) *unstructured.Unstructured {
	gvk := schema.GroupVersionKind{
		Group:   probe.Group,
		Kind:    probe.Kind,
		Version: probe.Version,
	}
	u := &unstructured.Unstructured{}
	u.SetName(probe.Name)
	u.SetGroupVersionKind(gvk)
	u.SetNamespace(fmt.Sprintf("%s-system", po.Spec.PackageName))
	return u
}

// buildBundleInstance is responsible for taking a name and image to create an embedded BundleInstance
func buildBundleInstance(b *sourcer.Bundle) *rukpakv1alpha1.BundleInstanceSpec {
	return &rukpakv1alpha1.BundleInstanceSpec{
		ProvisionerClassName: plainProvisionerID,
		// TODO(tflannag): Investigate why the metadata key is empty when this
		// resource has been created on cluster despite the field being omitempty.
		Template: &rukpakv1alpha1.BundleTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"platformoperators.openshift.io/package": b.PackageName,
				},
				Annotations: map[string]string{
					"platformoperators.openshift.io/version":   b.Version,
					"platformoperators.openshift.io/image":     b.Image,
					"platformoperators.openshift.io/mediatype": bundleMediaType,
				},
			},
			Spec: rukpakv1alpha1.BundleSpec{
				// TODO(tflannag): Dynamically determine provisioner ID based on bundle
				// format? Do we need an API for discovering available provisioner IDs
				// in the cluster, and to map those ID(s) to bundle formats?
				ProvisionerClassName: registryProvisionerID,
				Source: rukpakv1alpha1.BundleSource{
					Type: rukpakv1alpha1.SourceTypeImage,
					Image: &rukpakv1alpha1.ImageSource{
						Ref: b.Image,
					},
				},
			},
		},
	}
}
