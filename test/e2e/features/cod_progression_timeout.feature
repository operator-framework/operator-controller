@BoxcutterRuntime
@ProgressDeadline
Feature: ClusterObjectDeployment progression timeout

  As an OLM user applying a ClusterObjectDeployment, I want spec.template.spec.progressDeadlineMinutes
  to be passed through to the ClusterObjectSet revisions it creates, so that a revision that does not
  roll out within the deadline is reported as failed and the deployment is reported unavailable.

  Background:
    Given OLM is available

  Scenario: ClusterObjectDeployment passes progressDeadlineMinutes through to its revision and times out
    Given namespace "${TEST_NAMESPACE}" is available
    And min value for ClusterObjectDeployment .spec.template.spec.progressDeadlineMinutes is set to 1
    And min value for ClusterObjectSet .spec.progressDeadlineMinutes is set to 1
    When resource is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectDeployment
      metadata:
        name: ${NAME}
      spec:
        template:
          spec:
            collisionProtection: Prevent
            progressDeadlineMinutes: 1
            progressionProbes:
            - selector:
                type: GroupKind
                groupKind:
                  group: apps
                  kind: Deployment
              assertions:
              - type: ConditionEqual
                conditionEqual:
                  type: Available
                  status: "True"
            phases:
            - name: resources
              objects:
              - object:
                  apiVersion: apps/v1
                  kind: Deployment
                  metadata:
                    name: never-ready
                    namespace: ${TEST_NAMESPACE}
                  spec:
                    replicas: 1
                    selector:
                      matchLabels:
                        app: never-ready
                    template:
                      metadata:
                        labels:
                          app: never-ready
                      spec:
                        containers:
                        - name: never-ready
                          image: does-not-exist:latest
      """
    Then ClusterObjectSet "${NAME}-1" has group "${NAME}"
    And ClusterObjectSet "${NAME}-1" reports Ready as False with Reason ProgressDeadlineExceeded
    And ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason ProgressDeadlineExceeded
    And ClusterObjectDeployment "${NAME}" reports Available as False with Reason Unavailable
