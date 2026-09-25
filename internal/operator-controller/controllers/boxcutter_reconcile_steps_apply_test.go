/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

func TestApplyBundleWithBoxcutter(t *testing.T) {
	type args struct {
		activeRevisions []ocv1.RevisionStatus
		revisionStates  *RevisionStates
	}
	type want struct {
		activeRevisions []ocv1.RevisionStatus
	}

	for _, tc := range []struct {
		name string
		args args
		want want
	}{
		{
			name: "two active revisions during update",
			args: args{
				activeRevisions: []ocv1.RevisionStatus{
					{Name: "ce-1"},
				},
				revisionStates: &RevisionStates{
					Installed: &RevisionMetadata{
						RevisionName: "ce-1",
						BundleMetadata: ocv1.BundleMetadata{
							Name:    "test-bundle",
							Version: "1.0.0",
						},
					},
					RollingOut: []*RevisionMetadata{
						{RevisionName: "ce-2"},
					},
				},
			},
			want: want{
				activeRevisions: []ocv1.RevisionStatus{
					{Name: "ce-1"},
					{Name: "ce-2"},
				},
			},
		},
		{
			name: "replaces existing with new active revisions",
			args: args{
				activeRevisions: []ocv1.RevisionStatus{
					{Name: "ce-1"},
				},
				revisionStates: &RevisionStates{
					Installed: &RevisionMetadata{
						RevisionName: "ce-2",
						BundleMetadata: ocv1.BundleMetadata{
							Name:    "test-bundle",
							Version: "1.0.1",
						},
					},
				},
			},
			want: want{
				activeRevisions: []ocv1.RevisionStatus{
					{Name: "ce-2"},
				},
			},
		},
		{
			name: "ongoing installation",
			args: args{
				activeRevisions: []ocv1.RevisionStatus{
					{Name: "ce-1"},
				},
				revisionStates: &RevisionStates{
					RollingOut: []*RevisionMetadata{
						{RevisionName: "ce-1"},
					},
				},
			},
			want: want{
				activeRevisions: []ocv1.RevisionStatus{
					{Name: "ce-1"},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			ext := &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-ext",
					Generation: 1,
				},
				Status: ocv1.ClusterExtensionStatus{
					ActiveRevisions: tc.args.activeRevisions,
				},
			}

			state := &reconcileState{
				revisionStates: tc.args.revisionStates,
				resolvedRevisionMetadata: &RevisionMetadata{
					BundleMetadata: ocv1.BundleMetadata{
						Name:    "test-bundle",
						Version: "1.0.0",
					},
				},
				imageFS: fstest.MapFS{},
			}

			stepFunc := ApplyBundleWithBoxcutter(func(_ context.Context, _ fs.FS, _ *ocv1.ClusterExtension, _, _ map[string]string) (bool, string, error) {
				return true, "", nil
			})
			result, err := stepFunc(ctx, state, ext)
			require.NoError(t, err)
			require.Nil(t, result)

			require.Len(t, ext.Status.ActiveRevisions, len(tc.want.activeRevisions))
			for i, expected := range tc.want.activeRevisions {
				require.Equal(t, expected.Name, ext.Status.ActiveRevisions[i].Name,
					"ActiveRevisions[%d].Name mismatch", i)
			}
		})
	}
}

func Test_ceProgressingFromReady(t *testing.T) {
	cases := []struct {
		reason     string
		wantStatus metav1.ConditionStatus
		wantReason string
	}{
		{ocv1.ClusterObjectSetReasonReady, metav1.ConditionTrue, ocv1.ReasonSucceeded},
		{ocv1.ClusterObjectSetReasonIncomplete, metav1.ConditionTrue, ocv1.ReasonRollingOut},
		{ocv1.ClusterObjectSetReasonProgressDeadlineExceeded, metav1.ConditionFalse, ocv1.ReasonProgressDeadlineExceeded},
		{ocv1.ClusterObjectSetReasonBlocked, metav1.ConditionFalse, ocv1.ReasonBlocked},
		{ocv1.ClusterObjectSetReasonInvalid, metav1.ConditionFalse, ocv1.ReasonInvalidConfiguration},
		{ocv1.ClusterObjectSetReasonReconcileError, metav1.ConditionTrue, ocv1.ReasonRetrying},
		{ocv1.ClusterObjectSetReasonTeardownError, metav1.ConditionTrue, ocv1.ReasonRetrying},
		{ocv1.ClusterObjectSetReasonInternalError, metav1.ConditionTrue, ocv1.ReasonRetrying},
	}
	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			gotStatus, gotReason := ceProgressingFromReady(metav1.Condition{
				Type:   ocv1.ClusterObjectSetTypeReady,
				Reason: tc.reason,
			})
			require.Equal(t, tc.wantStatus, gotStatus)
			require.Equal(t, tc.wantReason, gotReason)
		})
	}
}

func TestApplyBundleWithBoxcutter_HandoverMultipleActiveRevisions(t *testing.T) {
	ctx := context.Background()

	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-ext",
			Generation: 2,
		},
	}

	state := &reconcileState{
		revisionStates: &RevisionStates{
			Installed: &RevisionMetadata{
				RevisionName: "ce-1",
				BundleMetadata: ocv1.BundleMetadata{
					Name:    "test-bundle",
					Version: "1.0.0",
				},
				Conditions: []metav1.Condition{
					{
						Type:   ocv1.ClusterObjectSetTypeReady,
						Status: metav1.ConditionTrue,
						Reason: ocv1.ClusterObjectSetReasonReady,
						Message: "installed revision ready",
					},
				},
			},
			RollingOut: []*RevisionMetadata{
				{
					RevisionName: "ce-2",
					Conditions: []metav1.Condition{
						{
							Type:   ocv1.ClusterObjectSetTypeReady,
							Status: metav1.ConditionFalse,
							Reason: ocv1.ClusterObjectSetReasonIncomplete,
							Message: "rolling out revision incomplete",
						},
					},
				},
			},
		},
		resolvedRevisionMetadata: &RevisionMetadata{
			BundleMetadata: ocv1.BundleMetadata{
				Name:    "test-bundle",
				Version: "1.0.1",
			},
		},
		imageFS: fstest.MapFS{},
	}

	stepFunc := ApplyBundleWithBoxcutter(func(_ context.Context, _ fs.FS, _ *ocv1.ClusterExtension, _, _ map[string]string) (bool, string, error) {
		return true, "", nil
	})
	result, err := stepFunc(ctx, state, ext)
	require.NoError(t, err)
	require.Nil(t, result)

	// CE Available comes from the installed revision (Ready=True/Ready retyped).
	avail := apimeta.FindStatusCondition(ext.Status.Conditions, "Available")
	require.NotNil(t, avail)
	require.Equal(t, metav1.ConditionTrue, avail.Status)
	require.Equal(t, ocv1.ClusterObjectSetReasonReady, avail.Reason)
	require.Equal(t, "installed revision ready", avail.Message)
	require.Equal(t, int64(2), avail.ObservedGeneration)

	// CE Progressing comes from the LATEST rolling revision (Incomplete -> RollingOut).
	prog := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeProgressing)
	require.NotNil(t, prog)
	require.Equal(t, metav1.ConditionTrue, prog.Status)
	require.Equal(t, ocv1.ReasonRollingOut, prog.Reason)
	require.Equal(t, "rolling out revision incomplete", prog.Message)
	require.Equal(t, int64(2), prog.ObservedGeneration)

	// Each active revision mirrors its own single Ready condition.
	require.Len(t, ext.Status.ActiveRevisions, 2)
	
	// Installed revision
	installedRS := ext.Status.ActiveRevisions[0]
	require.Equal(t, "ce-1", installedRS.Name)
	require.Len(t, installedRS.Conditions, 1)
	installedReady := installedRS.Conditions[0]
	require.Equal(t, ocv1.ClusterObjectSetTypeReady, installedReady.Type)
	require.Equal(t, metav1.ConditionTrue, installedReady.Status)
	require.Equal(t, ocv1.ClusterObjectSetReasonReady, installedReady.Reason)
	require.Equal(t, int64(2), installedReady.ObservedGeneration)
	
	// Rolling out revision
	rollingRS := ext.Status.ActiveRevisions[1]
	require.Equal(t, "ce-2", rollingRS.Name)
	require.Len(t, rollingRS.Conditions, 1)
	rollingReady := rollingRS.Conditions[0]
	require.Equal(t, ocv1.ClusterObjectSetTypeReady, rollingReady.Type)
	require.Equal(t, metav1.ConditionFalse, rollingReady.Status)
	require.Equal(t, ocv1.ClusterObjectSetReasonIncomplete, rollingReady.Reason)
	require.Equal(t, int64(2), rollingReady.ObservedGeneration)
}

func TestApplyBundleWithBoxcutter_ArchivedLeavesProgressingUntouched(t *testing.T) {
	ctx := context.Background()

	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-ext",
			Generation: 3,
		},
		Status: ocv1.ClusterExtensionStatus{
			// Pre-existing Progressing condition that should NOT be overwritten
			Conditions: []metav1.Condition{
				{
					Type:               ocv1.TypeProgressing,
					Status:             metav1.ConditionTrue,
					Reason:             ocv1.ReasonRollingOut,
					Message:            "pre-existing progressing state",
					ObservedGeneration: 2,
				},
			},
		},
	}

	state := &reconcileState{
		revisionStates: &RevisionStates{
			Installed: &RevisionMetadata{
				RevisionName: "ce-archived",
				BundleMetadata: ocv1.BundleMetadata{
					Name:    "test-bundle",
					Version: "1.0.0",
				},
				Conditions: []metav1.Condition{
					{
						Type:    ocv1.ClusterObjectSetTypeReady,
						Status:  metav1.ConditionFalse,
						Reason:  ocv1.ClusterObjectSetReasonArchived,
						Message: "revision archived",
					},
				},
			},
		},
		resolvedRevisionMetadata: &RevisionMetadata{
			BundleMetadata: ocv1.BundleMetadata{
				Name:    "test-bundle",
				Version: "1.0.0",
			},
		},
		imageFS: fstest.MapFS{},
	}

	stepFunc := ApplyBundleWithBoxcutter(func(_ context.Context, _ fs.FS, _ *ocv1.ClusterExtension, _, _ map[string]string) (bool, string, error) {
		return true, "", nil
	})
	result, err := stepFunc(ctx, state, ext)
	require.NoError(t, err)
	require.Nil(t, result)

	// CE Available should be set (Ready retyped to Available)
	avail := apimeta.FindStatusCondition(ext.Status.Conditions, "Available")
	require.NotNil(t, avail)
	require.Equal(t, metav1.ConditionFalse, avail.Status)
	require.Equal(t, ocv1.ClusterObjectSetReasonArchived, avail.Reason)
	require.Equal(t, "revision archived", avail.Message)
	require.Equal(t, int64(3), avail.ObservedGeneration)

	// CE Progressing MUST remain untouched (still the pre-existing value)
	prog := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeProgressing)
	require.NotNil(t, prog)
	require.Equal(t, metav1.ConditionTrue, prog.Status)
	require.Equal(t, ocv1.ReasonRollingOut, prog.Reason)
	require.Equal(t, "pre-existing progressing state", prog.Message)
	require.Equal(t, int64(2), prog.ObservedGeneration, "Archived must not overwrite Progressing")

	// ActiveRevision should still reflect the archived revision with Ready condition
	require.Len(t, ext.Status.ActiveRevisions, 1)
	rs := ext.Status.ActiveRevisions[0]
	require.Equal(t, "ce-archived", rs.Name)
	require.Len(t, rs.Conditions, 1)
	require.Equal(t, ocv1.ClusterObjectSetTypeReady, rs.Conditions[0].Type)
	require.Equal(t, metav1.ConditionFalse, rs.Conditions[0].Status)
	require.Equal(t, ocv1.ClusterObjectSetReasonArchived, rs.Conditions[0].Reason)
}
