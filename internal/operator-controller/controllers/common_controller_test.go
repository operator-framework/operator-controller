package controllers

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

func TestSetStatusProgressing(t *testing.T) {
	for _, tc := range []struct {
		name             string
		err              error
		clusterExtension *ocv1.ClusterExtension
		expected         metav1.Condition
	}{
		{
			name:             "non-nil ClusterExtension, nil error, Progressing condition has status True with reason Success",
			err:              nil,
			clusterExtension: &ocv1.ClusterExtension{},
			expected: metav1.Condition{
				Type:    ocv1.TypeProgressing,
				Status:  metav1.ConditionTrue,
				Reason:  ocv1.ReasonSucceeded,
				Message: "desired state reached",
			},
		},
		{
			name:             "non-nil ClusterExtension, non-terminal error, Progressing condition has status True with reason Retrying",
			err:              errors.New("boom"),
			clusterExtension: &ocv1.ClusterExtension{},
			expected: metav1.Condition{
				Type:    ocv1.TypeProgressing,
				Status:  metav1.ConditionTrue,
				Reason:  ocv1.ReasonRetrying,
				Message: "boom",
			},
		},
		{
			name:             "non-nil ClusterExtension, terminal error, Progressing condition has status False with reason Blocked",
			err:              reconcile.TerminalError(errors.New("boom")),
			clusterExtension: &ocv1.ClusterExtension{},
			expected: metav1.Condition{
				Type:    ocv1.TypeProgressing,
				Status:  metav1.ConditionFalse,
				Reason:  ocv1.ReasonBlocked,
				Message: "terminal error: boom",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setStatusProgressing(tc.clusterExtension, tc.err)
			progressingCond := meta.FindStatusCondition(tc.clusterExtension.Status.Conditions, ocv1.TypeProgressing)
			require.NotNil(t, progressingCond, "progressing condition should be set but was not")
			diff := cmp.Diff(*progressingCond, tc.expected, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration"))
			require.Empty(t, diff, "difference between actual and expected Progressing conditions")
		})
	}
}

func TestTruncateMessage(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		expected string
	}{
		{
			name:     "short message unchanged",
			message:  "This is a short message",
			expected: "This is a short message",
		},
		{
			name:     "empty message unchanged",
			message:  "",
			expected: "",
		},
		{
			name:     "exact max length message unchanged",
			message:  strings.Repeat("a", maxConditionMessageLength),
			expected: strings.Repeat("a", maxConditionMessageLength),
		},
		{
			name:     "message just over limit gets truncated",
			message:  strings.Repeat("a", maxConditionMessageLength+1),
			expected: strings.Repeat("a", maxConditionMessageLength-len(truncationSuffix)) + truncationSuffix,
		},
		{
			name:     "very long message gets truncated",
			message:  strings.Repeat("word ", 10000) + "finalword",
			expected: strings.Repeat("word ", 10000)[:maxConditionMessageLength-len(truncationSuffix)] + truncationSuffix,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := truncateMessage(tc.message)
			require.Equal(t, tc.expected, result)

			// Verify the result is within the limit
			require.LessOrEqual(t, len(result), maxConditionMessageLength,
				"truncated message should not exceed max length")

			// If the original message was over the limit, verify truncation occurred
			if len(tc.message) > maxConditionMessageLength {
				require.Contains(t, result, truncationSuffix,
					"long messages should contain truncation suffix")
				require.Less(t, len(result), len(tc.message),
					"truncated message should be shorter than original")
			}
		})
	}
}

func TestSetStatusProgressingWithLongMessage(t *testing.T) {
	// Simulate a real ClusterExtension CRD upgrade safety check failure with many validation errors
	longError := fmt.Sprintf("validating CRD upgrade safety for ClusterExtension 'my-operator': %s",
		strings.Repeat("CRD \"myresources.example.com\" v1beta1->v1: field .spec.replicas changed from optional to required, field .spec.config.timeout type changed from string to integer, field .status.conditions[].observedGeneration removed\n", 500))

	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "my-operator"}}
	err := errors.New(longError)
	setStatusProgressing(ext, err)

	cond := meta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeProgressing)
	require.NotNil(t, cond)
	require.LessOrEqual(t, len(cond.Message), maxConditionMessageLength)
	require.Contains(t, cond.Message, truncationSuffix)
	require.Contains(t, cond.Message, "validating CRD upgrade safety")
}

func TestClusterExtensionDeprecationMessageTruncation(t *testing.T) {
	// Test truncation for ClusterExtension deprecation warnings with many deprecated APIs
	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "legacy-operator"}}

	// Simulate many deprecation warnings that would overflow the message limit
	deprecationMessages := []string{}
	for i := 0; i < 1000; i++ {
		deprecationMessages = append(deprecationMessages, fmt.Sprintf("API version 'v1beta1' of resource 'customresources%d.example.com' is deprecated, use 'v1' instead", i))
	}

	longDeprecationMsg := strings.Join(deprecationMessages, "; ")
	setInstalledStatusConditionUnknown(ext, longDeprecationMsg)

	cond := meta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeInstalled)
	require.NotNil(t, cond)
	require.LessOrEqual(t, len(cond.Message), maxConditionMessageLength)
	require.Contains(t, cond.Message, truncationSuffix, "deprecation messages should be truncated when too long")
	require.Contains(t, cond.Message, "API version", "should preserve important deprecation context")
}

func TestClusterExtensionInstallationFailureTruncation(t *testing.T) {
	// Test truncation for ClusterExtension installation failures with many bundle validation errors
	installError := "failed to install ClusterExtension 'argocd-operator': bundle validation errors: " +
		strings.Repeat("resource 'deployments/argocd-server' missing required label 'app.kubernetes.io/name', resource 'services/argocd-server-metrics' has invalid port configuration, resource 'configmaps/argocd-cm' contains invalid YAML in data field 'application.yaml'\n", 400)

	ext := &ocv1.ClusterExtension{ObjectMeta: metav1.ObjectMeta{Name: "argocd-operator"}}
	setInstalledStatusConditionFailed(ext, installError)

	cond := meta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeInstalled)
	require.NotNil(t, cond)

	// Verify message was truncated due to length
	require.LessOrEqual(t, len(cond.Message), maxConditionMessageLength)
	require.Contains(t, cond.Message, truncationSuffix, "installation failure messages should be truncated when too long")
	require.Contains(t, cond.Message, "failed to install ClusterExtension", "should preserve important context")
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, ocv1.ReasonFailed, cond.Reason)

	// Verify original message was actually longer than the limit
	require.Greater(t, len(installError), maxConditionMessageLength, "test should use a message that exceeds the limit")
}

func TestSetStatusConditionWrapper(t *testing.T) {
	tests := []struct {
		name              string
		message           string
		expectedTruncated bool
	}{
		{
			name:              "short message not truncated",
			message:           "This is a short message",
			expectedTruncated: false,
		},
		{
			name:              "long message gets truncated",
			message:           strings.Repeat("This is a very long message. ", 2000),
			expectedTruncated: true,
		},
		{
			name:              "message at exact limit not truncated",
			message:           strings.Repeat("a", maxConditionMessageLength),
			expectedTruncated: false,
		},
		{
			name:              "message over limit gets truncated",
			message:           strings.Repeat("a", maxConditionMessageLength+1),
			expectedTruncated: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var conditions []metav1.Condition

			// Use our wrapper function
			SetStatusCondition(&conditions, metav1.Condition{
				Type:    "TestCondition",
				Status:  metav1.ConditionTrue,
				Reason:  "Testing",
				Message: tc.message,
			})

			require.Len(t, conditions, 1, "should have exactly one condition")
			cond := conditions[0]

			// Verify message is within limits
			require.LessOrEqual(t, len(cond.Message), maxConditionMessageLength,
				"condition message should not exceed max length")

			// Check if truncation occurred as expected
			if tc.expectedTruncated {
				require.Contains(t, cond.Message, truncationSuffix,
					"long messages should contain truncation suffix")
				require.Less(t, len(cond.Message), len(tc.message),
					"truncated message should be shorter than original")
			} else {
				require.Equal(t, tc.message, cond.Message,
					"short messages should remain unchanged")
				require.NotContains(t, cond.Message, truncationSuffix,
					"short messages should not contain truncation suffix")
			}
		})
	}
}
