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
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
)

type BoxcutterRevisionStatesGetter struct {
	Reader client.Reader
}

func (d *BoxcutterRevisionStatesGetter) GetRevisionStates(ctx context.Context, ext *ocv1.ClusterExtension) (RevisionStates, error) {
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

	var rs RevisionStates
	for _, rev := range existingRevisionList.Items {
		if rev.Spec.LifecycleState == ocv1.ClusterExtensionRevisionLifecycleStateArchived {
			continue
		}

		// TODO: the setting of these annotations (happens in boxcutter applier when we pass in "revisionAnnotations")
		//   is fairly decoupled from this code where we get the annotations back out. We may want to co-locate
		//   the set/get logic a bit better to make it more maintainable and less likely to get out of sync.
		state := RevisionStateRollingOut
		if apimeta.IsStatusConditionTrue(rev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeReady) {
			state = RevisionStateInstalled
		}

		generation := int64(0)
		if genStr, ok := rev.Annotations[labels.ClusterExtensionGenerationKey]; ok {
			if gen, err := strconv.ParseInt(genStr, 10, 64); err == nil {
				generation = gen
			}
		}

		rm := &RevisionMetadata{
			RevisionName:               rev.Name,
			Package:                    rev.Annotations[labels.PackageNameKey],
			Image:                      rev.Annotations[labels.BundleReferenceKey],
			State:                      state,
			ClusterExtensionGeneration: generation,
			Conditions:                 rev.Status.Conditions,
			BundleMetadata: ocv1.BundleMetadata{
				Name:    rev.Annotations[labels.BundleNameKey],
				Version: rev.Annotations[labels.BundleVersionKey],
			},
		}

		rs = append(rs, rm)
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

func ApplyBundleWithBoxcutter(apply func(ctx context.Context, contentFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (bool, string, error)) ReconcileStepFunc {
	return func(ctx context.Context, state *reconcileState, ext *ocv1.ClusterExtension) (*ctrl.Result, error) {
		l := log.FromContext(ctx)
		revisionAnnotations := map[string]string{
			labels.BundleNameKey:                 state.resolvedRevisionMetadata.Name,
			labels.PackageNameKey:                state.resolvedRevisionMetadata.Package,
			labels.BundleVersionKey:              state.resolvedRevisionMetadata.Version,
			labels.BundleReferenceKey:            state.resolvedRevisionMetadata.Image,
			labels.ClusterExtensionGenerationKey: fmt.Sprintf("%d", ext.GetGeneration()),
		}
		objLbls := map[string]string{
			labels.OwnerKindKey: ocv1.ClusterExtensionKind,
			labels.OwnerNameKey: ext.GetName(),
		}

		l.Info("applying bundle contents")
		_, _, err := apply(ctx, state.imageFS, ext, objLbls, revisionAnnotations)
		if err != nil {
			// If there was an error applying the resolved bundle,
			// report the error via the Progressing condition.
			setStatusProgressing(ext, wrapErrorWithResolutionInfo(state.resolvedRevisionMetadata.BundleMetadata, err))
			// Only set Installed condition for retryable errors.
			// For terminal errors (Progressing: False with a terminal reason such as Blocked or InvalidConfiguration),
			// the Progressing condition already provides all necessary information about the failure.
			if !errors.Is(err, reconcile.TerminalError(nil)) {
				setInstalledStatusFromRevisionStates(ext, state.revisionStates)
			}
			return nil, err
		}

		ext.Status.ActiveRevisions = []ocv1.RevisionStatus{}
		// Mirror Ready condition from the installed revision
		if i := state.revisionStates.Installed(); i != nil {
			if cnd := apimeta.FindStatusCondition(i.Conditions, ocv1.ClusterExtensionRevisionTypeReady); cnd != nil {
				cnd.ObservedGeneration = ext.GetGeneration()
				apimeta.SetStatusCondition(&ext.Status.Conditions, *cnd)
			}
			ext.Status.Install = &ocv1.ClusterExtensionInstallStatus{
				Bundle: i.BundleMetadata,
			}
			ext.Status.ActiveRevisions = []ocv1.RevisionStatus{{Name: i.RevisionName}}
		}
		for idx, r := range state.revisionStates.RollingOut() {
			rs := ocv1.RevisionStatus{Name: r.RevisionName}
			if cnd := apimeta.FindStatusCondition(r.Conditions, ocv1.ClusterExtensionRevisionTypeReady); cnd != nil {
				cnd.ObservedGeneration = ext.GetGeneration()
				apimeta.SetStatusCondition(&rs.Conditions, *cnd)
			}
			// Mirror Ready condition from the latest active revision
			if idx == len(state.revisionStates.RollingOut())-1 {
				if rcnd := apimeta.FindStatusCondition(r.Conditions, ocv1.ClusterExtensionRevisionTypeReady); rcnd != nil {
					rcnd.ObservedGeneration = ext.GetGeneration()
					apimeta.SetStatusCondition(&ext.Status.Conditions, *rcnd)
				}
			}
			ext.Status.ActiveRevisions = append(ext.Status.ActiveRevisions, rs)
		}

		setInstalledStatusFromRevisionStates(ext, state.revisionStates)

		// Set Progressing condition based on revision states
		// If there's an installed revision and no rolling revisions, we've reached the desired state
		if state.revisionStates.Installed() != nil && len(state.revisionStates.RollingOut()) == 0 {
			setStatusProgressing(ext, nil)
		} else {
			// There are rolling revisions, so we're still progressing
			SetStatusCondition(&ext.Status.Conditions, metav1.Condition{
				Type:               ocv1.TypeProgressing,
				Status:             metav1.ConditionTrue,
				Reason:             ocv1.ReasonRollingOut,
				Message:            "Rolling out new revision",
				ObservedGeneration: ext.GetGeneration(),
			})
		}
		return nil, nil
	}
}
