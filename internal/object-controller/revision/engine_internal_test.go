package revision

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"pkg.package-operator.run/boxcutter/machinery/types"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// newRevision builds a types.Revision whose phases are ordered as given.
func newRevision(phaseNames ...string) types.Revision {
	phases := make([]types.Phase, 0, len(phaseNames))
	for _, n := range phaseNames {
		phases = append(phases, types.NewPhase(n, nil))
	}
	return types.NewRevision("test-rev", 1, phases)
}

// gatedSet builds the set of gated phase names used by splitPhases/phasesAfter.
func gatedSet(names ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(names))
	for _, n := range names {
		m[n] = struct{}{}
	}
	return m
}

// completedSet builds the completed-phase lookup used by splitPhases.
func completedSet(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// names extracts phase names for order-sensitive comparison. Always returns a
// non-nil slice so nil and empty results compare equal in assertions.
func names(phases []types.Phase) []string {
	out := make([]string, 0, len(phases))
	for _, p := range phases {
		out = append(out, p.GetName())
	}
	return out
}

func Test_completedPhaseNames(t *testing.T) {
	setAt := metav1.Now()

	for _, tc := range []struct {
		name     string
		observed []ocv1.ObservedPhase
		want     map[string]bool
	}{
		{
			name:     "nil input yields empty map",
			observed: nil,
			want:     map[string]bool{},
		},
		{
			name: "phase with zero completedAt is excluded",
			observed: []ocv1.ObservedPhase{
				{Name: "a", CompletedAt: metav1.Time{}},
			},
			want: map[string]bool{},
		},
		{
			name: "phase with set completedAt is included",
			observed: []ocv1.ObservedPhase{
				{Name: "a", CompletedAt: setAt},
			},
			want: map[string]bool{"a": true},
		},
		{
			name: "only phases with a set completedAt are included",
			observed: []ocv1.ObservedPhase{
				{Name: "a", CompletedAt: setAt},
				{Name: "b", CompletedAt: metav1.Time{}},
				{Name: "c", CompletedAt: setAt},
			},
			want: map[string]bool{"a": true, "c": true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, completedPhaseNames(tc.observed))
		})
	}
}

func Test_phasesAfter(t *testing.T) {
	for _, tc := range []struct {
		name      string
		rev       types.Revision
		gated     map[string]struct{}
		afterName string
		want      []string
	}{
		{
			name:      "returns phases following the named phase in order",
			rev:       newRevision("a", "b", "c"),
			gated:     gatedSet(),
			afterName: "a",
			want:      []string{"b", "c"},
		},
		{
			name:      "returns empty when the named phase is last",
			rev:       newRevision("a", "b", "c"),
			gated:     gatedSet(),
			afterName: "c",
			want:      []string{},
		},
		{
			name:      "returns empty when the named phase is absent",
			rev:       newRevision("a", "b", "c"),
			gated:     gatedSet(),
			afterName: "missing",
			want:      []string{},
		},
		{
			name:      "skips gated phases that follow",
			rev:       newRevision("a", "b", "c", "d"),
			gated:     gatedSet("c"),
			afterName: "a",
			want:      []string{"b", "d"},
		},
		{
			name:      "the named phase itself is excluded even when gated",
			rev:       newRevision("a", "b", "c"),
			gated:     gatedSet("b"),
			afterName: "b",
			want:      []string{"c"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, names(phasesAfter(tc.rev, tc.gated, tc.afterName)))
		})
	}
}

func Test_splitPhases(t *testing.T) {
	for _, tc := range []struct {
		name         string
		rev          types.Revision
		gated        map[string]struct{}
		completed    map[string]bool
		wantDrift    []string
		wantReadOnly []string
	}{
		{
			name:         "all phases completed go to drift",
			rev:          newRevision("a", "b", "c"),
			gated:        gatedSet(),
			completed:    completedSet("a", "b", "c"),
			wantDrift:    []string{"a", "b", "c"},
			wantReadOnly: []string{},
		},
		{
			name:         "all phases gated yields nothing",
			rev:          newRevision("a", "b"),
			gated:        gatedSet("a", "b"),
			completed:    completedSet(),
			wantDrift:    []string{},
			wantReadOnly: []string{},
		},
		{
			name:         "leading non-completed phase makes everything read-only",
			rev:          newRevision("a", "b", "c"),
			gated:        gatedSet(),
			completed:    completedSet(),
			wantDrift:    []string{},
			wantReadOnly: []string{"a", "b", "c"},
		},
		{
			name: "first non-completed phase after completed run joins drift, rest read-only",
			rev:  newRevision("a", "b", "c"),
			// a is completed; b is the boundary phase (drift), c is read-only.
			gated:        gatedSet(),
			completed:    completedSet("a"),
			wantDrift:    []string{"a", "b"},
			wantReadOnly: []string{"c"},
		},
		{
			name:         "gated phases are skipped while splitting completed and remaining",
			rev:          newRevision("a", "b", "c", "d"),
			gated:        gatedSet("b"),
			completed:    completedSet("a", "c"),
			wantDrift:    []string{"a", "c", "d"},
			wantReadOnly: []string{},
		},
		{
			name: "boundary read-only respects gated phases",
			rev:  newRevision("a", "b", "c", "d"),
			// a completed -> drift; b boundary non-completed -> drift; c gated -> skipped; d -> read-only.
			gated:        gatedSet("c"),
			completed:    completedSet("a"),
			wantDrift:    []string{"a", "b"},
			wantReadOnly: []string{"d"},
		},
		{
			name: "leading read-only respects gated phases",
			rev:  newRevision("a", "b", "c", "d"),
			// a gated -> skipped; b non-completed with none completed yet -> read-only + rest.
			gated:        gatedSet("a"),
			completed:    completedSet(),
			wantDrift:    []string{},
			wantReadOnly: []string{"b", "c", "d"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			drift, readOnly := splitPhases(tc.rev, tc.gated, tc.completed)
			assert.Equal(t, tc.wantDrift, names(drift), "drift phases")
			assert.Equal(t, tc.wantReadOnly, names(readOnly), "read-only phases")
		})
	}
}