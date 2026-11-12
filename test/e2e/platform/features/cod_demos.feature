@demo @BoxcutterRuntime
Feature: ClusterObjectDeployment demos

  # Direct COD use in the experimental E2E environment. No ClusterExtension or
  # catalog is needed by this scenario; this does not test an isolated install.
  Scenario: COD Revisions and Ownership
    Given resource "customresourcedefinition/clusterobjectdeployments.olm.operatorframework.io" exists
    And namespace "${TEST_NAMESPACE}" is available

    # A: the deployment creates and owns its first immutable revision.
    When resource is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectDeployment
      metadata:
        name: cod-${SCENARIO_ID}
      spec:
        revisionHistoryLimit: 2
        template:
          spec:
            collisionProtection: Prevent
            phases:
            - name: resources
              objects:
              - type: InlineManifest
                inlineManifest:
                  apiVersion: v1
                  kind: ConfigMap
                  metadata:
                    name: cod-demo-settings
                    namespace: ${TEST_NAMESPACE}
                  data:
                    message: Hello from revision A
      """
    Then ClusterObjectSet "cod-${SCENARIO_ID}-1" reports Ready as True with Reason AllObjectsReady
    And ClusterObjectDeployment "cod-${SCENARIO_ID}" reports Progressing as False with Reason NewClusterObjectSetProgressed
    And ClusterObjectDeployment "cod-${SCENARIO_ID}" reports Available as True with Reason Available
    And ClusterObjectDeployment "cod-${SCENARIO_ID}" owns 1 ClusterObjectSet
    And resource "configmap/cod-demo-settings" matches
      """
      data:
        message: Hello from revision A
      """
    And resource "configmap/cod-demo-settings" is controlled by ClusterObjectSet "cod-${SCENARIO_ID}-1"
    And resource "configmap/cod-demo-settings" UID is remembered
    And resource "clusterobjectdeployment/cod-${SCENARIO_ID}" is displayed with columns "NAME:.metadata.name,CONDITION:.status.conditions[*].type,STATUS:.status.conditions[*].status,REASON:.status.conditions[*].reason"
    And resource "clusterobjectsets" matching field selector "spec.group=cod-${SCENARIO_ID}" is displayed with columns "NAME:.metadata.name,REVISION:.spec.revision,STATE:.spec.lifecycleState,OWNER:.metadata.ownerReferences[0].name"
    And resource "configmap/cod-demo-settings" is displayed with columns "MESSAGE:.data.message,OWNER:.metadata.ownerReferences[0].name,UID:.metadata.uid"

    # B: wait for the new COS, not Available=True from the old revision.
    When resource is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectDeployment
      metadata:
        name: cod-${SCENARIO_ID}
      spec:
        revisionHistoryLimit: 2
        template:
          spec:
            collisionProtection: Prevent
            phases:
            - name: resources
              objects:
              - type: InlineManifest
                inlineManifest:
                  apiVersion: v1
                  kind: ConfigMap
                  metadata:
                    name: cod-demo-settings
                    namespace: ${TEST_NAMESPACE}
                  data:
                    message: Hello from revision B
      """
    Then ClusterObjectSet "cod-${SCENARIO_ID}-2" reports Ready as True with Reason AllObjectsReady
    And ClusterObjectDeployment "cod-${SCENARIO_ID}" reports Progressing as False with Reason NewClusterObjectSetProgressed
    And ClusterObjectDeployment "cod-${SCENARIO_ID}" reports Available as True with Reason Available
    And ClusterObjectDeployment "cod-${SCENARIO_ID}" owns 2 ClusterObjectSets
    And resource "configmap/cod-demo-settings" matches
      """
      data:
        message: Hello from revision B
      """
    And resource "configmap/cod-demo-settings" is controlled by ClusterObjectSet "cod-${SCENARIO_ID}-2"
    And resource "configmap/cod-demo-settings" keeps its UID
    And resource "configmap/cod-demo-settings" is displayed with columns "MESSAGE:.data.message,OWNER:.metadata.ownerReferences[0].name,UID:.metadata.uid"

    # A again: restoring a template creates revision 3, not a reactivated revision 1.
    When resource is applied
      """
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterObjectDeployment
      metadata:
        name: cod-${SCENARIO_ID}
      spec:
        revisionHistoryLimit: 2
        template:
          spec:
            collisionProtection: Prevent
            phases:
            - name: resources
              objects:
              - type: InlineManifest
                inlineManifest:
                  apiVersion: v1
                  kind: ConfigMap
                  metadata:
                    name: cod-demo-settings
                    namespace: ${TEST_NAMESPACE}
                  data:
                    message: Hello from revision A
      """
    Then ClusterObjectSet "cod-${SCENARIO_ID}-3" reports Ready as True with Reason AllObjectsReady
    And ClusterObjectDeployment "cod-${SCENARIO_ID}" reports Progressing as False with Reason NewClusterObjectSetProgressed
    And ClusterObjectDeployment "cod-${SCENARIO_ID}" reports Available as True with Reason Available
    And ClusterObjectDeployment "cod-${SCENARIO_ID}" owns 3 ClusterObjectSets
    And ClusterObjectSet "cod-${SCENARIO_ID}-1" is archived
    And ClusterObjectSet "cod-${SCENARIO_ID}-2" is archived
    And resource "clusterobjectset/cod-${SCENARIO_ID}-3" matches
      """
      spec:
        group: cod-${SCENARIO_ID}
        revision: 3
        lifecycleState: Active
      """
    And resource "configmap/cod-demo-settings" matches
      """
      data:
        message: Hello from revision A
      """
    And resource "configmap/cod-demo-settings" is controlled by ClusterObjectSet "cod-${SCENARIO_ID}-3"
    And resource "configmap/cod-demo-settings" keeps its UID
    And resource "clusterobjectsets" matching field selector "spec.group=cod-${SCENARIO_ID}" is displayed with columns "NAME:.metadata.name,REVISION:.spec.revision,STATE:.spec.lifecycleState,OWNER:.metadata.ownerReferences[0].name"
    And resource "configmap/cod-demo-settings" is displayed with columns "MESSAGE:.data.message,OWNER:.metadata.ownerReferences[0].name,UID:.metadata.uid"

    # Remove the parent and verify cascading cleanup before deleting the namespace.
    When resource "clusterobjectdeployment/cod-${SCENARIO_ID}" is removed
    Then resource "clusterobjectdeployment/cod-${SCENARIO_ID}" is not found
    And resource "clusterobjectset/cod-${SCENARIO_ID}-1" is not found
    And resource "clusterobjectset/cod-${SCENARIO_ID}-2" is not found
    And resource "clusterobjectset/cod-${SCENARIO_ID}-3" is not found
    And resource "configmap/cod-demo-settings" is not found
