@BoxcutterRuntime
Feature: ClusterObjectDeployment template metadata propagation

  As an OLM user applying a ClusterObjectDeployment, I want the labels and
  annotations in spec.template.metadata to be propagated verbatim onto each
  ClusterObjectSet revision the controller creates, and invalid metadata to be
  reported on the deployment rather than silently stalling the rollout.

  Background:
    Given OLM is available

  Scenario: Template metadata is propagated to the revision
    Given namespace "${TEST_NAMESPACE}" is available
    When resource is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectDeployment
      metadata:
        name: ${NAME}
      spec:
        template:
          metadata:
            labels:
              team: platform
            annotations:
              note: hello
          spec:
            collisionProtection: Prevent
            phases:
            - name: resources
              objects:
              - object:
                  apiVersion: v1
                  kind: ConfigMap
                  metadata:
                    name: cod-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: v
      """
    Then ClusterObjectSet "${NAME}-1" has group "${NAME}"
    And ClusterObjectSet "${NAME}-1" has label "team" with value "platform"
    And ClusterObjectSet "${NAME}-1" contains annotation "note" with value
      """
      hello
      """

  Scenario: Invalid template metadata is reported as InvalidSpecification
    Given namespace "${TEST_NAMESPACE}" is available
    When resource is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectDeployment
      metadata:
        name: ${NAME}
      spec:
        template:
          metadata:
            labels:
              "not a valid key": value
          spec:
            collisionProtection: Prevent
            phases:
            - name: resources
              objects:
              - object:
                  apiVersion: v1
                  kind: ConfigMap
                  metadata:
                    name: cod-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: v
      """
    Then ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason InvalidSpecification and Message includes:
      """
      template metadata is invalid: spec.template.metadata.labels: Invalid value: "not a valid key": name part must consist of alphanumeric characters, '-', '_' or '.', and must start and end with an alphanumeric character
      """
