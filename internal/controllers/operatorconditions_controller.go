/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"reflect"

	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	operatorsv2 "github.com/operator-framework/api/pkg/operators/v2"
	"github.com/operator-framework/operator-controller/pkg/lib/ownerutil"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	OperatorConditionEnvVarKey = "OPERATOR_CONDITION_NAME"
)

type OperatorConditionsReconciler struct {
	client.Client
	BundleProvider
}

var _ reconcile.Reconciler = (*OperatorConditionsReconciler)(nil)

func (r *OperatorConditionsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	condition := &operatorsv2.OperatorCondition{}
	if err := r.Get(ctx, req.NamespacedName, condition); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Setup OperatorCondition role
	if err := r.ensureOperatorConditionRole(ctx, condition); err != nil {
		return ctrl.Result{}, err
	}

	// Setup OperatorCondition role bindings
	if err := r.ensureOperatorConditionRoleBinding(ctx, condition); err != nil {
		return ctrl.Result{}, err
	}

	// Setup deployment envvars
	if err := r.ensureDeploymentEnvvars(ctx, condition); err != nil {
		return ctrl.Result{}, err
	}

	// Set OperatorConditions status
	if err := r.syncOperatorConditionStatus(ctx, condition); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *OperatorConditionsReconciler) ensureOperatorConditionRole(ctx context.Context, operatorCondition *operatorsv2.OperatorCondition) error {
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorCondition.GetName(),
			Namespace: operatorCondition.GetNamespace(),
			Labels:    map[string]string{},
		},
		Rules: []rbacv1.PolicyRule{
			{
				Verbs:         []string{"get", "update", "patch"},
				APIGroups:     []string{"operators.coreos.com"},
				Resources:     []string{"operatorconditions"},
				ResourceNames: []string{operatorCondition.GetName()},
			},
		},
	}
	ownerutil.AddOwner(role, operatorCondition, false, true)

	existing := &rbacv1.Role{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(role), existing); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}

		err := r.Create(ctx, role)
		if apierrors.IsAlreadyExists(err) {
			return r.Update(ctx, role)
		}
	}

	// compare existing and new roles, update existing if different
	if ownerutil.IsOwnedBy(existing, operatorCondition) &&
		reflect.DeepEqual(role.Rules, existing.Rules) {
		return nil
	}

	existing.OwnerReferences = role.OwnerReferences
	existing.Rules = role.Rules

	return r.Update(ctx, existing)
}

func (r *OperatorConditionsReconciler) ensureOperatorConditionRoleBinding(ctx context.Context, operatorCondition *operatorsv2.OperatorCondition) error {
	subjects := []rbacv1.Subject{}
	for _, sa := range operatorCondition.Spec.ServiceAccounts {
		subjects = append(subjects, rbacv1.Subject{
			Kind:     rbacv1.ServiceAccountKind,
			Name:     sa,
			APIGroup: "",
		})
	}

	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorCondition.GetName(),
			Namespace: operatorCondition.GetNamespace(),
			Labels:    map[string]string{}, // TODO: set labels
		},
		Subjects: subjects,
		RoleRef: rbacv1.RoleRef{
			Kind:     "Role",
			Name:     operatorCondition.GetName(),
			APIGroup: "rbac.authorization.k8s.io",
		},
	}
	ownerutil.AddOwner(roleBinding, operatorCondition, false, true)

	existing := &rbacv1.RoleBinding{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(operatorCondition), existing); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil
		}
		err := r.Create(ctx, roleBinding)
		if apierrors.IsAlreadyExists(err) {
			return r.Client.Update(ctx, roleBinding)
		}
	}

	if ownerutil.IsOwnedBy(existing, operatorCondition) &&
		existing.RoleRef == roleBinding.RoleRef &&
		reflect.DeepEqual(roleBinding.Subjects, existing.Subjects) {
		return nil
	}

	existing.OwnerReferences = roleBinding.OwnerReferences
	existing.Subjects = roleBinding.Subjects
	existing.RoleRef = roleBinding.RoleRef

	return r.Update(ctx, existing)
}

func (r *OperatorConditionsReconciler) ensureDeploymentEnvvars(ctx context.Context, operatorCondition *operatorsv2.OperatorCondition) error {
	// ensure deployments have the OPERATOR_CONDITION_NAME variable
	for _, deployName := range operatorCondition.Spec.Deployments {
		deployment := &appsv1.Deployment{}
		if err := r.Get(ctx,
			types.NamespacedName{
				Name:      deployName,
				Namespace: operatorCondition.GetNamespace(),
			},
			deployment,
		); err != nil {
			return err
		}

		// Check the deployment is owned by a CSV with the same name as the OperatorCondition.
		deploymentOwner := ownerutil.GetOwnerByKind(deployment, operatorsv1alpha1.ClusterServiceVersionKind)
		if deploymentOwner == nil || deploymentOwner.Name != operatorCondition.GetName() {
			continue
		}

		var deploymentNeedsUpdate bool
		for i := range deployment.Spec.Template.Spec.Containers {
			envVars, found := ensureEnvVarIsPresent(
				deployment.Spec.Template.Spec.Containers[i].Env,
				corev1.EnvVar{
					Name:  OperatorConditionEnvVarKey,
					Value: operatorCondition.GetName(),
				},
			)
			if !found {
				deploymentNeedsUpdate = true
				deployment.Spec.Template.Spec.Containers[i].Env = envVars
			}
		}
		if !deploymentNeedsUpdate {
			continue
		}

		if err := r.Update(ctx, deployment); err != nil {
			return err
		}
	}

	return nil
}

func (r *OperatorConditionsReconciler) syncOperatorConditionStatus(ctx context.Context, operatorCondition *operatorsv2.OperatorCondition) error {
	objectGen := operatorCondition.ObjectMeta.GetGeneration()
	var changed bool

	for _, cond := range operatorCondition.Spec.Conditions {
		if c := meta.FindStatusCondition(operatorCondition.Status.Conditions, cond.Type); c != nil {
			if cond.Status == c.Status && c.ObservedGeneration == objectGen {
				continue
			}
		}
		cond.ObservedGeneration = objectGen
		meta.SetStatusCondition(&operatorCondition.Status.Conditions, cond)
		changed = true
	}

	if changed {
		return r.Status().Update(ctx, operatorCondition)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *OperatorConditionsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	deployHandler := handler.EnqueueRequestsFromMapFunc(r.mapToOperatorCondition)

	handler := handler.EnqueueRequestForOwner(
		mgr.GetScheme(),
		mgr.GetRESTMapper(),
		&operatorsv2.OperatorCondition{},
		handler.OnlyControllerOwner(),
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorsv2.OperatorCondition{}).
		Watches(&rbacv1.Role{}, handler).
		Watches(&rbacv1.RoleBinding{}, handler).
		Watches(&appsv1.Deployment{}, deployHandler).
		Complete(r)
}

func (r *OperatorConditionsReconciler) mapToOperatorCondition(_ context.Context, obj client.Object) (requests []reconcile.Request) {
	if obj == nil {
		return nil
	}

	owner := ownerutil.GetOwnerByKind(obj, operatorsv1alpha1.ClusterServiceVersionKind)
	if owner == nil {
		return nil
	}

	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{
				Name:      obj.GetName(),
				Namespace: obj.GetNamespace(),
			},
		},
	}
}

func GetOwnerByKind(obj metav1.Object, ownerKind string) *metav1.OwnerReference {
	for _, ownerRef := range obj.GetOwnerReferences() {
		if ownerRef.Kind == ownerKind {
			return &ownerRef
		}
	}
	return nil
}

func ensureEnvVarIsPresent(envVars []corev1.EnvVar, envVar corev1.EnvVar) ([]corev1.EnvVar, bool) {
	for i, each := range envVars {
		if each.Name == envVar.Name {
			if each.Value == envVar.Value {
				return envVars, true
			}
			envVars[i].Value = envVar.Value
			return envVars, false
		}
	}
	return append(envVars, envVar), false
}
