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
	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	rukpakapi "github.com/operator-framework/operator-controller/internal/rukpak/api"
	"github.com/operator-framework/operator-controller/internal/rukpak/source"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func updateStatusUnpackFailing(status *ocv1alpha1.ClusterExtensionStatus, err error) error {
	status.ResolvedBundle = nil
	status.InstalledBundle = nil
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:    rukpakapi.TypeUnpacked,
		Status:  metav1.ConditionFalse,
		Reason:  rukpakapi.ReasonUnpackFailed,
		Message: err.Error(),
	})
	return err
}

// TODO: verify if we need to update the installBundle status or leave it as is.
func updateStatusUnpackPending(status *ocv1alpha1.ClusterExtensionStatus, result *source.Result) {
	status.ResolvedBundle = nil
	status.InstalledBundle = nil
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:    rukpakapi.TypeUnpacked,
		Status:  metav1.ConditionFalse,
		Reason:  rukpakapi.ReasonUnpackPending,
		Message: result.Message,
	})
}

// TODO: verify if we need to update the installBundle status or leave it as is.
func updateStatusUnpacking(status *ocv1alpha1.ClusterExtensionStatus, result *source.Result) {
	status.ResolvedBundle = nil
	status.InstalledBundle = nil
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:    rukpakapi.TypeUnpacked,
		Status:  metav1.ConditionFalse,
		Reason:  rukpakapi.ReasonUnpacking,
		Message: result.Message,
	})
}

func updateStatusUnpacked(status *ocv1alpha1.ClusterExtensionStatus, result *source.Result, contentURL string) {
	// TODO: Expose content URL through CE status.
	status.ResolvedBundle = &ocv1alpha1.BundleMetadata{
		Name: result.ResolvedSource.Image.Ref,
	}
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:    rukpakapi.TypeUnpacked,
		Status:  metav1.ConditionTrue,
		Reason:  rukpakapi.ReasonUnpackSuccessful,
		Message: result.Message,
	})
}
