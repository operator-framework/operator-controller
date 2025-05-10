package applier

import (
	"context"
	"fmt"
	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/authorization"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type Arbackulator struct {
	Client client.Client
}

func (t *Arbackulator) GrantPassage(ctx context.Context, cExt *ocv1.ClusterExtension, perms []authorization.ScopedPolicyRules) error {
	permMap := map[string][]rbacv1.PolicyRule{}
	for _, perm := range perms {
		permMap[perm.Namespace] = append(permMap[perm.Namespace], perm.MissingRules...)
	}

	for ns, rules := range permMap {
		switch ns {
		case "":
			if err := t.updateClusterPerms(ctx, cExt, rules); err != nil {
				return err
			}
		default:
			if err := t.updateNamespacedPerms(ctx, cExt, ns, rules); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *Arbackulator) updateClusterPerms(ctx context.Context, cExt *ocv1.ClusterExtension, rules []rbacv1.PolicyRule) error {
	clusterRoleName := fmt.Sprintf("%s-mananaged-cluster-role", cExt.Name)
	role := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterRoleName,
		},
	}

	if _, err := controllerutil.CreateOrUpdate(ctx, t.Client, role, func() error {
		role.Rules = append(role.Rules, rules...)
		return nil
	}); err != nil {
		return err
	}

	clusterRoleBindingName := fmt.Sprintf("%s-mananaged-cluster-rolebinding", cExt.Name)
	roleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterRoleBindingName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      cExt.Spec.ServiceAccount.Name,
				Namespace: cExt.Spec.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     clusterRoleName,
		},
	}

	if _, err := controllerutil.CreateOrUpdate(ctx, t.Client, roleBinding, func() error {
		return nil
	}); err != nil {
		return err
	}

	return nil
}

func (t *Arbackulator) updateNamespacedPerms(ctx context.Context, cExt *ocv1.ClusterExtension, namespace string, rules []rbacv1.PolicyRule) error {
	roleName := fmt.Sprintf("%s-mananaged-role", cExt.Name)
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: namespace,
		},
	}

	if _, err := controllerutil.CreateOrUpdate(ctx, t.Client, role, func() error {
		role.Rules = append(role.Rules, rules...)
		return nil
	}); err != nil {
		return err
	}

	roleBindingName := fmt.Sprintf("%s-mananaged-rolebinding", cExt.Name)
	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleBindingName,
			Namespace: namespace,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      cExt.Spec.ServiceAccount.Name,
				Namespace: cExt.Spec.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     roleName,
		},
	}

	if _, err := controllerutil.CreateOrUpdate(ctx, t.Client, roleBinding, func() error {
		return nil
	}); err != nil {
		return err
	}

	return nil
}
