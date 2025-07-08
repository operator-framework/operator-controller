/*
Copyright 2025.

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

package controller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crfinalizer "sigs.k8s.io/controller-runtime/pkg/finalizer"
	"sigs.k8s.io/controller-runtime/pkg/log"

	testolmv1 "github.com/operator-framework/operator-controller/testdata/operators/test-operator/v1/api/v1"
)

const (
	peaceOutFinalizer = "olm.operatorframework.io/peace-out"
)

// TestOperatorReconciler reconciles a TestOperator object
type TestOperatorReconciler struct {
	client.Client
	Finalizers crfinalizer.Finalizers
}

// +kubebuilder:rbac:groups=testolm.operatorframework.io,resources=testoperators,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=testolm.operatorframework.io,resources=testoperators/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=testolm.operatorframework.io,resources=testoperators/finalizers,verbs=update

func (r *TestOperatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("test-operator")
	ctx = log.IntoContext(ctx, l)

	l.Info("reconcile starting")
	defer l.Info("reconcile ending")

	existingTestOp := &testolmv1.TestOperator{}
	if err := r.Get(ctx, req.NamespacedName, existingTestOp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Reconcile TestOperator
	reconciledTestOp := existingTestOp.DeepCopy()

	// Reconcile finalizer
	finalizeResult, err := r.Finalizers.Finalize(ctx, reconciledTestOp)
	if err != nil {
		return ctrl.Result{}, err
	}
	if finalizeResult.Updated || finalizeResult.StatusUpdated {
		return ctrl.Result{}, r.Update(ctx, reconciledTestOp)
	}

	// Reconcile status
	reconciledTestOp.Status.Echo = reconciledTestOp.Spec.Message

	if !equality.Semantic.DeepEqual(existingTestOp.Status, reconciledTestOp.Status) {
		return ctrl.Result{}, r.Client.Status().Update(ctx, reconciledTestOp)
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TestOperatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := r.setupFinalizers(); err != nil {
		return fmt.Errorf("failed to setup finalizers: %v", err)
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&testolmv1.TestOperator{}).
		Named("testoperator").
		Complete(r)
}

type finalizerFunc func(ctx context.Context, obj client.Object) (crfinalizer.Result, error)

func (f finalizerFunc) Finalize(ctx context.Context, obj client.Object) (crfinalizer.Result, error) {
	return f(ctx, obj)
}

func (r *TestOperatorReconciler) setupFinalizers() error {
	f := crfinalizer.NewFinalizers()
	err := f.Register(peaceOutFinalizer, finalizerFunc(func(ctx context.Context, obj client.Object) (crfinalizer.Result, error) {
		if _, ok := obj.(*testolmv1.TestOperator); !ok {
			panic("could not convert object to testoperator")
		}
		log.FromContext(ctx).Info("peace out, bruh!")
		return crfinalizer.Result{StatusUpdated: true}, nil
	}))
	if err != nil {
		return err
	}
	r.Finalizers = f
	return nil
}
