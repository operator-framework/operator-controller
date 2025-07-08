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

package webhook_test

import (
	"context"
	testolmv2 "github.com/operator-framework/operator-controller/testdata/images/bundles/test-operator/v2.0.0/api/v2"
	"github.com/operator-framework/operator-controller/testdata/images/bundles/test-operator/v2.0.0/internal/webhook"
	"github.com/stretchr/testify/require"
	"testing"
)

func Test_DefaultingWebhook(t *testing.T) {
	obj := &testolmv2.TestOperator{}
	defaulter := webhook.TestOperatorCustomDefaulter{}
	t.Log("simulating a scenario where defaults should be applied")
	obj.Spec.EchoMessage = ""
	t.Log("calling the Default method to apply defaults")
	require.NoError(t, defaulter.Default(context.Background(), obj))
	t.Log("checking that the default values are set")
	require.Equal(t, "Echo", obj.Spec.EchoMessage)
}

func Test_ValidatingWebhook(t *testing.T) {
	validator := webhook.TestOperatorCustomValidator{}

	t.Log("checking creation validation")
	obj := &testolmv2.TestOperator{}
	obj.Spec.EchoMessage = "let's talk about fight club"
	_, err := validator.ValidateCreate(context.Background(), obj)
	require.Error(t, err)
	require.Contains(t, err.Error(), "we DO NOT talk about fight club")

	t.Log("checking update validation")
	t.Log("simulating a scenario where validating should be applied")
	obj = &testolmv2.TestOperator{}
	oldObj := &testolmv2.TestOperator{}
	obj.Spec.EchoMessage = "let's talk about fight club"
	_, err = validator.ValidateUpdate(context.Background(), oldObj, obj)
	require.Error(t, err)
	require.Contains(t, err.Error(), "we DO NOT talk about fight club")

	t.Log("checking there's no deletion validation")
	obj = &testolmv2.TestOperator{}
	obj.Spec.EchoMessage = "let's talk about fight club"
	_, err = validator.ValidateDelete(context.Background(), obj)
	require.NoError(t, err)
}
