package authorization

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	authv1 "k8s.io/api/authorization/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	authorizationv1client "k8s.io/client-go/kubernetes/typed/authorization/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	SelfSubjectRulesReview  string = "SelfSubjectRulesReview"
	SelfSubjectAccessReview string = "SelfSubjectAccessReview"
)

func CheckObjectPermissions(ctx context.Context, authcl *authorizationv1client.AuthorizationV1Client, objects []client.Object, ext *ocv1.ClusterExtension) error {
	ssrr := &authv1.SelfSubjectRulesReview{
		Spec: authv1.SelfSubjectRulesReviewSpec{
			Namespace: ext.Spec.Namespace,
		},
	}

	ssrr, err := authcl.SelfSubjectRulesReviews().Create(ctx, ssrr, v1.CreateOptions{})
	if err != nil {
		return err
	}

	rules := []rbacv1.PolicyRule{}
	for _, rule := range ssrr.Status.ResourceRules {
		rules = append(rules, rbacv1.PolicyRule{
			Verbs:         rule.Verbs,
			APIGroups:     rule.APIGroups,
			Resources:     rule.Resources,
			ResourceNames: rule.ResourceNames,
		})
	}

	for _, rule := range ssrr.Status.NonResourceRules {
		rules = append(rules, rbacv1.PolicyRule{
			Verbs:           rule.Verbs,
			NonResourceURLs: rule.NonResourceURLs,
		})
	}

	resAttrs := []authv1.ResourceAttributes{}
	namespacedErrs := []error{}
	clusterScopedErrs := []error{}
	requiredVerbs := []string{"get", "create", "update", "list", "watch", "delete", "patch"}

	for _, o := range objects {
		for _, verb := range requiredVerbs {
			resAttrs = append(resAttrs, authv1.ResourceAttributes{
				Namespace: o.GetNamespace(),
				Verb:      verb,
				Resource:  sanitizeResourceName(o.GetObjectKind().GroupVersionKind().Kind),
				Group:     o.GetObjectKind().GroupVersionKind().Group,
				Name:      o.GetName(),
			})
		}
	}

	for _, resAttr := range resAttrs {
		if !canI(resAttr, rules) {
			if resAttr.Namespace != "" {
				namespacedErrs = append(namespacedErrs, fmt.Errorf("cannot %s %s %s in namespace %s",
					resAttr.Verb,
					strings.TrimSuffix(resAttr.Resource, "s"),
					resAttr.Name,
					resAttr.Namespace))
				continue
			}
			clusterScopedErrs = append(clusterScopedErrs, fmt.Errorf("cannot %s %s %s",
				resAttr.Verb,
				strings.TrimSuffix(resAttr.Resource, "s"),
				resAttr.Name))
		}
	}
	errs := append(namespacedErrs, clusterScopedErrs...)
	if len(errs) > 0 {
		errs = append([]error{fmt.Errorf("installer service account %s is missing required permissions", ext.Spec.ServiceAccount.Name)}, errs...)
	}

	return errors.Join(errs...)

}

// Checks if the rules allow the verb on the GroupVersionKind in resAttr
func canI(resAttr authv1.ResourceAttributes, rules []rbacv1.PolicyRule) bool {
	var canI bool
	for _, rule := range rules {
		if (slices.Contains(rule.APIGroups, resAttr.Group) || slices.Contains(rule.APIGroups, "*")) &&
			(slices.Contains(rule.Resources, resAttr.Resource) || slices.Contains(rule.Resources, "*")) &&
			(slices.Contains(rule.Verbs, resAttr.Verb) || slices.Contains(rule.Verbs, "*")) &&
			(slices.Contains(rule.ResourceNames, resAttr.Name) || len(rule.ResourceNames) == 0) {
			canI = true
			break
		}
	}
	return canI
}

// SelfSubjectRulesReview formats the resource names as lowercase and plural
func sanitizeResourceName(resourceName string) string {
	return strings.ToLower(resourceName) + "s"
}
