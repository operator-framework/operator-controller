@BoxcutterRuntime
Feature: ClusterObjectDeployment impeded-rollout Progressing reasons

  As an OLM user applying a ClusterObjectDeployment, I want the Progressing
  condition to tell me when a rollout is impeded by the head active
  ClusterObjectSet revision and whether I need to intervene, so that I can tell a
  rollout that is merely in progress from one that is stalled on something I must
  fix. The deployment reports its own generic message naming the revision and
  directs me to inspect it for the specific cause.

  Background:
    Given OLM is available

  # A revision that cannot take ownership of its objects will not become Ready
  # without user action, so the deployment reports Progressing=False.
  Scenario: Progressing is False with RevisionObjectCollision when the head revision collides
    Given namespace "${TEST_NAMESPACE}" is available
    And resource is applied
      """
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: cod-collide-cm
        namespace: ${TEST_NAMESPACE}
      data:
        k: pre-existing
      """
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
            phases:
            - name: resources
              objects:
              - object:
                  apiVersion: v1
                  kind: ConfigMap
                  metadata:
                    name: cod-collide-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: managed
      """
    Then ClusterObjectSet "${NAME}-1" reports Ready as False with Reason ObjectCollision
    And ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason RevisionObjectCollision and Message includes:
    """
      ${NAME}-1
    """
    And ClusterObjectDeployment "${NAME}" reports Available as False with Reason Unavailable
    # Resolve the collision: remove the conflicting object. The revision takes
    # ownership, becomes Ready, and the deployment progresses.
    When resource "configmap/cod-collide-cm" is removed
    Then ClusterObjectSet "${NAME}-1" reports Ready as True with Reason AllObjectsReady
    And ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason NewClusterObjectSetProgressed
    And ClusterObjectDeployment "${NAME}" reports Available as True with Reason Available

  # A revision waiting on an unsatisfied precondition keeps retrying, so the
  # deployment stays Progressing=True with an actionable reason.
  Scenario: Progressing is True with RevisionPreconditionsNotMet when the head revision's precondition is unmet
    Given namespace "${TEST_NAMESPACE}" is available
    And resource is applied
      """
      apiVersion: v1
      kind: Secret
      metadata:
        name: ${NAME}-mutable-secret
        namespace: ${TEST_NAMESPACE}
      type: olm.operatorframework.io/object-data
      stringData:
        configmap: |
          {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "metadata": {
              "name": "cod-precond-cm",
              "namespace": "${TEST_NAMESPACE}"
            },
            "data": {
              "key": "value"
            }
          }
      """
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
            phases:
            - name: resources
              objects:
              - ref:
                  name: ${NAME}-mutable-secret
                  namespace: ${TEST_NAMESPACE}
                  key: configmap
      """
    Then ClusterObjectSet "${NAME}-1" reports Ready as False with Reason PreconditionsNotMet
    And ClusterObjectDeployment "${NAME}" reports Progressing as True with Reason RevisionPreconditionsNotMet and Message includes:
    """
      ${NAME}-1
    """
    # Satisfy the precondition: replace the mutable Secret with an immutable one
    # carrying the same content. The revision recovers and the deployment progresses.
    When resource "secret/${NAME}-mutable-secret" is removed
    And resource is applied
      """
      apiVersion: v1
      kind: Secret
      metadata:
        name: ${NAME}-mutable-secret
        namespace: ${TEST_NAMESPACE}
      type: olm.operatorframework.io/object-data
      immutable: true
      stringData:
        configmap: |
          {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "metadata": {
              "name": "cod-precond-cm",
              "namespace": "${TEST_NAMESPACE}"
            },
            "data": {
              "key": "value"
            }
          }
      """
    Then ClusterObjectSet "${NAME}-1" reports Ready as True with Reason AllObjectsReady
    And ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason NewClusterObjectSetProgressed

  # A revision that never becomes ready is bounded by the progress deadline: once
  # the budget expires the deployment reports Progressing=False/ProgressDeadlineExceeded
  # while the revision keeps reporting its own readiness failure.
  @ProgressDeadline
  Scenario: Progressing is False with ProgressDeadlineExceeded when the revision never becomes ready
    Given namespace "${TEST_NAMESPACE}" is available
    And min value for ClusterObjectDeployment .spec.template.spec.progressDeadlineMinutes is set to 1
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
    And ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason ProgressDeadlineExceeded
    And ClusterObjectSet "${NAME}-1" reports Ready as False with Reason ObjectsNotReady
    And ClusterObjectDeployment "${NAME}" reports Available as False with Reason Unavailable

  # Hard stalls short-circuit the progress deadline: a collision keeps reporting
  # RevisionObjectCollision even once the deadline would have elapsed, rather than
  # flipping to ProgressDeadlineExceeded. The consistency window (70s) spans past the
  # 1-minute deadline, so a controller that let the deadline win would be caught.
  @ProgressDeadline
  Scenario: A hard stall is reported instead of the progress deadline even after it elapses
    Given namespace "${TEST_NAMESPACE}" is available
    And min value for ClusterObjectDeployment .spec.template.spec.progressDeadlineMinutes is set to 1
    And resource is applied
      """
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: cod-collide-deadline-cm
        namespace: ${TEST_NAMESPACE}
      data:
        k: pre-existing
      """
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
            phases:
            - name: resources
              objects:
              - object:
                  apiVersion: v1
                  kind: ConfigMap
                  metadata:
                    name: cod-collide-deadline-cm
                    namespace: ${TEST_NAMESPACE}
                  data:
                    k: managed
      """
    Then ClusterObjectSet "${NAME}-1" reports Ready as False with Reason ObjectCollision
    # Wait for the deployment to converge before starting the consistency window,
    # so it begins from a known-good state rather than racing the COD reconcile.
    And ClusterObjectDeployment "${NAME}" reports Progressing as False with Reason RevisionObjectCollision
    And ClusterObjectDeployment "${NAME}" consistently reports Progressing as False with Reason RevisionObjectCollision for "70s"
