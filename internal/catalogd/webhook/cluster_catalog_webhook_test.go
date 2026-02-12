package webhook

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

func TestClusterCatalogDefaulting(t *testing.T) {
	tests := map[string]struct {
		clusterCatalog *ocv1.ClusterCatalog
		expectedLabels map[string]string
	}{
		"no labels provided, name label added": {
			clusterCatalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-catalog",
				},
			},
			expectedLabels: map[string]string{
				"olm.operatorframework.io/metadata.name": "test-catalog",
			},
		},
		"labels already present, name label added": {
			clusterCatalog: &ocv1.ClusterCatalog{
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
		},
		"name label already present, no changes": {
			clusterCatalog: &ocv1.ClusterCatalog{
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
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// Arrange
			clusterCatalogWrapper := &ClusterCatalog{}

			// Act - with type-safe API, no type assertion errors are possible
			err := clusterCatalogWrapper.Default(context.TODO(), tc.clusterCatalog)

			// Assert
			require.NoError(t, err)
			if tc.expectedLabels != nil {
				assert.Equal(t, tc.expectedLabels, tc.clusterCatalog.Labels)
			}
		})
	}
}
