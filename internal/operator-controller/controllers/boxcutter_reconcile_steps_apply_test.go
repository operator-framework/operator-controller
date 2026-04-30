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

func TestApplyBundleWithBoxcutterInstalledSatisfiesSpecIgnoresStaleRollingProgressing(t *testing.T) {
	ctx := context.Background()
	ext := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-ext",
			Generation: 3,
		},
		Spec: ocv1.ClusterExtensionSpec{
			Source: ocv1.SourceConfig{
				Catalog: &ocv1.CatalogFilter{
					PackageName: "test",
					Version:     "1.2.0",
				},
			},
		},
	}

	state := &reconcileState{
		revisionStates: &RevisionStates{
			Installed: &RevisionMetadata{
				RevisionName: "test-ext-3",
				BundleMetadata: ocv1.BundleMetadata{
					Name:    "test.v1.2.0",
					Version: "1.2.0",
				},
				Conditions: []metav1.Condition{
					{
						Type:   ocv1.ClusterObjectSetTypeProgressing,
						Status: metav1.ConditionTrue,
						Reason: ocv1.ReasonSucceeded,
					},
				},
			},
			RollingOut: []*RevisionMetadata{{
				RevisionName: "test-ext-2",
				BundleMetadata: ocv1.BundleMetadata{
					Name:    "test.v1.0.2",
					Version: "1.0.2",
				},
				Conditions: []metav1.Condition{
					{
						Type:   ocv1.ClusterObjectSetTypeProgressing,
						Status: metav1.ConditionTrue,
						Reason: ocv1.ReasonRollingOut,
					},
				},
			}},
		},
		resolvedRevisionMetadata: &RevisionMetadata{
			BundleMetadata: ocv1.BundleMetadata{
				Name:    "test.v1.2.0",
				Version: "1.2.0",
			},
		},
		imageFS: fstest.MapFS{},
	}

	stepFunc := ApplyBundleWithBoxcutter(func(_ context.Context, _ fs.FS, _ *ocv1.ClusterExtension, _, _ map[string]string) (bool, string, error) {
		return true, "", nil
	})
	_, err := stepFunc(ctx, state, ext)
	require.NoError(t, err)

	pcnd := apimeta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeProgressing)
	require.NotNil(t, pcnd)
	require.Equal(t, ocv1.ReasonSucceeded, pcnd.Reason,
		"stale rolling revision must not overwrite Progressing when installed matches spec")
}
