package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	deploymentutil "github.com/operator-framework/operator-controller/internal/operator-controller/clusterobjectdeployment"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
	shared "github.com/operator-framework/operator-controller/test/internal/e2e/steps"
)

// The upgrade targets choose installer scripts explicitly. Keep COD assertions
// confined to the standard-to-experimental paths; other upgrades share the feature.
func standardToExperimentalUpgrade() bool {
	return strings.Contains(os.Getenv("RELEASE_UPGRADE"), "experimental") &&
		!strings.Contains(os.Getenv("RELEASE_INSTALL"), "experimental")
}

func RememberInstalledExtensionResources(ctx context.Context) error {
	if !standardToExperimentalUpgrade() {
		return nil
	}
	sc := shared.Scenario(ctx)
	objects, err := listHelmReleaseResources(sc.ClusterExtensionName)
	if err != nil {
		return err
	}
	if len(objects) == 0 {
		return fmt.Errorf("no installed Helm resources to remember")
	}
	sc.ExtensionObjects = nil
	for _, desired := range objects {
		live, err := shared.GetResource(desired.GetObjectKind().GroupVersionKind().Kind, desired.GetName(), desired.GetNamespace())
		if err != nil {
			return err
		}
		sc.ExtensionObjects = append(sc.ExtensionObjects, live)
	}
	return nil
}

func HelmMigrationIsCompleteAfterUpgrade(ctx context.Context) error {
	if !standardToExperimentalUpgrade() {
		return nil
	}
	sc := shared.Scenario(ctx)
	shared.WaitFor(ctx, func() bool {
		out, err := shared.K8sClient(ctx, "get", "clusterobjectdeployment", sc.ClusterExtensionName, "-o", "json")
		if err != nil {
			return false
		}
		var cod ocv1.ClusterObjectDeployment
		if json.Unmarshal([]byte(out), &cod) != nil || !deploymentutil.RolloutComplete(&cod) {
			return false
		}
		if _, marked := cod.Annotations[labels.HelmMigrationKey]; marked {
			return false
		}
		ext, err := shared.GetResource("clusterextension", sc.ClusterExtensionName, "")
		if err != nil || !metav1.IsControlledBy(&cod, ext) {
			return false
		}
		revisions, err := shared.ListClusterObjectSetsInGroup(ctx, sc.ClusterExtensionName)
		if err != nil {
			return false
		}
		return migratedTemplatesComplete(&cod, revisions)
	})
	for _, before := range sc.ExtensionObjects {
		after, err := shared.GetResource(before.GetObjectKind().GroupVersionKind().Kind, before.GetName(), before.GetNamespace())
		if err != nil {
			return err
		}
		if before.GetUID() != after.GetUID() {
			return fmt.Errorf("migration replaced %s %s/%s: UID changed from %s to %s", before.GetObjectKind().GroupVersionKind().Kind, before.GetNamespace(), before.GetName(), before.GetUID(), after.GetUID())
		}
	}
	return nil
}

// Observe the two migration templates independently of subsequent normal bundle
// rendering, which can legitimately create further revisions of the same bundle.
func migratedTemplatesComplete(cod *ocv1.ClusterObjectDeployment, revisions []ocv1.ClusterObjectSet) bool {
	var adopting, protecting *ocv1.ClusterObjectSet
	for i := range revisions {
		rev := &revisions[i]
		if !metav1.IsControlledBy(rev, cod) {
			continue
		}
		switch rev.Spec.Revision {
		case 1:
			adopting = rev
		case 2:
			protecting = rev
		}
	}
	if adopting == nil || protecting == nil || adopting.Status.CompletedAt.IsZero() || protecting.Status.CompletedAt.IsZero() {
		return false
	}
	return adopting.Spec.CollisionProtection == ocv1.CollisionProtectionNone &&
		protecting.Spec.CollisionProtection == ocv1.CollisionProtectionPrevent &&
		adopting.Spec.LifecycleState == ocv1.ClusterObjectSetLifecycleStateArchived &&
		reflect.DeepEqual(adopting.Spec.Phases, protecting.Spec.Phases) &&
		reflect.DeepEqual(adopting.Spec.ProgressionProbes, protecting.Spec.ProgressionProbes) &&
		reflect.DeepEqual(adopting.Annotations, protecting.Annotations)
}
