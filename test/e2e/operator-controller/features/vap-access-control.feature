Feature: As an OLM admin, I would like to use ValidatingAdmissionPolicy to restrict
          which users and groups can create or modify ClusterExtensions, and constrain them
          to approved packages, versions, namespaces, and catalogs

  Background:
    Given OLM is available
    And an image registry is available
    And a catalog "test" with packages:
      | package | version | channel | replaces | contents                   |
      | test    | 1.0.0   | stable  |          | CRD, Deployment, ConfigMap |
    And catalog "test" is labeled with "team=monitoring"
    And namespace "${TEST_NAMESPACE}" is available
    And resource is applied
      """
      apiVersion: rbac.authorization.k8s.io/v1
      kind: ClusterRole
      metadata:
        name: ce-editor-${NAME}
      rules:
        - apiGroups: ["olm.operatorframework.io"]
          resources: ["clusterextensions"]
          verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
      """

  Scenario: Allowed - correct package, namespace, and catalog selector
    Given resource is applied
      """
      apiVersion: rbac.authorization.k8s.io/v1
      kind: ClusterRoleBinding
      metadata:
        name: ce-editor-${NAME}
      roleRef:
        apiGroup: rbac.authorization.k8s.io
        kind: ClusterRole
        name: ce-editor-${NAME}
      subjects:
        - kind: Group
          name: team-allowed
          apiGroup: rbac.authorization.k8s.io
      """
    And resource is applied
      """
      apiVersion: admissionregistration.k8s.io/v1
      kind: ValidatingAdmissionPolicy
      metadata:
        name: ce-policy-${NAME}
      spec:
        failurePolicy: Fail
        matchConstraints:
          resourceRules:
            - apiGroups: ["olm.operatorframework.io"]
              apiVersions: ["v1"]
              resources: ["clusterextensions"]
              operations: ["CREATE", "UPDATE"]
        matchConditions:
          - name: scoped-group
            expression: >-
              request.userInfo.groups.exists(g, g == "team-allowed")
        validations:
          - expression: >-
              object.spec.source.catalog.packageName == "${PACKAGE:test}"
            messageExpression: >-
              "package not allowed; permitted: ${PACKAGE:test}"
      """
    And resource is applied
      """
      apiVersion: admissionregistration.k8s.io/v1
      kind: ValidatingAdmissionPolicyBinding
      metadata:
        name: ce-policy-${NAME}
      spec:
        policyName: ce-policy-${NAME}
        validationActions: ["Deny"]
      """
    And ValidatingAdmissionPolicy "ce-policy-${NAME}" is active
    When resource is applied as user "alice" in group "team-allowed"
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            version: "1.0.0"
            selector:
              matchLabels:
                team: monitoring
      """
    Then ClusterExtension is rolled out
    And ClusterExtension is available

  Scenario: Denied - wrong package name
    Given resource is applied
      """
      apiVersion: rbac.authorization.k8s.io/v1
      kind: ClusterRoleBinding
      metadata:
        name: ce-editor-${NAME}
      roleRef:
        apiGroup: rbac.authorization.k8s.io
        kind: ClusterRole
        name: ce-editor-${NAME}
      subjects:
        - kind: Group
          name: team-package
          apiGroup: rbac.authorization.k8s.io
      """
    And resource is applied
      """
      apiVersion: admissionregistration.k8s.io/v1
      kind: ValidatingAdmissionPolicy
      metadata:
        name: ce-policy-${NAME}
      spec:
        failurePolicy: Fail
        matchConstraints:
          resourceRules:
            - apiGroups: ["olm.operatorframework.io"]
              apiVersions: ["v1"]
              resources: ["clusterextensions"]
              operations: ["CREATE", "UPDATE"]
        matchConditions:
          - name: scoped-group
            expression: >-
              request.userInfo.groups.exists(g, g == "team-package")
        validations:
          - expression: >-
              object.spec.source.catalog.packageName == "${PACKAGE:test}"
            messageExpression: >-
              "package not allowed; permitted: ${PACKAGE:test}"
      """
    And resource is applied
      """
      apiVersion: admissionregistration.k8s.io/v1
      kind: ValidatingAdmissionPolicyBinding
      metadata:
        name: ce-policy-${NAME}
      spec:
        policyName: ce-policy-${NAME}
        validationActions: ["Deny"]
      """
    And ValidatingAdmissionPolicy "ce-policy-${NAME}" is active
    When resource apply as user "bob" in group "team-package" fails with error msg containing "package not allowed"
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        source:
          sourceType: Catalog
          catalog:
            packageName: grafana
            version: "10.0.0"
            selector:
              matchLabels:
                team: monitoring
      """
    Then resource "clusterextension/${NAME}" is not found

  Scenario: Denied - wrong namespace
    Given resource is applied
      """
      apiVersion: rbac.authorization.k8s.io/v1
      kind: ClusterRoleBinding
      metadata:
        name: ce-editor-${NAME}
      roleRef:
        apiGroup: rbac.authorization.k8s.io
        kind: ClusterRole
        name: ce-editor-${NAME}
      subjects:
        - kind: Group
          name: team-namespace
          apiGroup: rbac.authorization.k8s.io
      """
    And resource is applied
      """
      apiVersion: admissionregistration.k8s.io/v1
      kind: ValidatingAdmissionPolicy
      metadata:
        name: ce-policy-${NAME}
      spec:
        failurePolicy: Fail
        matchConstraints:
          resourceRules:
            - apiGroups: ["olm.operatorframework.io"]
              apiVersions: ["v1"]
              resources: ["clusterextensions"]
              operations: ["CREATE", "UPDATE"]
        matchConditions:
          - name: scoped-group
            expression: >-
              request.userInfo.groups.exists(g, g == "team-namespace")
        validations:
          - expression: >-
              object.spec.namespace.startsWith("ns-")
            messageExpression: >-
              "namespace must match the test namespace convention (ns-*)"
      """
    And resource is applied
      """
      apiVersion: admissionregistration.k8s.io/v1
      kind: ValidatingAdmissionPolicyBinding
      metadata:
        name: ce-policy-${NAME}
      spec:
        policyName: ce-policy-${NAME}
        validationActions: ["Deny"]
      """
    And ValidatingAdmissionPolicy "ce-policy-${NAME}" is active
    When resource apply as user "carol" in group "team-namespace" fails with error msg containing "namespace must match the test namespace convention"
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: default
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            version: "1.0.0"
            selector:
              matchLabels:
                team: monitoring
      """
    Then resource "clusterextension/${NAME}" is not found

  Scenario: Denied - missing catalog selector
    Given resource is applied
      """
      apiVersion: rbac.authorization.k8s.io/v1
      kind: ClusterRoleBinding
      metadata:
        name: ce-editor-${NAME}
      roleRef:
        apiGroup: rbac.authorization.k8s.io
        kind: ClusterRole
        name: ce-editor-${NAME}
      subjects:
        - kind: Group
          name: team-selector
          apiGroup: rbac.authorization.k8s.io
      """
    And resource is applied
      """
      apiVersion: admissionregistration.k8s.io/v1
      kind: ValidatingAdmissionPolicy
      metadata:
        name: ce-policy-${NAME}
      spec:
        failurePolicy: Fail
        matchConstraints:
          resourceRules:
            - apiGroups: ["olm.operatorframework.io"]
              apiVersions: ["v1"]
              resources: ["clusterextensions"]
              operations: ["CREATE", "UPDATE"]
        matchConditions:
          - name: scoped-group
            expression: >-
              request.userInfo.groups.exists(g, g == "team-selector")
        validations:
          - expression: >-
              has(object.spec.source.catalog) &&
              has(object.spec.source.catalog.selector) &&
              has(object.spec.source.catalog.selector.matchLabels) &&
              "team" in object.spec.source.catalog.selector.matchLabels &&
              object.spec.source.catalog.selector.matchLabels["team"] == "monitoring"
            messageExpression: >-
              "catalog selector must include team=monitoring"
      """
    And resource is applied
      """
      apiVersion: admissionregistration.k8s.io/v1
      kind: ValidatingAdmissionPolicyBinding
      metadata:
        name: ce-policy-${NAME}
      spec:
        policyName: ce-policy-${NAME}
        validationActions: ["Deny"]
      """
    And ValidatingAdmissionPolicy "ce-policy-${NAME}" is active
    When resource apply as user "dave" in group "team-selector" fails with error msg containing "catalog selector must include team=monitoring"
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            version: "1.0.0"
      """
    Then resource "clusterextension/${NAME}" is not found

  Scenario: Denied - SelfCertified upgrade constraint policy
    Given resource is applied
      """
      apiVersion: rbac.authorization.k8s.io/v1
      kind: ClusterRoleBinding
      metadata:
        name: ce-editor-${NAME}
      roleRef:
        apiGroup: rbac.authorization.k8s.io
        kind: ClusterRole
        name: ce-editor-${NAME}
      subjects:
        - kind: Group
          name: team-upgrade
          apiGroup: rbac.authorization.k8s.io
      """
    And resource is applied
      """
      apiVersion: admissionregistration.k8s.io/v1
      kind: ValidatingAdmissionPolicy
      metadata:
        name: ce-policy-${NAME}
      spec:
        failurePolicy: Fail
        matchConstraints:
          resourceRules:
            - apiGroups: ["olm.operatorframework.io"]
              apiVersions: ["v1"]
              resources: ["clusterextensions"]
              operations: ["CREATE", "UPDATE"]
        matchConditions:
          - name: scoped-group
            expression: >-
              request.userInfo.groups.exists(g, g == "team-upgrade")
        validations:
          - expression: >-
              !has(object.spec.source.catalog.upgradeConstraintPolicy) ||
              object.spec.source.catalog.upgradeConstraintPolicy != "SelfCertified"
            message: >-
              upgradeConstraintPolicy SelfCertified is not allowed
      """
    And resource is applied
      """
      apiVersion: admissionregistration.k8s.io/v1
      kind: ValidatingAdmissionPolicyBinding
      metadata:
        name: ce-policy-${NAME}
      spec:
        policyName: ce-policy-${NAME}
        validationActions: ["Deny"]
      """
    And ValidatingAdmissionPolicy "ce-policy-${NAME}" is active
    When resource apply as user "eve" in group "team-upgrade" fails with error msg containing "SelfCertified is not allowed"
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            version: "1.0.0"
            upgradeConstraintPolicy: SelfCertified
            selector:
              matchLabels:
                team: monitoring
      """
    Then resource "clusterextension/${NAME}" is not found

  Scenario: Denied - CRD upgrade safety disabled
    Given resource is applied
      """
      apiVersion: rbac.authorization.k8s.io/v1
      kind: ClusterRoleBinding
      metadata:
        name: ce-editor-${NAME}
      roleRef:
        apiGroup: rbac.authorization.k8s.io
        kind: ClusterRole
        name: ce-editor-${NAME}
      subjects:
        - kind: Group
          name: team-crd
          apiGroup: rbac.authorization.k8s.io
      """
    And resource is applied
      """
      apiVersion: admissionregistration.k8s.io/v1
      kind: ValidatingAdmissionPolicy
      metadata:
        name: ce-policy-${NAME}
      spec:
        failurePolicy: Fail
        matchConstraints:
          resourceRules:
            - apiGroups: ["olm.operatorframework.io"]
              apiVersions: ["v1"]
              resources: ["clusterextensions"]
              operations: ["CREATE", "UPDATE"]
        matchConditions:
          - name: scoped-group
            expression: >-
              request.userInfo.groups.exists(g, g == "team-crd")
        validations:
          - expression: >-
              !has(object.spec.install) ||
              !has(object.spec.install.preflight) ||
              !has(object.spec.install.preflight.crdUpgradeSafety) ||
              object.spec.install.preflight.crdUpgradeSafety.enforcement != "None"
            message: >-
              CRD upgrade safety checks may not be disabled
      """
    And resource is applied
      """
      apiVersion: admissionregistration.k8s.io/v1
      kind: ValidatingAdmissionPolicyBinding
      metadata:
        name: ce-policy-${NAME}
      spec:
        policyName: ce-policy-${NAME}
        validationActions: ["Deny"]
      """
    And ValidatingAdmissionPolicy "ce-policy-${NAME}" is active
    When resource apply as user "frank" in group "team-crd" fails with error msg containing "CRD upgrade safety checks may not be disabled"
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterExtension
      metadata:
        name: ${NAME}
      spec:
        namespace: ${TEST_NAMESPACE}
        install:
          preflight:
            crdUpgradeSafety:
              enforcement: None
        source:
          sourceType: Catalog
          catalog:
            packageName: ${PACKAGE:test}
            version: "1.0.0"
            selector:
              matchLabels:
                team: monitoring
      """
    Then resource "clusterextension/${NAME}" is not found
