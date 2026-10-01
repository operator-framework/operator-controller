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
	"testing"
	"time"

	"github.com/go-logr/logr"
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
	"github.com/operator-framework/operator-controller/internal/operator-controller/controllers"
)

func TestDefaultRevisionEngineFactory_FieldOwnership(t *testing.T) {
	scheme := newScheme(t)
	require.NoError(t, corev1.AddToScheme(scheme))
	cl, err := client.New(config, client.Options{Scheme: scheme})
	require.NoError(t, err)

	// A core resource serves as the owner to isolate the factory's SSA and
	// metadata configuration from the COS controller's lifecycle behavior.
	owner := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "factory-field-owner-"}}
	require.NoError(t, cl.Create(t.Context(), owner))
	objectKey := client.ObjectKey{Namespace: owner.Name, Name: "payload"}
	t.Cleanup(func() {
		require.NoError(t, client.IgnoreNotFound(cl.Delete(context.Background(), &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: objectKey.Namespace, Name: objectKey.Name},
		})))
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
	cos := &ocv1.ClusterObjectSet{
		ObjectMeta: metav1.ObjectMeta{Name: "factory-test-1"},
		Spec:       ocv1.ClusterObjectSetSpec{Group: "factory-test", Revision: 1},
	}
	engine, err := factory.CreateRevisionEngine(ctx, cos)
	require.NoError(t, err)

	obj := &corev1.ConfigMap{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{Namespace: objectKey.Namespace, Name: objectKey.Name},
		Data:       map[string]string{"value": "revision-1"},
	}
	revision := boxcutter.NewRevisionWithOwner(cos.Name, cos.Spec.Revision,
		[]boxcutter.Phase{boxcutter.NewPhase("deploy", []client.Object{obj})},
		owner, ownerhandling.NewNative(scheme),
	)
	result, err := engine.Reconcile(ctx, revision)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.GetValidationError(), "%v", result.GetValidationError())

	actual := &corev1.ConfigMap{}
	require.NoError(t, cl.Get(ctx, objectKey, actual))
	require.Equal(t, "1", actual.Annotations["olm.operatorframework.io/revision"])
	require.Equal(t, "olm.operatorframework.io", actual.Labels["app.kubernetes.io/managed-by"])

	var applyManagers []string
	for _, fields := range actual.ManagedFields {
		if fields.Operation == metav1.ManagedFieldsOperationApply {
			applyManagers = append(applyManagers, fields.Manager)
		}
	}
	require.Contains(t, applyManagers, "cos-group/factory-test")

	// A subsequent revision updates the same field using the same SSA manager.
	cos.Name = "factory-test-2"
	cos.Spec.Revision = 2
	engine, err = factory.CreateRevisionEngine(ctx, cos)
	require.NoError(t, err)
	obj.Data["value"] = "revision-2"
	objectMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	require.NoError(t, err)
	desired := &unstructured.Unstructured{Object: objectMap}
	revision = boxcutter.NewRevisionWithOwner(cos.Name, cos.Spec.Revision,
		[]boxcutter.Phase{boxcutter.NewPhase("deploy", []client.Object{desired})},
		owner, ownerhandling.NewNative(scheme),
	)
	require.Eventually(t, func() bool {
		if _, err := engine.Reconcile(ctx, revision); err != nil {
			return false
		}
		return cl.Get(ctx, objectKey, actual) == nil && actual.Data["value"] == "revision-2"
	}, 5*time.Second, 10*time.Millisecond)
	applyManagers = nil
	for _, fields := range actual.ManagedFields {
		if fields.Operation == metav1.ManagedFieldsOperationApply {
			applyManagers = append(applyManagers, fields.Manager)
		}
	}
	require.Equal(t, []string{"cos-group/factory-test"}, applyManagers)
	require.Equal(t, "2", actual.Annotations["olm.operatorframework.io/revision"])

	// Another group gets its own SSA manager.
	cos.Name = "factory-other-1"
	cos.Spec.Group = "factory-other"
	cos.Spec.Revision = 1
	engine, err = factory.CreateRevisionEngine(ctx, cos)
	require.NoError(t, err)
	other := desired.DeepCopy()
	other.SetName("other-payload")
	t.Cleanup(func() { require.NoError(t, client.IgnoreNotFound(cl.Delete(context.Background(), other))) })
	revision = boxcutter.NewRevisionWithOwner(cos.Name, cos.Spec.Revision,
		[]boxcutter.Phase{boxcutter.NewPhase("deploy", []client.Object{other})},
		owner, ownerhandling.NewNative(scheme),
	)
	_, err = engine.Reconcile(ctx, revision)
	require.NoError(t, err)
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(other), actual))
	applyManagers = nil
	for _, fields := range actual.ManagedFields {
		if fields.Operation == metav1.ManagedFieldsOperationApply {
			applyManagers = append(applyManagers, fields.Manager)
		}
	}
	require.Equal(t, []string{"cos-group/factory-other"}, applyManagers)
}
