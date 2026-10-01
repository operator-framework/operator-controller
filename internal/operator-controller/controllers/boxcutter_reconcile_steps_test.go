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
	"github.com/operator-framework/operator-controller/internal/operator-controller/clusterobjectset"
	"github.com/operator-framework/operator-controller/internal/operator-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
)

func TestBoxcutterRevisionStatesGetter_GroupIndex(t *testing.T) {
	scheme := newScheme(t)
	cl, err := client.New(config, client.Options{Scheme: scheme})
	require.NoError(t, err)
	group := "cache-group"
	for _, tc := range []struct {
		name      string
		group     string
		revision  int64
		archived  bool
		succeeded bool
	}{
		{name: "cache-installed", group: group, revision: 1, succeeded: true},
		{name: "cache-rolling", group: group, revision: 2},
		{name: "cache-archived", group: group, revision: 3, archived: true, succeeded: true},
		{name: "cache-other-group", group: "other-group", revision: 99, succeeded: true},
	} {
		cos := &ocv1.ClusterObjectSet{
			ObjectMeta: metav1.ObjectMeta{Name: tc.name},
			Spec: ocv1.ClusterObjectSetSpec{
				Group: tc.group, Revision: tc.revision,
				LifecycleState:      ocv1.ClusterObjectSetLifecycleStateActive,
				CollisionProtection: ocv1.CollisionProtectionPrevent,
			},
		}
		if tc.group != group {
			cos.Labels = map[string]string{labels.OwnerNameKey: group}
		}
		if tc.archived {
			cos.Spec.LifecycleState = ocv1.ClusterObjectSetLifecycleStateArchived
		}
		require.NoError(t, cl.Create(t.Context(), cos))
		t.Cleanup(func() { require.NoError(t, client.IgnoreNotFound(cl.Delete(context.Background(), cos))) })
		if tc.succeeded {
			cos.Status.Conditions = []metav1.Condition{{
				Type: ocv1.ClusterObjectSetTypeSucceeded, Status: metav1.ConditionTrue,
				Reason: ocv1.ReasonSucceeded, LastTransitionTime: metav1.Now(),
			}}
			require.NoError(t, cl.Status().Update(t.Context(), cos))
		}
	}

	managerCache, err := cache.New(config, cache.Options{Scheme: scheme})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	require.NoError(t, managerCache.IndexField(ctx, &ocv1.ClusterObjectSet{}, clusterobjectset.GroupField, clusterobjectset.ExtractGroup))
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
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: group}}
	states, err := getter.GetRevisionStates(ctx, ext)
	require.NoError(t, err)
	require.NotNil(t, states.Installed)
	require.Equal(t, "cache-installed", states.Installed.RevisionName)
	require.Len(t, states.RollingOut, 1)
	require.Equal(t, "cache-rolling", states.RollingOut[0].RevisionName)

	require.NoError(t, cl.Delete(ctx, &ocv1.ClusterObjectSet{ObjectMeta: metav1.ObjectMeta{Name: "cache-rolling"}}))
	require.Eventually(t, func() bool {
		states, err := getter.GetRevisionStates(ctx, ext)
		return err == nil && len(states.RollingOut) == 0
	}, 5*time.Second, 10*time.Millisecond)
}
