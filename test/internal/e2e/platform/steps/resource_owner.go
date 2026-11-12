package steps

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shared "github.com/operator-framework/operator-controller/test/internal/e2e/steps"
)

// ResourceControlledByClusterObjectSet verifies the controller owner by UID after
// the named revision is Ready, so merely retaining an old owner name cannot pass.
func ResourceControlledByClusterObjectSet(ctx context.Context, resource, revision string) error {
	sc := shared.Scenario(ctx)
	resource = shared.SubstituteScenarioVars(resource, sc)
	revision = shared.SubstituteScenarioVars(revision, sc)
	kind, name, found := strings.Cut(resource, "/")
	if !found {
		return fmt.Errorf("resource %s is not in the format <kind>/<name>", resource)
	}
	obj, err := shared.GetResource(kind, name, sc.Namespace)
	if err != nil {
		return err
	}
	cos, err := shared.GetResource("clusterobjectset", revision, "")
	if err != nil {
		return err
	}
	owner := metav1.GetControllerOf(obj)
	if owner == nil || owner.APIVersion != cos.GetAPIVersion() || owner.Kind != cos.GetKind() ||
		owner.Name != cos.GetName() || owner.UID != cos.GetUID() {
		return fmt.Errorf("resource %s is not controlled by ClusterObjectSet %s (uid %s)", resource, revision, cos.GetUID())
	}
	if rec := shared.CommandRecorderFromContext(ctx); rec != nil {
		rec.RecordCustom("", fmt.Sprintf("Controller owner UID: %s\nClusterObjectSet UID: %s\n", owner.UID, cos.GetUID()), "")
	}
	return nil
}
