package clusteroperator

import (
	"context"

	configv1 "github.com/openshift/api/config/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// From https://github.com/operator-framework/operator-lifecycle-manager/pkg/lib/operatorstatus/writer.go

// NewWriter returns a new instance of Writer.
func NewWriter(client client.Client) *Writer {
	return &Writer{
		Client: client,
	}
}

// Writer encapsulates logic for cluster operator object API. It is used to
// update ClusterOperator resource.
type Writer struct {
	client.Client
}

// UpdateStatus updates the clusteroperator object with the new status specified.
func (w *Writer) UpdateStatus(ctx context.Context, existingCO *configv1.ClusterOperator, newStatus configv1.ClusterOperatorStatus) error {
	if existingCO == nil {
		panic("BUG: existingCO parameter was nil")
	}

	existingStatus := existingCO.Status
	if equality.Semantic.DeepEqual(existingStatus, newStatus) {
		return nil
	}

	existingCO.Status = newStatus
	return w.Status().Update(ctx, existingCO)
}
