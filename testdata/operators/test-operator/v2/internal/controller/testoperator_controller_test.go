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

package controller_test

import (
	"context"
	"github.com/stretchr/testify/require"
	apimachineryruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"log"
	"os"
	"path/filepath"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	crfinalizer "sigs.k8s.io/controller-runtime/pkg/finalizer"
	"testing"
	"testolmv2/internal/controller"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	testolmv2 "testolmv2/api/v2"
)

var (
	config *rest.Config
)

func TestMain(m *testing.M) {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "dist", "chart", "crds"),
		},
		ErrorIfCRDPathMissing: true,
	}

	if getFirstFoundEnvTestBinaryDir() != "" {
		testEnv.BinaryAssetsDirectory = getFirstFoundEnvTestBinaryDir()
	}

	var err error
	config, err = testEnv.Start()
	utilruntime.Must(err)
	if config == nil {
		log.Panic("expected cfg to not be nil")
	}

	code := m.Run()
	utilruntime.Must(testEnv.Stop())
	os.Exit(code)
}

// getFirstFoundEnvTestBinaryDir finds and returns the first directory under the given path.
func getFirstFoundEnvTestBinaryDir() string {
	basePath := filepath.Join("..", "..", "..", "..", "..", "..", "bin", "envtest-binaries", "k8s")
	entries, _ := os.ReadDir(basePath)
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}

func Test_Reconcile(t *testing.T) {
	cl, reconciler := newClientAndReconciler(t)

	const resourceName = "test-resource"

	ctx := context.Background()
	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
	}
	testoperator := &testolmv2.TestOperator{}

	err := cl.Get(ctx, typeNamespacedName, testoperator)
	if err != nil && errors.IsNotFound(err) {
		resource := &testolmv2.TestOperator{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: "default",
			},
			Spec: testolmv2.TestOperatorSpec{
				EchoMessage: "this is the message",
			},
		}
		require.NoError(t, cl.Create(ctx, resource))
		defer func() {
			t.Log("Cleanup the specific resource instance TestOperator")
			require.NoError(t, cl.Delete(ctx, resource))
		}()
	}

	require.NoError(t, cl.Get(ctx, typeNamespacedName, testoperator))

	t.Log("Reconciling the created resource")
	_, err = reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: typeNamespacedName,
	})
	require.NoError(t, err)

	t.Log("Checking that the resource was reconciled successfully")
	err = cl.Get(ctx, typeNamespacedName, testoperator)
	require.NoError(t, err)
	require.Equal(t, testoperator.Status.Echo, "this is the message")
}

func newClientAndReconciler(t *testing.T) (client.Client, *controller.TestOperatorReconciler) {
	sch := apimachineryruntime.NewScheme()
	require.NoError(t, testolmv2.AddToScheme(sch))
	cl, err := client.New(config, client.Options{Scheme: sch})
	require.NoError(t, err)
	require.NotNil(t, cl)

	reconciler := &controller.TestOperatorReconciler{
		Client:     cl,
		Finalizers: crfinalizer.NewFinalizers(),
	}
	return cl, reconciler
}
