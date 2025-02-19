package authorization

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/convert"
	authv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	authorizationv1client "k8s.io/client-go/kubernetes/typed/authorization/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	SelfSubjectRulesReview  string = "SelfSubjectRulesReview"
	SelfSubjectAccessReview string = "SelfSubjectAccessReview"
)

type RestConfigMapper func(context.Context, client.Object, *rest.Config) (*rest.Config, error)

type NewForConfigFunc func(*rest.Config) (authorizationv1client.AuthorizationV1Interface, error)

type AuthorizationClientMapper struct {
	rcm          RestConfigMapper
	baseCfg      *rest.Config
	NewForConfig NewForConfigFunc
}

func NewAuthorizationClientMapper(rcm RestConfigMapper, baseCfg *rest.Config) AuthorizationClientMapper {
	return AuthorizationClientMapper{
		rcm:     rcm,
		baseCfg: baseCfg,
	}
}

func (acm *AuthorizationClientMapper) GetAuthorizationClient(ctx context.Context, ext *ocv1.ClusterExtension) (authorizationv1client.AuthorizationV1Interface, error) {
	authcfg, err := acm.rcm(ctx, ext, acm.baseCfg)
	if err != nil {
		return nil, err
	}

	return acm.NewForConfig(authcfg)
}

// Check if RBAC allows the installer service account necessary permissions on the objects in the contentFS
func (acm *AuthorizationClientMapper) CheckContentPermissions(ctx context.Context, contentFS fs.FS, authcl authorizationv1client.AuthorizationV1Interface, ext *ocv1.ClusterExtension) error {
	reg, err := convert.ParseFS(ctx, contentFS)
	if err != nil {
		return err
	}

	plain, err := convert.Convert(reg, ext.Spec.Namespace, []string{corev1.NamespaceAll})
	if err != nil {
		return err
	}

	err = checkObjectPermissions(ctx, authcl, plain.Objects, ext)

	return err
}

func checkObjectPermissions(ctx context.Context, authcl authorizationv1client.AuthorizationV1Interface, objects []client.Object, ext *ocv1.ClusterExtension) error {
	ssrr := &authv1.SelfSubjectRulesReview{
		Spec: authv1.SelfSubjectRulesReviewSpec{
			Namespace: ext.Spec.Namespace,
		},
	}

	opts := v1.CreateOptions{}
	ssrr, err := authcl.SelfSubjectRulesReviews().Create(ctx, ssrr, opts)
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

	namespacedErrs := []error{}
	clusterScopedErrs := []error{}
	requiredVerbs := []string{"get", "create", "update", "list", "watch", "delete", "patch"}
	resAttrs := make([]authv1.ResourceAttributes, 0, len(requiredVerbs)*len(objects))

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
				namespacedErrs = append(namespacedErrs, fmt.Errorf("cannot %q %q %q in namespace %q",
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
