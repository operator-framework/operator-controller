package prometheus

import rego.v1

# Check that a NetworkPolicy exists that allows both ingress and egress traffic to prometheus pods
is_prometheus_policy(policy) if {
	policy.kind == "NetworkPolicy"
	policy.apiVersion == "networking.k8s.io/v1"

	# Must target prometheus pods
	policy.spec.podSelector.matchLabels["app.kubernetes.io/name"] == "prometheus"

	# Must have both Ingress and Egress policy types
	policy_types := {t | some t in policy.spec.policyTypes}
	policy_types["Ingress"]
	policy_types["Egress"]

	# Must have ingress rules defined (allowing traffic)
	policy.spec.ingress

	# Must have egress rules defined (allowing traffic)
	policy.spec.egress
}

has_prometheus_policy if {
	some i in numbers.range(0, count(input) - 1)
	is_prometheus_policy(input[i].contents)
}

deny contains msg if {
	not has_prometheus_policy
	msg := "No NetworkPolicy found that allows both ingress and egress traffic to prometheus pods. A NetworkPolicy targeting prometheus pods with ingress and egress rules is required."
}
