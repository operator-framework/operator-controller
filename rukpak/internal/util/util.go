package util

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"hash/fnv"
	"io/ioutil"
	"os"
	"sort"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
)

func BundleProvisionerFilter(provisionerClassName string) predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		b := obj.(*rukpakv1alpha1.Bundle)
		return b.Spec.ProvisionerClassName == provisionerClassName
	})
}

func BundleInstanceProvisionerFilter(provisionerClassName string) predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		b := obj.(*rukpakv1alpha1.BundleInstance)
		return b.Spec.ProvisionerClassName == provisionerClassName
	})
}

type ProvisionerClassNameGetter interface {
	client.Object
	ProvisionerClassName() string
}

// MapOwneeToOwnerProvisionerHandler is a handler implementation that finds an owner reference in the event object that
// references the provided owner. If a reference for the provided owner is found AND that owner's provisioner class name
// matches the provided provisionerClassName, this handler enqueues a request for that owner to be reconciled.
func MapOwneeToOwnerProvisionerHandler(ctx context.Context, cl client.Client, log logr.Logger, provisionerClassName string, owner ProvisionerClassNameGetter) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(obj client.Object) []reconcile.Request {
		gvks, unversioned, err := cl.Scheme().ObjectKinds(owner)
		if err != nil {
			log.Error(err, "get GVKs for owner")
			return nil
		}
		if unversioned {
			log.Error(err, "owner cannot be an unversioned type")
			return nil
		}

		type ownerInfo struct {
			key types.NamespacedName
			gvk schema.GroupVersionKind
		}
		var oi *ownerInfo

	refLoop:
		for _, ref := range obj.GetOwnerReferences() {
			gv, err := schema.ParseGroupVersion(ref.APIVersion)
			if err != nil {
				log.Error(err, fmt.Sprintf("parse group version %q", ref.APIVersion))
				return nil
			}
			refGVK := gv.WithKind(ref.Kind)
			for _, gvk := range gvks {
				if refGVK == gvk && ref.Controller != nil && *ref.Controller {
					oi = &ownerInfo{
						key: types.NamespacedName{Name: ref.Name},
						gvk: gvk,
					}
					break refLoop
				}
			}
		}
		if oi == nil {
			return nil
		}
		if err := cl.Get(ctx, oi.key, owner); err != nil {
			log.Error(err, "get owner", "kind", oi.gvk, "name", oi.key.Name)
			return nil
		}
		if owner.ProvisionerClassName() != provisionerClassName {
			return nil
		}
		return []reconcile.Request{{NamespacedName: oi.key}}
	})
}

func MapBundleInstanceToBundles(ctx context.Context, c client.Client, bi rukpakv1alpha1.BundleInstance) *rukpakv1alpha1.BundleList {
	bundles := &rukpakv1alpha1.BundleList{}
	if err := c.List(ctx, bundles, &client.ListOptions{
		LabelSelector: NewBundleInstanceLabelSelector(&bi),
	}); err != nil {
		return nil
	}
	return bundles
}

func MapBundleToBundleInstances(ctx context.Context, c client.Client, b rukpakv1alpha1.Bundle) []*rukpakv1alpha1.BundleInstance {
	bundleInstances := &rukpakv1alpha1.BundleInstanceList{}
	if err := c.List(context.Background(), bundleInstances); err != nil {
		return nil
	}
	var bis []*rukpakv1alpha1.BundleInstance
	for _, bi := range bundleInstances.Items {
		bi := bi

		bundles := MapBundleInstanceToBundles(ctx, c, bi)
		for _, bundle := range bundles.Items {
			if bundle.GetName() == b.GetName() {
				bis = append(bis, &bi)
			}
		}
	}
	return bis
}

func MapBundleToBundleInstanceHandler(cl client.Client, log logr.Logger) handler.MapFunc {
	return func(object client.Object) []reconcile.Request {
		b := object.(*rukpakv1alpha1.Bundle)

		var requests []reconcile.Request
		matchingBIs := MapBundleToBundleInstances(context.Background(), cl, *b)
		for _, bi := range matchingBIs {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(bi)})
		}
		return requests
	}
}

// GetBundlesForBundleInstanceSelector is responsible for returning a list of
// Bundle resource that exist on cluster that match the label selector specified
// in the BI parameter's spec.Selector field.
func GetBundlesForBundleInstanceSelector(ctx context.Context, c client.Client, bi *rukpakv1alpha1.BundleInstance) (*rukpakv1alpha1.BundleList, error) {
	selector := NewBundleInstanceLabelSelector(bi)
	bundleList := &rukpakv1alpha1.BundleList{}
	if err := c.List(ctx, bundleList, &client.ListOptions{
		LabelSelector: selector,
	}); err != nil {
		return nil, fmt.Errorf("failed to list bundles using the %s selector: %v", selector.String(), err)
	}
	return bundleList, nil
}

// CheckExistingBundlesMatchesTemplate evaluates whether the existing list of Bundle objects
// match the desired Bundle template that's specified in a BundleInstance object. If a match
// is found, that Bundle object is returned, so callers are responsible for nil checking the result.
func CheckExistingBundlesMatchesTemplate(existingBundles *rukpakv1alpha1.BundleList, desiredBundleTemplate *rukpakv1alpha1.BundleTemplate) *rukpakv1alpha1.Bundle {
	for _, bundle := range existingBundles.Items {
		if !CheckDesiredBundleTemplate(&bundle, desiredBundleTemplate) {
			continue
		}
		return bundle.DeepCopy()
	}
	return nil
}

// CheckDesiredBundleTemplate is responsible for determining whether the existingBundle
// hash is equal to the desiredBundle Bundle template hash.
func CheckDesiredBundleTemplate(existingBundle *rukpakv1alpha1.Bundle, desiredBundle *rukpakv1alpha1.BundleTemplate) bool {
	if len(existingBundle.Labels) == 0 {
		// Existing Bundle has no labels set, which should never be the case.
		// Return false so that the Bundle is forced to be recreated with the expected labels.
		return false
	}

	existingHash, ok := existingBundle.Labels[CoreBundleTemplateHashKey]
	if !ok {
		// Existing Bundle has no template hash associated with it.
		// Return false so that the Bundle is forced to be recreated with the template hash label.
		return false
	}

	// Check whether the hash of the desired bundle template matches the existing bundle on-cluster.
	desiredHash := GenerateTemplateHash(desiredBundle)
	return existingHash == desiredHash
}

func GenerateTemplateHash(template *rukpakv1alpha1.BundleTemplate) string {
	hasher := fnv.New32a()
	DeepHashObject(hasher, template)
	return rand.SafeEncodeString(fmt.Sprint(hasher.Sum32()))
}

func GenerateBundleName(biName, hash string) string {
	return fmt.Sprintf("%s-%s", biName, hash)
}

// SortBundlesByCreation sorts a BundleList's items by it's
// metadata.CreationTimestamp value.
func SortBundlesByCreation(bundles *rukpakv1alpha1.BundleList) {
	sort.Slice(bundles.Items, func(a, b int) bool {
		return bundles.Items[a].CreationTimestamp.Before(&bundles.Items[b].CreationTimestamp)
	})
}

// GetPodNamespace checks whether the controller is running in a Pod vs.
// being run locally by inspecting the namespace file that gets mounted
// automatically for Pods at runtime. If that file doesn't exist, then
// return the @defaultNamespace namespace parameter.
func PodNamespace(defaultNamespace string) string {
	namespace, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return defaultNamespace
	}
	return string(namespace)
}

func PodName(provisionerName, bundleName string) string {
	return fmt.Sprintf("%s-unpack-bundle-%s", provisionerName, bundleName)
}

func BundleLabels(bundleName string) map[string]string {
	return map[string]string{"core.rukpak.io/bundle-name": bundleName}
}

func newLabelSelector(name, kind string) labels.Selector {
	kindRequirement, err := labels.NewRequirement(CoreOwnerKindKey, selection.Equals, []string{kind})
	if err != nil {
		return nil
	}
	nameRequirement, err := labels.NewRequirement(CoreOwnerNameKey, selection.Equals, []string{name})
	if err != nil {
		return nil
	}
	return labels.NewSelector().Add(*kindRequirement, *nameRequirement)
}

// NewBundleLabelSelector is responsible for constructing a label.Selector
// for any underlying resources that are associated with the Bundle parameter.
func NewBundleLabelSelector(bundle *rukpakv1alpha1.Bundle) labels.Selector {
	return newLabelSelector(bundle.GetName(), rukpakv1alpha1.BundleKind)
}

// NewBundleInstanceLabelSelector is responsible for constructing a label.Selector
// for any underlying resources that are associated with the BundleInstance parameter.
func NewBundleInstanceLabelSelector(bi *rukpakv1alpha1.BundleInstance) labels.Selector {
	return newLabelSelector(bi.GetName(), rukpakv1alpha1.BundleInstanceKind)
}

func CreateOrRecreate(ctx context.Context, cl client.Client, obj client.Object, f controllerutil.MutateFn) (controllerutil.OperationResult, error) {
	key := client.ObjectKeyFromObject(obj)
	if err := cl.Get(ctx, key, obj); err != nil {
		if !apierrors.IsNotFound(err) {
			return controllerutil.OperationResultNone, err
		}
		if err := mutate(f, key, obj); err != nil {
			return controllerutil.OperationResultNone, err
		}
		if err := cl.Create(ctx, obj); err != nil {
			return controllerutil.OperationResultNone, err
		}
		return controllerutil.OperationResultCreated, nil
	}

	existing := obj.DeepCopyObject() //nolint
	if err := mutate(f, key, obj); err != nil {
		return controllerutil.OperationResultNone, err
	}

	if equality.Semantic.DeepEqual(existing, obj) {
		return controllerutil.OperationResultNone, nil
	}

	if err := wait.PollImmediateUntil(time.Millisecond*5, func() (bool, error) {
		if err := cl.Delete(ctx, obj); err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	}, ctx.Done()); err != nil {
		return controllerutil.OperationResultNone, err
	}

	obj.SetUID("")
	obj.SetResourceVersion("")
	obj.SetGeneration(0)
	if err := cl.Create(ctx, obj); err != nil {
		return controllerutil.OperationResultNone, err
	}
	return controllerutil.OperationResultUpdated, nil
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

func MergeMaps(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func LoadCertPool(certFile string) (*x509.CertPool, error) {
	rootCAPEM, err := os.ReadFile(certFile)
	if err != nil {
		return nil, err
	}
	certPool := x509.NewCertPool()
	for block, rest := pem.Decode(rootCAPEM); block != nil; block, rest = pem.Decode(rest) {
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		certPool.AddCert(cert)
	}
	return certPool, nil
}
