package applier

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io/fs"
	"maps"
	"slices"

	"github.com/davecgh/go-spew/spew"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
)

const (
	RevisionHashAnnotation = "olm.operatorframework.io/hash"
)

type ClusterExtensionRevisionGenerator interface {
	GenerateRevision(bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels map[string]string) (*ocv1.ClusterExtensionRevision, error)
}

type SimpleRevisionGenerator struct {
	Scheme         *runtime.Scheme
	BundleRenderer BundleRenderer
}

func (r *SimpleRevisionGenerator) GenerateRevision(bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels map[string]string) (*ocv1.ClusterExtensionRevision, error) {
	// extract plain manifests
	plain, err := r.BundleRenderer.Render(bundleFS, ext)
	if err != nil {
		return nil, err
	}

	// objectLabels
	objs := make([]ocv1.ClusterExtensionRevisionObject, 0, len(plain))
	for _, obj := range plain {
		if len(obj.GetLabels()) > 0 {
			labels := maps.Clone(obj.GetLabels())
			if labels == nil {
				labels = map[string]string{}
			}
			maps.Copy(labels, objectLabels)
			obj.SetLabels(labels)
		}

		gvk, err := apiutil.GVKForObject(obj, r.Scheme)
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

	// Build desired revision
	return &ocv1.ClusterExtensionRevision{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{},
			Labels: map[string]string{
				controllers.ClusterExtensionRevisionOwnerLabel: ext.Name,
			},
		},
		Spec: ocv1.ClusterExtensionRevisionSpec{
			Phases: []ocv1.ClusterExtensionRevisionPhase{
				{
					Name:    "everything",
					Objects: objs,
				},
			},
		},
	}, nil
}

type Boxcutter struct {
	Client            client.Client
	Scheme            *runtime.Scheme
	RevisionGenerator ClusterExtensionRevisionGenerator
}

func (bc *Boxcutter) Apply(ctx context.Context, contentFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, storageLabels map[string]string) ([]client.Object, string, error) {
	objs, err := bc.apply(ctx, contentFS, ext, objectLabels, storageLabels)
	return objs, "", err
}

func (bc *Boxcutter) apply(ctx context.Context, contentFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, _ map[string]string) ([]client.Object, error) {
	// Generate desired revision
	desiredRevision, err := bc.RevisionGenerator.GenerateRevision(contentFS, ext, objectLabels)
	if err != nil {
		return nil, err
	}

	// List all existing revisions
	existingRevisions, err := bc.getExistingRevisions(ctx, ext.GetName())
	if err != nil {
		return nil, err
	}
	desiredHash := computeSHA256Hash(desiredRevision.Spec.Phases)

	// Sort into current and previous revisions.
	var (
		currentRevision *ocv1.ClusterExtensionRevision
	)
	if len(existingRevisions) > 0 {
		maybeCurrentRevision := existingRevisions[len(existingRevisions)-1]
		annotations := maybeCurrentRevision.GetAnnotations()
		if annotations != nil {
			if revisionHash, ok := annotations[RevisionHashAnnotation]; ok && revisionHash == desiredHash {
				currentRevision = &maybeCurrentRevision
			}
		}
	}

	if currentRevision == nil {
		// all Revisions are outdated => create a new one.
		prevRevisions := existingRevisions
		revisionNumber := latestRevisionNumber(prevRevisions) + 1

		newRevision := desiredRevision
		newRevision.Name = fmt.Sprintf("%s-%d", ext.Name, revisionNumber)
		if newRevision.GetAnnotations() == nil {
			newRevision.Annotations = map[string]string{}
		}
		newRevision.Annotations[RevisionHashAnnotation] = desiredHash
		newRevision.Spec.Revision = revisionNumber
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

	// TODO: Delete archived previous revisions over a certain revision limit

	// TODO: Read status from revision.

	// Collect objects
	var plain []client.Object
	for _, phase := range desiredRevision.Spec.Phases {
		for _, phaseObject := range phase.Objects {
			plain = append(plain, &phaseObject.Object)
		}
	}
	return plain, nil
}

// getExistingRevisions returns the list of ClusterExtensionRevisions for a ClusterExtension with name extName in revision order (oldest to newest)
func (bc *Boxcutter) getExistingRevisions(ctx context.Context, extName string) ([]ocv1.ClusterExtensionRevision, error) {
	existingRevisionList := &ocv1.ClusterExtensionRevisionList{}
	if err := bc.Client.List(ctx, existingRevisionList, client.MatchingLabels{
		controllers.ClusterExtensionRevisionOwnerLabel: extName,
	}); err != nil {
		return nil, fmt.Errorf("listing revisions: %w", err)
	}
	slices.SortFunc(existingRevisionList.Items, func(a, b ocv1.ClusterExtensionRevision) int {
		return cmp.Compare(a.Spec.Revision, b.Spec.Revision)
	})
	return existingRevisionList.Items, nil
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

func latestRevisionNumber(prevRevisions []ocv1.ClusterExtensionRevision) int64 {
	if len(prevRevisions) == 0 {
		return 0
	}
	return prevRevisions[len(prevRevisions)-1].Spec.Revision
}

type BundleRenderer interface {
	Render(bundleFS fs.FS, ext *ocv1.ClusterExtension) ([]client.Object, error)
}

type RegistryV1BundleRenderer struct {
	BundleRenderer render.BundleRenderer
}

func (r *RegistryV1BundleRenderer) Render(bundleFS fs.FS, ext *ocv1.ClusterExtension) ([]client.Object, error) {
	reg, err := source.FromFS(bundleFS).GetBundle()
	if err != nil {
		return nil, err
	}
	watchNamespace, err := GetWatchNamespace(ext)
	if err != nil {
		return nil, err
	}
	return r.BundleRenderer.Render(reg, ext.Spec.Namespace, render.WithTargetNamespaces(watchNamespace))
}
