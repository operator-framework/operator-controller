package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cucumber/godog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// ResourceWithNamespaceOwnerApplied resolves a real cluster-scoped Namespace owner
// before applying the resource so garbage collection sees valid owner metadata.
func ResourceWithNamespaceOwnerApplied(ctx context.Context, ownerName string, template *godog.DocString) error {
	sc := Scenario(ctx)
	obj, err := ToUnstructured(SubstituteScenarioVars(template.Content, sc))
	if err != nil {
		return err
	}
	ownerName = SubstituteScenarioVars(ownerName, sc)
	if ownerName != "" {
		owner, err := GetResource("namespace", ownerName, "")
		if err != nil {
			return err
		}
		obj.SetOwnerReferences([]metav1.OwnerReference{{
			APIVersion: "v1", Kind: "Namespace", Name: ownerName, UID: owner.GetUID(),
			Controller: ptr.To(true), BlockOwnerDeletion: ptr.To(true),
		}})
	} else {
		obj.SetOwnerReferences(nil)
	}
	content, err := yaml.Marshal(obj.Object)
	if err != nil {
		return err
	}
	return ResourceIsApplied(ctx, &godog.DocString{Content: string(content)})
}

func RememberResourceUID(ctx context.Context, resourceName string) error {
	sc := Scenario(ctx)
	resourceName = SubstituteScenarioVars(resourceName, sc)
	kind, name, found := strings.Cut(resourceName, "/")
	if !found {
		return fmt.Errorf("resource %s is not in the format <kind>/<name>", resourceName)
	}
	obj, err := GetResource(kind, name, sc.Namespace)
	if err != nil {
		return err
	}
	if obj.GetUID() == "" {
		return fmt.Errorf("resource %s has no UID", resourceName)
	}
	if sc.ResourceUIDs == nil {
		sc.ResourceUIDs = map[string]types.UID{}
	}
	sc.ResourceUIDs[resourceName] = obj.GetUID()
	return nil
}

func ResourceKeepsUID(ctx context.Context, resourceName string) error {
	sc := Scenario(ctx)
	resourceName = SubstituteScenarioVars(resourceName, sc)
	uid, found := sc.ResourceUIDs[resourceName]
	if !found {
		return fmt.Errorf("no UID remembered for resource %s", resourceName)
	}
	kind, name, _ := strings.Cut(resourceName, "/")
	obj, err := GetResource(kind, name, sc.Namespace)
	if err != nil {
		return err
	}
	if obj.GetUID() != uid {
		return fmt.Errorf("resource %s was replaced: UID changed from %s to %s", resourceName, uid, obj.GetUID())
	}
	return nil
}

// ListClusterObjectSetsInGroup selects revisions by group on the API server.
func ListClusterObjectSetsInGroup(ctx context.Context, group string) ([]ocv1.ClusterObjectSet, error) {
	out, err := K8sClient(ctx, "get", "clusterobjectsets", "--field-selector", "spec.group="+group, "-o", "json")
	if err != nil {
		return nil, err
	}
	var list ocv1.ClusterObjectSetList
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return nil, err
	}
	return list.Items, nil
}
