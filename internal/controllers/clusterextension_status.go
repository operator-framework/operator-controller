/*
Copyright 2023.

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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rukpakv1alpha2 "github.com/operator-framework/rukpak/api/v1alpha2"
	"github.com/operator-framework/rukpak/pkg/source"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

func updateStatusUnpackFailing(status *ocv1alpha1.ClusterExtensionStatus, err error) error {
	status.ResolvedBundle = nil
	status.InstalledBundle = nil
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:    rukpakv1alpha2.TypeUnpacked,
		Status:  metav1.ConditionFalse,
		Reason:  rukpakv1alpha2.ReasonUnpackFailed,
		Message: err.Error(),
	})
	return err
}

// TODO: verify if we need to update the installBundle status or leave it as is.
func updateStatusUnpackPending(status *ocv1alpha1.ClusterExtensionStatus, result *source.Result) {
	status.ResolvedBundle = nil
	status.InstalledBundle = nil
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:    rukpakv1alpha2.TypeUnpacked,
		Status:  metav1.ConditionFalse,
		Reason:  rukpakv1alpha2.ReasonUnpackPending,
		Message: result.Message,
	})
}

// TODO: verify if we need to update the installBundle status or leave it as is.
func updateStatusUnpacking(status *ocv1alpha1.ClusterExtensionStatus, result *source.Result) {
	status.ResolvedBundle = nil
	status.InstalledBundle = nil
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:    rukpakv1alpha2.TypeUnpacked,
		Status:  metav1.ConditionFalse,
		Reason:  rukpakv1alpha2.ReasonUnpacking,
		Message: result.Message,
	})
}

func updateStatusUnpacked(status *ocv1alpha1.ClusterExtensionStatus, result *source.Result) {
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:    rukpakv1alpha2.TypeUnpacked,
		Status:  metav1.ConditionTrue,
		Reason:  rukpakv1alpha2.ReasonUnpackSuccessful,
		Message: result.Message,
	})
}
