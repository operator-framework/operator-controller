/*
Copyright 2022.

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
	"fmt"
	"sort"

	"github.com/operator-framework/deppy/api/v1alpha1"
	"github.com/operator-framework/deppy/internal/solver"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	knownConstrainPackageType = "olm.RequirePackage"
	knownPropertyPackageType  = "olm.package"
	knownPackageKey           = "package"
)

// ResolutionReconciler reconciles a Resolution object
type ResolutionReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=core.deppy.io,resources=resolutions,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core.deppy.io,resources=resolutions/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core.deppy.io,resources=resolutions/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.2/pkg/reconcile
func (r *ResolutionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	l.Info("reconciling request", "request", req.NamespacedName)
	defer l.Info("finished reconciling request")

	res := &v1alpha1.Resolution{}
	if err := r.Get(ctx, req.NamespacedName, res); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	defer func() {
		res := res.DeepCopy()
		res.ObjectMeta.ManagedFields = nil
		if err := r.Status().Patch(ctx, res, client.Apply, client.FieldOwner("resolutions")); err != nil {
			l.Error(err, "failed to patch status")
		}
	}()
	res.Status.IDs = nil

	inputs := &v1alpha1.InputList{}
	if err := r.List(context.Background(), inputs); err != nil {
		return ctrl.Result{}, err
	}
	if len(inputs.Items) == 0 {
		meta.SetStatusCondition(&res.Status.Conditions, metav1.Condition{
			Type:    "Resolved",
			Status:  metav1.ConditionFalse,
			Reason:  "NoRuntimeInputs",
			Message: "Waiting for runtime Input resources to be defined before performing resolution",
		})
		return ctrl.Result{}, nil
	}

	l.Info("evaluating constraint definitions")
	variables, err := r.EvaluateConstraints(res, inputs.Items)
	if err != nil {
		meta.SetStatusCondition(&res.Status.Conditions, metav1.Condition{
			Type:    "Resolved",
			Status:  metav1.ConditionFalse,
			Reason:  "ConstraintEvaluatorFailed",
			Message: err.Error(),
		})
		return ctrl.Result{}, err
	}
	if len(variables) == 0 {
		internalErr := fmt.Errorf("Failed to generate any internal solver.Variables")
		meta.SetStatusCondition(&res.Status.Conditions, metav1.Condition{
			Type:    "Resolved",
			Status:  metav1.ConditionFalse,
			Reason:  "ConstraintEvaluatorFailed",
			Message: internalErr.Error(),
		})
		return ctrl.Result{}, nil
	}

	l.Info("performing resolution")
	s, err := solver.New(solver.WithInput(variables))
	if err != nil {
		meta.SetStatusCondition(&res.Status.Conditions, metav1.Condition{
			Type:    "Resolved",
			Status:  metav1.ConditionFalse,
			Reason:  "SolverInitializationFailed",
			Message: err.Error(),
		})
		return ctrl.Result{}, err
	}
	installed, err := s.Solve(context.Background())
	if err != nil {
		meta.SetStatusCondition(&res.Status.Conditions, metav1.Condition{
			Type:    "Resolved",
			Status:  metav1.ConditionFalse,
			Reason:  "SolverProblemFailed",
			Message: err.Error(),
		})
		return ctrl.Result{}, err
	}
	meta.SetStatusCondition(&res.Status.Conditions, metav1.Condition{
		Type:    "Resolved",
		Status:  metav1.ConditionTrue,
		Reason:  "SuccessfulResolution",
		Message: "Successfully resolved runtime input resources",
	})

	l.Info("finished performing resolution", "length of installed IDs", len(installed))
	sort.SliceStable(installed, func(i, j int) bool {
		return installed[i].Identifier() < installed[j].Identifier()
	})

	res.Status.IDs = []string{}
	for _, install := range installed {
		res.Status.IDs = append(res.Status.IDs, install.Identifier().String())
	}

	return ctrl.Result{}, nil
}

func (r *ResolutionReconciler) EvaluateConstraints(res *v1alpha1.Resolution, items []v1alpha1.Input) ([]solver.Variable, error) {
	variables := make([]solver.Variable, 0)

	inputs, err := r.calculateInputVariables(items)
	if err != nil {
		return nil, err
	}
	for _, input := range inputs {
		variables = append(variables, input)
	}

	constraints, err := r.calculateConstraints(res, inputs, items)
	if err != nil {
		return nil, err
	}
	for _, constraint := range constraints {
		variables = append(variables, constraint)
	}

	return variables, nil
}

func (r *ResolutionReconciler) calculateInputVariables(
	items []v1alpha1.Input,
) (map[solver.Identifier]solver.Variable, error) {
	inputs := make(map[solver.Identifier]solver.Variable)

	for _, input := range items {
		id := solver.IdentifierFromString(input.GetName())
		if variable, ok := inputs[id]; ok {
			inputs[variable.Identifier()] = variable
			continue
		}
		inputs[id] = solver.GenericVariable{
			ID: id,
		}
	}
	return inputs, nil
}

func (r *ResolutionReconciler) calculateConstraints(
	res *v1alpha1.Resolution,
	visited map[solver.Identifier]solver.Variable,
	items []v1alpha1.Input,
) (map[solver.Identifier]solver.Variable, error) {
	inputs := make(map[solver.Identifier]solver.Variable)

	// for each constraint: create a solver.Variable and iterate over the set of properties
	// to determine which constraint rules need to be defined here.
	for _, constraint := range res.Spec.Constraints {
		// TODO: avoid hardcoding this logic
		if constraint.Type != knownConstrainPackageType {
			return nil, fmt.Errorf("unsupported constraint type %q", constraint.Type)
		}
		packageRef, ok := constraint.Value[knownPackageKey]
		if !ok {
			return nil, fmt.Errorf("invalid key for olm.packageVersion constraint type: missing package")
		}
		id := solver.IdentifierFromString(fmt.Sprintf("res-%s-package-%s", res.GetName(), packageRef))

		variable := solver.GenericVariable{
			ID:    id,
			Rules: []solver.Constraint{solver.Mandatory()},
		}
		inputDependencies := []solver.Identifier{}

		for _, input := range items {
			if len(input.Spec.Properties) == 0 {
				continue
			}
			if propertyExists(knownPropertyPackageType, knownPackageKey, packageRef, input.Spec.Properties) {
				inputDependencies = append(inputDependencies, solver.IdentifierFromString(input.GetName()))
			}
		}
		variable.Rules = append(variable.Rules, solver.Dependency(inputDependencies...))

		if len(inputDependencies) == 0 {
			variable.Rules = []solver.Constraint{
				solver.Mandatory(),
				solver.PrettyConstraint(solver.Prohibited(), fmt.Sprintf(`failed to find an input that matches the desired "%s" %s property`, packageRef, knownPropertyPackageType)),
			}
		}
		inputs[variable.ID] = variable
	}
	return inputs, nil
}

func propertyExists(tpe, key, value string, properties []v1alpha1.Property) bool {
	for _, property := range properties {
		if property.Type != tpe {
			continue
		}
		if property.Value[key] != value {
			continue
		}
		return true
	}
	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *ResolutionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Resolution{}).
		Watches(&source.Kind{Type: &v1alpha1.Input{}}, handler.EnqueueRequestsFromMapFunc(func(o client.Object) []reconcile.Request {
			resolutions := &v1alpha1.ResolutionList{}
			if err := r.List(context.Background(), resolutions); err != nil {
				return nil
			}
			res := make([]reconcile.Request, 0, len(resolutions.Items))
			for _, input := range resolutions.Items {
				res = append(res, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&input)})
			}
			return res
		})).
		Complete(r)
}
