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

package updater

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	platformoperatorv1alpha1 "github.com/timflannagan/platform-operators/api/v1alpha1"
)

func NewPlatformOperatorUpdater(client client.Client) PlatformOperatorUpdater {
	return PlatformOperatorUpdater{
		client: client,
	}
}

type PlatformOperatorUpdater struct {
	client            client.Client
	updateStatusFuncs []UpdatePlatformOperatorStatusFunc
}

type UpdatePlatformOperatorStatusFunc func(PlatformOperator *platformoperatorv1alpha1.PlatformOperatorStatus) bool

func (u *PlatformOperatorUpdater) UpdateStatus(fs ...UpdatePlatformOperatorStatusFunc) {
	u.updateStatusFuncs = append(u.updateStatusFuncs, fs...)
}

func (u *PlatformOperatorUpdater) Apply(ctx context.Context, b *platformoperatorv1alpha1.PlatformOperator) error {
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

func EnsureCondition(condition metav1.Condition) UpdatePlatformOperatorStatusFunc {
	return func(status *platformoperatorv1alpha1.PlatformOperatorStatus) bool {
		existing := meta.FindStatusCondition(status.Conditions, condition.Type)
		if existing == nil || !conditionsSemanticallyEqual(*existing, condition) {
			meta.SetStatusCondition(&status.Conditions, condition)
			return true
		}
		return false
	}
}

func SetPhase(phase string) UpdatePlatformOperatorStatusFunc {
	return func(status *platformoperatorv1alpha1.PlatformOperatorStatus) bool {
		if status.Phase == phase {
			return false
		}
		status.Phase = phase
		return true
	}
}
