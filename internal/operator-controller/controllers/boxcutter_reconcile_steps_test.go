//go:build !standard

package controllers_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	coscontrollers "github.com/operator-framework/operator-controller/internal/object-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
)

func TestBoxcutterRevisionStatesGetter_GroupIndex(t *testing.T) {
	scheme := newScheme(t)
	cl, err := client.New(config, client.Options{Scheme: scheme})
	require.NoError(t, err)
	group := "cache-group"
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: group, UID: "cache-group-ext-uid"}}
	cod := &ocv1.ClusterObjectDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            group,
			UID:             "cache-group-cod-uid",
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(ext, ocv1.GroupVersion.WithKind(ocv1.ClusterExtensionKind))},
		},
		Spec: ocv1.ClusterObjectDeploymentSpec{
			Template: ocv1.ClusterObjectDeploymentTemplate{
				Spec: ocv1.ClusterObjectDeploymentTemplateSpec{
					CollisionProtection: ocv1.CollisionProtectionPrevent,
					Phases:              []ocv1.ClusterObjectSetPhase{{Name: "deploy", Objects: []ocv1.ClusterObjectSetObject{}}},
				},
			},
		},
	}
	require.NoError(t, cl.Create(t.Context(), cod))
	t.Cleanup(func() { require.NoError(t, client.IgnoreNotFound(cl.Delete(context.Background(), cod))) })

	for _, cosDef := range []struct {
		name      string
		group     string
		revision  int64
		archived  bool
		succeeded bool
		ownerName string
	}{
		// Only keep non-archived COSs with the matching group AND ownerName
		{name: "cache-group-1", group: group, revision: 1, succeeded: true, ownerName: group},
		{name: "cache-group-2", group: group, revision: 2, ownerName: group},
		{name: "cache-group-3", group: group, revision: 3, archived: true, succeeded: true, ownerName: group},
		{name: "cache-group-4", group: group, revision: 4, succeeded: true},
		{name: "cache-group-5", group: group, revision: 5, ownerName: "different-group"},
		{name: "other-group-99", group: "other-group", revision: 99, succeeded: true, ownerName: group},
	} {
		cos := &ocv1.ClusterObjectSet{
			ObjectMeta: metav1.ObjectMeta{Name: cosDef.name},
			Spec: ocv1.ClusterObjectSetSpec{
				Group: cosDef.group, Revision: cosDef.revision,
				LifecycleState:      ocv1.ClusterObjectSetLifecycleStateActive,
				CollisionProtection: ocv1.CollisionProtectionPrevent,
			},
		}
		if cosDef.group != group {
			cos.Labels = map[string]string{labels.OwnerNameKey: group}
		}
		if cosDef.ownerName != "" {
			owner := *metav1.NewControllerRef(cod, ocv1.GroupVersion.WithKind(ocv1.ClusterObjectDeploymentKind))
			owner.Name = cosDef.ownerName
			if cosDef.ownerName != cod.Name {
				owner.UID = "other-owner-uid"
			}
			cos.OwnerReferences = []metav1.OwnerReference{owner}
		}
		if cosDef.archived {
			cos.Spec.LifecycleState = ocv1.ClusterObjectSetLifecycleStateArchived
		}
		require.NoError(t, cl.Create(t.Context(), cos))
		t.Cleanup(func() { require.NoError(t, client.IgnoreNotFound(cl.Delete(context.Background(), cos))) })
		if cosDef.succeeded {
			cos.Status.CompletedAt = metav1.Now()
			require.NoError(t, cl.Status().Update(t.Context(), cos))
		}
	}

	managerCache, err := cache.New(config, cache.Options{Scheme: scheme})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	require.NoError(t, coscontrollers.SetupIndexes(ctx, managerCache))
	done := make(chan error, 1)
	go func() { done <- managerCache.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("manager cache did not stop")
		}
	})
	require.True(t, managerCache.WaitForCacheSync(ctx))
	cachedClient, err := client.New(config, client.Options{Scheme: scheme, Cache: &client.CacheOptions{Reader: managerCache}})
	require.NoError(t, err)
	getter := controllers.BoxcutterRevisionStatesGetter{Reader: cachedClient}
	states, err := getter.GetRevisionStates(ctx, ext)
	require.NoError(t, err)
	require.NotNil(t, states.Installed)
	require.Equal(t, "cache-group-1", states.Installed.RevisionName)
	require.Len(t, states.RollingOut, 1)
	require.Equal(t, "cache-group-2", states.RollingOut[0].RevisionName)

	require.NoError(t, cl.Delete(ctx, &ocv1.ClusterObjectSet{ObjectMeta: metav1.ObjectMeta{Name: "cache-group-2"}}))
	require.Eventually(t, func() bool {
		states, err := getter.GetRevisionStates(ctx, ext)
		return err == nil && len(states.RollingOut) == 0
	}, 5*time.Second, 10*time.Millisecond)
}
