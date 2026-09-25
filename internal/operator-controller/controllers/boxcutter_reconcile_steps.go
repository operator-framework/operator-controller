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

func (d *BoxcutterRevisionStatesGetter) GetRevisionStates(ctx context.Context, ext *ocv1.ClusterExtension) (*RevisionStates, error) {
	// TODO: boxcutter applier has a nearly identical bit of code for listing and sorting revisions
	//   only difference here is that it sorts in reverse order to start iterating with the most
	//   recent revisions. We should consolidate to avoid code duplication.
	existingRevisionList := &ocv1.ClusterObjectSetList{}
	if err := d.Reader.List(ctx, existingRevisionList, client.MatchingLabels{
		labels.OwnerNameKey: ext.Name,
	}); err != nil {
		return nil, fmt.Errorf("listing revisions: %w", err)
	}
	slices.SortFunc(existingRevisionList.Items, func(a, b ocv1.ClusterObjectSet) int {
		return cmp.Compare(a.Spec.Revision, b.Spec.Revision)
	})

	rs := &RevisionStates{}
	for _, rev := range existingRevisionList.Items {
		if rev.Spec.LifecycleState == ocv1.ClusterObjectSetLifecycleStateArchived {
			continue
		}

		// TODO: the setting of these annotations (happens in boxcutter applier when we pass in "revisionAnnotations")
		//   is fairly decoupled from this code where we get the annotations back out. We may want to co-locate
		//   the set/get logic a bit better to make it more maintainable and less likely to get out of sync.
		rm := &RevisionMetadata{
			RevisionName: rev.Name,
			Package:      rev.Annotations[labels.PackageNameKey],
			Image:        rev.Annotations[labels.BundleReferenceKey],
			Conditions:   rev.Status.Conditions,
			BundleMetadata: ocv1.BundleMetadata{
				Name:    rev.Annotations[labels.BundleNameKey],
				Version: rev.Annotations[labels.BundleVersionKey],
			},
		}
		// Only set Release if the annotation key exists (to distinguish "not set" from "explicitly empty")
		if releaseValue, ok := rev.Annotations[labels.BundleReleaseKey]; ok {
			rm.Release = &releaseValue
		}

		// A revision is considered installed once it has been observed ready at
		// least once, recorded by status.completedAt.
		if !rev.Status.CompletedAt.IsZero() {
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

const ceAvailableConditionType = "Available"

// ceProgressingFromReady maps a revision's Ready condition reason onto the
// ClusterExtension's Progressing (status, reason).
func ceProgressingFromReady(ready metav1.Condition) (metav1.ConditionStatus, string) {
	switch ready.Reason {
	case ocv1.ClusterObjectSetReasonReady:
		return metav1.ConditionTrue, ocv1.ReasonSucceeded
	case ocv1.ClusterObjectSetReasonIncomplete:
		return metav1.ConditionTrue, ocv1.ReasonRollingOut
	case ocv1.ClusterObjectSetReasonProgressDeadlineExceeded:
		return metav1.ConditionFalse, ocv1.ReasonProgressDeadlineExceeded
	case ocv1.ClusterObjectSetReasonBlocked:
		return metav1.ConditionFalse, ocv1.ReasonBlocked
	case ocv1.ClusterObjectSetReasonInvalid:
		return metav1.ConditionFalse, ocv1.ReasonInvalidConfiguration
	default: // ReconcileError, TeardownError, InternalError
		return metav1.ConditionTrue, ocv1.ReasonRetrying
	}
}

// ceConditionsFromReady returns the ClusterExtension Available (Ready retyped)
// and Progressing (derived) conditions for a revision's Ready condition.
func ceConditionsFromReady(ready metav1.Condition, generation int64) (available, progressing metav1.Condition) {
	available = ready
	available.Type = ceAvailableConditionType
	available.ObservedGeneration = generation

	ps, pr := ceProgressingFromReady(ready)
	progressing = metav1.Condition{
		Type:               ocv1.TypeProgressing,
		Status:             ps,
		Reason:             pr,
		Message:            ready.Message,
		ObservedGeneration: generation,
	}
	return available, progressing
}

func ApplyBundleWithBoxcutter(apply func(ctx context.Context, contentFS fs.FS, ext *ocv1.ClusterExtension, objectLabels, revisionAnnotations map[string]string) (bool, string, error)) ReconcileStepFunc {
	return func(ctx context.Context, state *reconcileState, ext *ocv1.ClusterExtension) (*ctrl.Result, error) {
		l := log.FromContext(ctx)
		revisionAnnotations := map[string]string{
			labels.BundleNameKey:      state.resolvedRevisionMetadata.Name,
			labels.PackageNameKey:     state.resolvedRevisionMetadata.Package,
			labels.BundleVersionKey:   state.resolvedRevisionMetadata.Version,
			labels.BundleReferenceKey: state.resolvedRevisionMetadata.Image,
		}
		if state.resolvedRevisionMetadata.Release != nil {
			revisionAnnotations[labels.BundleReleaseKey] = *state.resolvedRevisionMetadata.Release
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
		gen := ext.GetGeneration()
		if i := state.revisionStates.Installed; i != nil {
			if ready := apimeta.FindStatusCondition(i.Conditions, ocv1.ClusterObjectSetTypeReady); ready != nil {
				rs := ocv1.RevisionStatus{Name: i.RevisionName}
				r := *ready
				r.ObservedGeneration = gen
				apimeta.SetStatusCondition(&rs.Conditions, r)

				avail, prog := ceConditionsFromReady(*ready, gen)
				apimeta.SetStatusCondition(&ext.Status.Conditions, avail)
				if ready.Reason != ocv1.ClusterObjectSetReasonArchived {
					apimeta.SetStatusCondition(&ext.Status.Conditions, prog)
				}

				ext.Status.Install = &ocv1.ClusterExtensionInstallStatus{Bundle: i.BundleMetadata}
				ext.Status.ActiveRevisions = []ocv1.RevisionStatus{rs}
			} else {
				ext.Status.Install = &ocv1.ClusterExtensionInstallStatus{Bundle: i.BundleMetadata}
				ext.Status.ActiveRevisions = []ocv1.RevisionStatus{{Name: i.RevisionName}}
			}
		}
		for idx, rr := range state.revisionStates.RollingOut {
			rs := ocv1.RevisionStatus{Name: rr.RevisionName}
			if ready := apimeta.FindStatusCondition(rr.Conditions, ocv1.ClusterObjectSetTypeReady); ready != nil {
				r := *ready
				r.ObservedGeneration = gen
				apimeta.SetStatusCondition(&rs.Conditions, r)
				// The latest rolling revision drives the ClusterExtension Progressing condition.
				if idx == len(state.revisionStates.RollingOut)-1 {
					_, prog := ceConditionsFromReady(*ready, gen)
					if ready.Reason != ocv1.ClusterObjectSetReasonArchived {
						apimeta.SetStatusCondition(&ext.Status.Conditions, prog)
					}
				}
			}
			ext.Status.ActiveRevisions = append(ext.Status.ActiveRevisions, rs)
		}

		setInstalledStatusFromRevisionStates(ext, state.revisionStates)
		return nil, nil
	}
}
