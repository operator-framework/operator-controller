/*
Copyright 2020 The Operator-SDK Authors.

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

package bundle

import (
	"context"
	"reflect"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"github.com/operator-framework/rukpak/internal/updater"
)

func NewBundleUpdater(client client.Client) Updater {
	return Updater{
		client: client,
	}
}

type Updater struct {
	client            client.Client
	updateStatusFuncs []UpdateStatusFunc
}

type UpdateStatusFunc func(bundle *rukpakv1alpha1.BundleStatus) bool

func (u *Updater) UpdateStatus(fs ...UpdateStatusFunc) {
	u.updateStatusFuncs = append(u.updateStatusFuncs, fs...)
}

func (u *Updater) Apply(ctx context.Context, b *rukpakv1alpha1.Bundle) error {
	backoff := retry.DefaultRetry

	return retry.RetryOnConflict(backoff, func() error {
		if err := u.client.Get(ctx, client.ObjectKeyFromObject(b), b); err != nil {
			return err
		}
		needsStatusUpdate := false
		for _, f := range u.updateStatusFuncs {
			needsStatusUpdate = f(&b.Status) || needsStatusUpdate
		}
		if needsStatusUpdate {
			log.FromContext(ctx).Info("applying status changes")
			return u.client.Status().Update(ctx, b)
		}
		return nil
	})
}

func EnsureCondition(condition metav1.Condition) UpdateStatusFunc {
	return func(status *rukpakv1alpha1.BundleStatus) bool {
		existing := meta.FindStatusCondition(status.Conditions, condition.Type)
		if existing == nil || !updater.ConditionsSemanticallyEqual(*existing, condition) {
			meta.SetStatusCondition(&status.Conditions, condition)
			return true
		}
		return false
	}
}

func EnsureObservedGeneration(observedGeneration int64) UpdateStatusFunc {
	return func(status *rukpakv1alpha1.BundleStatus) bool {
		if status.ObservedGeneration == observedGeneration {
			return false
		}
		status.ObservedGeneration = observedGeneration
		return true
	}
}

func EnsureResolvedSource(resolvedSource *rukpakv1alpha1.BundleSource) UpdateStatusFunc {
	return func(status *rukpakv1alpha1.BundleStatus) bool {
		if reflect.DeepEqual(status.ResolvedSource, resolvedSource) {
			return false
		}
		status.ResolvedSource = resolvedSource
		return true
	}
}

func EnsureContentURL(url string) UpdateStatusFunc {
	return func(status *rukpakv1alpha1.BundleStatus) bool {
		if status.ContentURL == url {
			return false
		}
		status.ContentURL = url
		return true
	}
}

func SetPhase(phase string) UpdateStatusFunc {
	return func(status *rukpakv1alpha1.BundleStatus) bool {
		if status.Phase == phase {
			return false
		}
		status.Phase = phase
		return true
	}
}
