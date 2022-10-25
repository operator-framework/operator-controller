package clusteroperator

import (
	configv1 "github.com/openshift/api/config/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// From https://github.com/operator-framework/operator-lifecycle-manager/pkg/lib/operatorstatus/builder.go
// Note: the clock api-machinery library in that package is now deprecated, so it has been removed here.

// NewBuilder returns a builder for ClusterOperatorStatus.
func NewBuilder() *Builder {
	return &Builder{
		status: &configv1.ClusterOperatorStatus{
			Conditions:     []configv1.ClusterOperatorStatusCondition{},
			Versions:       []configv1.OperandVersion{},
			RelatedObjects: []configv1.ObjectReference{},
		},
	}
}

// Builder helps build ClusterOperatorStatus with appropriate
// ClusterOperatorStatusCondition and OperandVersion.
type Builder struct {
	status *configv1.ClusterOperatorStatus
}

// GetStatus returns the ClusterOperatorStatus built.
func (b *Builder) GetStatus() configv1.ClusterOperatorStatus {
	return *b.status
}

// WithProgressing sets an OperatorProgressing type condition.
func (b *Builder) WithProgressing(status metav1.ConditionStatus, reason, message string) *Builder {
	return b.withCondition(string(configv1.OperatorProgressing), status, reason, message)
}

// WithDegraded sets an OperatorDegraded type condition.
func (b *Builder) WithDegraded(status metav1.ConditionStatus, reason, message string) *Builder {
	return b.withCondition(string(configv1.OperatorDegraded), status, reason, message)
}

// WithAvailable sets an OperatorAvailable type condition.
func (b *Builder) WithAvailable(status metav1.ConditionStatus, reason, message string) *Builder {
	return b.withCondition(string(configv1.OperatorAvailable), status, reason, message)
}

// WithUpgradeable sets an OperatorUpgradeable type condition.
func (b *Builder) WithUpgradeable(status metav1.ConditionStatus, reason, message string) *Builder {
	return b.withCondition(string(configv1.OperatorUpgradeable), status, reason, message)
}

func (b *Builder) withCondition(typ string, status metav1.ConditionStatus, reason, message string) *Builder {
	conditions := convertToMetaV1Conditions(b.status.Conditions)
	meta.SetStatusCondition(&conditions, metav1.Condition{
		Type:    typ,
		Status:  status,
		Reason:  reason,
		Message: message,
	})
	b.status.Conditions = convertToClusterOperatorConditions(conditions)

	return b
}

func convertToMetaV1Conditions(in []configv1.ClusterOperatorStatusCondition) []metav1.Condition {
	out := make([]metav1.Condition, 0, len(in))
	for _, c := range in {
		out = append(out, metav1.Condition{
			Type:               string(c.Type),
			Status:             metav1.ConditionStatus(c.Status),
			Reason:             c.Reason,
			Message:            c.Message,
			LastTransitionTime: c.LastTransitionTime,
		})
	}
	return out
}

func convertToClusterOperatorConditions(in []metav1.Condition) []configv1.ClusterOperatorStatusCondition {
	out := make([]configv1.ClusterOperatorStatusCondition, 0, len(in))
	for _, c := range in {
		out = append(out, configv1.ClusterOperatorStatusCondition{
			Type:               configv1.ClusterStatusConditionType(c.Type),
			Status:             configv1.ConditionStatus(c.Status),
			Reason:             c.Reason,
			Message:            c.Message,
			LastTransitionTime: c.LastTransitionTime,
		})
	}
	return out
}

// WithVersion adds the specific version into the status.
func (b *Builder) WithVersion(name, version string) *Builder {
	for i := range b.status.Versions {
		if b.status.Versions[i].Name == name {
			b.status.Versions[i].Version = version
			return b
		}
	}
	b.status.Versions = append(b.status.Versions, configv1.OperandVersion{
		Name:    name,
		Version: version,
	})
	return b
}

// WithoutVersion removes the specified version from the existing status.
func (b *Builder) WithoutVersion(name string) *Builder {
	out := b.status.Versions[:0]
	for i, v := range b.status.Versions {
		if v.Name == name {
			continue
		}
		out[i] = v
	}
	b.status.Versions = out
	return b
}

// WithRelatedObject adds the reference specified to the RelatedObjects list.
func (b *Builder) WithRelatedObject(reference configv1.ObjectReference) *Builder {
	for i := range b.status.RelatedObjects {
		if equality.Semantic.DeepEqual(b.status.RelatedObjects[i], reference) {
			return b
		}
	}
	b.status.RelatedObjects = append(b.status.RelatedObjects, reference)
	return b
}

// WithoutRelatedObject removes the reference specified from the RelatedObjects list.
func (b *Builder) WithoutRelatedObject(reference configv1.ObjectReference) *Builder {
	related := b.status.RelatedObjects[:0]
	for i, ro := range b.status.RelatedObjects {
		if equality.Semantic.DeepEqual(b.status.RelatedObjects[i], reference) {
			continue
		}
		related[i] = ro
	}
	b.status.RelatedObjects = related
	return b
}
