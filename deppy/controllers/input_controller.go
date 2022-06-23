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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	deppyv1alpha1 "github.com/operator-framework/deppy/api/v1alpha1"
)

// InputReconciler reconciles a Input object
type InputReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=core.deppy.io,resources=inputs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core.deppy.io,resources=inputs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core.deppy.io,resources=inputs/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *InputReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	l.Info("reconciling request")
	defer l.Info("finished reconciling request")

	input := &deppyv1alpha1.Input{}
	if err := r.Get(ctx, req.NamespacedName, input); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *InputReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&deppyv1alpha1.Input{}).
		Complete(r)
}
