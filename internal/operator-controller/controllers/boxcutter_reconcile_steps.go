/*
Copyright 2025.

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
	"cmp"
	"context"
	"fmt"
	"slices"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
)

type BoxcutterRevisionStatesGetter struct {
	Reader client.Reader
}

func (d *BoxcutterRevisionStatesGetter) GetRevisionStates(ctx context.Context, ext *ocv1.ClusterExtension) (*RevisionStates, error) {
	// TODO: boxcutter applier has a nearly identical bit of code for listing and sorting revisions
	//   only difference here is that it sorts in reverse order to start iterating with the most
	//   recent revisions. We should consolidate to avoid code duplication.
	existingRevisionList := &ocv1.ClusterExtensionRevisionList{}
	if err := d.Reader.List(ctx, existingRevisionList, client.MatchingLabels{
		labels.OwnerNameKey: ext.Name,
	}); err != nil {
		return nil, fmt.Errorf("listing revisions: %w", err)
	}
	slices.SortFunc(existingRevisionList.Items, func(a, b ocv1.ClusterExtensionRevision) int {
		return cmp.Compare(a.Spec.Revision, b.Spec.Revision)
	})

	rs := &RevisionStates{}
	for _, rev := range existingRevisionList.Items {
		switch rev.Spec.LifecycleState {
		case ocv1.ClusterExtensionRevisionLifecycleStateActive,
			ocv1.ClusterExtensionRevisionLifecycleStatePaused:
		default:
			// Skip anything not active or paused, which should only be "Archived".
			continue
		}

		// TODO: the setting of these annotations (happens in boxcutter applier when we pass in "storageLabels")
		//   is fairly decoupled from this code where we get the annotations back out. We may want to co-locate
		//   the set/get logic a bit better to make it more maintainable and less likely to get out of sync.
		rm := &RevisionMetadata{
			RevName:    rev.Name,
			Package:    rev.Labels[labels.PackageNameKey],
			Image:      rev.Annotations[labels.BundleReferenceKey],
			Conditions: rev.Status.Conditions,
			BundleMetadata: ocv1.BundleMetadata{
				Name:    rev.Annotations[labels.BundleNameKey],
				Version: rev.Annotations[labels.BundleVersionKey],
			},
		}

		if apimeta.IsStatusConditionTrue(rev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeSucceeded) {
			rs.Installed = rm
		} else {
			rs.RollingOut = append(rs.RollingOut, rm)
		}
	}

	return rs, nil
}

func MigrateStorage(m StorageMigrator) ReconcileStepFunc {
	return func(ctx context.Context, state *reconcileState, ext *ocv1.ClusterExtension) (*ctrl.Result, error) {
		objLbls := map[string]string{
			labels.OwnerKindKey: ocv1.ClusterExtensionKind,
			labels.OwnerNameKey: ext.GetName(),
		}

		if err := m.Migrate(ctx, ext, objLbls); err != nil {
			return nil, fmt.Errorf("migrating storage: %w", err)
		}
		return nil, nil
	}
}

func ApplyBundleWithBoxcutter(a Applier) ReconcileStepFunc {
	return func(ctx context.Context, state *reconcileState, ext *ocv1.ClusterExtension) (*ctrl.Result, error) {
		l := log.FromContext(ctx)
		storeLbls := map[string]string{
			labels.BundleNameKey:      state.resolvedRevisionMetadata.Name,
			labels.PackageNameKey:     state.resolvedRevisionMetadata.Package,
			labels.BundleVersionKey:   state.resolvedRevisionMetadata.Version,
			labels.BundleReferenceKey: state.resolvedRevisionMetadata.Image,
		}
		objLbls := map[string]string{
			labels.OwnerKindKey: ocv1.ClusterExtensionKind,
			labels.OwnerNameKey: ext.GetName(),
		}

		l.Info("applying bundle contents")
		if _, _, err := a.Apply(ctx, state.imageFS, ext, objLbls, storeLbls); err != nil {
			// If there was an error applying the resolved bundle,
			// report the error via the Progressing condition.
			setStatusProgressing(ext, wrapErrorWithResolutionInfo(state.resolvedRevisionMetadata.BundleMetadata, err))
			return nil, err
		}

		// Repopulate active revisions to avoid duplicates
		ext.Status.ActiveRevisions = nil

		// Mirror Available/Progressing conditions from the installed revision
		if i := state.revisionStates.Installed; i != nil {
			for _, cndType := range []string{ocv1.ClusterExtensionRevisionTypeAvailable, ocv1.ClusterExtensionRevisionTypeProgressing} {
				if cnd := apimeta.FindStatusCondition(i.Conditions, cndType); cnd != nil {
					apimeta.SetStatusCondition(&ext.Status.Conditions, *cnd)
				}
			}
			ext.Status.Install = &ocv1.ClusterExtensionInstallStatus{
				Bundle: i.BundleMetadata,
			}
			ext.Status.ActiveRevisions = []ocv1.RevisionStatus{{Name: i.RevName}}
		}
		for idx, r := range state.revisionStates.RollingOut {
			rs := ocv1.RevisionStatus{Name: r.RevName}
			// Mirror Progressing condition from the latest active revision
			if idx == len(state.revisionStates.RollingOut)-1 {
				pcnd := apimeta.FindStatusCondition(r.Conditions, ocv1.ClusterExtensionRevisionTypeProgressing)
				if pcnd != nil {
					apimeta.SetStatusCondition(&ext.Status.Conditions, *pcnd)
				}
				acnd := apimeta.FindStatusCondition(r.Conditions, ocv1.ClusterExtensionRevisionTypeAvailable)
				if acnd != nil && pcnd != nil && pcnd.Status == metav1.ConditionFalse && acnd.Status != metav1.ConditionTrue {
					apimeta.SetStatusCondition(&ext.Status.Conditions, *acnd)
				}
			}
			ext.Status.ActiveRevisions = append(ext.Status.ActiveRevisions, rs)
		}

		setInstalledStatusFromRevisionStates(ext, state.revisionStates)
		return nil, nil
	}
}
