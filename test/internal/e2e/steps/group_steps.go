package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cucumber/godog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
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

func ClusterObjectSetHasGroup(ctx context.Context, revisionName, group string) error {
	sc := Scenario(ctx)
	revisionName = SubstituteScenarioVars(revisionName, sc)
	group = SubstituteScenarioVars(group, sc)
	WaitFor(ctx, func() bool {
		obj, err := GetResource("clusterobjectset", revisionName, "")
		if err != nil {
			return false
		}
		var cos ocv1.ClusterObjectSet
		return runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &cos) == nil && cos.Spec.Group == group
	})
	return nil
}

// ClusterObjectSetObjectsUseSSAManager checks actual managed fields on every
// inline or externally stored phase object, including their revision owner.
func ClusterObjectSetObjectsUseSSAManager(ctx context.Context, revisionName, manager string) error {
	sc := Scenario(ctx)
	revisionName = SubstituteScenarioVars(revisionName, sc)
	manager = SubstituteScenarioVars(manager, sc)
	WaitFor(ctx, func() bool {
		obj, err := GetResource("clusterobjectset", revisionName, "")
		if err != nil {
			return false
		}
		var cos ocv1.ClusterObjectSet
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &cos); err != nil {
			return false
		}
		count := 0
		for _, phase := range cos.Spec.Phases {
			for _, phaseObj := range phase.Objects {
				desired := &phaseObj.InlineManifest
				if phaseObj.Type == ocv1.ObjectTypeObjectReference {
					desired, err = ResolveObjectRef(phaseObj.ObjectRef)
					if err != nil {
						return false
					}
				}
				// kubectl's JSON/YAML printers omit managedFields by default.
				args := []string{"get", desired.GetKind(), desired.GetName(), "-o", "json", "--show-managed-fields"}
				if desired.GetNamespace() != "" {
					args = append(args, "-n", desired.GetNamespace())
				}
				out, err := K8sClient(ctx, args...)
				if err != nil {
					return false
				}
				actual := &unstructured.Unstructured{}
				if err := json.Unmarshal([]byte(out), &actual.Object); err != nil {
					return false
				}
				owner := metav1.GetControllerOf(actual)
				if owner == nil || owner.Kind != ocv1.ClusterObjectSetKind || owner.UID != cos.UID {
					return false
				}
				found := false
				for _, fields := range actual.GetManagedFields() {
					if fields.Operation != metav1.ManagedFieldsOperationApply {
						continue
					}
					if strings.HasPrefix(fields.Manager, "cos-group/") && fields.Manager != manager {
						return false
					}
					if fields.Manager == manager {
						found = true
					}
				}
				if !found {
					return false
				}
				count++
			}
		}
		return count > 0
	})
	return nil
}

func RememberClusterObjectSetSecrets(ctx context.Context, revisionName string) error {
	sc := Scenario(ctx)
	revisionName = SubstituteScenarioVars(revisionName, sc)
	names, err := CollectReferredSecretNames(ctx, revisionName)
	if err != nil {
		return err
	}
	for _, name := range names {
		sc.RevisionSecrets = append(sc.RevisionSecrets, Resource{Kind: "secret", Name: name, Namespace: OLMNamespace})
	}
	return nil
}

func RememberedRevisionSecretsRemoved(ctx context.Context) error {
	sc := Scenario(ctx)
	if len(sc.RevisionSecrets) == 0 {
		return fmt.Errorf("no revision secrets were remembered")
	}
	for _, secret := range sc.RevisionSecrets {
		WaitFor(ctx, func() bool {
			out, err := K8sClient(ctx, "get", secret.Kind, secret.Name, "-n", secret.Namespace, "--ignore-not-found", "-o", "name")
			return err == nil && strings.TrimSpace(out) == ""
		})
	}
	return nil
}
