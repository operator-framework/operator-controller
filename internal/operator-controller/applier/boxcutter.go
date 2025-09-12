package applier

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io/fs"
	"maps"
	"slices"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"

	helmclient "github.com/operator-framework/helm-operator-plugins/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
)

const (
	RevisionHashAnnotation                = "olm.operatorframework.io/hash"
	ClusterExtensionRevisionPreviousLimit = 5
)

type ClusterExtensionRevisionGenerator interface {
	GenerateRevision(bundleFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (*ocv1.ClusterExtensionRevision, error)
	GenerateRevisionFromHelmRelease(
		helmRelease *release.Release, ext *ocv1.ClusterExtension,
		objectLabels map[string]string,
	) (*ocv1.ClusterExtensionRevision, error)
}

type SimpleRevisionGenerator struct {
	Scheme         *runtime.Scheme
	BundleRenderer BundleRenderer
}

func (r *SimpleRevisionGenerator) GenerateRevisionFromHelmRelease(
	helmRelease *release.Release, ext *ocv1.ClusterExtension,
	objectLabels map[string]string,
) (*ocv1.ClusterExtensionRevision, error) {
	docs := splitManifestDocuments(helmRelease.Manifest)
	objs := make([]ocv1.ClusterExtensionRevisionObject, 0, len(docs))
	for _, doc := range docs {
		obj := unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			return nil, err
		}

		labels := maps.Clone(obj.GetLabels())
		if labels == nil {
			labels = map[string]string{}
		}
		maps.Copy(labels, objectLabels)
		obj.SetLabels(labels)
		obj.SetOwnerReferences(nil) // reset OwnerReferences for migration.

		objs = append(objs, ocv1.ClusterExtensionRevisionObject{
			Object:              obj,
			CollisionProtection: ocv1.CollisionProtectionNone, // allow to adopt objects from previous release
		})
	}

	rev := r.buildClusterExtensionRevision(objs, ext, map[string]string{
		labels.BundleNameKey:      helmRelease.Labels[labels.BundleNameKey],
		labels.PackageNameKey:     helmRelease.Labels[labels.PackageNameKey],
		labels.BundleVersionKey:   helmRelease.Labels[labels.BundleVersionKey],
		labels.BundleReferenceKey: helmRelease.Labels[labels.BundleReferenceKey],
	})
	rev.Name = fmt.Sprintf("%s-1", ext.Name)
	rev.Spec.Revision = 1
	return rev, nil
}

func (r *SimpleRevisionGenerator) GenerateRevision(
	bundleFS fs.FS, ext *ocv1.ClusterExtension,
	objectLabels, revisionAnnotations map[string]string,
) (*ocv1.ClusterExtensionRevision, error) {
	// extract plain manifests
	plain, err := r.BundleRenderer.Render(bundleFS, ext)
	if err != nil {
		return nil, err
	}

	// objectLabels
	objs := make([]ocv1.ClusterExtensionRevisionObject, 0, len(plain))
	for _, obj := range plain {
		labels := maps.Clone(obj.GetLabels())
		if labels == nil {
			labels = map[string]string{}
		}
		maps.Copy(labels, objectLabels)
		obj.SetLabels(labels)

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

	if revisionAnnotations == nil {
		revisionAnnotations = map[string]string{}
	}

	return r.buildClusterExtensionRevision(objs, ext, revisionAnnotations), nil
}

func (r *SimpleRevisionGenerator) buildClusterExtensionRevision(
	objects []ocv1.ClusterExtensionRevisionObject,
	ext *ocv1.ClusterExtension,
	annotations map[string]string,
) *ocv1.ClusterExtensionRevision {
	return &ocv1.ClusterExtensionRevision{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: annotations,
			Labels: map[string]string{
				controllers.ClusterExtensionRevisionOwnerLabel: ext.Name,
			},
		},
		Spec: ocv1.ClusterExtensionRevisionSpec{
			Phases: PhaseSort(objects),
		},
	}
}

type BoxcutterStorageMigrator struct {
	ActionClientGetter helmclient.ActionClientGetter
	RevisionGenerator  ClusterExtensionRevisionGenerator
	Client             boxcutterStorageMigratorClient
}

type boxcutterStorageMigratorClient interface {
	List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
	Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error
}

func (m *BoxcutterStorageMigrator) Migrate(ctx context.Context, ext *ocv1.ClusterExtension, objectLabels map[string]string) error {
	existingRevisionList := ocv1.ClusterExtensionRevisionList{}
	if err := m.Client.List(ctx, &existingRevisionList, client.MatchingLabels{
		controllers.ClusterExtensionRevisionOwnerLabel: ext.Name,
	}); err != nil {
		return fmt.Errorf("listing ClusterExtensionRevisions before attempting migration: %w", err)
	}
	if len(existingRevisionList.Items) != 0 {
		// No migration needed.
		return nil
	}

	ac, err := m.ActionClientGetter.ActionClientFor(ctx, ext)
	if err != nil {
		return err
	}

	helmRelease, err := ac.Get(ext.GetName())
	if errors.Is(err, driver.ErrReleaseNotFound) {
		// no Helm Release -> no prior installation.
		return nil
	}
	if err != nil {
		return err
	}

	rev, err := m.RevisionGenerator.GenerateRevisionFromHelmRelease(helmRelease, ext, objectLabels)
	if err != nil {
		return err
	}

	if err := m.Client.Create(ctx, rev); err != nil {
		return err
	}
	return nil
}

type Boxcutter struct {
	Client            client.Client
	Scheme            *runtime.Scheme
	RevisionGenerator ClusterExtensionRevisionGenerator
	Preflights        []Preflight
}

func (bc *Boxcutter) Apply(ctx context.Context, contentFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (bool, string, error) {
	return bc.apply(ctx, contentFS, ext, objectLabels, revisionAnnotations)
}

func (bc *Boxcutter) getObjects(rev *ocv1.ClusterExtensionRevision) []client.Object {
	var objs []client.Object
	for _, phase := range rev.Spec.Phases {
		for _, phaseObject := range phase.Objects {
			objs = append(objs, &phaseObject.Object)
		}
	}
	return objs
}

func (bc *Boxcutter) apply(ctx context.Context, contentFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (bool, string, error) {
	// Generate desired revision
	desiredRevision, err := bc.RevisionGenerator.GenerateRevision(contentFS, ext, objectLabels, revisionAnnotations)
	if err != nil {
		return false, "", err
	}

	// List all existing revisions
	existingRevisions, err := bc.getExistingRevisions(ctx, ext.GetName())
	if err != nil {
		return false, "", err
	}
	desiredHash := computeSHA256Hash(desiredRevision.Spec.Phases)

	// Sort into current and previous revisions.
	var (
		currentRevision *ocv1.ClusterExtensionRevision
	)
	state := StateNeedsInstall
	if len(existingRevisions) > 0 {
		maybeCurrentRevision := existingRevisions[len(existingRevisions)-1]
		annotations := maybeCurrentRevision.GetAnnotations()
		if annotations != nil {
			if revisionHash, ok := annotations[RevisionHashAnnotation]; ok && revisionHash == desiredHash {
				currentRevision = &maybeCurrentRevision
			}
		}
		state = StateNeedsUpgrade
	}

	// Preflights
	plainObjs := bc.getObjects(desiredRevision)
	for _, preflight := range bc.Preflights {
		if shouldSkipPreflight(ctx, preflight, ext, state) {
			continue
		}
		switch state {
		case StateNeedsInstall:
			err := preflight.Install(ctx, plainObjs)
			if err != nil {
				return false, "", err
			}
		// TODO: jlanford's IDE says that "StateNeedsUpgrade" condition is always true, but
		//   it isn't immediately obvious why that is. Perhaps len(existingRevisions) is
		//   always greater than 0 (seems unlikely), or shouldSkipPreflight always returns
		//   true (and we continue) when state == StateNeedsInstall?
		case StateNeedsUpgrade:
			err := preflight.Upgrade(ctx, plainObjs)
			if err != nil {
				return false, "", err
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

		if err = bc.setPreviousRevisions(ctx, newRevision, prevRevisions); err != nil {
			return false, "", fmt.Errorf("garbage collecting old Revisions: %w", err)
		}

		if err := controllerutil.SetControllerReference(ext, newRevision, bc.Scheme); err != nil {
			return false, "", fmt.Errorf("set ownerref: %w", err)
		}
		if err := bc.Client.Create(ctx, newRevision); err != nil {
			return false, "", fmt.Errorf("creating new Revision: %w", err)
		}
		currentRevision = newRevision
	}

	progressingCondition := meta.FindStatusCondition(currentRevision.Status.Conditions, ocv1.TypeProgressing)
	availableCondition := meta.FindStatusCondition(currentRevision.Status.Conditions, ocv1.ClusterExtensionRevisionTypeAvailable)
	succeededCondition := meta.FindStatusCondition(currentRevision.Status.Conditions, ocv1.ClusterExtensionRevisionTypeSucceeded)

	if progressingCondition == nil && availableCondition == nil && succeededCondition == nil {
		return false, "New revision created", nil
	} else if progressingCondition != nil && progressingCondition.Status == metav1.ConditionTrue {
		return false, progressingCondition.Message, nil
	} else if availableCondition != nil && availableCondition.Status != metav1.ConditionTrue {
		return false, "", errors.New(availableCondition.Message)
	} else if succeededCondition != nil && succeededCondition.Status != metav1.ConditionTrue {
		return false, succeededCondition.Message, nil
	}
	return true, "", nil
}

// setPreviousRevisions populates spec.previous of latestRevision, trimming the list of previous _archived_ revisions down to
// ClusterExtensionRevisionPreviousLimit or to the first _active_ revision and deletes trimmed revisions from the cluster.
// NOTE: revisionList must be sorted in chronographical order, from oldest to latest.
func (bc *Boxcutter) setPreviousRevisions(ctx context.Context, latestRevision *ocv1.ClusterExtensionRevision, revisionList []ocv1.ClusterExtensionRevision) error {
	trimmedPrevious := make([]ocv1.ClusterExtensionRevisionPrevious, 0)
	for index, r := range revisionList {
		if index < len(revisionList)-ClusterExtensionRevisionPreviousLimit && r.Spec.LifecycleState == ocv1.ClusterExtensionRevisionLifecycleStateArchived {
			// Delete oldest CREs from the cluster and list to reach ClusterExtensionRevisionPreviousLimit or latest active revision
			if err := bc.Client.Delete(ctx, &ocv1.ClusterExtensionRevision{
				ObjectMeta: metav1.ObjectMeta{
					Name: r.Name,
				},
			}); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("deleting previous archived Revision: %w", err)
			}
		} else {
			// All revisions within the limit or still active are preserved
			trimmedPrevious = append(trimmedPrevious, ocv1.ClusterExtensionRevisionPrevious{Name: r.Name, UID: r.GetUID()})
		}
	}
	latestRevision.Spec.Previous = trimmedPrevious
	return nil
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

	// TODO: change this out to `json.Marshal`. Pretty sure we found issues in the past where
	//   spew would produce different output when internal structures changed without the
	//   external public API changing.
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

// TODO: in the next refactor iteration BundleRenderer and RegistryV1BundleRenderer into the RegistryV1ChartProvider

type BundleRenderer interface {
	Render(bundleFS fs.FS, ext *ocv1.ClusterExtension) ([]client.Object, error)
}

type RegistryV1BundleRenderer struct {
	BundleRenderer      render.BundleRenderer
	CertificateProvider render.CertificateProvider
}

func (r *RegistryV1BundleRenderer) Render(bundleFS fs.FS, ext *ocv1.ClusterExtension) ([]client.Object, error) {
	reg, err := source.FromFS(bundleFS).GetBundle()
	if err != nil {
		return nil, err
	}

	if len(reg.CSV.Spec.WebhookDefinitions) > 0 && r.CertificateProvider == nil {
		return nil, fmt.Errorf("unsupported bundle: webhookDefinitions are not supported")
	}

	watchNamespace, err := GetWatchNamespace(ext)
	if err != nil {
		return nil, err
	}
	return r.BundleRenderer.Render(reg, ext.Spec.Namespace, render.WithTargetNamespaces(watchNamespace), render.WithCertificateProvider(r.CertificateProvider))
}

func splitManifestDocuments(file string) []string {
	//nolint:prealloc
	var docs []string
	for _, manifest := range strings.Split(file, "\n") {
		manifest = strings.TrimSpace(manifest)
		if len(manifest) == 0 {
			continue
		}
		docs = append(docs, manifest)
	}
	return docs
}
