package main

import rego.v1

# Check that a deny-all NetworkPolicy exists
# A deny-all policy has:
# - podSelector: {} (empty, applies to all pods)
# - policyTypes containing both "Ingress" and "Egress"
# - No ingress or egress rules defined

is_deny_all(policy) if {
	policy.kind == "NetworkPolicy"
	policy.apiVersion == "networking.k8s.io/v1"

	# podSelector must be empty (applies to all pods)
	count(policy.spec.podSelector) == 0

	# Must have both Ingress and Egress policy types
	policy_types := {t | some t in policy.spec.policyTypes}
	policy_types["Ingress"]
	policy_types["Egress"]

	# Must not have any ingress rules
	not policy.spec.ingress

	# Must not have any egress rules
	not policy.spec.egress
}

has_deny_all_policy if {
	some i in numbers.range(0, count(input) - 1)
	is_deny_all(input[i].contents)
}

deny contains msg if {
	not has_deny_all_policy
	msg := "No deny-all NetworkPolicy found. A NetworkPolicy with empty podSelector, policyTypes [Ingress, Egress], and no ingress/egress rules is required."
}

# Check that a NetworkPolicy exists for catalogd-controller-manager that:
# - Allows ingress on TCP ports 7443, 8443, 9443
# - Allows general egress traffic

is_catalogd_policy(policy) if {
	policy.kind == "NetworkPolicy"
	policy.apiVersion == "networking.k8s.io/v1"
	policy.spec.podSelector.matchLabels["control-plane"] == "catalogd-controller-manager"
}

catalogd_policies contains policy if {
	some i in numbers.range(0, count(input) - 1)
	policy := input[i].contents
	is_catalogd_policy(policy)
}

catalogd_ingress_ports contains port if {
	some policy in catalogd_policies
	some rule in policy.spec.ingress
	some port in rule.ports
	port.protocol == "TCP"
}

catalogd_ingress_port_numbers contains num if {
	some port in catalogd_ingress_ports
	num := port.port
}

catalogd_has_egress if {
	some policy in catalogd_policies
	policy.spec.egress
}

deny contains msg if {
	count(catalogd_policies) == 0
	msg := "No NetworkPolicy found for catalogd-controller-manager. A NetworkPolicy allowing ingress on TCP ports 7443, 8443, 9443 and general egress is required."
}

deny contains msg if {
	count(catalogd_policies) > 1
	msg := sprintf("Expected exactly 1 NetworkPolicy for catalogd-controller-manager, found %d.", [count(catalogd_policies)])
}

deny contains msg if {
	count(catalogd_policies) == 1
	not catalogd_ingress_port_numbers[7443]
	msg := "Allow traffic to port 7443. Permit Prometheus to scrape metrics from catalogd, which is essential for monitoring its performance and health."
}

deny contains msg if {
	count(catalogd_policies) == 1
	not catalogd_ingress_port_numbers[8443]
	msg := "Allow traffic to port 8443. Permit clients (eg. operator-controller) to query catalog metadata from catalogd, which is a core function for bundle resolution and operator discovery."
}

deny contains msg if {
	count(catalogd_policies) == 1
	not catalogd_ingress_port_numbers[9443]
	msg := "Allow traffic to port 9443. Permit Kubernetes API server to reach catalogd's mutating admission webhook, ensuring integrity of catalog resources."
}

deny contains msg if {
	count(catalogd_policies) == 1
	not catalogd_has_egress
	msg := "Missing egress rules in catalogd-controller-manager NetworkPolicy. General egress is required to enable catalogd-controller to pull bundle images from arbitrary image registries, and interact with the Kubernetes API server."
}

# Check that a NetworkPolicy exists for operator-controller-controller-manager that:
# - Allows ingress on TCP port 8443
# - Allows general egress traffic

is_operator_controller_policy(policy) if {
	policy.kind == "NetworkPolicy"
	policy.apiVersion == "networking.k8s.io/v1"
	policy.spec.podSelector.matchLabels["control-plane"] == "operator-controller-controller-manager"
}

operator_controller_policies contains policy if {
	some i in numbers.range(0, count(input) - 1)
	policy := input[i].contents
	is_operator_controller_policy(policy)
}

operator_controller_ingress_ports contains port if {
	some policy in operator_controller_policies
	some rule in policy.spec.ingress
	some port in rule.ports
	port.protocol == "TCP"
}

operator_controller_ingress_port_numbers contains num if {
	some port in operator_controller_ingress_ports
	num := port.port
}

operator_controller_has_egress if {
	some policy in operator_controller_policies
	policy.spec.egress
}

deny contains msg if {
	count(operator_controller_policies) == 0
	msg := "No NetworkPolicy found for operator-controller-controller-manager. A NetworkPolicy allowing ingress on TCP port 8443 and general egress is required."
}

deny contains msg if {
	count(operator_controller_policies) > 1
	msg := sprintf("Expected exactly 1 NetworkPolicy for operator-controller-controller-manager, found %d.", [count(operator_controller_policies)])
}

deny contains msg if {
	count(operator_controller_policies) == 1
	not operator_controller_ingress_port_numbers[8443]
	msg := "Allow traffic to port 8443. Permit Prometheus to scrape metrics from catalogd, which is essential for monitoring its performance and health."
}

deny contains msg if {
	count(operator_controller_policies) == 1
	not operator_controller_has_egress
	msg := "Missing egress rules in operator-controller-controller-manager NetworkPolicy. General egress is required to enable operator-controller to pull bundle images from arbitrary image registries, connect to catalogd's HTTPS server for metadata, and interact with the Kubernetes API server."
}
