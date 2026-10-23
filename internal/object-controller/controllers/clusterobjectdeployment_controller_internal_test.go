//go:build !standard

package controllers

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
)

func testTemplate() ocv1.ClusterObjectDeploymentTemplate {
	return ocv1.ClusterObjectDeploymentTemplate{
		Spec: ocv1.ClusterObjectDeploymentTemplateSpec{
			CollisionProtection: ocv1.CollisionProtectionNone,
			Phases: []ocv1.ClusterObjectSetPhase{
				{
					Name: "resources",
					Objects: []ocv1.ClusterObjectSetObject{
						{Object: unstructured.Unstructured{Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "ConfigMap",
							"metadata":   map[string]interface{}{"name": "cm", "namespace": "ns"},
						}}},
					},
				},
			},
		},
	}
}

func Test_templateHash_isStableAndSensitive(t *testing.T) {
	tmpl := testTemplate()

	h1 := templateHash(tmpl)
	require.NotEmpty(t, h1)
	require.LessOrEqual(t, len(h1), 63, "digest must fit within the label-value length limit")
	require.Regexp(t, "^[0-9a-z]+$", h1, "digest must be label-value safe")

	h2 := templateHash(tmpl)
	require.Equal(t, h1, h2, "same template must hash the same")

	// A change to a phase object changes the hash.
	changed := testTemplate()
	changed.Spec.Phases[0].Objects[0].Object.Object["data"] = map[string]interface{}{"k": "v"}
	require.NotEqual(t, h1, templateHash(changed))

	// A change to ONLY progressionProbes (a mutable COS field) still changes the hash.
	probesChanged := testTemplate()
	probesChanged.Spec.ProgressionProbes = []ocv1.ProgressionProbe{{
		Selector:   ocv1.ObjectSelector{Type: ocv1.SelectorTypeGroupKind, GroupKind: metav1.GroupKind{Kind: "ConfigMap"}},
		Assertions: []ocv1.Assertion{{Type: ocv1.ProbeTypeFieldValue}},
	}}
	require.NotEqual(t, h1, templateHash(probesChanged), "a progressionProbes-only change must create a new revision")
}

func Test_nextRevisionNumber(t *testing.T) {
	revision, err := nextRevisionNumber(nil)
	require.NoError(t, err)
	require.Equal(t, int64(1), revision)
	revs := []ocv1.ClusterObjectSet{
		{Spec: ocv1.ClusterObjectSetSpec{Revision: 1}},
		{Spec: ocv1.ClusterObjectSetSpec{Revision: 3, LifecycleState: ocv1.ClusterObjectSetLifecycleStateArchived}},
	}
	revision, err = nextRevisionNumber(revs)
	require.NoError(t, err)
	require.Equal(t, int64(4), revision, "must count archived revisions so numbers never repeat")
}

func Test_latestActiveRevision(t *testing.T) {
	require.Nil(t, latestActiveRevision(nil))

	// Highest-revision active revision wins.
	revs := []ocv1.ClusterObjectSet{
		{ObjectMeta: metav1.ObjectMeta{Name: "cod-1"}, Spec: ocv1.ClusterObjectSetSpec{Revision: 1, LifecycleState: ocv1.ClusterObjectSetLifecycleStateActive}},
		{ObjectMeta: metav1.ObjectMeta{Name: "cod-2"}, Spec: ocv1.ClusterObjectSetSpec{Revision: 2, LifecycleState: ocv1.ClusterObjectSetLifecycleStateActive}},
	}
	require.Equal(t, "cod-2", latestActiveRevision(revs).Name)

	// All archived -> nil.
	archived := []ocv1.ClusterObjectSet{
		{Spec: ocv1.ClusterObjectSetSpec{Revision: 1, LifecycleState: ocv1.ClusterObjectSetLifecycleStateArchived}},
	}
	require.Nil(t, latestActiveRevision(archived))

	// A higher-revision revision being deleted is skipped.
	deleting := metav1.NewTime(time.Now())
	mixed := []ocv1.ClusterObjectSet{
		{ObjectMeta: metav1.ObjectMeta{Name: "cod-1"}, Spec: ocv1.ClusterObjectSetSpec{Revision: 1, LifecycleState: ocv1.ClusterObjectSetLifecycleStateActive}},
		{ObjectMeta: metav1.ObjectMeta{Name: "cod-2", DeletionTimestamp: &deleting, Finalizers: []string{"x"}}, Spec: ocv1.ClusterObjectSetSpec{Revision: 2, LifecycleState: ocv1.ClusterObjectSetLifecycleStateActive}},
	}
	require.Equal(t, "cod-1", latestActiveRevision(mixed).Name)
}

func Test_buildClusterObjectSet(t *testing.T) {
	cod := &ocv1.ClusterObjectDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: "cod"},
		Spec:       ocv1.ClusterObjectDeploymentSpec{Template: testTemplate()},
	}

	cos := buildClusterObjectSet(cod, 1, "deadbeef")

	require.Equal(t, "cod-1", cos.Name)
	require.Equal(t, "deadbeef", cos.Labels[labels.TemplateHashKey])
	require.NotContains(t, cos.Labels, labels.OwnerNameKey)
	require.Equal(t, "cod", cos.Spec.Group)
	require.Equal(t, int64(1), cos.Spec.Revision)
	require.Equal(t, ocv1.ClusterObjectSetLifecycleStateActive, cos.Spec.LifecycleState)
	require.Equal(t, cod.Spec.Template.Spec.Phases, cos.Spec.Phases)
	require.Equal(t, ocv1.CollisionProtectionNone, cos.Spec.CollisionProtection, "template collisionProtection must be copied through")
}

func Test_validateTemplateMetadata(t *testing.T) {
	// A label value is not length-bounded by the template schema (only MaxProperties is
	// enforced on labels), so an oversized value must be omitted from the diagnostic
	// rather than echoed verbatim. Otherwise it bloats the message that is written to
	// the Progressing condition.
	oversizedValue := strings.Repeat("a", 40000)

	for _, tc := range []struct {
		name          string
		md            ocv1.ClusterObjectDeploymentTemplateMetadata
		wantDiag      bool // whether a non-empty diagnostic is expected
		omitsOversize bool // whether the oversized value must be omitted from the diagnostic
	}{
		{
			name: "valid metadata returns no diagnostic",
			md: ocv1.ClusterObjectDeploymentTemplateMetadata{
				Labels:      map[string]string{"team": "platform"},
				Annotations: map[string]string{"note": "hello"},
			},
			wantDiag: false,
		},
		{
			name:     "invalid label key is reported",
			md:       ocv1.ClusterObjectDeploymentTemplateMetadata{Labels: map[string]string{"not a valid key": "v"}},
			wantDiag: true,
		},
		{
			name:          "oversized label value is omitted",
			md:            ocv1.ClusterObjectDeploymentTemplateMetadata{Labels: map[string]string{"team": oversizedValue}},
			wantDiag:      true,
			omitsOversize: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := validateTemplateMetadata(tc.md)
			if !tc.wantDiag {
				require.Empty(t, msg)
				return
			}
			require.NotEmpty(t, msg)
			if tc.omitsOversize {
				require.NotContains(t, msg, oversizedValue, "the oversized rejected value must not be echoed verbatim")
				require.Contains(t, msg, "bytes omitted", "the diagnostic should note that the value was omitted")
			}
		})
	}
}

func Test_setCondition_capsMessage(t *testing.T) {
	// setCondition must bound the message so Status().Update is never rejected for an
	// oversized condition message (the API server caps it at 32768 bytes).
	cod := &ocv1.ClusterObjectDeployment{}
	setCondition(cod, ocv1.ClusterObjectDeploymentTypeProgressing, metav1.ConditionFalse, "Reason", strings.Repeat("x", 5000))

	cond := apimeta.FindStatusCondition(cod.Status.Conditions, ocv1.ClusterObjectDeploymentTypeProgressing)
	require.NotNil(t, cond)
	require.LessOrEqual(t, len(cond.Message), maxConditionMessageBytes)
}

func Test_mapClusterObjectSetToClusterObjectDeployment(t *testing.T) {
	for _, tc := range []struct {
		name string
		obj  client.Object
		want []ctrl.Request
	}{
		{
			name: "maps spec.group to a ClusterObjectDeployment request",
			obj: &ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{Name: "cod-1-1"},
				Spec:       ocv1.ClusterObjectSetSpec{Group: "cod-1"},
			},
			want: []ctrl.Request{{NamespacedName: types.NamespacedName{Name: "cod-1"}}},
		},
		{
			name: "returns nil when spec.group is empty",
			obj: &ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{Name: "cod-1-1"},
			},
			want: nil,
		},
		{
			name: "returns nil for a non-ClusterObjectSet object",
			obj:  &ocv1.ClusterObjectDeployment{ObjectMeta: metav1.ObjectMeta{Name: "cod-1"}},
			want: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := mapClusterObjectSetToClusterObjectDeployment(t.Context(), tc.obj)
			require.Equal(t, tc.want, got)
		})
	}
}
