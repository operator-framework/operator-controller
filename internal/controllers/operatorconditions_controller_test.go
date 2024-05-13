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

package controllers_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

func TestOperatorConditionDoesNotExist(t *testing.T) {
	_, reconciler := newClientAndOperatorConditionsReconciler(t)

	t.Log("When the extension does not exist")
	t.Log("It returns no error")
	res, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "non-existent", Namespace: "non-existent"}})
	require.Equal(t, ctrl.Result{}, res)
	require.NoError(t, err)
}
