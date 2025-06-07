package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	olmSystemNamespace      = "olmv1-system"
	minJustificationLength  = 40
	catalogdManagerSelector = "control-plane=catalogd-controller-manager"
	operatorManagerSelector = "control-plane=operator-controller-controller-manager"
)

type PortWithJustification struct {
	Port          []networkingv1.NetworkPolicyPort
	Justification string
}

// IngressRule defines a k8s IngressRule, along with a justification.
type IngressRule struct {
	Ports []PortWithJustification
	From  []networkingv1.NetworkPolicyPeer
}

// EgressRule defines a k8s EgressRule, along with a justification.
type EgressRule struct {
	Ports []PortWithJustification
	To    []networkingv1.NetworkPolicyPeer
}

// AllowedPolicyDefinition defines the expected structure and justifications for a NetworkPolicy.
type AllowedPolicyDefinition struct {
	Selector                    metav1.LabelSelector
	PolicyTypes                 []networkingv1.PolicyType
	IngressRule                 IngressRule
	EgressRule                  EgressRule
	DenyAllIngressJustification string // Justification if Ingress is in PolicyTypes and IngressRules is empty
	DenyAllEgressJustification  string // Justification if Egress is in PolicyTypes and EgressRules is empty
}

// Ref: https://docs.google.com/document/d/1bHEEWzA65u-kjJFQRUY1iBuMIIM1HbPy4MeDLX4NI3o/edit?usp=sharing
var allowedNetworkPolicies = map[string]AllowedPolicyDefinition{
	"catalogd-controller-manager": {
		Selector:    metav1.LabelSelector{MatchLabels: map[string]string{"control-plane": "catalogd-controller-manager"}},
		PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
		IngressRule: IngressRule{
			Ports: []PortWithJustification{
				{
					Port:          []networkingv1.NetworkPolicyPort{{Protocol: ptr.To(corev1.ProtocolTCP), Port: intOrStrPtr(8443)}},
					Justification: "Allows Prometheus to scrape metrics from catalogd, which is essential for monitoring its performance and health.",
				},
				{
					Port:          []networkingv1.NetworkPolicyPort{{Protocol: ptr.To(corev1.ProtocolTCP), Port: intOrStrPtr(9443)}},
					Justification: "Permits Kubernetes API server to reach catalogd's mutating admission webhook, ensuring integrity of catalog resources.",
				},
				{
					Port:          []networkingv1.NetworkPolicyPort{{Protocol: ptr.To(corev1.ProtocolTCP), Port: intOrStrPtr(7443)}},
					Justification: "Enables clients (eg. operator-controller) to query catalog metadata from catalogd, which is a core function for bundle resolution and operator discovery.",
				},
			},
		},
		EgressRule: EgressRule{
			Ports: []PortWithJustification{
				{
					Port:          nil, // Empty Ports means allow all egress
					Justification: "Permits catalogd to fetch catalog images from arbitrary container registries and communicate with the Kubernetes API server for its operational needs.",
				},
			},
		},
	},
	"operator-controller-controller-manager": {
		Selector:    metav1.LabelSelector{MatchLabels: map[string]string{"control-plane": "operator-controller-controller-manager"}},
		PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
		IngressRule: IngressRule{
			Ports: []PortWithJustification{
				{
					Port:          []networkingv1.NetworkPolicyPort{{Protocol: ptr.To(corev1.ProtocolTCP), Port: intOrStrPtr(8443)}},
					Justification: "Allows Prometheus to scrape metrics from operator-controller, which is crucial for monitoring its activity, reconciliations, and overall health.",
				},
			},
		},
		EgressRule: EgressRule{
			Ports: []PortWithJustification{
				{
					Port:          nil, // Empty Ports means allow all egress
					Justification: "Enables operator-controller to pull bundle images from arbitrary image registries, connect to catalogd's HTTPS server for metadata, and interact with the Kubernetes API server.",
				},
			},
		},
	},
	"default-deny-all-traffic": {
		Selector:    metav1.LabelSelector{}, // Empty selector, matches all pods
		PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
		// No IngressRules means deny all ingress if PolicyTypeIngress is present
		// No EgressRules means deny all egress if PolicyTypeEgress is present
		DenyAllIngressJustification: "Denies all ingress traffic to pods selected by this policy by default, unless explicitly allowed by other policy rules, ensuring a baseline secure posture.",
		DenyAllEgressJustification:  "Denies all egress traffic from pods selected by this policy by default, unless explicitly allowed by other policy rules, minimizing potential exfiltration paths.",
	},
}

func TestNetworkPolicyJustifications(t *testing.T) {
	ctx := context.Background()

	// Validate justifications have min length in the allowedNetworkPolicies definition
	for name, policyDef := range allowedNetworkPolicies {
		for i, pwj := range policyDef.IngressRule.Ports {
			assert.GreaterOrEqualf(t, len(pwj.Justification), minJustificationLength,
				"Justification for ingress PortWithJustification entry %d in policy %q is too short: %q", i, name, pwj.Justification)
		}
		for i, pwj := range policyDef.EgressRule.Ports { // Corrected variable name from 'rule' to 'pwj'
			assert.GreaterOrEqualf(t, len(pwj.Justification), minJustificationLength,
				"Justification for egress PortWithJustification entry %d in policy %q is too short: %q", i, name, pwj.Justification)
		}
		if policyDef.DenyAllIngressJustification != "" {
			assert.GreaterOrEqualf(t, len(policyDef.DenyAllIngressJustification), minJustificationLength,
				"DenyAllIngressJustification for policy %q is too short: %q", name, policyDef.DenyAllIngressJustification)
		}
		if policyDef.DenyAllEgressJustification != "" {
			assert.GreaterOrEqualf(t, len(policyDef.DenyAllEgressJustification), minJustificationLength,
				"DenyAllEgressJustification for policy %q is too short: %q", name, policyDef.DenyAllEgressJustification)
		}
	}

	clusterPolicies := &networkingv1.NetworkPolicyList{}
	err := c.List(ctx, clusterPolicies, client.InNamespace(olmSystemNamespace))
	require.NoError(t, err, "Failed to list NetworkPolicies in namespace %q", olmSystemNamespace)

	validatedRegistryPolicies := make(map[string]bool)

	for _, policy := range clusterPolicies.Items {
		t.Run(fmt.Sprintf("Policy_%s", strings.ReplaceAll(policy.Name, "-", "_")), func(t *testing.T) {
			expectedPolicy, found := allowedNetworkPolicies[policy.Name]
			require.Truef(t, found, "NetworkPolicy %q found in cluster but not in allowed registry. Namespace: %s", policy.Name, policy.Namespace)
			validatedRegistryPolicies[policy.Name] = true

			// 1. Compare PodSelector
			assert.True(t, equality.Semantic.DeepEqual(expectedPolicy.Selector, policy.Spec.PodSelector),
				"PodSelector mismatch for policy %q. Expected: %+v, Got: %+v", policy.Name, expectedPolicy.Selector, policy.Spec.PodSelector)

			// 2. Compare PolicyTypes
			require.ElementsMatchf(t, expectedPolicy.PolicyTypes, policy.Spec.PolicyTypes,
				"PolicyTypes mismatch for policy %q.", policy.Name)

			// 3. Validate Ingress Rules
			hasIngressPolicyType := false
			for _, pt := range policy.Spec.PolicyTypes {
				if pt == networkingv1.PolicyTypeIngress {
					hasIngressPolicyType = true
					break
				}
			}

			if hasIngressPolicyType {
				switch len(policy.Spec.Ingress) {
				case 0:
					validateDenyAllIngress(t, policy.Name, expectedPolicy)
				case 1:
					validateSingleIngressRule(t, policy.Name, policy.Spec.Ingress[0], expectedPolicy)
				default:
					assert.Failf(t, "Policy %q in cluster has %d ingress rules. Allowed definition supports at most 1 explicit ingress rule.", policy.Name, len(policy.Spec.Ingress))
				}
			} else {
				validateNoIngress(t, policy.Name, policy, expectedPolicy)
			}

			// 4. Validate Egress Rules
			hasEgressPolicyType := false
			for _, pt := range policy.Spec.PolicyTypes {
				if pt == networkingv1.PolicyTypeEgress {
					hasEgressPolicyType = true
					break
				}
			}

			if hasEgressPolicyType {
				switch len(policy.Spec.Egress) {
				case 0:
					validateDenyAllEgress(t, policy.Name, expectedPolicy)
				case 1:
					validateSingleEgressRule(t, policy.Name, policy.Spec.Egress[0], expectedPolicy)
				default:
					assert.Failf(t, "Policy %q in cluster has %d egress rules. Allowed definition supports at most 1 explicit egress rule.", policy.Name, len(policy.Spec.Egress))
				}
			} else {
				validateNoEgress(t, policy, expectedPolicy)
			}
		})
	}

	// 5. Ensure all policies in the registry were found in the cluster
	assert.Equal(t, len(allowedNetworkPolicies), len(validatedRegistryPolicies),
		"Mismatch between number of expected policies in registry (%d) and number of policies found & validated in cluster (%d). Missing policies from registry: %v", len(allowedNetworkPolicies), len(validatedRegistryPolicies), missingPolicies(allowedNetworkPolicies, validatedRegistryPolicies))
}

// Helper function to create a pointer to intstr.IntOrString from an int
func intOrStrPtr(port int32) *intstr.IntOrString {
	val := intstr.FromInt(int(port))
	return &val
}

func missingPolicies(expected map[string]AllowedPolicyDefinition, actual map[string]bool) []string {
	missing := []string{}
	for k := range expected {
		if !actual[k] {
			missing = append(missing, k)
		}
	}
	return missing
}

// validateNoEgress confirms that a policy which does not have spec.PolicyType=Egress specified
// has no corresponding egress rules or expectations defined.
func validateNoEgress(t *testing.T, policy networkingv1.NetworkPolicy, expectedPolicy AllowedPolicyDefinition) {
	// Policy is NOT expected to affect Egress traffic (no Egress in PolicyTypes)
	// Expected: Cluster has no egress rules; Registry has no DenyAllEgressJustification and empty EgressRule.
	require.Emptyf(t, policy.Spec.Egress,
		"Policy %q: Cluster does not have Egress PolicyType, but has Egress rules defined.", policy.Name)
	require.Emptyf(t, expectedPolicy.DenyAllEgressJustification,
		"Policy %q: Cluster does not have Egress PolicyType. Registry's DenyAllEgressJustification is not empty.", policy.Name)
	require.Emptyf(t, expectedPolicy.EgressRule.Ports,
		"Policy %q: Cluster does not have Egress PolicyType. Registry's EgressRule.Ports is not empty.", policy.Name)
	require.Emptyf(t, expectedPolicy.EgressRule.To,
		"Policy %q: Cluster does not have Egress PolicyType. Registry's EgressRule.To is not empty.", policy.Name)
}

// validateDenyAllEgress confirms that a policy with Egress PolicyType but no explicit rules
// correctly corresponds to a "deny all" expectation.
func validateDenyAllEgress(t *testing.T, policyName string, expectedPolicy AllowedPolicyDefinition) {
	// Cluster: PolicyType Egress is present, but no explicit egress rules -> Deny All Egress by this policy.
	// Expected: DenyAllEgressJustification is set; EgressRule.Ports and .To are empty.
	require.NotEmptyf(t, expectedPolicy.DenyAllEgressJustification,
		"Policy %q: Cluster has Egress PolicyType but no rules (deny all). Registry's DenyAllEgressJustification is empty.", policyName)
	require.Emptyf(t, expectedPolicy.EgressRule.Ports,
		"Policy %q: Cluster has Egress PolicyType but no rules (deny all). Registry's EgressRule.Ports is not empty.", policyName)
	require.Emptyf(t, expectedPolicy.EgressRule.To,
		"Policy %q: Cluster has Egress PolicyType but no rules (deny all). Registry's EgressRule.To is not empty.", policyName)
}

// validateSingleEgressRule validates a policy that has exactly one explicit egress rule,
// distinguishing between "allow-all" and more specific rules.
func validateSingleEgressRule(t *testing.T, policyName string, clusterEgressRule networkingv1.NetworkPolicyEgressRule, expectedPolicy AllowedPolicyDefinition) {
	// Cluster: PolicyType Egress is present, and there's one explicit egress rule.
	// Expected: DenyAllEgressJustification is empty; EgressRule matches the cluster's rule.
	expectedEgressRule := expectedPolicy.EgressRule

	require.Emptyf(t, expectedPolicy.DenyAllEgressJustification,
		"Policy %q: Cluster has a specific Egress rule. Registry's DenyAllEgressJustification should be empty.", policyName)

	isClusterRuleAllowAllPorts := len(clusterEgressRule.Ports) == 0
	isClusterRuleAllowAllPeers := len(clusterEgressRule.To) == 0

	if isClusterRuleAllowAllPorts && isClusterRuleAllowAllPeers { // Handles egress: [{}] - allow all ports to all peers
		require.Lenf(t, expectedEgressRule.Ports, 1,
			"Policy %q (allow-all egress): Expected EgressRule.Ports to have 1 justification entry, got %d", policyName, len(expectedEgressRule.Ports))
		if len(expectedEgressRule.Ports) == 1 { // Guard against panic
			assert.Nilf(t, expectedEgressRule.Ports[0].Port,
				"Policy %q (allow-all egress): Expected EgressRule.Ports[0].Port to be nil, got %+v", policyName, expectedEgressRule.Ports[0].Port)
		}
		assert.Conditionf(t, func() bool { return len(expectedEgressRule.To) == 0 },
			"Policy %q (allow-all egress): Expected EgressRule.To to be empty for allow-all peers, got %+v", policyName, expectedEgressRule.To)
	} else {
		// Specific egress rule (not the simple allow-all ports and allow-all peers)
		assert.True(t, equality.Semantic.DeepEqual(expectedEgressRule.To, clusterEgressRule.To),
			"Policy %q, Egress Rule: 'To' mismatch.\nExpected: %+v\nGot:      %+v", policyName, expectedEgressRule.To, clusterEgressRule.To)

		var allExpectedPortsFromPwJ []networkingv1.NetworkPolicyPort
		for _, pwj := range expectedEgressRule.Ports {
			allExpectedPortsFromPwJ = append(allExpectedPortsFromPwJ, pwj.Port...)
		}
		require.ElementsMatchf(t, allExpectedPortsFromPwJ, clusterEgressRule.Ports,
			"Policy %q, Egress Rule: 'Ports' mismatch (aggregated from PortWithJustification). Expected: %+v, Got: %+v", policyName, allExpectedPortsFromPwJ, clusterEgressRule.Ports)
	}
}

// validateNoIngress confirms that a policy which does not have the Ingress PolicyType
// has no corresponding ingress rules or expectations defined.
func validateNoIngress(t *testing.T, policyName string, clusterPolicy networkingv1.NetworkPolicy, expectedPolicy AllowedPolicyDefinition) {
	// Policy is NOT expected to affect Ingress traffic (no Ingress in PolicyTypes)
	// Expected: Cluster has no ingress rules; Registry has no DenyAllIngressJustification and empty IngressRule.
	require.Emptyf(t, clusterPolicy.Spec.Ingress,
		"Policy %q: Cluster does not have Ingress PolicyType, but has Ingress rules defined.", policyName)
	require.Emptyf(t, expectedPolicy.DenyAllIngressJustification,
		"Policy %q: Cluster does not have Ingress PolicyType. Registry's DenyAllIngressJustification is not empty.", policyName)
	require.Emptyf(t, expectedPolicy.IngressRule.Ports,
		"Policy %q: Cluster does not have Ingress PolicyType. Registry's IngressRule.Ports is not empty.", policyName)
	require.Emptyf(t, expectedPolicy.IngressRule.From,
		"Policy %q: Cluster does not have Ingress PolicyType. Registry's IngressRule.From is not empty.", policyName)
}

// validateDenyAllIngress confirms that a policy with Ingress PolicyType but no explicit rules
// correctly corresponds to a "deny all" expectation.
func validateDenyAllIngress(t *testing.T, policyName string, expectedPolicy AllowedPolicyDefinition) {
	// Cluster: PolicyType Ingress is present, but no explicit ingress rules -> Deny All Ingress by this policy.
	// Expected: DenyAllIngressJustification is set; IngressRule.Ports and .From are empty.
	require.NotEmptyf(t, expectedPolicy.DenyAllIngressJustification,
		"Policy %q: Cluster has Ingress PolicyType but no rules (deny all). Registry's DenyAllIngressJustification is empty.", policyName)
	require.Emptyf(t, expectedPolicy.IngressRule.Ports,
		"Policy %q: Cluster has Ingress PolicyType but no rules (deny all). Registry's IngressRule.Ports is not empty.", policyName)
	require.Emptyf(t, expectedPolicy.IngressRule.From,
		"Policy %q: Cluster has Ingress PolicyType but no rules (deny all). Registry's IngressRule.From is not empty.", policyName)
}

// validateSingleIngressRule validates a policy that has exactly one explicit ingress rule.
func validateSingleIngressRule(t *testing.T, policyName string, clusterIngressRule networkingv1.NetworkPolicyIngressRule, expectedPolicy AllowedPolicyDefinition) {
	// Cluster: PolicyType Ingress is present, and there's one explicit ingress rule.
	// Expected: DenyAllIngressJustification is empty; IngressRule matches the cluster's rule.
	expectedIngressRule := expectedPolicy.IngressRule

	require.Emptyf(t, expectedPolicy.DenyAllIngressJustification,
		"Policy %q: Cluster has a specific Ingress rule. Registry's DenyAllIngressJustification should be empty.", policyName)

	// Compare 'From'
	assert.True(t, equality.Semantic.DeepEqual(expectedIngressRule.From, clusterIngressRule.From),
		"Policy %q, Ingress Rule: 'From' mismatch.\nExpected: %+v\nGot:      %+v", policyName, expectedIngressRule.From, clusterIngressRule.From)

	// Compare 'Ports' by aggregating the ports from our justified structure
	var allExpectedPortsFromPwJ []networkingv1.NetworkPolicyPort
	for _, pwj := range expectedIngressRule.Ports {
		allExpectedPortsFromPwJ = append(allExpectedPortsFromPwJ, pwj.Port...)
	}
	require.ElementsMatchf(t, allExpectedPortsFromPwJ, clusterIngressRule.Ports,
		"Policy %q, Ingress Rule: 'Ports' mismatch (aggregated from PortWithJustification). Expected: %+v, Got: %+v", policyName, allExpectedPortsFromPwJ, clusterIngressRule.Ports)
}
