//go:build !standard

/*
Copyright 2026.

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
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"pkg.package-operator.run/boxcutter"
	"pkg.package-operator.run/boxcutter/managedcache"
	"pkg.package-operator.run/boxcutter/ownerhandling"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/object-controller/controllers"
)

// TestDefaultRevisionEngineFactory_FieldOwnership uses envtest to verify the
// factory's SSA manager and OLM metadata configuration against a real API server.
func TestDefaultRevisionEngineFactory_FieldOwnership(t *testing.T) {
	t.Run("revisions in the same group reuse the SSA manager", func(t *testing.T) {
		env := newRevisionEngineFactoryTestEnv(t)
		objectKey := client.ObjectKey{Namespace: env.owner.Name, Name: "payload"}

		for _, revisionNumber := range []int64{1, 2} {
			cos := &ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("factory-test-%d", revisionNumber)},
				Spec:       ocv1.ClusterObjectSetSpec{Group: "factory-test", Revision: revisionNumber},
			}
			expectedRevision := fmt.Sprint(revisionNumber)
			expectedValue := "revision-" + expectedRevision
			desired := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata":   map[string]any{"namespace": objectKey.Namespace, "name": objectKey.Name},
				"data":       map[string]any{"value": expectedValue},
			}}
			engine, err := env.factory.CreateRevisionEngine(env.ctx, cos)
			require.NoError(t, err)
			revision := boxcutter.NewRevisionWithOwner(cos.Name, cos.Spec.Revision,
				[]boxcutter.Phase{boxcutter.NewPhase("deploy", []client.Object{desired})},
				env.owner, ownerhandling.NewNative(env.scheme),
			)

			// Reconcile until the cached engine applies this revision's value.
			actual := &corev1.ConfigMap{}
			require.EventuallyWithT(t, func(collect *assert.CollectT) {
				result, err := engine.Reconcile(env.ctx, revision)
				if !assert.NoError(collect, err) || !assert.NotNil(collect, result) {
					return
				}
				assert.Nil(collect, result.GetValidationError())
				if assert.NoError(collect, env.client.Get(env.ctx, objectKey, actual)) {
					assert.Equal(collect, expectedValue, actual.Data["value"])
				}
			}, 5*time.Second, 10*time.Millisecond)

			// Both revisions must leave one group-scoped SSA manager and OLM metadata.
			require.Equal(t, []string{"cos-group/factory-test"}, applyFieldManagers(actual))
			require.Equal(t, expectedRevision, actual.Annotations["olm.operatorframework.io/revision"])
			require.Equal(t, "olm.operatorframework.io", actual.Labels["app.kubernetes.io/managed-by"])
		}
	})

	t.Run("different groups use distinct SSA managers", func(t *testing.T) {
		env := newRevisionEngineFactoryTestEnv(t)

		for _, tc := range []struct {
			group           string
			objectName      string
			expectedManager string
		}{
			{group: "factory-test", objectName: "payload", expectedManager: "cos-group/factory-test"},
			{group: "factory-other", objectName: "other-payload", expectedManager: "cos-group/factory-other"},
		} {
			cos := &ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{Name: tc.group + "-1"},
				Spec:       ocv1.ClusterObjectSetSpec{Group: tc.group, Revision: 1},
			}
			desired := &corev1.ConfigMap{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				ObjectMeta: metav1.ObjectMeta{Namespace: env.owner.Name, Name: tc.objectName},
				Data:       map[string]string{"value": "revision-1"},
			}
			engine, err := env.factory.CreateRevisionEngine(env.ctx, cos)
			require.NoError(t, err)
			revision := boxcutter.NewRevisionWithOwner(cos.Name, cos.Spec.Revision,
				[]boxcutter.Phase{boxcutter.NewPhase("deploy", []client.Object{desired})},
				env.owner, ownerhandling.NewNative(env.scheme),
			)

			// Apply each group's revision to its own ConfigMap.
			result, err := engine.Reconcile(env.ctx, revision)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Nil(t, result.GetValidationError())

			// Each resource must record its group's manager and the OLM metadata.
			actual := &corev1.ConfigMap{}
			require.NoError(t, env.client.Get(env.ctx, client.ObjectKeyFromObject(desired), actual))
			require.Equal(t, "revision-1", actual.Data["value"])
			require.Equal(t, []string{tc.expectedManager}, applyFieldManagers(actual))
			require.Equal(t, "1", actual.Annotations["olm.operatorframework.io/revision"])
			require.Equal(t, "olm.operatorframework.io", actual.Labels["app.kubernetes.io/managed-by"])
		}
	})
}

type revisionEngineFactoryTestEnv struct {
	ctx     context.Context
	client  client.Client
	scheme  *runtime.Scheme
	owner   *corev1.Namespace
	factory controllers.RevisionEngineFactory
}

func newRevisionEngineFactoryTestEnv(t *testing.T) *revisionEngineFactoryTestEnv {
	t.Helper()

	scheme := newScheme(t)
	require.NoError(t, corev1.AddToScheme(scheme))
	cl, err := client.New(config, client.Options{Scheme: scheme})
	require.NoError(t, err)

	// A core resource serves as the owner to isolate the factory's SSA and
	// metadata configuration from the COS controller's lifecycle behavior.
	owner := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "factory-field-owner-"}}
	require.NoError(t, cl.Create(t.Context(), owner))
	t.Cleanup(func() {
		require.NoError(t, cl.DeleteAllOf(context.Background(), &corev1.ConfigMap{}, client.InNamespace(owner.Name)))
		require.NoError(t, cl.Delete(context.Background(), owner))
	})

	trackingCache, err := managedcache.NewTrackingCache(logr.Discard(), config, cache.Options{
		Scheme: scheme,
		Mapper: cl.RESTMapper(),
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	cacheDone := make(chan error, 1)
	go func() { cacheDone <- trackingCache.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-cacheDone:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("tracking cache did not stop")
		}
	})
	require.NoError(t, trackingCache.Watch(ctx, owner, sets.New(corev1.SchemeGroupVersion.WithKind("ConfigMap"))))

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	require.NoError(t, err)
	factory, err := controllers.NewDefaultRevisionEngineFactory(
		scheme, trackingCache, memory.NewMemCacheClient(discoveryClient), cl.RESTMapper(), config,
	)
	require.NoError(t, err)
	return &revisionEngineFactoryTestEnv{
		ctx: ctx, client: cl, scheme: scheme, owner: owner, factory: factory,
	}
}

func applyFieldManagers(obj client.Object) []string {
	var managers []string
	for _, fields := range obj.GetManagedFields() {
		if fields.Operation == metav1.ManagedFieldsOperationApply {
			managers = append(managers, fields.Manager)
		}
	}
	return managers
}
