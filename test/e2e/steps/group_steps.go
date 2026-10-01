package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// A local cache index does not make spec.group selectable through kubectl.
func listClusterObjectSetsInGroup(ctx context.Context, group string) ([]ocv1.ClusterObjectSet, error) {
	out, err := k8sClient(ctx, "get", "clusterobjectsets", "-o", "json")
	if err != nil {
		return nil, err
	}
	var list ocv1.ClusterObjectSetList
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return nil, err
	}
	var revisions []ocv1.ClusterObjectSet
	for _, rev := range list.Items {
		if rev.Spec.Group == group {
			revisions = append(revisions, rev)
		}
	}
	return revisions, nil
}

func ClusterObjectSetHasGroup(ctx context.Context, revisionName, group string) error {
	sc := scenarioCtx(ctx)
	revisionName = substituteScenarioVars(revisionName, sc)
	group = substituteScenarioVars(group, sc)
	waitFor(ctx, func() bool {
		obj, err := getResource("clusterobjectset", revisionName, "")
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
	sc := scenarioCtx(ctx)
	revisionName = substituteScenarioVars(revisionName, sc)
	manager = substituteScenarioVars(manager, sc)
	waitFor(ctx, func() bool {
		obj, err := getResource("clusterobjectset", revisionName, "")
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
				desired := &phaseObj.Object
				if phaseObj.Ref.Name != "" {
					desired, err = resolveObjectRef(phaseObj.Ref)
					if err != nil {
						return false
					}
				}
				// kubectl's JSON/YAML printers omit managedFields by default.
				args := []string{"get", desired.GetKind(), desired.GetName(), "-o", "json", "--show-managed-fields"}
				if desired.GetNamespace() != "" {
					args = append(args, "-n", desired.GetNamespace())
				}
				out, err := k8sClient(ctx, args...)
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
	sc := scenarioCtx(ctx)
	revisionName = substituteScenarioVars(revisionName, sc)
	names, err := collectReferredSecretNames(ctx, revisionName)
	if err != nil {
		return err
	}
	for _, name := range names {
		sc.revisionSecrets = append(sc.revisionSecrets, resource{kind: "secret", name: name, namespace: olmNamespace})
	}
	return nil
}

func RememberedRevisionSecretsRemoved(ctx context.Context) error {
	sc := scenarioCtx(ctx)
	if len(sc.revisionSecrets) == 0 {
		return fmt.Errorf("no revision secrets were remembered")
	}
	for _, secret := range sc.revisionSecrets {
		waitFor(ctx, func() bool {
			out, err := k8sClient(ctx, "get", secret.kind, secret.name, "-n", secret.namespace, "--ignore-not-found", "-o", "name")
			return err == nil && strings.TrimSpace(out) == ""
		})
	}
	return nil
}
