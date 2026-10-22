package applier

import (
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"pkg.package-operator.run/boxcutter/probing"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// daemonSetProber builds the DaemonSet progression probe the way the object-controller does:
// the assertions joined with And, wrapped in an ObservedGenerationProbe.
func daemonSetProber(t *testing.T) probing.Prober {
	t.Helper()
	for _, p := range defaultProgressionProbes {
		if p.Selector.GroupKind == nil || p.Selector.GroupKind.Kind != "DaemonSet" {
			continue
		}
		require.Equal(t, appsv1.GroupName, p.Selector.GroupKind.Group)
		and := probing.And{}
		for _, a := range p.Assertions {
			require.Equal(t, ocv1.ProbeTypeFieldsEqual, *a.Type)
			and = append(and, &probing.FieldsEqualProbe{FieldA: *a.FieldsEqual.FieldA, FieldB: *a.FieldsEqual.FieldB})
		}
		return &probing.ObservedGenerationProbe{Prober: and}
	}
	t.Fatal("no DaemonSet progression probe in defaultProgressionProbes")
	return nil
}

func TestDaemonSetProgressionProbe(t *testing.T) {
	prober := daemonSetProber(t)
	for _, tc := range []struct {
		name   string
		status appsv1.DaemonSetStatus
		want   probing.Status
	}{
		{
			name:   "all scheduled pods updated and available",
			status: appsv1.DaemonSetStatus{ObservedGeneration: 2, DesiredNumberScheduled: 3, UpdatedNumberScheduled: 3, NumberAvailable: 3},
			want:   probing.StatusTrue,
		},
		{
			name:   "rollout in progress",
			status: appsv1.DaemonSetStatus{ObservedGeneration: 2, DesiredNumberScheduled: 3, UpdatedNumberScheduled: 1, NumberAvailable: 3},
			want:   probing.StatusFalse,
		},
		{
			name:   "pods not yet available",
			status: appsv1.DaemonSetStatus{ObservedGeneration: 2, DesiredNumberScheduled: 3, UpdatedNumberScheduled: 3, NumberAvailable: 2},
			want:   probing.StatusFalse,
		},
		{
			name:   "status from an older generation",
			status: appsv1.DaemonSetStatus{ObservedGeneration: 1, DesiredNumberScheduled: 3, UpdatedNumberScheduled: 3, NumberAvailable: 3},
			want:   probing.StatusUnknown,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ds := &appsv1.DaemonSet{
				TypeMeta:   metav1.TypeMeta{Kind: "DaemonSet", APIVersion: "apps/v1"},
				ObjectMeta: metav1.ObjectMeta{Name: "node-plugin", Generation: 2},
				Status:     tc.status,
			}
			require.Equal(t, tc.want, prober.Probe(ds).Status)
		})
	}
}
