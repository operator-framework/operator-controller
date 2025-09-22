package v1

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestClusterExtensionRevisionImmutability(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	i := 0
	for name, tc := range map[string]struct {
		spec       ClusterExtensionRevisionSpec
		updateFunc func(*ClusterExtensionRevision)
		allowed    bool
	}{
		"revision is immutable": {
			spec: ClusterExtensionRevisionSpec{
				Revision: 1,
			},
			updateFunc: func(cer *ClusterExtensionRevision) {
				cer.Spec.Revision = 2
			},
		},
		"phases may be initially empty": {
			spec: ClusterExtensionRevisionSpec{
				Phases: []ClusterExtensionRevisionPhase{},
			},
			updateFunc: func(cer *ClusterExtensionRevision) {
				cer.Spec.Phases = []ClusterExtensionRevisionPhase{
					{
						Name:    "foo",
						Objects: []ClusterExtensionRevisionObject{},
					},
				}
			},
			allowed: true,
		},
		"phases may be initially unset": {
			spec: ClusterExtensionRevisionSpec{},
			updateFunc: func(cer *ClusterExtensionRevision) {
				cer.Spec.Phases = []ClusterExtensionRevisionPhase{
					{
						Name:    "foo",
						Objects: []ClusterExtensionRevisionObject{},
					},
				}
			},
			allowed: true,
		},
		"phases are immutable if not empty": {
			spec: ClusterExtensionRevisionSpec{
				Phases: []ClusterExtensionRevisionPhase{
					{
						Name:    "foo",
						Objects: []ClusterExtensionRevisionObject{},
					},
				},
			},
			updateFunc: func(cer *ClusterExtensionRevision) {
				cer.Spec.Phases = []ClusterExtensionRevisionPhase{
					{
						Name:    "foo2",
						Objects: []ClusterExtensionRevisionObject{},
					},
				}
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			cer := &ClusterExtensionRevision{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("foo%d", i),
				},
				Spec: tc.spec,
			}
			i = i + 1
			require.NoError(t, c.Create(ctx, cer))
			tc.updateFunc(cer)
			err := c.Update(ctx, cer)
			if tc.allowed && err != nil {
				t.Fatal("expected update to succeed, but got:", err)
			}
			if !tc.allowed && !errors.IsInvalid(err) {
				t.Fatal("expected update to fail due to invalid payload, but got:", err)
			}
		})
	}
}
