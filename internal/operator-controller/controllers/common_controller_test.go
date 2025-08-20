package controllers

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

func TestSetStatusProgressing(t *testing.T) {
	for _, tc := range []struct {
		name             string
		err              error
		clusterExtension *ocv1.ClusterExtension
		expected         metav1.Condition
	}{
		{
			name:             "non-nil ClusterExtension, nil error, Progressing condition has status True with reason Success",
			err:              nil,
			clusterExtension: &ocv1.ClusterExtension{},
			expected: metav1.Condition{
				Type:    ocv1.TypeProgressing,
				Status:  metav1.ConditionTrue,
				Reason:  ocv1.ReasonSucceeded,
				Message: "Desired state reached",
			},
		},
		{
			name:             "non-nil ClusterExtension, non-terminal error, Progressing condition has status True with reason Retrying",
			err:              errors.New("boom"),
			clusterExtension: &ocv1.ClusterExtension{},
			expected: metav1.Condition{
				Type:    ocv1.TypeProgressing,
				Status:  metav1.ConditionTrue,
				Reason:  ocv1.ReasonRetrying,
				Message: "boom",
			},
		},
		{
			name:             "non-nil ClusterExtension, terminal error, Progressing condition has status False with reason Blocked",
			err:              reconcile.TerminalError(errors.New("boom")),
			clusterExtension: &ocv1.ClusterExtension{},
			expected: metav1.Condition{
				Type:    ocv1.TypeProgressing,
				Status:  metav1.ConditionFalse,
				Reason:  ocv1.ReasonBlocked,
				Message: "terminal error: boom",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setStatusProgressing(tc.clusterExtension, tc.err)
			progressingCond := meta.FindStatusCondition(tc.clusterExtension.Status.Conditions, ocv1.TypeProgressing)
			require.NotNil(t, progressingCond, "progressing condition should be set but was not")
			diff := cmp.Diff(*progressingCond, tc.expected, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration"))
			require.Empty(t, diff, "difference between actual and expected Progressing conditions")
		})
	}
}
