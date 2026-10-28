//go:build !standard

package controllers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/object-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
	hashutil "github.com/operator-framework/operator-controller/internal/shared/util/hash"
	"github.com/operator-framework/operator-controller/internal/testutil"
)

const clusterObjectDeploymentName = "test-cod-1"

func codFixture() *ocv1.ClusterObjectDeployment {
	return &ocv1.ClusterObjectDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: clusterObjectDeploymentName, UID: "test-cod-uid"},
		Spec: ocv1.ClusterObjectDeploymentSpec{
			Template: ocv1.ClusterObjectDeploymentTemplate{
				Spec: ocv1.ClusterObjectDeploymentTemplateSpec{
					CollisionProtection:     ocv1.CollisionProtectionPrevent,
					ProgressDeadlineMinutes: 15,
					Phases: []ocv1.ClusterObjectSetPhase{{
						Name: "resources",
						Objects: []ocv1.ClusterObjectSetObject{{
							Object: unstructured.Unstructured{Object: map[string]interface{}{
								"apiVersion": "v1",
								"kind":       "ConfigMap",
								"metadata":   map[string]interface{}{"name": "cm", "namespace": "ns"},
							}},
						}},
					}},
				},
			},
		},
	}
}

func newCODClient(t *testing.T, objs ...client.Object) client.WithWatch {
	t.Helper()
	return testutil.WithIndexes(t, fake.NewClientBuilder().WithScheme(newScheme(t)), controllers.SetupIndexes).
		WithStatusSubresource(&ocv1.ClusterObjectDeployment{}, &ocv1.ClusterObjectSet{}).
		WithObjects(objs...).
		Build()
}

func reconcileCOD(t *testing.T, c client.Client) (ctrl.Result, error) {
	t.Helper()
	return (&controllers.ClusterObjectDeploymentReconciler{Client: c, Scheme: newScheme(t)}).
		Reconcile(t.Context(), ctrl.Request{NamespacedName: types.NamespacedName{Name: clusterObjectDeploymentName}})
}

func getCOS(t *testing.T, c client.Client, name string) (*ocv1.ClusterObjectSet, bool) {
	t.Helper()
	cos := &ocv1.ClusterObjectSet{}
	err := c.Get(t.Context(), types.NamespacedName{Name: name}, cos)
	if apierrors.IsNotFound(err) {
		return nil, false
	}
	require.NoError(t, err)
	return cos, true
}

func progressing(t *testing.T, c client.Client) *metav1.Condition {
	t.Helper()
	cod := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: clusterObjectDeploymentName}, cod))
	return apimeta.FindStatusCondition(cod.Status.Conditions, ocv1.ClusterObjectDeploymentTypeProgressing)
}

func available(t *testing.T, c client.Client) *metav1.Condition {
	t.Helper()
	cod := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: clusterObjectDeploymentName}, cod))
	return apimeta.FindStatusCondition(cod.Status.Conditions, ocv1.ClusterObjectDeploymentTypeAvailable)
}

func markCOSReady(t *testing.T, c client.Client, name string, ready bool) {
	t.Helper()
	cos, ok := getCOS(t, c, name)
	require.True(t, ok)
	status, reason := metav1.ConditionFalse, ocv1.ClusterObjectSetReasonObjectsNotReady
	if ready {
		status, reason = metav1.ConditionTrue, ocv1.ClusterObjectSetReasonAllObjectsReady
	}
	apimeta.SetStatusCondition(&cos.Status.Conditions, metav1.Condition{
		Type:    ocv1.ClusterObjectSetTypeReady,
		Status:  status,
		Reason:  reason,
		Message: "test",
	})
	require.NoError(t, c.Status().Update(t.Context(), cos))
}

// expireRevisionDeadline moves the revision's creation timestamp before the
// deployment's deadline without changing the revision's readiness condition.
func expireRevisionDeadline(t *testing.T, c client.Client, name string) {
	t.Helper()
	cos, ok := getCOS(t, c, name)
	require.True(t, ok)
	cos.CreationTimestamp = metav1.NewTime(time.Now().Add(-time.Hour))
	require.NoError(t, c.Update(t.Context(), cos))
}

// revisionReadyProbeMessage is a distinctive message written onto a revision's
// Ready condition so tests can assert the COD surfaces its own generic message
// rather than echoing the revision's message.
const revisionReadyProbeMessage = "COS-ONLY-REVISION-READY-MESSAGE"

// setRevisionReadyReason sets the named revision's Ready condition to False with
// the given reason and a distinctive message (revisionReadyProbeMessage).
func setRevisionReadyReason(t *testing.T, c client.Client, name, reason string) {
	t.Helper()
	cos, ok := getCOS(t, c, name)
	require.True(t, ok)
	apimeta.SetStatusCondition(&cos.Status.Conditions, metav1.Condition{
		Type:    ocv1.ClusterObjectSetTypeReady,
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: revisionReadyProbeMessage,
	})
	require.NoError(t, c.Status().Update(t.Context(), cos))
}

func Test_ClusterObjectDeploymentReconciler_Available(t *testing.T) {
	t.Run("no active revision before the first revision exists", func(t *testing.T) {
		c := newCODClient(t, codFixture())
		_, err := reconcileCOD(t, c)
		require.NoError(t, err)

		cond := available(t, c)
		require.NotNil(t, cond)
		require.Equal(t, metav1.ConditionFalse, cond.Status)
		require.Equal(t, ocv1.ClusterObjectDeploymentReasonNoActiveRevision, cond.Reason)
	})

	t.Run("unavailable when the active revision is not Ready", func(t *testing.T) {
		c := newCODClient(t, codFixture())
		_, err := reconcileCOD(t, c) // creates revision 1
		require.NoError(t, err)
		_, err = reconcileCOD(t, c) // revision 1 is now listed but not Ready
		require.NoError(t, err)

		cond := available(t, c)
		require.NotNil(t, cond)
		require.Equal(t, metav1.ConditionFalse, cond.Status)
		require.Equal(t, ocv1.ClusterObjectDeploymentReasonUnavailable, cond.Reason)
	})

	t.Run("available when an active revision is Ready", func(t *testing.T) {
		c := newCODClient(t, codFixture())
		_, err := reconcileCOD(t, c) // creates revision 1
		require.NoError(t, err)
		markCOSReady(t, c, clusterObjectDeploymentName+"-1", true)
		_, err = reconcileCOD(t, c)
		require.NoError(t, err)

		cond := available(t, c)
		require.NotNil(t, cond)
		require.Equal(t, metav1.ConditionTrue, cond.Status)
		require.Equal(t, ocv1.ClusterObjectDeploymentReasonAvailable, cond.Reason)
	})

	t.Run("stays available while a new revision rolls out", func(t *testing.T) {
		c := newCODClient(t, codFixture())
		_, err := reconcileCOD(t, c) // creates revision 1
		require.NoError(t, err)
		markCOSReady(t, c, clusterObjectDeploymentName+"-1", true)
		_, err = reconcileCOD(t, c)
		require.NoError(t, err)
		require.Equal(t, metav1.ConditionTrue, available(t, c).Status)

		// Change the template so a new revision is created while revision 1 stays Ready.
		cod := &ocv1.ClusterObjectDeployment{}
		require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: clusterObjectDeploymentName}, cod))
		cod.Spec.Template.Spec.Phases[0].Objects[0].Object.Object["data"] = map[string]interface{}{"k": "v"}
		require.NoError(t, c.Update(t.Context(), cod))

		_, err = reconcileCOD(t, c) // creates revision 2; revision 1 is still active and Ready
		require.NoError(t, err)

		cond := available(t, c)
		require.NotNil(t, cond)
		require.Equal(t, metav1.ConditionTrue, cond.Status, "deployment must stay available while a prior revision is still Ready")
	})

	t.Run("stays available when a new revision exceeds its progress deadline but a prior revision is Ready", func(t *testing.T) {
		c := newCODClient(t, codFixture())
		_, err := reconcileCOD(t, c) // creates revision 1
		require.NoError(t, err)
		markCOSReady(t, c, clusterObjectDeploymentName+"-1", true)
		_, err = reconcileCOD(t, c)
		require.NoError(t, err)
		require.Equal(t, metav1.ConditionTrue, available(t, c).Status)

		// Change the template so a new revision is created while revision 1 stays Ready.
		cod := &ocv1.ClusterObjectDeployment{}
		require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: clusterObjectDeploymentName}, cod))
		cod.Spec.Template.Spec.Phases[0].Objects[0].Object.Object["data"] = map[string]interface{}{"k": "v"}
		require.NoError(t, c.Update(t.Context(), cod))

		_, err = reconcileCOD(t, c) // creates revision 2
		require.NoError(t, err)

		// Revision 2 blows its deadline while revision 1 is still Active and Ready.
		expireRevisionDeadline(t, c, clusterObjectDeploymentName+"-2")
		_, err = reconcileCOD(t, c)
		require.NoError(t, err)

		require.Equal(t, metav1.ConditionTrue, available(t, c).Status,
			"deployment must stay available while a prior revision is still Ready, even if the new revision exceeds its deadline")

		prog := progressing(t, c)
		require.NotNil(t, prog)
		require.Equal(t, metav1.ConditionFalse, prog.Status)
		require.Equal(t, ocv1.ClusterObjectDeploymentReasonProgressDeadlineExceeded, prog.Reason,
			"Progressing must surface the deadline breach of the latest revision")
	})
}

func Test_ClusterObjectDeploymentReconciler_CreatesFirstRevision(t *testing.T) {
	c := newCODClient(t, codFixture())

	_, err := reconcileCOD(t, c)
	require.NoError(t, err)

	cos, ok := getCOS(t, c, clusterObjectDeploymentName+"-1")
	require.True(t, ok, "revision 1 should be created")
	require.Equal(t, clusterObjectDeploymentName, cos.Spec.Group)
	require.Equal(t, int64(1), cos.Spec.Revision)
	require.Equal(t, ocv1.ClusterObjectSetLifecycleStateActive, cos.Spec.LifecycleState)
	require.Equal(t, ocv1.CollisionProtectionPrevent, cos.Spec.CollisionProtection)
	require.Zero(t, cos.Spec.ProgressDeadlineMinutes, "the deployment enforces its own progress deadline")
	require.NotEmpty(t, cos.Labels[labels.TemplateHashKey])

	ref := metav1.GetControllerOf(cos)
	require.NotNil(t, ref)
	require.Equal(t, ocv1.ClusterObjectDeploymentKind, ref.Kind)
	require.Equal(t, clusterObjectDeploymentName, ref.Name)

	cond := progressing(t, c)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing, cond.Reason)
}

func Test_ClusterObjectDeploymentReconciler_PropagatesTemplateMetadata(t *testing.T) {
	cod := codFixture()
	cod.Spec.Template.Metadata = ocv1.ClusterObjectDeploymentTemplateMetadata{
		Labels:      map[string]string{"team": "platform"},
		Annotations: map[string]string{"note": "hello"},
	}
	c := newCODClient(t, cod)

	_, err := reconcileCOD(t, c)
	require.NoError(t, err)

	cos, ok := getCOS(t, c, clusterObjectDeploymentName+"-1")
	require.True(t, ok, "revision 1 should be created")
	require.Equal(t, "platform", cos.Labels["team"], "template.metadata.labels should propagate to the revision")
	require.Equal(t, "hello", cos.Annotations["note"], "template.metadata.annotations should propagate to the revision")
	require.NotEmpty(t, cos.Labels[labels.TemplateHashKey], "controller-managed template-hash label should still be set")
}

func Test_ClusterObjectDeploymentReconciler_NoTemplateMetadata(t *testing.T) {
	// codFixture sets no template.metadata, so the revision must carry only the
	// controller-managed template-hash label and no annotations.
	c := newCODClient(t, codFixture())

	_, err := reconcileCOD(t, c)
	require.NoError(t, err)

	cos, ok := getCOS(t, c, clusterObjectDeploymentName+"-1")
	require.True(t, ok, "revision 1 should be created")
	require.Len(t, cos.Labels, 1, "only the controller-managed template-hash label should be set")
	require.NotEmpty(t, cos.Labels[labels.TemplateHashKey])
	require.Empty(t, cos.Annotations, "no annotations should be set when template.metadata has none")
}

func Test_ClusterObjectDeploymentReconciler_InvalidLabelMetadataSetsInvalidSpecification(t *testing.T) {
	cod := codFixture()
	cod.Spec.Template.Metadata = ocv1.ClusterObjectDeploymentTemplateMetadata{
		Labels: map[string]string{"not a valid key": "v"},
	}
	c := newCODClient(t, cod)

	_, err := reconcileCOD(t, c)
	require.NoError(t, err)

	cond := progressing(t, c)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, ocv1.ClusterObjectDeploymentReasonInvalidSpecification, cond.Reason)
	require.NotEmpty(t, cond.Message)

	_, ok := getCOS(t, c, clusterObjectDeploymentName+"-1")
	require.False(t, ok, "no revision should be created while the template metadata is invalid")
}

func Test_ClusterObjectDeploymentReconciler_InvalidAnnotationMetadataSetsInvalidSpecification(t *testing.T) {
	cod := codFixture()
	cod.Spec.Template.Metadata = ocv1.ClusterObjectDeploymentTemplateMetadata{
		Annotations: map[string]string{"not a valid key": "v"},
	}
	c := newCODClient(t, cod)

	_, err := reconcileCOD(t, c)
	require.NoError(t, err)

	cond := progressing(t, c)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, ocv1.ClusterObjectDeploymentReasonInvalidSpecification, cond.Reason)

	_, ok := getCOS(t, c, clusterObjectDeploymentName+"-1")
	require.False(t, ok, "no revision should be created while the template metadata is invalid")
}

func Test_ClusterObjectDeploymentReconciler_OversizedAnnotationsSetInvalidSpecification(t *testing.T) {
	// Each value is individually under the 256 KiB limit, but their combined size
	// (keys + values) exceeds the Kubernetes total annotation limit, which only the
	// aggregate check catches.
	big := strings.Repeat("a", 150*1024)
	cod := codFixture()
	cod.Spec.Template.Metadata = ocv1.ClusterObjectDeploymentTemplateMetadata{
		Annotations: map[string]string{"a": big, "b": big},
	}
	c := newCODClient(t, cod)

	_, err := reconcileCOD(t, c)
	require.NoError(t, err)

	cond := progressing(t, c)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, ocv1.ClusterObjectDeploymentReasonInvalidSpecification, cond.Reason)

	_, ok := getCOS(t, c, clusterObjectDeploymentName+"-1")
	require.False(t, ok, "no revision should be created when annotations exceed the total size limit")
}

func Test_ClusterObjectDeploymentReconciler_TemplateHashLabelOverridesCaller(t *testing.T) {
	cod := codFixture()
	cod.Spec.Template.Metadata = ocv1.ClusterObjectDeploymentTemplateMetadata{
		Labels: map[string]string{labels.TemplateHashKey: "caller-wins-NOT"},
	}
	c := newCODClient(t, cod)

	_, err := reconcileCOD(t, c)
	require.NoError(t, err)

	cos, ok := getCOS(t, c, clusterObjectDeploymentName+"-1")
	require.True(t, ok, "revision 1 should be created")
	require.Equal(t, hashutil.DeepHashObject(cod.Spec.Template), cos.Labels[labels.TemplateHashKey],
		"controller-managed template-hash must win over a caller-supplied value")
	require.NotEqual(t, "caller-wins-NOT", cos.Labels[labels.TemplateHashKey])
}

func Test_TemplateHash_MetadataParticipates(t *testing.T) {
	base := codFixture().Spec.Template

	withLabelA := base
	withLabelA.Metadata = ocv1.ClusterObjectDeploymentTemplateMetadata{Labels: map[string]string{"app": "a"}}
	withLabelB := base
	withLabelB.Metadata = ocv1.ClusterObjectDeploymentTemplateMetadata{Labels: map[string]string{"app": "b"}}
	require.NotEqual(t, hashutil.DeepHashObject(withLabelA), hashutil.DeepHashObject(withLabelB),
		"metadata must participate in the template hash")

	// No-churn guarantee: a template that sets no metadata must serialize without a
	// "metadata" key, so its hash is identical to the pre-field serialization and
	// existing CODs are not re-rolled out on upgrade. This asserts the omitzero
	// mechanism directly; it fails if omitzero is ever dropped from the field.
	raw, err := json.Marshal(base)
	require.NoError(t, err)
	var top map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &top))
	_, hasMetadata := top["metadata"]
	require.False(t, hasMetadata,
		"zero-value template metadata must be omitted (omitzero) so metadata-less CODs keep the same hash")
}

func Test_ClusterObjectDeploymentReconciler_NoNewRevisionWhenTemplateUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name string
		prep func(t *testing.T, c client.Client, cosName string)

		wantStatus metav1.ConditionStatus
		wantReason string
		// wantMsgContains, when set, must be a substring of the COD Progressing
		// message; wantMsgExcludes, when set, must not be.
		wantMsgContains string
		wantMsgExcludes string
	}{
		{
			name: "completed latest revision reports Progressed",
			prep: func(t *testing.T, c client.Client, cosName string) {
				cos, ok := getCOS(t, c, cosName)
				require.True(t, ok)
				cos.Status.CompletedAt = metav1.Now()
				require.NoError(t, c.Status().Update(t.Context(), cos))
			},
			wantStatus: metav1.ConditionFalse,
			wantReason: ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed,
		},
		{
			name:       "not-yet-completed latest revision reports Progressing",
			prep:       nil,
			wantStatus: metav1.ConditionTrue,
			wantReason: ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing,
		},
		{
			name:       "deadline-exceeded latest revision reports ProgressDeadlineExceeded",
			prep:       func(t *testing.T, c client.Client, cosName string) { expireRevisionDeadline(t, c, cosName) },
			wantStatus: metav1.ConditionFalse,
			wantReason: ocv1.ClusterObjectDeploymentReasonProgressDeadlineExceeded,
		},
		{
			name: "head revision ObjectCollision reports RevisionObjectCollision and is not progressing",
			prep: func(t *testing.T, c client.Client, cosName string) {
				setRevisionReadyReason(t, c, cosName, ocv1.ClusterObjectSetReasonObjectCollision)
			},
			wantStatus:      metav1.ConditionFalse,
			wantReason:      ocv1.ClusterObjectDeploymentReasonRevisionObjectCollision,
			wantMsgContains: clusterObjectDeploymentName + "-1",
			wantMsgExcludes: revisionReadyProbeMessage,
		},
		{
			name: "head revision InvalidSpecification reports RevisionInvalidSpecification and is not progressing",
			prep: func(t *testing.T, c client.Client, cosName string) {
				setRevisionReadyReason(t, c, cosName, ocv1.ClusterObjectSetReasonInvalidSpecification)
			},
			wantStatus:      metav1.ConditionFalse,
			wantReason:      ocv1.ClusterObjectDeploymentReasonRevisionInvalidSpecification,
			wantMsgContains: clusterObjectDeploymentName + "-1",
			wantMsgExcludes: revisionReadyProbeMessage,
		},
		{
			name: "head revision PreconditionsNotMet reports RevisionPreconditionsNotMet and keeps progressing",
			prep: func(t *testing.T, c client.Client, cosName string) {
				setRevisionReadyReason(t, c, cosName, ocv1.ClusterObjectSetReasonPreconditionsNotMet)
			},
			wantStatus:      metav1.ConditionTrue,
			wantReason:      ocv1.ClusterObjectDeploymentReasonRevisionPreconditionsNotMet,
			wantMsgContains: clusterObjectDeploymentName + "-1",
			wantMsgExcludes: revisionReadyProbeMessage,
		},
		{
			name:       "head revision with no Ready condition falls through to NewClusterObjectSetProgressing",
			prep:       nil,
			wantStatus: metav1.ConditionTrue,
			wantReason: ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing,
		},
		{
			name: "head revision with unrecognized Ready reason falls through to NewClusterObjectSetProgressing",
			prep: func(t *testing.T, c client.Client, cosName string) {
				setRevisionReadyReason(t, c, cosName, ocv1.ClusterObjectSetReasonObjectsNotReady)
			},
			wantStatus: metav1.ConditionTrue,
			wantReason: ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing,
		},
		{
			name: "completed revision that also reports ObjectCollision still reports Progressed",
			prep: func(t *testing.T, c client.Client, cosName string) {
				setRevisionReadyReason(t, c, cosName, ocv1.ClusterObjectSetReasonObjectCollision)
				cos, ok := getCOS(t, c, cosName)
				require.True(t, ok)
				cos.Status.CompletedAt = metav1.Now()
				require.NoError(t, c.Status().Update(t.Context(), cos))
			},
			wantStatus: metav1.ConditionFalse,
			wantReason: ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newCODClient(t, codFixture())

			// First reconcile creates revision 1 with the correct template-hash label.
			_, err := reconcileCOD(t, c)
			require.NoError(t, err)

			if tc.prep != nil {
				tc.prep(t, c, clusterObjectDeploymentName+"-1")
			}

			// Second reconcile must not create a new revision.
			_, err = reconcileCOD(t, c)
			require.NoError(t, err)

			_, ok := getCOS(t, c, clusterObjectDeploymentName+"-2")
			require.False(t, ok, "no second revision should be created when the template is unchanged")

			cond := progressing(t, c)
			require.NotNil(t, cond)
			require.Equal(t, tc.wantStatus, cond.Status)
			require.Equal(t, tc.wantReason, cond.Reason)
			if tc.wantMsgContains != "" {
				require.Contains(t, cond.Message, tc.wantMsgContains)
			}
			if tc.wantMsgExcludes != "" {
				require.NotContains(t, cond.Message, tc.wantMsgExcludes)
			}
		})
	}
}

func Test_ClusterObjectDeploymentReconciler_CreatesNewRevisionOnTemplateChange(t *testing.T) {
	c := newCODClient(t, codFixture())

	_, err := reconcileCOD(t, c)
	require.NoError(t, err)

	// Change the template.
	cod := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: clusterObjectDeploymentName}, cod))
	cod.Spec.Template.Spec.Phases[0].Objects[0].Object.Object["data"] = map[string]interface{}{"k": "v"}
	require.NoError(t, c.Update(t.Context(), cod))

	_, err = reconcileCOD(t, c)
	require.NoError(t, err)

	cos, ok := getCOS(t, c, clusterObjectDeploymentName+"-2")
	require.True(t, ok, "a template change should create revision 2")
	require.Equal(t, int64(2), cos.Spec.Revision)

	cond := progressing(t, c)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
}

func Test_ClusterObjectDeploymentReconciler_CreatesNewRevisionWhenLatestArchived(t *testing.T) {
	c := newCODClient(t, codFixture())

	_, err := reconcileCOD(t, c)
	require.NoError(t, err)

	// Archive revision 1.
	cos, ok := getCOS(t, c, clusterObjectDeploymentName+"-1")
	require.True(t, ok)
	cos.Spec.LifecycleState = ocv1.ClusterObjectSetLifecycleStateArchived
	require.NoError(t, c.Update(t.Context(), cos))

	_, err = reconcileCOD(t, c)
	require.NoError(t, err)

	rev2, ok := getCOS(t, c, clusterObjectDeploymentName+"-2")
	require.True(t, ok, "archiving the only revision should trigger a new revision")
	require.Equal(t, int64(2), rev2.Spec.Revision)
}

// revisionCOS builds a ClusterObjectSet revision in the test deployment's group
// with the given revision number and lifecycle state, for garbage-collection tests.
// An archived revision is given Ready=False/Archived, mirroring a revision whose
// teardown has completed — the state in which garbage collection may prune it.
func revisionCOS(revision int64, lifecycle ocv1.ClusterObjectSetLifecycleState) *ocv1.ClusterObjectSet {
	cos := &ocv1.ClusterObjectSet{
		ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("%s-%d", clusterObjectDeploymentName, revision)},
		Spec: ocv1.ClusterObjectSetSpec{
			Group:               clusterObjectDeploymentName,
			Revision:            revision,
			CollisionProtection: ocv1.CollisionProtectionPrevent,
			LifecycleState:      lifecycle,
		},
	}
	if lifecycle == ocv1.ClusterObjectSetLifecycleStateArchived {
		setCOSReady(cos, metav1.ConditionFalse, ocv1.ClusterObjectSetReasonArchived)
	}
	return cos
}

// setCOSReady sets the revision's Ready condition to the given status and reason.
func setCOSReady(cos *ocv1.ClusterObjectSet, status metav1.ConditionStatus, reason string) {
	apimeta.SetStatusCondition(&cos.Status.Conditions, metav1.Condition{
		Type:    ocv1.ClusterObjectSetTypeReady,
		Status:  status,
		Reason:  reason,
		Message: "test",
	})
}

func Test_ClusterObjectDeploymentReconciler_GarbageCollectsArchivedRevisions(t *testing.T) {
	const active = ocv1.ClusterObjectSetLifecycleStateActive
	const archived = ocv1.ClusterObjectSetLifecycleStateArchived

	type rev struct {
		num   int64
		state ocv1.ClusterObjectSetLifecycleState
		// tearingDown marks an archived revision whose teardown has not yet
		// completed (Ready=False/Archiving). Such a revision must not be pruned, so
		// garbage collection does not short-circuit ordered reverse-phase cleanup.
		tearingDown bool
	}

	for _, tc := range []struct {
		name      string
		limit     *int32
		revisions []rev
		wantGone  []int64
		wantKept  []int64
	}{
		{
			name:  "retains the newest archived revisions up to the limit",
			limit: ptr.To(int32(2)),
			revisions: []rev{
				{num: 1, state: archived}, {num: 2, state: archived}, {num: 3, state: archived}, {num: 4, state: active},
			},
			wantGone: []int64{1},
			wantKept: []int64{2, 3, 4},
		},
		{
			name:  "a limit of zero deletes all archived revisions",
			limit: ptr.To(int32(0)),
			revisions: []rev{
				{num: 1, state: archived}, {num: 2, state: archived}, {num: 3, state: active},
			},
			wantGone: []int64{1, 2},
			wantKept: []int64{3},
		},
		{
			name:  "active revisions are never deleted even below the limit",
			limit: ptr.To(int32(0)),
			revisions: []rev{
				{num: 1, state: active}, {num: 2, state: active}, {num: 3, state: active},
			},
			wantGone: nil,
			wantKept: []int64{1, 2, 3},
		},
		{
			name:  "a nil limit defaults to retaining five archived revisions",
			limit: nil,
			revisions: []rev{
				{num: 1, state: archived}, {num: 2, state: archived}, {num: 3, state: archived},
				{num: 4, state: archived}, {num: 5, state: archived}, {num: 6, state: archived},
			},
			wantGone: []int64{1},
			wantKept: []int64{2, 3, 4, 5, 6},
		},
		{
			name:  "an archived revision still tearing down is not pruned",
			limit: ptr.To(int32(0)),
			revisions: []rev{
				{num: 1, state: archived, tearingDown: true},
				{num: 2, state: archived},
				{num: 3, state: active},
			},
			// Revision 1 is still tearing down (Ready=False/Archiving), so it is
			// excluded from pruning until its controller completes teardown;
			// revision 2 has finished tearing down and is pruned.
			wantGone: []int64{2},
			wantKept: []int64{1, 3},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cod := codFixture()
			cod.Spec.RevisionHistoryLimit = tc.limit

			objs := make([]client.Object, 0, 1+len(tc.revisions))
			objs = append(objs, cod)
			for _, r := range tc.revisions {
				cos := revisionCOS(r.num, r.state)
				if r.tearingDown {
					setCOSReady(cos, metav1.ConditionFalse, ocv1.ClusterObjectSetReasonArchiving)
				}
				objs = append(objs, cos)
			}
			c := newCODClient(t, objs...)

			_, err := reconcileCOD(t, c)
			require.NoError(t, err)

			for _, n := range tc.wantGone {
				_, ok := getCOS(t, c, fmt.Sprintf("%s-%d", clusterObjectDeploymentName, n))
				require.Falsef(t, ok, "archived revision %d beyond the limit should be garbage collected", n)
			}
			for _, n := range tc.wantKept {
				_, ok := getCOS(t, c, fmt.Sprintf("%s-%d", clusterObjectDeploymentName, n))
				require.Truef(t, ok, "revision %d within the limit should be retained", n)
			}
		})
	}
}

func Test_ClusterObjectDeploymentReconciler_GarbageCollectContinuesAfterDeleteError(t *testing.T) {
	cod := codFixture()
	cod.Spec.RevisionHistoryLimit = ptr.To(int32(0)) // every archived revision is excess

	failRev := clusterObjectDeploymentName + "-1"

	base := testutil.WithIndexes(t, fake.NewClientBuilder().WithScheme(newScheme(t)), controllers.SetupIndexes).
		WithStatusSubresource(&ocv1.ClusterObjectDeployment{}, &ocv1.ClusterObjectSet{}).
		WithObjects(
			cod,
			revisionCOS(1, ocv1.ClusterObjectSetLifecycleStateArchived),
			revisionCOS(2, ocv1.ClusterObjectSetLifecycleStateArchived),
			revisionCOS(3, ocv1.ClusterObjectSetLifecycleStateArchived),
		).
		Build()

	// Fail the delete of the oldest excess revision; the rest must still be attempted.
	c := interceptor.NewClient(base, interceptor.Funcs{
		Delete: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if obj.GetName() == failRev {
				return apierrors.NewInternalError(fmt.Errorf("boom"))
			}
			return cl.Delete(ctx, obj, opts...)
		},
	})

	_, err := (&controllers.ClusterObjectDeploymentReconciler{Client: c, Scheme: newScheme(t)}).
		Reconcile(t.Context(), ctrl.Request{NamespacedName: types.NamespacedName{Name: clusterObjectDeploymentName}})
	require.Error(t, err, "a delete failure during garbage collection must surface as a reconcile error")

	_, ok := getCOS(t, c, failRev)
	require.True(t, ok, "the revision whose delete failed should remain")
	_, ok = getCOS(t, c, clusterObjectDeploymentName+"-2")
	require.False(t, ok, "later excess revisions must still be deleted despite an earlier delete failure")
	_, ok = getCOS(t, c, clusterObjectDeploymentName+"-3")
	require.False(t, ok, "later excess revisions must still be deleted despite an earlier delete failure")
}

// Test_ClusterObjectDeploymentReconciler_RolloutProceedsDespiteGarbageCollectError
// verifies that a persistently failing archived-revision prune does not block the
// rollout: a new revision is still created, status is still reconciled, and the
// cleanup error is surfaced so the reconcile is retried.
func Test_ClusterObjectDeploymentReconciler_RolloutProceedsDespiteGarbageCollectError(t *testing.T) {
	cod := codFixture()
	cod.Spec.RevisionHistoryLimit = ptr.To(int32(0)) // the archived revision is excess

	failRev := clusterObjectDeploymentName + "-1"

	base := testutil.WithIndexes(t, fake.NewClientBuilder().WithScheme(newScheme(t)), controllers.SetupIndexes).
		WithStatusSubresource(&ocv1.ClusterObjectDeployment{}, &ocv1.ClusterObjectSet{}).
		WithObjects(
			cod,
			// An archived revision whose prune is denied, and no active revision, so
			// the rollout must create a new one.
			revisionCOS(1, ocv1.ClusterObjectSetLifecycleStateArchived),
		).
		Build()

	c := interceptor.NewClient(base, interceptor.Funcs{
		Delete: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if obj.GetName() == failRev {
				return apierrors.NewInternalError(fmt.Errorf("boom"))
			}
			return cl.Delete(ctx, obj, opts...)
		},
	})

	_, err := (&controllers.ClusterObjectDeploymentReconciler{Client: c, Scheme: newScheme(t)}).
		Reconcile(t.Context(), ctrl.Request{NamespacedName: types.NamespacedName{Name: clusterObjectDeploymentName}})
	require.Error(t, err, "the garbage-collection failure must still be surfaced")

	_, ok := getCOS(t, c, failRev)
	require.True(t, ok, "the archived revision whose delete failed should remain")
	rev2, ok := getCOS(t, c, clusterObjectDeploymentName+"-2")
	require.True(t, ok, "the rollout must create a new revision despite the garbage-collection failure")
	require.Equal(t, int64(2), rev2.Spec.Revision)

	require.NotNil(t, available(t, c), "the Available condition must be reconciled despite the garbage-collection failure")
	require.NotNil(t, progressing(t, c), "the Progressing condition must be reconciled despite the garbage-collection failure")
}

func Test_ClusterObjectDeploymentReconciler_DeletionIsNoop(t *testing.T) {
	cod := codFixture()
	now := metav1.Now()
	cod.DeletionTimestamp = &now
	cod.Finalizers = []string{"test.only/keep"}

	c := newCODClient(t, cod)

	_, err := reconcileCOD(t, c)
	require.NoError(t, err)

	_, ok := getCOS(t, c, clusterObjectDeploymentName+"-1")
	require.False(t, ok, "a deleting ClusterObjectDeployment must not create revisions")

	require.Nil(t, progressing(t, c))
}

func Test_ClusterObjectDeploymentReconciler_SurfacesAlreadyExists(t *testing.T) {
	base := testutil.WithIndexes(t, fake.NewClientBuilder().WithScheme(newScheme(t)), controllers.SetupIndexes).
		WithStatusSubresource(&ocv1.ClusterObjectDeployment{}, &ocv1.ClusterObjectSet{}).
		WithObjects(codFixture()).
		Build()

	c := interceptor.NewClient(base, interceptor.Funcs{
		Create: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.CreateOption) error {
			gr := ocv1.GroupVersion.WithResource("clusterobjectsets").GroupResource()
			return apierrors.NewAlreadyExists(gr, obj.GetName())
		},
	})

	_, err := (&controllers.ClusterObjectDeploymentReconciler{Client: c, Scheme: newScheme(t)}).
		Reconcile(t.Context(), ctrl.Request{NamespacedName: types.NamespacedName{Name: clusterObjectDeploymentName}})
	require.Error(t, err, "an AlreadyExists on create must surface as a reconcile error, not be silently tolerated")
}

func Test_ClusterObjectDeploymentReconciler_RevisionBounds(t *testing.T) {
	for _, tc := range []struct {
		name           string
		latest         int64
		lifecycleState ocv1.ClusterObjectSetLifecycleState
		changed        bool
		wantErr        bool
		wantCreates    int
		foreign        bool
	}{
		{
			name:           "allocates final revision after archived foreign-owned history",
			latest:         ocv1.ClusterObjectSetMaxRevision - 1,
			lifecycleState: ocv1.ClusterObjectSetLifecycleStateArchived,
			changed:        false,
			wantErr:        false,
			wantCreates:    1,
			foreign:        true,
		},
		{
			name:           "changed template at maximum creates nothing",
			latest:         ocv1.ClusterObjectSetMaxRevision,
			lifecycleState: ocv1.ClusterObjectSetLifecycleStateActive,
			changed:        true,
			wantErr:        true,
			wantCreates:    0,
		},
		{
			name:           "foreign active maximum reports owner conflict instead of revision exhaustion",
			latest:         ocv1.ClusterObjectSetMaxRevision,
			lifecycleState: ocv1.ClusterObjectSetLifecycleStateActive,
			changed:        true,
			foreign:        true,
		},
		{
			name:           "stored maximum int64 cannot overflow",
			latest:         math.MaxInt64,
			lifecycleState: ocv1.ClusterObjectSetLifecycleStateArchived,
			changed:        false,
			wantErr:        true,
			wantCreates:    0,
		},
		{
			name:           "unchanged template at maximum still reconciles",
			latest:         ocv1.ClusterObjectSetMaxRevision,
			lifecycleState: ocv1.ClusterObjectSetLifecycleStateActive,
			changed:        false,
			wantErr:        false,
			wantCreates:    0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cod := codFixture()
			owner := cod.DeepCopy()
			if tc.foreign {
				owner.Name = "other-deployment"
				owner.UID = "other-uid"
			}
			latest := &ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-%d", cod.Name, tc.latest),
					Labels: map[string]string{
						labels.TemplateHashKey: hashutil.DeepHashObject(cod.Spec.Template),
					},
					OwnerReferences: []metav1.OwnerReference{
						*metav1.NewControllerRef(owner, ocv1.GroupVersion.WithKind(ocv1.ClusterObjectDeploymentKind)),
					},
				},
				Spec: ocv1.ClusterObjectSetSpec{
					Group:               cod.Name,
					Revision:            tc.latest,
					LifecycleState:      tc.lifecycleState,
					CollisionProtection: ocv1.CollisionProtectionPrevent,
					Phases:              cod.Spec.Template.Spec.Phases,
				},
			}
			if tc.changed {
				cod.Spec.Template.Spec.CollisionProtection = ocv1.CollisionProtectionNone
			}
			creates := 0
			c := interceptor.NewClient(newCODClient(t, cod, latest), interceptor.Funcs{
				Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					creates++
					return cl.Create(ctx, obj, opts...)
				},
			})

			_, err := reconcileCOD(t, c)

			if tc.wantErr {
				require.ErrorContains(t, err, fmt.Sprintf("revision limit %d reached", ocv1.ClusterObjectSetMaxRevision))
				require.ErrorContains(t, err, fmt.Sprintf("group %q", cod.Name))
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.wantCreates, creates)
			if tc.foreign && tc.lifecycleState == ocv1.ClusterObjectSetLifecycleStateActive {
				require.Equal(t, ocv1.ClusterObjectDeploymentReasonOwnerConflict, progressing(t, c).Reason)
			}
			retained, ok := getCOS(t, c, latest.Name)
			require.True(t, ok)
			require.Equal(t, latest.Spec, retained.Spec)
			if tc.wantCreates == 1 {
				created, ok := getCOS(t, c, fmt.Sprintf("%s-%d", cod.Name, ocv1.ClusterObjectSetMaxRevision))
				require.True(t, ok)
				require.Equal(t, ocv1.ClusterObjectSetMaxRevision, created.Spec.Revision)
			}
		})
	}
}

// deploymentRevision is an owned revision that matches the current template.
func deploymentRevision(cod *ocv1.ClusterObjectDeployment, revision int64, lifecycle ocv1.ClusterObjectSetLifecycleState) *ocv1.ClusterObjectSet {
	cos := revisionCOS(revision, lifecycle)
	cos.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(cod, ocv1.GroupVersion.WithKind(ocv1.ClusterObjectDeploymentKind))}
	cos.Labels = map[string]string{labels.TemplateHashKey: hashutil.DeepHashObject(cod.Spec.Template)}
	cos.Spec.Phases = cod.Spec.Template.DeepCopy().Spec.Phases
	return cos
}

func foreignControllerReference() metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: ocv1.GroupVersion.String(), Kind: "ClusterExtension",
		Name: "another-controller", UID: "foreign-uid", Controller: ptr.To(true),
	}
}

func Test_ClusterObjectDeploymentReconciler_AdoptsGroupRevisions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		owners []metav1.OwnerReference
	}{
		{name: "orphan"},
		{
			name: "preserves non-controller owner",
			owners: []metav1.OwnerReference{{
				APIVersion: ocv1.GroupVersion.String(), Kind: "ClusterExtension",
				Name: "observer", UID: "observer-uid", Controller: ptr.To(false),
			}},
		},
		{
			name: "same deployment with an older UID",
			owners: []metav1.OwnerReference{{
				APIVersion: ocv1.GroupVersion.String(), Kind: ocv1.ClusterObjectDeploymentKind,
				Name: clusterObjectDeploymentName, UID: "old-cod-uid", Controller: ptr.To(true),
			}},
		},
		{
			name: "same deployment through an older API version",
			owners: []metav1.OwnerReference{{
				APIVersion: ocv1.GroupVersion.Group + "/v1alpha1", Kind: ocv1.ClusterObjectDeploymentKind,
				Name: clusterObjectDeploymentName, UID: "old-cod-uid", Controller: ptr.To(true),
			}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cod := codFixture()
			cos := deploymentRevision(cod, 7, ocv1.ClusterObjectSetLifecycleStateActive)
			cos.OwnerReferences = tc.owners
			cos.Annotations = map[string]string{"preserve": "annotation"}
			cos.Status.CompletedAt = metav1.Now()
			setCOSReady(cos, metav1.ConditionTrue, ocv1.ClusterObjectSetReasonAllObjectsReady)
			c := newCODClient(t, cod, cos)
			before, found := getCOS(t, c, cos.Name)
			require.True(t, found)

			_, err := reconcileCOD(t, c)
			require.NoError(t, err)
			adopted, found := getCOS(t, c, cos.Name)
			require.True(t, found)
			require.True(t, metav1.IsControlledBy(adopted, cod))
			require.Equal(t, cos.Spec, adopted.Spec)
			require.Equal(t, cos.Labels, adopted.Labels)
			require.Equal(t, cos.Annotations, adopted.Annotations)
			require.Equal(t, before.Status, adopted.Status)
			for _, owner := range tc.owners {
				if owner.Controller != nil && !*owner.Controller {
					require.Contains(t, adopted.OwnerReferences, owner)
				}
			}
			require.Equal(t, metav1.ConditionTrue, available(t, c).Status)
			require.Equal(t, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed, progressing(t, c).Reason)
			var revisions ocv1.ClusterObjectSetList
			require.NoError(t, c.List(t.Context(), &revisions))
			require.Len(t, revisions.Items, 1, "adoption must not create a matching revision")
		})
	}
}

func Test_ClusterObjectDeploymentReconciler_OwnerConflict(t *testing.T) {
	for _, tc := range []struct {
		name          string
		owned         bool
		ready         bool
		completed     bool
		changed       bool
		owner         *metav1.OwnerReference
		ownedReason   string
		wantAvailable string
	}{
		{name: "no owned active revision", wantAvailable: ocv1.ClusterObjectDeploymentReasonNoActiveRevision},
		{name: "incomplete owned revision", owned: true, wantAvailable: ocv1.ClusterObjectDeploymentReasonUnavailable},
		{name: "completed owned revision", owned: true, ready: true, completed: true, wantAvailable: ocv1.ClusterObjectDeploymentReasonAvailable},
		{name: "expired owned deadline", owned: true, ready: true, wantAvailable: ocv1.ClusterObjectDeploymentReasonAvailable},
		{name: "changed template", owned: true, ready: true, completed: true, changed: true, wantAvailable: ocv1.ClusterObjectDeploymentReasonAvailable},
		{name: "owned revision preconditions are unmet", owned: true, ownedReason: ocv1.ClusterObjectSetReasonPreconditionsNotMet, wantAvailable: ocv1.ClusterObjectDeploymentReasonUnavailable},
		{name: "owned revision has an object collision", owned: true, ownedReason: ocv1.ClusterObjectSetReasonObjectCollision, wantAvailable: ocv1.ClusterObjectDeploymentReasonUnavailable},
		{name: "owned revision has an invalid specification", owned: true, ownedReason: ocv1.ClusterObjectSetReasonInvalidSpecification, wantAvailable: ocv1.ClusterObjectDeploymentReasonUnavailable},
		{
			name: "same deployment name with a different controller kind",
			owner: &metav1.OwnerReference{
				APIVersion: ocv1.GroupVersion.String(), Kind: "ClusterExtension",
				Name: clusterObjectDeploymentName, UID: "foreign-uid", Controller: ptr.To(true),
			},
			wantAvailable: ocv1.ClusterObjectDeploymentReasonNoActiveRevision,
		},
		{
			name: "same deployment name and kind from a different API group",
			owner: &metav1.OwnerReference{
				APIVersion: "other.example.com/v1", Kind: ocv1.ClusterObjectDeploymentKind,
				Name: clusterObjectDeploymentName, UID: "foreign-uid", Controller: ptr.To(true),
			},
			wantAvailable: ocv1.ClusterObjectDeploymentReasonNoActiveRevision,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cod := codFixture()
			cod.Generation = 3
			foreign := deploymentRevision(cod, 3, ocv1.ClusterObjectSetLifecycleStateActive)
			foreign.OwnerReferences = []metav1.OwnerReference{foreignControllerReference()}
			if tc.owner != nil {
				foreign.OwnerReferences = []metav1.OwnerReference{*tc.owner}
			}
			foreign.Spec.Phases[0].Objects[0].Object.SetName("foreign-cm")
			setCOSReady(foreign, metav1.ConditionTrue, ocv1.ClusterObjectSetReasonAllObjectsReady)
			objects := []client.Object{cod, foreign}
			if tc.owned {
				owned := deploymentRevision(cod, 1, ocv1.ClusterObjectSetLifecycleStateActive)
				owned.CreationTimestamp = metav1.NewTime(time.Now().Add(-time.Hour))
				setCOSReady(owned, metav1.ConditionFalse, ocv1.ClusterObjectSetReasonObjectsNotReady)
				if tc.ownedReason != "" {
					setCOSReady(owned, metav1.ConditionFalse, tc.ownedReason)
				}
				if tc.ready {
					setCOSReady(owned, metav1.ConditionTrue, ocv1.ClusterObjectSetReasonAllObjectsReady)
				}
				if tc.completed {
					owned.Status.CompletedAt = metav1.Now()
				}
				objects = append(objects, owned)
			}
			if tc.changed {
				cod.Spec.Template.Spec.CollisionProtection = ocv1.CollisionProtectionNone
			}
			creates := 0
			c := interceptor.NewClient(newCODClient(t, objects...), interceptor.Funcs{
				Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					creates++
					return cl.Create(ctx, obj, opts...)
				},
			})
			before, found := getCOS(t, c, foreign.Name)
			require.True(t, found)
			for range 2 {
				result, err := reconcileCOD(t, c)
				require.NoError(t, err)
				require.Zero(t, result.RequeueAfter)
				condition := progressing(t, c)
				require.Equal(t, metav1.ConditionFalse, condition.Status)
				require.Equal(t, ocv1.ClusterObjectDeploymentReasonOwnerConflict, condition.Reason)
				require.Equal(t, cod.Generation, condition.ObservedGeneration)
				owner := metav1.GetControllerOf(foreign)
				for _, fragment := range []string{foreign.Name, "revision 3", owner.APIVersion, owner.Kind, owner.Name, string(owner.UID), "no higher revision"} {
					require.Contains(t, condition.Message, fragment)
				}
				require.Equal(t, tc.wantAvailable, available(t, c).Reason)
			}
			require.Zero(t, creates)
			retained, found := getCOS(t, c, foreign.Name)
			require.True(t, found)
			require.Equal(t, foreign.OwnerReferences, retained.OwnerReferences)
			require.Equal(t, foreign.Spec, retained.Spec)
			require.Equal(t, before.Status, retained.Status)
		})
	}
}

func Test_ClusterObjectDeploymentReconciler_OwnerConflictRecovery(t *testing.T) {
	for _, resolution := range []string{"archived", "orphaned", "old deployment UID", "deleted", "deleting"} {
		t.Run(resolution, func(t *testing.T) {
			cod := codFixture()
			owned := deploymentRevision(cod, 1, ocv1.ClusterObjectSetLifecycleStateActive)
			setCOSReady(owned, metav1.ConditionTrue, ocv1.ClusterObjectSetReasonAllObjectsReady)
			foreign := deploymentRevision(cod, 3, ocv1.ClusterObjectSetLifecycleStateActive)
			foreign.OwnerReferences = []metav1.OwnerReference{foreignControllerReference()}
			if resolution == "deleting" {
				foreign.Finalizers = []string{"test/hold"}
			}
			c := newCODClient(t, cod, owned, foreign)
			_, err := reconcileCOD(t, c)
			require.NoError(t, err)
			require.Equal(t, ocv1.ClusterObjectDeploymentReasonOwnerConflict, progressing(t, c).Reason)

			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(cod), cod))
			cod.Spec.Template.Spec.CollisionProtection = ocv1.CollisionProtectionNone
			require.NoError(t, c.Update(t.Context(), cod))
			_, err = reconcileCOD(t, c)
			require.NoError(t, err)
			_, found := getCOS(t, c, cod.Name+"-4")
			require.False(t, found, "template changes must remain blocked during the conflict")

			expectedRevision := int64(4)
			switch resolution {
			case "archived":
				foreign.Spec.LifecycleState = ocv1.ClusterObjectSetLifecycleStateArchived
				require.NoError(t, c.Update(t.Context(), foreign))
			case "orphaned":
				foreign.OwnerReferences = nil
				require.NoError(t, c.Update(t.Context(), foreign))
			case "old deployment UID":
				stale := cod.DeepCopy()
				stale.UID = "old-cod-uid"
				foreign.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(stale, ocv1.GroupVersion.WithKind(ocv1.ClusterObjectDeploymentKind))}
				require.NoError(t, c.Update(t.Context(), foreign))
			case "deleted":
				require.NoError(t, c.Delete(t.Context(), foreign))
				expectedRevision = 2
			case "deleting":
				require.NoError(t, c.Delete(t.Context(), foreign))
			}
			_, err = reconcileCOD(t, c)
			require.NoError(t, err)
			created, found := getCOS(t, c, fmt.Sprintf("%s-%d", cod.Name, expectedRevision))
			require.True(t, found)
			require.True(t, metav1.IsControlledBy(created, cod))
			require.Equal(t, metav1.ConditionTrue, progressing(t, c).Status)
			require.Equal(t, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing, progressing(t, c).Reason)
			require.Equal(t, metav1.ConditionTrue, available(t, c).Status)
			if resolution == "orphaned" || resolution == "old deployment UID" {
				adopted, found := getCOS(t, c, foreign.Name)
				require.True(t, found)
				require.True(t, metav1.IsControlledBy(adopted, cod))
			}
		})
	}
}

func Test_ClusterObjectDeploymentReconciler_ForeignHistoryExcluded(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprintf("template changed %t", changed), func(t *testing.T) {
			cod := codFixture()
			cod.Spec.RevisionHistoryLimit = ptr.To(int32(1))
			archived := deploymentRevision(cod, 2, ocv1.ClusterObjectSetLifecycleStateArchived)
			active := deploymentRevision(cod, 4, ocv1.ClusterObjectSetLifecycleStateActive)
			foreignLower := deploymentRevision(cod, 1, ocv1.ClusterObjectSetLifecycleStateActive)
			foreignArchived := deploymentRevision(cod, 8, ocv1.ClusterObjectSetLifecycleStateArchived)
			foreignDeleting := deploymentRevision(cod, 9, ocv1.ClusterObjectSetLifecycleStateActive)
			foreignDeleting.DeletionTimestamp = ptr.To(metav1.Now())
			foreignDeleting.Finalizers = []string{"test/hold"}
			foreign := []*ocv1.ClusterObjectSet{foreignLower, foreignArchived, foreignDeleting}
			for _, cos := range foreign {
				cos.OwnerReferences = []metav1.OwnerReference{foreignControllerReference()}
			}
			setCOSReady(foreignLower, metav1.ConditionTrue, ocv1.ClusterObjectSetReasonAllObjectsReady)
			if changed {
				cod.Spec.Template.Spec.CollisionProtection = ocv1.CollisionProtectionNone
			}
			c := newCODClient(t, cod, archived, active, foreignLower, foreignArchived, foreignDeleting)

			_, err := reconcileCOD(t, c)
			require.NoError(t, err)
			require.Equal(t, ocv1.ClusterObjectDeploymentReasonUnavailable, available(t, c).Reason, "a ready foreign revision must not make the deployment available")
			require.Equal(t, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing, progressing(t, c).Reason)
			_, found := getCOS(t, c, archived.Name)
			require.True(t, found, "foreign history must not count toward the retention limit")
			for _, cos := range foreign {
				retained, found := getCOS(t, c, cos.Name)
				require.True(t, found)
				require.Equal(t, cos.Spec, retained.Spec)
				require.Equal(t, cos.OwnerReferences, retained.OwnerReferences)
			}
			created, found := getCOS(t, c, cod.Name+"-10")
			require.Equal(t, changed, found, "allocation must include foreign archived and deleting revisions")
			if changed {
				require.Equal(t, int64(10), created.Spec.Revision)
			}
		})
	}
}

func Test_ClusterObjectDeploymentReconciler_AdoptionFailure(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprintf("owner conflict %t", conflict), func(t *testing.T) {
			cod := codFixture()
			owned := deploymentRevision(cod, 1, ocv1.ClusterObjectSetLifecycleStateActive)
			setCOSReady(owned, metav1.ConditionTrue, ocv1.ClusterObjectSetReasonAllObjectsReady)
			orphan := deploymentRevision(cod, 3, ocv1.ClusterObjectSetLifecycleStateActive)
			orphan.OwnerReferences = nil
			orphan.Labels = nil // Successful adoption will require a new matching revision.
			foreign := deploymentRevision(cod, 4, ocv1.ClusterObjectSetLifecycleStateArchived)
			foreign.OwnerReferences = []metav1.OwnerReference{foreignControllerReference()}
			if conflict {
				foreign.Spec.LifecycleState = ocv1.ClusterObjectSetLifecycleStateActive
			}
			adoptionErr := errors.New("adoption denied")
			deny := true
			creates := 0
			c := interceptor.NewClient(newCODClient(t, cod, owned, orphan, foreign), interceptor.Funcs{
				Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if deny && obj.GetName() == orphan.Name {
						return adoptionErr
					}
					return cl.Patch(ctx, obj, patch, opts...)
				},
				Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					creates++
					return cl.Create(ctx, obj, opts...)
				},
			})
			_, err := reconcileCOD(t, c)
			require.ErrorIs(t, err, adoptionErr)
			require.Zero(t, creates)
			require.Equal(t, metav1.ConditionTrue, available(t, c).Status)
			unadopted, found := getCOS(t, c, orphan.Name)
			require.True(t, found)
			require.Nil(t, metav1.GetControllerOf(unadopted))
			if conflict {
				require.Equal(t, ocv1.ClusterObjectDeploymentReasonOwnerConflict, progressing(t, c).Reason)
			} else {
				require.Equal(t, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing, progressing(t, c).Reason)
				require.Contains(t, progressing(t, c).Message, "adopted")
			}

			deny = false
			_, err = reconcileCOD(t, c)
			require.NoError(t, err)
			adopted, found := getCOS(t, c, orphan.Name)
			require.True(t, found)
			require.True(t, metav1.IsControlledBy(adopted, cod))
			if conflict {
				require.Zero(t, creates)
				require.Equal(t, ocv1.ClusterObjectDeploymentReasonOwnerConflict, progressing(t, c).Reason)
			} else {
				require.Equal(t, 1, creates)
				_, found := getCOS(t, c, cod.Name+"-5")
				require.True(t, found)
			}
		})
	}
}

func Test_ClusterObjectDeploymentReconciler_ConflictContinuesOwnedPruning(t *testing.T) {
	cod := codFixture()
	cod.Spec.RevisionHistoryLimit = ptr.To(int32(0))
	archived := deploymentRevision(cod, 1, ocv1.ClusterObjectSetLifecycleStateArchived)
	foreign := deploymentRevision(cod, 2, ocv1.ClusterObjectSetLifecycleStateActive)
	foreign.OwnerReferences = []metav1.OwnerReference{foreignControllerReference()}
	c := newCODClient(t, cod, archived, foreign)

	_, err := reconcileCOD(t, c)
	require.NoError(t, err)
	_, found := getCOS(t, c, archived.Name)
	require.False(t, found, "owned history cleanup continues during a conflict")
	retained, found := getCOS(t, c, foreign.Name)
	require.True(t, found)
	require.Equal(t, foreign.Spec, retained.Spec)
	require.Equal(t, foreign.OwnerReferences, retained.OwnerReferences)
	require.Equal(t, ocv1.ClusterObjectDeploymentReasonOwnerConflict, progressing(t, c).Reason)
}

func Test_ClusterObjectDeploymentReconciler_ConcurrentOwnerClaim(t *testing.T) {
	for _, operation := range []string{"adoption", "pruning"} {
		t.Run(operation, func(t *testing.T) {
			cod := codFixture()
			cod.Spec.RevisionHistoryLimit = ptr.To(int32(0))
			cos := deploymentRevision(cod, 1, ocv1.ClusterObjectSetLifecycleStateActive)
			if operation == "adoption" {
				cos.OwnerReferences = nil
			} else {
				cos.Spec.LifecycleState = ocv1.ClusterObjectSetLifecycleStateArchived
				setCOSReady(cos, metav1.ConditionFalse, ocv1.ClusterObjectSetReasonArchived)
			}
			claimed := false
			claim := func(ctx context.Context, cl client.WithWatch) {
				t.Helper()
				if claimed {
					return
				}
				current := &ocv1.ClusterObjectSet{}
				require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(cos), current))
				current.OwnerReferences = []metav1.OwnerReference{foreignControllerReference()}
				require.NoError(t, cl.Update(ctx, current))
				claimed = true
			}
			c := interceptor.NewClient(newCODClient(t, cod, cos), interceptor.Funcs{
				Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if operation == "adoption" && obj.GetName() == cos.Name {
						claim(ctx, cl)
					}
					return cl.Patch(ctx, obj, patch, opts...)
				},
				Delete: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					if operation == "pruning" && obj.GetName() == cos.Name {
						claim(ctx, cl)
					}
					return cl.Delete(ctx, obj, opts...)
				},
			})

			_, err := reconcileCOD(t, c)
			require.True(t, apierrors.IsConflict(err), "a concurrent ownership change must reject the stale mutation: %v", err)
			require.True(t, claimed)
			retained, found := getCOS(t, c, cos.Name)
			require.True(t, found)
			require.Equal(t, foreignControllerReference(), *metav1.GetControllerOf(retained))
			if operation == "adoption" {
				_, found := getCOS(t, c, cod.Name+"-2")
				require.False(t, found, "failed adoption must not create a competing revision")
				_, err = reconcileCOD(t, c)
				require.NoError(t, err)
				require.Equal(t, ocv1.ClusterObjectDeploymentReasonOwnerConflict, progressing(t, c).Reason)
			}
		})
	}
}
