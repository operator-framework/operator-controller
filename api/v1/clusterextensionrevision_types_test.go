/*
Copyright 2024.

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

package v1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestClusterExtensionRevisionTypes(t *testing.T) {
	// Test that we can create a ClusterExtensionRevision with all required fields
	revision := &ClusterExtensionRevision{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-extension-1.2.0",
		},
		Spec: ClusterExtensionRevisionSpec{
			ClusterExtensionRef: ClusterExtensionReference{
				Name: "test-extension",
			},
			Version: "1.2.0",
			BundleMetadata: BundleMetadata{
				Name:    "test-operator.v1.2.0",
				Version: "1.2.0",
			},
			AvailableSince: metav1.Now(),
			Approved:       false,
		},
		Status: ClusterExtensionRevisionStatus{
			Conditions: []metav1.Condition{
				{
					Type:   "Available",
					Status: metav1.ConditionTrue,
					Reason: "UpgradeDetected",
				},
			},
		},
	}

	// Verify the spec fields are accessible
	if revision.Spec.ClusterExtensionRef.Name != "test-extension" {
		t.Errorf("expected ClusterExtensionRef.Name to be 'test-extension', got %q", revision.Spec.ClusterExtensionRef.Name)
	}

	if revision.Spec.Version != "1.2.0" {
		t.Errorf("expected Version to be '1.2.0', got %q", revision.Spec.Version)
	}

}

func TestClusterExtensionRevisionList(t *testing.T) {
	// Test that we can create a ClusterExtensionRevisionList
	revisionList := &ClusterExtensionRevisionList{
		Items: []ClusterExtensionRevision{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-extension-1.2.0",
				},
				Spec: ClusterExtensionRevisionSpec{
					ClusterExtensionRef: ClusterExtensionReference{
						Name: "test-extension",
					},
					Version: "1.2.0",
					BundleMetadata: BundleMetadata{
						Name:    "test-operator.v1.2.0",
						Version: "1.2.0",
					},
					AvailableSince: metav1.Now(),
				},
			},
		},
	}

	if len(revisionList.Items) != 1 {
		t.Errorf("expected 1 item in list, got %d", len(revisionList.Items))
	}
}
