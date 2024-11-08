package webhook

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	catalogdv1 "github.com/operator-framework/catalogd/api/v1"
)

// Define a dummy struct that implements runtime.Object but isn't a ClusterCatalog
type NotClusterCatalog struct {
	metav1.TypeMeta
	metav1.ObjectMeta
}

func (n *NotClusterCatalog) DeepCopyObject() runtime.Object {
	return &NotClusterCatalog{}
}

func TestClusterCatalogDefaulting(t *testing.T) {
	tests := map[string]struct {
		clusterCatalog runtime.Object
		expectedLabels map[string]string
		expectError    bool
		errorMessage   string
	}{
		"no labels provided, name label added": {
			clusterCatalog: &catalogdv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-catalog",
				},
			},
			expectedLabels: map[string]string{
				"olm.operatorframework.io/metadata.name": "test-catalog",
			},
			expectError: false,
		},
		"labels already present, name label added": {
			clusterCatalog: &catalogdv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-catalog",
					Labels: map[string]string{
						"existing": "label",
					},
				},
			},
			expectedLabels: map[string]string{
				"olm.operatorframework.io/metadata.name": "test-catalog",
				"existing":                               "label",
			},
			expectError: false,
		},
		"name label already present, no changes": {
			clusterCatalog: &catalogdv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-catalog",
					Labels: map[string]string{
						"olm.operatorframework.io/metadata.name": "existing-name",
					},
				},
			},
			expectedLabels: map[string]string{
				"olm.operatorframework.io/metadata.name": "test-catalog", // Defaulting should still override this to match the object name
			},
			expectError: false,
		},
		"invalid object type, expect error": {
			clusterCatalog: &NotClusterCatalog{
				TypeMeta: metav1.TypeMeta{
					Kind:       "NotClusterCatalog",
					APIVersion: "v1",
				},
			},
			expectedLabels: nil,
			expectError:    true,
			errorMessage:   "expected a ClusterCatalog but got a *webhook.NotClusterCatalog",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// Arrange
			clusterCatalogWrapper := &ClusterCatalog{}

			// Act
			err := clusterCatalogWrapper.Default(context.TODO(), tc.clusterCatalog)

			// Assert
			if tc.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tc.errorMessage)
			} else {
				assert.NoError(t, err)
				if tc.expectedLabels != nil {
					labels := tc.clusterCatalog.(*catalogdv1.ClusterCatalog).Labels
					assert.Equal(t, tc.expectedLabels, labels)
				}
			}
		})
	}
}
