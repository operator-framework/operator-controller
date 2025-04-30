package applier

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io/fs"
	"slices"
	"sort"

	"github.com/davecgh/go-spew/spew"
	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	revisionHashAnnotation = "olm.operatorframework.io/hash"
	revisionHistoryLimit   = 5
)

type Boxcutter struct {
	Client         client.Client
	Scheme         *runtime.Scheme
	BundleRenderer render.BundleRenderer
}

func (bc *Boxcutter) Apply(
	ctx context.Context, contentFS fs.FS,
	ext *ocv1.ClusterExtension,
	objectLabels, storageLabels map[string]string,
) ([]client.Object, string, error) {
	objs, err := bc.apply(ctx, contentFS, ext, objectLabels, storageLabels)
	return objs, "", err
}

func (bc *Boxcutter) apply(
	ctx context.Context, contentFS fs.FS,
	ext *ocv1.ClusterExtension,
	objectLabels, _ map[string]string,
) ([]client.Object, error) {
	reg, err := source.FromFS(contentFS).GetBundle()
	if err != nil {
		return nil, err
	}

	watchNamespace, err := GetWatchNamespace(ext)
	if err != nil {
		return nil, err
	}

	plain, err := bc.BundleRenderer.Render(reg, ext.Spec.Namespace, render.WithTargetNamespaces(watchNamespace))
	if err != nil {
		return nil, err
	}

	// objectLabels
	objs := make([]ocv1.ClusterExtensionRevisionObject, 0, len(plain))
	for _, obj := range plain {
		labels := obj.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		for k, v := range objectLabels {
			labels[k] = v
		}
		obj.SetLabels(labels)

		gvk, err := apiutil.GVKForObject(obj, bc.Scheme)
		if err != nil {
			return nil, err
		}

		unstrObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			return nil, err
		}
		unstr := unstructured.Unstructured{Object: unstrObj}
		unstr.SetGroupVersionKind(gvk)

		objs = append(objs, ocv1.ClusterExtensionRevisionObject{
			Object: unstr,
		})
	}

	// List all existing revisions
	existingRevisionList := &ocv1.ClusterExtensionRevisionList{}
	if err := bc.Client.List(ctx, existingRevisionList, client.MatchingLabels{
		controllers.ClusterExtensionRevisionOwnerLabel: ext.Name,
	}); err != nil {
		return nil, fmt.Errorf("listing revisions: %w", err)
	}
	sort.Sort(revisionAscending(existingRevisionList.Items))
	existingRevisions := existingRevisionList.Items

	// Build desired revision
	desiredRevision := &ocv1.ClusterExtensionRevision{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				controllers.ClusterExtensionRevisionOwnerLabel: ext.Name,
			},
		},
		Spec: ocv1.ClusterExtensionRevisionSpec{
			Revision: 1,
			Phases: []ocv1.ClusterExtensionRevisionPhase{
				{
					Name:    "everything",
					Objects: objs,
				},
			},
		},
	}
	desiredHash := computeSHA256Hash(desiredRevision.Spec.Phases)

	// Sort into current and previous revisions.
	var (
		currentRevision *ocv1.ClusterExtensionRevision
		prevRevisions   []ocv1.ClusterExtensionRevision
	)
	if len(existingRevisions) > 0 {
		maybeCurrentRevision := existingRevisions[len(existingRevisions)-1]

		annotations := maybeCurrentRevision.GetAnnotations()
		if annotations != nil {
			if hash, ok := annotations[revisionHashAnnotation]; ok &&
				hash == desiredHash {
				currentRevision = &maybeCurrentRevision
				prevRevisions = existingRevisions[0 : len(existingRevisions)-1] // previous is everything excluding current
			}
		}
	}

	if currentRevision == nil {
		// all Revisions are outdated => create a new one.
		prevRevisions = existingRevisions
		revisionNumber := latestRevisionNumber(prevRevisions)
		revisionNumber++

		newRevision := desiredRevision
		newRevision.Spec.Revision = revisionNumber
		// newRevision.Spec.Previous
		for _, prevRevision := range prevRevisions {
			newRevision.Spec.Previous = append(newRevision.Spec.Previous, ocv1.ClusterExtensionRevisionPrevious{
				Name: prevRevision.Name,
				UID:  prevRevision.UID,
			})
		}

		if err := controllerutil.SetControllerReference(ext, newRevision, bc.Scheme); err != nil {
			return nil, fmt.Errorf("set ownerref: %w", err)
		}
		if err := bc.Client.Create(ctx, newRevision); err != nil {
			return nil, fmt.Errorf("creating new Revision: %w", err)
		}
	}

	// Delete archived previous revisions over revisionHistory limit
	numToDelete := len(prevRevisions) - revisionHistoryLimit
	slices.Reverse(prevRevisions)

	for _, prevRev := range prevRevisions {
		if numToDelete <= 0 {
			break
		}

		if err := client.IgnoreNotFound(bc.Client.Delete(ctx, &prevRev)); err != nil {
			return nil, fmt.Errorf("failed to delete revision (history limit): %w", err)
		}
		numToDelete--
	}

	// TODO: Read status from revision.

	return plain, nil
}

// computeSHA256Hash returns a sha236 hash value calculated from object.
func computeSHA256Hash(obj any) string {
	hasher := sha256.New()
	deepHashObject(hasher, obj)
	return hex.EncodeToString(hasher.Sum(nil))
}

// deepHashObject writes specified object to hash using the spew library
// which follows pointers and prints actual values of the nested objects
// ensuring the hash does not change when a pointer changes.
func deepHashObject(hasher hash.Hash, objectToWrite any) {
	hasher.Reset()

	printer := spew.ConfigState{
		Indent:         " ",
		SortKeys:       true,
		DisableMethods: true,
		SpewKeys:       true,
	}
	if _, err := printer.Fprintf(hasher, "%#v", objectToWrite); err != nil {
		panic(err)
	}
}

type revisionAscending []ocv1.ClusterExtensionRevision

func (a revisionAscending) Len() int      { return len(a) }
func (a revisionAscending) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a revisionAscending) Less(i, j int) bool {
	iObj := a[i]
	jObj := a[j]

	return iObj.Spec.Revision < jObj.Spec.Revision
}

func latestRevisionNumber(prevRevisions []ocv1.ClusterExtensionRevision) int64 {
	if len(prevRevisions) == 0 {
		return 0
	}

	return prevRevisions[len(prevRevisions)-1].Spec.Revision
}
