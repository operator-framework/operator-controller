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

	"github.com/operator-framework/operator-controller/test/utils"
)

const (
	minJustificationLength        = 40
	catalogdManagerSelector       = "control-plane=catalogd-controller-manager"
	operatorManagerSelector       = "control-plane=operator-controller-controller-manager"
	catalogdMetricsPort           = 7443
	catalogdWebhookPort           = 9443
	catalogServerPort             = 8443
	operatorControllerMetricsPort = 8443
)

type portWithJustification struct {
	port          []networkingv1.NetworkPolicyPort
	justification string
}

// ingressRule defines a k8s IngressRule, along with a justification.
type ingressRule struct {
	ports []portWithJustification
	from  []networkingv1.NetworkPolicyPeer
}

// egressRule defines a k8s egressRule, along with a justification.
type egressRule struct {
	ports []portWithJustification
	to    []networkingv1.NetworkPolicyPeer
}

// AllowedPolicyDefinition defines the expected structure and justifications for a NetworkPolicy.
type allowedPolicyDefinition struct {
	selector                    metav1.LabelSelector
	policyTypes                 []networkingv1.PolicyType
	ingressRule                 ingressRule
	egressRule                  egressRule
	denyAllIngressJustification string // Justification if Ingress is in PolicyTypes and IngressRules is empty
	denyAllEgressJustification  string // Justification if Egress is in PolicyTypes and EgressRules is empty
}

var denyAllPolicySpec = allowedPolicyDefinition{
	selector:    metav1.LabelSelector{}, // Empty selector, matches all pods
	policyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
	// No IngressRules means deny all ingress if PolicyTypeIngress is present
	// No EgressRules means deny all egress if PolicyTypeEgress is present
	denyAllIngressJustification: "Denies all ingress traffic to pods selected by this policy by default, unless explicitly allowed by other policy rules, ensuring a baseline secure posture.",
	denyAllEgressJustification:  "Denies all egress traffic from pods selected by this policy by default, unless explicitly allowed by other policy rules, minimizing potential exfiltration paths.",
}

var prometheuSpec = allowedPolicyDefinition{
	selector:    metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": "prometheus"}},
	policyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
	ingressRule: ingressRule{
		ports: []portWithJustification{
			{
				port:          nil,
				justification: "Allows access to the prometheus pod",
			},
		},
	},
	egressRule: egressRule{
		ports: []portWithJustification{
			{
				port:          nil,
				justification: "Allows prometheus to access other pods",
			},
		},
	},
}

// Ref: https://docs.google.com/document/d/1bHEEWzA65u-kjJFQRUY1iBuMIIM1HbPy4MeDLX4NI3o/edit?usp=sharing
var allowedNetworkPolicies = map[string]allowedPolicyDefinition{
	"catalogd-controller-manager": {
		selector:    metav1.LabelSelector{MatchLabels: map[string]string{"control-plane": "catalogd-controller-manager"}},
		policyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
		ingressRule: ingressRule{
			ports: []portWithJustification{
				{
					port:          []networkingv1.NetworkPolicyPort{{Protocol: ptr.To(corev1.ProtocolTCP), Port: &intstr.IntOrString{Type: intstr.Int, IntVal: catalogdMetricsPort}}},
					justification: "Allows Prometheus to scrape metrics from catalogd, which is essential for monitoring its performance and health.",
				},
				{
					port:          []networkingv1.NetworkPolicyPort{{Protocol: ptr.To(corev1.ProtocolTCP), Port: &intstr.IntOrString{Type: intstr.Int, IntVal: catalogdWebhookPort}}},
					justification: "Permits Kubernetes API server to reach catalogd's mutating admission webhook, ensuring integrity of catalog resources.",
				},
				{
					port:          []networkingv1.NetworkPolicyPort{{Protocol: ptr.To(corev1.ProtocolTCP), Port: &intstr.IntOrString{Type: intstr.Int, IntVal: catalogServerPort}}},
					justification: "Enables clients (eg. operator-controller) to query catalog metadata from catalogd, which is a core function for bundle resolution and operator discovery.",
				},
			},
		},
		egressRule: egressRule{
			ports: []portWithJustification{
				{
					port:          nil, // Empty Ports means allow all egress
					justification: "Permits catalogd to fetch catalog images from arbitrary container registries and communicate with the Kubernetes API server for its operational needs.",
				},
			},
		},
	},
	"operator-controller-controller-manager": {
		selector:    metav1.LabelSelector{MatchLabels: map[string]string{"control-plane": "operator-controller-controller-manager"}},
		policyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
		ingressRule: ingressRule{
			ports: []portWithJustification{
				{
					port:          []networkingv1.NetworkPolicyPort{{Protocol: ptr.To(corev1.ProtocolTCP), Port: &intstr.IntOrString{Type: intstr.Int, IntVal: operatorControllerMetricsPort}}},
					justification: "Allows Prometheus to scrape metrics from operator-controller, which is crucial for monitoring its activity, reconciliations, and overall health.",
				},
			},
		},
		egressRule: egressRule{
			ports: []portWithJustification{
				{
					port:          nil, // Empty Ports means allow all egress
					justification: "Enables operator-controller to pull bundle images from arbitrary image registries, connect to catalogd's HTTPS server for metadata, and interact with the Kubernetes API server.",
				},
			},
		},
	},
}

func TestNetworkPolicyJustifications(t *testing.T) {
	ctx := context.Background()

	// Validate justifications have min length in the allowedNetworkPolicies definition
	for name, policyDef := range allowedNetworkPolicies {
		for i, pwj := range policyDef.ingressRule.ports {
			assert.GreaterOrEqualf(t, len(pwj.justification), minJustificationLength,
				"Justification for ingress PortWithJustification entry %d in policy %q is too short: %q", i, name, pwj.justification)
		}
		for i, pwj := range policyDef.egressRule.ports { // Corrected variable name from 'rule' to 'pwj'
			assert.GreaterOrEqualf(t, len(pwj.justification), minJustificationLength,
				"Justification for egress PortWithJustification entry %d in policy %q is too short: %q", i, name, pwj.justification)
		}
		if policyDef.denyAllIngressJustification != "" {
			assert.GreaterOrEqualf(t, len(policyDef.denyAllIngressJustification), minJustificationLength,
				"DenyAllIngressJustification for policy %q is too short: %q", name, policyDef.denyAllIngressJustification)
		}
		if policyDef.denyAllEgressJustification != "" {
			assert.GreaterOrEqualf(t, len(policyDef.denyAllEgressJustification), minJustificationLength,
				"DenyAllEgressJustification for policy %q is too short: %q", name, policyDef.denyAllEgressJustification)
		}
	}

	clientForComponent := utils.FindK8sClient(t)

	operatorControllerNamespace := getComponentNamespace(t, clientForComponent, operatorManagerSelector)
	catalogDNamespace := getComponentNamespace(t, clientForComponent, catalogdManagerSelector)

	policies := &networkingv1.NetworkPolicyList{}
	err := c.List(ctx, policies, client.InNamespace(operatorControllerNamespace))
	require.NoError(t, err, "Failed to list NetworkPolicies in namespace %q", operatorControllerNamespace)

	clusterPolicies := policies.Items

	if operatorControllerNamespace != catalogDNamespace {
		policies := &networkingv1.NetworkPolicyList{}
		err := c.List(ctx, policies, client.InNamespace(catalogDNamespace))
		require.NoError(t, err, "Failed to list NetworkPolicies in namespace %q", catalogDNamespace)
		clusterPolicies = append(clusterPolicies, policies.Items...)

		t.Log("Detected dual-namespace configuration, expecting two prefixed 'default-deny-all-traffic' policies.")
		allowedNetworkPolicies["catalogd-default-deny-all-traffic"] = denyAllPolicySpec
		allowedNetworkPolicies["operator-controller-default-deny-all-traffic"] = denyAllPolicySpec
	} else {
		t.Log("Detected single-namespace configuration, expecting one 'default-deny-all-traffic' policy.")
		allowedNetworkPolicies["default-deny-all-traffic"] = denyAllPolicySpec
		t.Log("Detected single-namespace configuration, expecting 'prometheus' policy.")
		allowedNetworkPolicies["prometheus"] = prometheuSpec
	}

	validatedRegistryPolicies := make(map[string]bool)

	for _, policy := range clusterPolicies {
		t.Run(fmt.Sprintf("Policy_%s", strings.ReplaceAll(policy.Name, "-", "_")), func(t *testing.T) {
			expectedPolicy, found := allowedNetworkPolicies[policy.Name]
			require.Truef(t, found, "NetworkPolicy %q found in cluster but not in allowed registry. Namespace: %s", policy.Name, policy.Namespace)
			validatedRegistryPolicies[policy.Name] = true

			// 1. Compare PodSelector
			assert.True(t, equality.Semantic.DeepEqual(expectedPolicy.selector, policy.Spec.PodSelector),
				"PodSelector mismatch for policy %q. Expected: %+v, Got: %+v", policy.Name, expectedPolicy.selector, policy.Spec.PodSelector)

			// 2. Compare PolicyTypes
			require.ElementsMatchf(t, expectedPolicy.policyTypes, policy.Spec.PolicyTypes,
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
	assert.Len(t, validatedRegistryPolicies, len(allowedNetworkPolicies),
		"Mismatch between number of expected policies in registry (%d) and number of policies found & validated in cluster (%d). Missing policies from registry: %v", len(allowedNetworkPolicies), len(validatedRegistryPolicies), missingPolicies(allowedNetworkPolicies, validatedRegistryPolicies))
}

func missingPolicies(expected map[string]allowedPolicyDefinition, actual map[string]bool) []string {
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
func validateNoEgress(t *testing.T, policy networkingv1.NetworkPolicy, expectedPolicy allowedPolicyDefinition) {
	// Policy is NOT expected to affect Egress traffic (no Egress in PolicyTypes)
	// Expected: Cluster has no egress rules; Registry has no DenyAllEgressJustification and empty EgressRule.
	require.Emptyf(t, policy.Spec.Egress,
		"Policy %q: Cluster does not have Egress PolicyType, but has Egress rules defined.", policy.Name)
	require.Emptyf(t, expectedPolicy.denyAllEgressJustification,
		"Policy %q: Cluster does not have Egress PolicyType. Registry's DenyAllEgressJustification is not empty.", policy.Name)
	require.Emptyf(t, expectedPolicy.egressRule.ports,
		"Policy %q: Cluster does not have Egress PolicyType. Registry's EgressRule.Ports is not empty.", policy.Name)
	require.Emptyf(t, expectedPolicy.egressRule.to,
		"Policy %q: Cluster does not have Egress PolicyType. Registry's EgressRule.To is not empty.", policy.Name)
}

// validateDenyAllEgress confirms that a policy with Egress PolicyType but no explicit rules
// correctly corresponds to a "deny all" expectation.
func validateDenyAllEgress(t *testing.T, policyName string, expectedPolicy allowedPolicyDefinition) {
	// Cluster: PolicyType Egress is present, but no explicit egress rules -> Deny All Egress by this policy.
	// Expected: DenyAllEgressJustification is set; EgressRule.Ports and .To are empty.
	require.NotEmptyf(t, expectedPolicy.denyAllEgressJustification,
		"Policy %q: Cluster has Egress PolicyType but no rules (deny all). Registry's DenyAllEgressJustification is empty.", policyName)
	require.Emptyf(t, expectedPolicy.egressRule.ports,
		"Policy %q: Cluster has Egress PolicyType but no rules (deny all). Registry's EgressRule.Ports is not empty.", policyName)
	require.Emptyf(t, expectedPolicy.egressRule.to,
		"Policy %q: Cluster has Egress PolicyType but no rules (deny all). Registry's EgressRule.To is not empty.", policyName)
}

// validateSingleEgressRule validates a policy that has exactly one explicit egress rule,
// distinguishing between "allow-all" and more specific rules.
func validateSingleEgressRule(t *testing.T, policyName string, clusterEgressRule networkingv1.NetworkPolicyEgressRule, expectedPolicy allowedPolicyDefinition) {
	// Cluster: PolicyType Egress is present, and there's one explicit egress rule.
	// Expected: DenyAllEgressJustification is empty; EgressRule matches the cluster's rule.
	expectedEgressRule := expectedPolicy.egressRule

	require.Emptyf(t, expectedPolicy.denyAllEgressJustification,
		"Policy %q: Cluster has a specific Egress rule. Registry's DenyAllEgressJustification should be empty.", policyName)

	isClusterRuleAllowAllPorts := len(clusterEgressRule.Ports) == 0
	isClusterRuleAllowAllPeers := len(clusterEgressRule.To) == 0

	if isClusterRuleAllowAllPorts && isClusterRuleAllowAllPeers { // Handles egress: [{}] - allow all ports to all peers
		require.Lenf(t, expectedEgressRule.ports, 1,
			"Policy %q (allow-all egress): Expected EgressRule.Ports to have 1 justification entry, got %d", policyName, len(expectedEgressRule.ports))
		if len(expectedEgressRule.ports) == 1 { // Guard against panic
			assert.Nilf(t, expectedEgressRule.ports[0].port,
				"Policy %q (allow-all egress): Expected EgressRule.Ports[0].Port to be nil, got %+v", policyName, expectedEgressRule.ports[0].port)
		}
		assert.Conditionf(t, func() bool { return len(expectedEgressRule.to) == 0 },
			"Policy %q (allow-all egress): Expected EgressRule.To to be empty for allow-all peers, got %+v", policyName, expectedEgressRule.to)
	} else {
		// Specific egress rule (not the simple allow-all ports and allow-all peers)
		assert.True(t, equality.Semantic.DeepEqual(expectedEgressRule.to, clusterEgressRule.To),
			"Policy %q, Egress Rule: 'To' mismatch.\nExpected: %+v\nGot:      %+v", policyName, expectedEgressRule.to, clusterEgressRule.To)

		var allExpectedPortsFromPwJ []networkingv1.NetworkPolicyPort
		for _, pwj := range expectedEgressRule.ports {
			allExpectedPortsFromPwJ = append(allExpectedPortsFromPwJ, pwj.port...)
		}
		require.ElementsMatchf(t, allExpectedPortsFromPwJ, clusterEgressRule.Ports,
			"Policy %q, Egress Rule: 'Ports' mismatch (aggregated from PortWithJustification). Expected: %+v, Got: %+v", policyName, allExpectedPortsFromPwJ, clusterEgressRule.Ports)
	}
}

// validateNoIngress confirms that a policy which does not have the Ingress PolicyType
// has no corresponding ingress rules or expectations defined.
func validateNoIngress(t *testing.T, policyName string, clusterPolicy networkingv1.NetworkPolicy, expectedPolicy allowedPolicyDefinition) {
	// Policy is NOT expected to affect Ingress traffic (no Ingress in PolicyTypes)
	// Expected: Cluster has no ingress rules; Registry has no DenyAllIngressJustification and empty IngressRule.
	require.Emptyf(t, clusterPolicy.Spec.Ingress,
		"Policy %q: Cluster does not have Ingress PolicyType, but has Ingress rules defined.", policyName)
	require.Emptyf(t, expectedPolicy.denyAllIngressJustification,
		"Policy %q: Cluster does not have Ingress PolicyType. Registry's DenyAllIngressJustification is not empty.", policyName)
	require.Emptyf(t, expectedPolicy.ingressRule.ports,
		"Policy %q: Cluster does not have Ingress PolicyType. Registry's IngressRule.Ports is not empty.", policyName)
	require.Emptyf(t, expectedPolicy.ingressRule.from,
		"Policy %q: Cluster does not have Ingress PolicyType. Registry's IngressRule.From is not empty.", policyName)
}

// validateDenyAllIngress confirms that a policy with Ingress PolicyType but no explicit rules
// correctly corresponds to a "deny all" expectation.
func validateDenyAllIngress(t *testing.T, policyName string, expectedPolicy allowedPolicyDefinition) {
	// Cluster: PolicyType Ingress is present, but no explicit ingress rules -> Deny All Ingress by this policy.
	// Expected: DenyAllIngressJustification is set; IngressRule.Ports and .From are empty.
	require.NotEmptyf(t, expectedPolicy.denyAllIngressJustification,
		"Policy %q: Cluster has Ingress PolicyType but no rules (deny all). Registry's DenyAllIngressJustification is empty.", policyName)
	require.Emptyf(t, expectedPolicy.ingressRule.ports,
		"Policy %q: Cluster has Ingress PolicyType but no rules (deny all). Registry's IngressRule.Ports is not empty.", policyName)
	require.Emptyf(t, expectedPolicy.ingressRule.from,
		"Policy %q: Cluster has Ingress PolicyType but no rules (deny all). Registry's IngressRule.From is not empty.", policyName)
}

// validateSingleIngressRule validates a policy that has exactly one explicit ingress rule.
func validateSingleIngressRule(t *testing.T, policyName string, clusterIngressRule networkingv1.NetworkPolicyIngressRule, expectedPolicy allowedPolicyDefinition) {
	// Cluster: PolicyType Ingress is present, and there's one explicit ingress rule.
	// Expected: DenyAllIngressJustification is empty; IngressRule matches the cluster's rule.
	expectedIngressRule := expectedPolicy.ingressRule

	require.Emptyf(t, expectedPolicy.denyAllIngressJustification,
		"Policy %q: Cluster has a specific Ingress rule. Registry's DenyAllIngressJustification should be empty.", policyName)

	// Compare 'From'
	assert.True(t, equality.Semantic.DeepEqual(expectedIngressRule.from, clusterIngressRule.From),
		"Policy %q, Ingress Rule: 'From' mismatch.\nExpected: %+v\nGot:      %+v", policyName, expectedIngressRule.from, clusterIngressRule.From)

	// Compare 'Ports' by aggregating the ports from our justified structure
	var allExpectedPortsFromPwJ []networkingv1.NetworkPolicyPort
	for _, pwj := range expectedIngressRule.ports {
		allExpectedPortsFromPwJ = append(allExpectedPortsFromPwJ, pwj.port...)
	}
	require.ElementsMatchf(t, allExpectedPortsFromPwJ, clusterIngressRule.Ports,
		"Policy %q, Ingress Rule: 'Ports' mismatch (aggregated from PortWithJustification). Expected: %+v, Got: %+v", policyName, allExpectedPortsFromPwJ, clusterIngressRule.Ports)
}
