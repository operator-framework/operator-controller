//go:build !standard

package controllers_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	ocv1ac "github.com/operator-framework/operator-controller/applyconfigurations/api/v1"
	"github.com/operator-framework/operator-controller/internal/object-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
)

// archivalRevision describes one revision in an archival test case: how it differs
// from an active, owned, managed, incomplete revision, and the lifecycle state it
// must have after reconciliation.
type archivalRevision struct {
	traits []func(*ocv1.ClusterObjectSet)
	want   ocv1.ClusterObjectSetLifecycleState
}

func wantActive(traits ...func(*ocv1.ClusterObjectSet)) archivalRevision {
	return archivalRevision{traits: traits, want: ocv1.ClusterObjectSetLifecycleStateActive}
}

func wantArchived(traits ...func(*ocv1.ClusterObjectSet)) archivalRevision {
	return archivalRevision{traits: traits, want: ocv1.ClusterObjectSetLifecycleStateArchived}
}

// completed marks historical completion; the revision is not currently Ready.
func completed(r *ocv1.ClusterObjectSet) { r.Status.CompletedAt = metav1.Now() }

func alreadyArchived(r *ocv1.ClusterObjectSet) {
	r.Spec.LifecycleState = ocv1.ClusterObjectSetLifecycleStateArchived
}

func deleting(r *ocv1.ClusterObjectSet) {
	r.DeletionTimestamp = &metav1.Time{Time: metav1.Now().Time}
	r.Finalizers = []string{"test/hold"}
}

func foreignOwned(r *ocv1.ClusterObjectSet) {
	r.OwnerReferences = []metav1.OwnerReference{foreignControllerReference()}
}

func ownerless(r *ocv1.ClusterObjectSet) { r.OwnerReferences = nil }

// foreignRefWithDeploymentUID replaces the controller reference with one naming another
// controller kind and name but carrying the deployment's UID.
func foreignRefWithDeploymentUID(r *ocv1.ClusterObjectSet) {
	ref := foreignControllerReference()
	ref.UID = r.OwnerReferences[0].UID
	r.OwnerReferences = []metav1.OwnerReference{ref}
}

func unmanaged(r *ocv1.ClusterObjectSet) { delete(r.Labels, labels.ManagedByKey) }

func staleOwnerUID(r *ocv1.ClusterObjectSet) { r.OwnerReferences[0].UID = "previous-cod" }

// TestCODArchivesOwnedPredecessors ensures the deployment archives exactly the right
// predecessors: every active, non-deleting revision it controls that is below its
// highest completed, active, non-deleting controlled revision (the boundary).
// It verifies that:
//   - completion is historical, so a completed boundary authorizes archival even when
//     it is not currently Ready, a newer rollout is pending, or the template changed;
//   - without an eligible boundary (none completed, or the completed revision is
//     archived, deleting, or not controlled by the deployment) nothing is archived;
//   - revisions the deployment does not control (foreign owners, unmanaged orphans,
//     stale owner UIDs, forged references) and deleting revisions are never archived,
//     while safely adopted orphans are;
//   - a revision history limit does not short-circuit archival; and
//   - a second reconcile is a no-op once archival has settled.
//
// Missing or ownership-blind archival must fail these lifecycle assertions.
func TestCODArchivesOwnedPredecessors(t *testing.T) {
	for _, tc := range []struct {
		name string
		// revisions are numbered from 1 in order. Unless traits say otherwise, each
		// starts Active, controlled by the deployment, managed, and not completed.
		revisions []archivalRevision
		// deployment, if set, mutates the deployment after its revisions are built.
		deployment func(*ocv1.ClusterObjectDeployment)
	}{
		{name: "completed intermediate before pending head", revisions: []archivalRevision{
			wantArchived(), wantArchived(), wantActive(completed), wantActive(), wantActive(),
		}},
		{name: "no completion", revisions: []archivalRevision{
			wantActive(), wantActive(), wantActive(), wantActive(), wantActive(),
		}},
		{name: "history limit zero waits for teardown", revisions: []archivalRevision{
			wantArchived(), wantArchived(), wantActive(completed), wantActive(), wantActive(),
		}, deployment: func(c *ocv1.ClusterObjectDeployment) { c.Spec.RevisionHistoryLimit = ptr.To(int32(0)) }},
		{name: "completed head", revisions: []archivalRevision{
			wantArchived(), wantArchived(), wantArchived(completed), wantArchived(), wantActive(completed),
		}},
		{name: "foreign completion does not authorize archival", revisions: []archivalRevision{
			wantActive(), wantActive(), wantActive(completed, foreignOwned), wantActive(), wantActive(),
		}},
		{name: "foreign reference with deployment UID does not authorize archival", revisions: []archivalRevision{
			wantActive(), wantActive(), wantActive(completed, foreignRefWithDeploymentUID), wantActive(), wantActive(),
		}},
		{name: "archived completion is excluded", revisions: []archivalRevision{
			wantActive(), wantActive(), wantArchived(completed, alreadyArchived), wantActive(), wantActive(),
		}},
		{name: "deleting completion is excluded", revisions: []archivalRevision{
			wantActive(), wantActive(), wantActive(completed, deleting), wantActive(), wantActive(),
		}},
		{name: "foreign predecessor is excluded", revisions: []archivalRevision{
			wantActive(foreignOwned), wantArchived(), wantActive(completed), wantActive(), wantActive(),
		}},
		{name: "foreign reference with deployment UID is excluded", revisions: []archivalRevision{
			wantActive(foreignRefWithDeploymentUID), wantArchived(), wantActive(completed), wantActive(), wantActive(),
		}},
		{name: "unmanaged orphan is excluded", revisions: []archivalRevision{
			wantActive(ownerless, unmanaged), wantArchived(), wantActive(completed), wantActive(), wantActive(),
		}},
		{name: "safely adopted predecessor is archived", revisions: []archivalRevision{
			wantArchived(ownerless), wantArchived(), wantActive(completed), wantActive(), wantActive(),
		}},
		{name: "stale owner UID is excluded", revisions: []archivalRevision{
			wantActive(staleOwnerUID), wantArchived(), wantActive(completed), wantActive(), wantActive(),
		}},
		{name: "deleting predecessor is excluded", revisions: []archivalRevision{
			wantActive(deleting), wantArchived(), wantActive(completed), wantActive(), wantActive(),
		}},
		{name: "foreign leader does not block owned cleanup", revisions: []archivalRevision{
			wantArchived(), wantArchived(), wantActive(completed), wantActive(), wantActive(foreignOwned),
		}},
		{name: "new desired template does not block cleanup", revisions: []archivalRevision{
			wantArchived(), wantArchived(), wantActive(completed), wantActive(), wantActive(),
		}, deployment: func(c *ocv1.ClusterObjectDeployment) {
			c.Spec.Template.Metadata.Annotations = map[string]string{"test/change": "new"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cod := codFixture()
			revs := make([]*ocv1.ClusterObjectSet, len(tc.revisions))
			for i, rc := range tc.revisions {
				revs[i] = deploymentRevision(cod, int64(i+1), ocv1.ClusterObjectSetLifecycleStateActive)
				revs[i].UID = types.UID(fmt.Sprintf("revision-%d", i+1))
				for _, trait := range rc.traits {
					trait(revs[i])
				}
			}
			if tc.deployment != nil {
				tc.deployment(cod)
			}
			objs := make([]client.Object, 0, 1+len(revs))
			objs = append(objs, cod)
			for _, r := range revs {
				objs = append(objs, r)
			}
			base := newCODClient(t, objs...)
			applies := 0
			c := interceptor.NewClient(base, interceptor.Funcs{Apply: func(ctx context.Context, cl client.WithWatch, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
				if _, ok := obj.(*ocv1ac.ClusterObjectSetApplyConfiguration); ok {
					applies++
				}
				return cl.Apply(ctx, obj, opts...)
			}})
			_, err := reconcileCOD(t, c)
			require.NoError(t, err)
			for i, r := range revs {
				got, ok := getCOS(t, base, r.Name)
				require.True(t, ok)
				require.Equal(t, tc.revisions[i].want, got.Spec.LifecycleState, r.Name)
			}
			firstApplies := applies
			_, err = reconcileCOD(t, c)
			require.NoError(t, err)
			require.Equal(t, firstApplies, applies, "settled revisions must not be applied again")
		})
	}
}

func ready(r *ocv1.ClusterObjectSet) {
	setCOSReady(r, metav1.ConditionTrue, ocv1.ClusterObjectSetReasonAllObjectsReady)
}

// wantGone describes a revision that must no longer exist after reconciliation.
func wantGone(traits ...func(*ocv1.ClusterObjectSet)) archivalRevision {
	return archivalRevision{traits: traits}
}

// TestCODArchivalFailures ensures archival failures are isolated housekeeping errors.
// When archiving or adopting one revision fails, the reconcile returns the error but
// still archives the remaining candidates and keeps reconciling rollout and status
// (including creating a new revision for a changed template); archived Ready
// predecessors do not establish availability. It also verifies that a guarded
// archival that conflicts because the revision changed owner or was deleted is
// surfaced rather than retried with refreshed guards, and that an archival the API
// server does not persist is surfaced and leaves the revision counted as active.
func TestCODArchivalFailures(t *testing.T) {
	cosResource := ocv1.GroupVersion.WithResource("clusterobjectsets").GroupResource()
	for _, tc := range []struct {
		name string
		// revisions are numbered from 1 in order. Unless traits say otherwise, each
		// starts Active, controlled by the deployment, managed, and not completed.
		revisions []archivalRevision
		// deployment, if set, mutates the deployment after its revisions are built.
		deployment func(*ocv1.ClusterObjectDeployment)
		// onArchive, if set, runs when the controller applies archival to revision 1.
		// A non-nil error is returned in place of performing the apply.
		onArchive func(t *testing.T, ctx context.Context, cl client.Client, name string) error
		// mutateArchive, if set, rewrites the archival apply to revision 1 before it is
		// stored, simulating a mutating admission webhook.
		mutateArchive func(*ocv1ac.ClusterObjectSetApplyConfiguration)
		// adoptionErr, if set, is returned in place of adopting revision 4.
		adoptionErr error

		wantErr               bool
		wantFirstOwnerUID     types.UID
		wantNewRevision       bool
		wantProgressingReason string
		wantAvailable         metav1.ConditionStatus
	}{
		{
			name:      "denied",
			revisions: []archivalRevision{wantActive(), wantArchived(), wantActive(completed)},
			onArchive: func(*testing.T, context.Context, client.Client, string) error {
				return errors.New("archive denied")
			},
			wantErr:               true,
			wantProgressingReason: ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed,
		},
		{
			name:      "new rollout",
			revisions: []archivalRevision{wantActive(), wantArchived(), wantActive(completed)},
			deployment: func(c *ocv1.ClusterObjectDeployment) {
				c.Spec.Template.Metadata.Annotations = map[string]string{"test/change": "new"}
			},
			onArchive: func(*testing.T, context.Context, client.Client, string) error {
				return errors.New("archive denied")
			},
			wantErr:         true,
			wantNewRevision: true,
		},
		{
			name:      "deleted",
			revisions: []archivalRevision{wantGone(), wantArchived(), wantActive(completed)},
			onArchive: func(t *testing.T, ctx context.Context, cl client.Client, name string) error {
				require.NoError(t, cl.Delete(ctx, &ocv1.ClusterObjectSet{ObjectMeta: metav1.ObjectMeta{Name: name}}))
				// Server-side apply of a UID-guarded object that no longer exists is
				// rejected as a Conflict rather than NotFound. The conflict is surfaced
				// and retried; the next reconcile no longer lists the revision.
				return apierrors.NewConflict(cosResource, name, errors.New("uid mismatch"))
			},
			wantErr:               true,
			wantProgressingReason: ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed,
		},
		{
			name:      "ownership conflict",
			revisions: []archivalRevision{wantActive(), wantArchived(), wantActive(completed)},
			onArchive: func(t *testing.T, ctx context.Context, cl client.Client, name string) error {
				changed, _ := getCOS(t, cl, name)
				foreignOwned(changed)
				require.NoError(t, cl.Update(ctx, changed))
				return nil
			},
			wantErr:               true,
			wantFirstOwnerUID:     "foreign-uid",
			wantProgressingReason: ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed,
		},
		{
			// The apply succeeds, but admission keeps the revision Active. The stored state,
			// not the requested one, must drive availability, and the failure must surface.
			name:      "archival not persisted",
			revisions: []archivalRevision{wantActive(ready), wantArchived(), wantActive(completed)},
			mutateArchive: func(cos *ocv1ac.ClusterObjectSetApplyConfiguration) {
				cos.Spec.WithLifecycleState(ocv1.ClusterObjectSetLifecycleStateActive)
			},
			wantErr:       true,
			wantAvailable: metav1.ConditionTrue,
		},
		{
			name:          "adoption failure",
			revisions:     []archivalRevision{wantArchived(ready), wantArchived(), wantActive(completed), wantActive(ownerless)},
			adoptionErr:   errors.New("adoption denied"),
			wantErr:       true,
			wantAvailable: metav1.ConditionFalse,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cod := codFixture()
			revs := make([]*ocv1.ClusterObjectSet, len(tc.revisions))
			objs := make([]client.Object, 0, 1+len(tc.revisions))
			objs = append(objs, cod)
			for i, rc := range tc.revisions {
				revs[i] = deploymentRevision(cod, int64(i+1), ocv1.ClusterObjectSetLifecycleStateActive)
				revs[i].UID = types.UID(fmt.Sprintf("revision-%d", i+1))
				for _, trait := range rc.traits {
					trait(revs[i])
				}
				objs = append(objs, revs[i])
			}
			if tc.deployment != nil {
				tc.deployment(cod)
			}
			base := newCODClient(t, objs...)
			c := interceptor.NewClient(base, interceptor.Funcs{
				Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if tc.adoptionErr != nil && len(revs) >= 4 && obj.GetName() == revs[3].Name {
						return tc.adoptionErr
					}
					return cl.Patch(ctx, obj, patch, opts...)
				},
				Apply: func(ctx context.Context, cl client.WithWatch, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
					if cos, ok := obj.(*ocv1ac.ClusterObjectSetApplyConfiguration); ok && *cos.GetName() == revs[0].Name {
						if tc.onArchive != nil {
							if err := tc.onArchive(t, ctx, cl, revs[0].Name); err != nil {
								return err
							}
						}
						if tc.mutateArchive != nil {
							tc.mutateArchive(cos)
						}
					}
					return cl.Apply(ctx, obj, opts...)
				},
			})

			_, err := reconcileCOD(t, c)

			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			for i, r := range revs {
				got, exists := getCOS(t, base, r.Name)
				if tc.revisions[i].want == "" {
					require.False(t, exists, r.Name)
					continue
				}
				require.True(t, exists, r.Name)
				require.Equal(t, tc.revisions[i].want, got.Spec.LifecycleState, r.Name)
			}
			if tc.wantFirstOwnerUID != "" {
				first, _ := getCOS(t, base, revs[0].Name)
				require.Equal(t, tc.wantFirstOwnerUID, first.OwnerReferences[0].UID)
			}
			if tc.wantNewRevision {
				created, exists := getCOS(t, base, fmt.Sprintf("%s-%d", cod.Name, len(revs)+1))
				require.True(t, exists, "archival failure must not prevent rollout creation")
				require.Equal(t, int64(len(revs)+1), created.Spec.Revision)
			}
			if tc.wantProgressingReason != "" {
				require.Equal(t, tc.wantProgressingReason, progressing(t, base).Reason)
			}
			if tc.wantAvailable != "" {
				require.Equal(t, tc.wantAvailable, available(t, base).Status, "archived ready predecessors must not establish availability")
			}
		})
	}
}

// TestCODArchivalPreconditions ensures the archival write is safe against concurrent
// changes, using a real API server (envtest) rather than the fake client.
//
// The controller decides what to archive from a cached view of the group, so a
// revision can change between being observed and being archived. The archival apply
// therefore carries the observed UID and resourceVersion as preconditions, and the API
// server must reject the write if either no longer matches. Each case lets the
// reconciler observe a snapshot (the cache), mutates the live revision just before the
// archival apply, and checks the outcome on the API server:
//   - unchanged: archival succeeds, including taking ownership of lifecycleState from
//     the manager that created the revision;
//   - owner changed: the stale resourceVersion is rejected as a Conflict, so a revision
//     handed to another owner is not archived;
//   - same-name replacement: given the replacement's resourceVersion, only the UID
//     guard stops archival of an object the controller never observed (Invalid,
//     because metadata.uid is immutable);
//   - deleted: server-side apply of a UID-guarded revision that no longer exists is
//     rejected as a Conflict instead of recreating it.
//
// This is the only coverage of the UID guard; the fake client cannot model it.
func TestCODArchivalPreconditions(t *testing.T) {
	for _, tc := range []struct {
		name string
		// beforeArchive, if set, runs against the API server just before the
		// controller applies archival to the older revision, which the controller
		// observed through a snapshot taken before the change.
		beforeArchive func(t *testing.T, ctx context.Context, api client.Client, older *ocv1.ClusterObjectSet, archival *ocv1ac.ClusterObjectSetApplyConfiguration)

		wantErr   func(error) bool
		wantState ocv1.ClusterObjectSetLifecycleState // "" means the revision must be gone
	}{
		{
			name:      "unchanged",
			wantState: ocv1.ClusterObjectSetLifecycleStateArchived,
		},
		{
			name: "owner changed",
			beforeArchive: func(t *testing.T, ctx context.Context, api client.Client, older *ocv1.ClusterObjectSet, _ *ocv1ac.ClusterObjectSetApplyConfiguration) {
				changed := older.DeepCopy()
				foreignOwned(changed)
				require.NoError(t, api.Update(ctx, changed))
			},
			wantErr:   apierrors.IsConflict,
			wantState: ocv1.ClusterObjectSetLifecycleStateActive,
		},
		{
			name: "same-name replacement",
			beforeArchive: func(t *testing.T, ctx context.Context, api client.Client, older *ocv1.ClusterObjectSet, archival *ocv1ac.ClusterObjectSetApplyConfiguration) {
				require.NoError(t, api.Delete(ctx, older))
				replacement := older.DeepCopy()
				replacement.UID = ""
				replacement.ResourceVersion = ""
				replacement.CreationTimestamp = metav1.Time{}
				require.NoError(t, api.Create(ctx, replacement))
				// Isolate UID protection by supplying the replacement's resourceVersion.
				archival.WithResourceVersion(replacement.ResourceVersion)
			},
			wantErr:   apierrors.IsInvalid,
			wantState: ocv1.ClusterObjectSetLifecycleStateActive,
		},
		{
			// Server-side apply must reject, not recreate, a UID-guarded deleted revision.
			name: "deleted",
			beforeArchive: func(t *testing.T, ctx context.Context, api client.Client, older *ocv1.ClusterObjectSet, _ *ocv1ac.ClusterObjectSetApplyConfiguration) {
				require.NoError(t, api.Delete(ctx, older))
			},
			wantErr: apierrors.IsConflict,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, err := client.New(config, client.Options{Scheme: newScheme(t)})
			require.NoError(t, err)
			cod := codFixture()
			cod.Name = "archive-preconditions-" + strings.ReplaceAll(tc.name, " ", "-")
			cod.UID = ""
			require.NoError(t, api.Create(t.Context(), cod))
			t.Cleanup(func() { require.NoError(t, client.IgnoreNotFound(api.Delete(context.Background(), cod))) })
			older := deploymentRevision(cod, 1, ocv1.ClusterObjectSetLifecycleStateActive)
			older.Name = cod.Name + "-1"
			older.Spec.Group = cod.Name
			head := deploymentRevision(cod, 2, ocv1.ClusterObjectSetLifecycleStateActive)
			head.Name = cod.Name + "-2"
			head.Spec.Group = cod.Name
			for _, r := range []*ocv1.ClusterObjectSet{older, head} {
				require.NoError(t, api.Create(t.Context(), r))
				t.Cleanup(func() { require.NoError(t, client.IgnoreNotFound(api.Delete(context.Background(), r))) })
			}
			head.Status.CompletedAt = metav1.Now()
			require.NoError(t, api.Status().Update(t.Context(), head))
			snapshot := newCODClient(t, cod.DeepCopy(), older.DeepCopy(), head.DeepCopy())
			c := interceptor.NewClient(snapshot, interceptor.Funcs{Apply: func(ctx context.Context, _ client.WithWatch, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
				archival, ok := obj.(*ocv1ac.ClusterObjectSetApplyConfiguration)
				require.True(t, ok)
				require.Equal(t, older.Name, *archival.GetName())
				if tc.beforeArchive != nil {
					tc.beforeArchive(t, ctx, api, older, archival)
				}
				return api.Apply(ctx, obj, opts...)
			}})

			_, err = (&controllers.ClusterObjectDeploymentReconciler{Client: c, Scheme: newScheme(t)}).Reconcile(t.Context(), ctrl.Request{NamespacedName: types.NamespacedName{Name: cod.Name}})

			if tc.wantErr != nil {
				require.True(t, tc.wantErr(err), "expected an API-server precondition rejection, got %v", err)
			} else {
				require.NoError(t, err)
			}
			got := &ocv1.ClusterObjectSet{}
			getErr := api.Get(t.Context(), client.ObjectKeyFromObject(older), got)
			if tc.wantState == "" {
				require.True(t, apierrors.IsNotFound(getErr), "archival must not recreate a deleted revision, got %v", getErr)
				return
			}
			require.NoError(t, getErr)
			require.Equal(t, tc.wantState, got.Spec.LifecycleState)
		})
	}
}
