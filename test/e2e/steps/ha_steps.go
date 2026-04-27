package steps

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/component-base/featuregate"
)

// catalogdHAFeature gates scenarios that require a multi-node cluster.
// It is set to true in BeforeSuite when the cluster has at least 2 nodes,
// which is the case for the experimental e2e suite (kind-config-2node.yaml)
// but not the standard suite.
const catalogdHAFeature featuregate.Feature = "CatalogdHA"

// CatalogdLeaderPodIsForceDeleted force-deletes the catalogd leader pod to simulate leader loss.
// The pod is identified from sc.leaderPods["catalogd"] (populated by a prior
// "catalogd is ready to reconcile resources" step).  Force-deletion is equivalent to
// an abrupt process crash: the lease is no longer renewed and the surviving pod
// acquires leadership after the lease expires.
//
// Note: stopping the kind node container is not used here because both nodes in the
// experimental 2-node cluster are control-plane nodes that run etcd — stopping either
// would break etcd quorum and make the API server unreachable for the rest of the test.
func CatalogdLeaderPodIsForceDeleted(ctx context.Context) error {
	sc := scenarioCtx(ctx)
	leaderPod := sc.leaderPods["catalogd"]
	if leaderPod == "" {
		return fmt.Errorf("catalogd leader pod not found in scenario context; run 'catalogd is ready to reconcile resources' first")
	}

	logger.Info("Force-deleting catalogd leader pod", "pod", leaderPod)
	if _, err := k8sClient("delete", "pod", leaderPod, "-n", olmNamespace,
		"--force", "--grace-period=0"); err != nil {
		return fmt.Errorf("failed to force-delete catalogd leader pod %q: %w", leaderPod, err)
	}
	return nil
}

// NewCatalogdLeaderIsElected polls the catalogd leader election lease until the holder
// identity changes to a pod other than the deleted leader.  It updates
// sc.leaderPods["catalogd"] with the new leader pod name.
func NewCatalogdLeaderIsElected(ctx context.Context) error {
	sc := scenarioCtx(ctx)
	oldLeader := sc.leaderPods["catalogd"]

	waitFor(ctx, func() bool {
		holder, err := k8sClient("get", "lease", leaseNames["catalogd"], "-n", olmNamespace,
			"-o", "jsonpath={.spec.holderIdentity}")
		if err != nil || holder == "" {
			return false
		}
		newPod := strings.Split(strings.TrimSpace(holder), "_")[0]
		if newPod == oldLeader {
			return false
		}
		sc.leaderPods["catalogd"] = newPod
		logger.Info("New catalogd leader elected", "pod", newPod)
		return true
	})
	return nil
}
