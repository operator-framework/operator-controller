package steps

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/component-base/featuregate"
)

// catalogdHAFeature gates scenarios that require a multi-node cluster.
// It is set to true in BeforeSuite when the cluster has at least 2 nodes,
// which is the case for the experimental e2e suite (Kind-config-2node.yaml)
// but not the standard suite.
const catalogdHAFeature featuregate.Feature = "CatalogdHA"

// CatalogdLeaderPodIsForceDeleted force-deletes the catalogd leader pod to simulate leader loss.
// The pod is identified from sc.LeaderPods["catalogd"] (populated by a prior
// "catalogd is ready to reconcile resources" step).  Force-deletion is equivalent to
// an abrupt process crash: the lease is no longer renewed and the surviving pod
// acquires leadership after the lease expires.
//
// Note: stopping the Kind node container is not used here because both nodes in the
// experimental 2-node cluster are control-plane nodes that run etcd — stopping either
// would break etcd quorum and make the API server unreachable for the rest of the test.
func CatalogdLeaderPodIsForceDeleted(ctx context.Context) error {
	sc := Scenario(ctx)
	leaderPod := sc.LeaderPods["catalogd"]
	if leaderPod == "" {
		return fmt.Errorf("catalogd leader pod not found in scenario context; run 'catalogd is ready to reconcile resources' first")
	}

	logger.Info("Force-deleting catalogd leader pod", "pod", leaderPod)
	if _, err := K8sClient(ctx, "delete", "pod", leaderPod, "-n", NamespaceForComponent("catalogd"),
		"--force", "--grace-period=0"); err != nil {
		return fmt.Errorf("failed to force-delete catalogd leader pod %q: %w", leaderPod, err)
	}
	return nil
}

// NewCatalogdLeaderIsElected polls the catalogd leader election lease until the holder
// identity changes to a pod other than the deleted leader.  It updates
// sc.LeaderPods["catalogd"] with the new leader pod Name.
func NewCatalogdLeaderIsElected(ctx context.Context) error {
	sc := Scenario(ctx)
	oldLeader := sc.LeaderPods["catalogd"]

	WaitFor(ctx, func() bool {
		holder, err := K8sClient(ctx, "get", "lease", leaseNames["catalogd"], "-n", NamespaceForComponent("catalogd"),
			"-o", "jsonpath={.spec.holderIdentity}")
		if err != nil || holder == "" {
			return false
		}
		newPod := strings.Split(strings.TrimSpace(holder), "_")[0]
		if newPod == oldLeader {
			return false
		}
		sc.LeaderPods["catalogd"] = newPod
		logger.Info("New catalogd leader elected", "pod", newPod)
		return true
	})
	return nil
}
