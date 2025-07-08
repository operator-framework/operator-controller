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

package webhook

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	testolmv2 "github.com/operator-framework/operator-controller/testdata/operators/test-operator/v2/api/v2"
)

const (
	DefaultMessageValue = "Echo"
)

// nolint:unused
// log is for logging in this package.
var testoperatorlog = logf.Log.WithName("testoperator-resource")

// SetupTestOperatorWebhookWithManager registers the webhook for TestOperator in the manager.
func SetupTestOperatorWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&testolmv2.TestOperator{}).
		WithValidator(&TestOperatorCustomValidator{}).
		WithDefaulter(&TestOperatorCustomDefaulter{}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-testolm-operatorframework-io-v2-testoperator,mutating=true,failurePolicy=fail,sideEffects=None,groups=testolm.operatorframework.io,resources=testoperators,verbs=create;update,versions=v2,name=mtestoperator-v2.kb.io,admissionReviewVersions=v2

type TestOperatorCustomDefaulter struct{}

var _ webhook.CustomDefaulter = &TestOperatorCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind TestOperator.
func (d *TestOperatorCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	testOp, ok := obj.(*testolmv2.TestOperator)

	if !ok {
		return fmt.Errorf("expected an TestOperator object but got %T", obj)
	}
	testoperatorlog.Info("Defaulting for TestOperator", "name", testOp.GetName())

	if len(strings.TrimSpace(testOp.Spec.EchoMessage)) == 0 {
		testOp.Spec.EchoMessage = DefaultMessageValue
	}
	return nil
}

// +kubebuilder:webhook:path=/validate-testolm-operatorframework-io-v2-testoperator,mutating=false,failurePolicy=fail,sideEffects=None,groups=testolm.operatorframework.io,resources=testoperators,verbs=create;update,versions=v2,name=vtestoperator-v2.kb.io,admissionReviewVersions=v2

type TestOperatorCustomValidator struct{}

var _ webhook.CustomValidator = &TestOperatorCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type TestOperator.
func (v *TestOperatorCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	testOp, ok := obj.(*testolmv2.TestOperator)
	if !ok {
		return nil, fmt.Errorf("expected a TestOperator object but got %T", obj)
	}
	testoperatorlog.Info("Validation for TestOperator upon creation", "name", testOp.GetName())
	var allErrs field.ErrorList
	if err := validateTestOperatorSpec(testOp); err != nil {
		allErrs = append(allErrs, err)
	}
	if len(allErrs) == 0 {
		return nil, nil
	}
	return nil, apierrors.NewInvalid(
		schema.GroupKind{Group: testolmv2.GroupVersion.Group, Kind: "TestOperator"},
		testOp.GetName(),
		allErrs,
	)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type TestOperator.
func (v *TestOperatorCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	testoperator, ok := newObj.(*testolmv2.TestOperator)
	if !ok {
		return nil, fmt.Errorf("expected a TestOperator object for the newObj but got %T", newObj)
	}
	testoperatorlog.Info("Validation for TestOperator upon update", "name", testoperator.GetName())
	return v.ValidateCreate(ctx, testoperator)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type TestOperator.
func (v *TestOperatorCustomValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func validateTestOperatorSpec(testOp *testolmv2.TestOperator) *field.Error {
	if strings.Contains(strings.ToLower(testOp.Spec.EchoMessage), "fight club") {
		return field.Invalid(field.NewPath("spec").Child("echoMessage"), testOp.Spec.EchoMessage, "we DO NOT talk about fight club")
	}
	return nil
}
