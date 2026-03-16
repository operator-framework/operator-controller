---
hide:
  - toc
---

# Explore Available Content

After you [add a catalog of extensions](add-catalog.md) to your cluster, you must port forward your catalog as a service.
Then you can query the catalog by using `curl` commands and the `jq` CLI tool to find extensions to install.

## Prerequisites

* You have added a ClusterCatalog of extensions, such as [OperatorHub.io](https://operatorhub.io), to your cluster.
* You have installed the `jq` CLI tool.

!!! note
    By default, Catalogd is installed with TLS enabled for the catalog webserver.
    The following examples will show this default behavior, but for simplicity's sake will ignore TLS verification in the curl commands using the `-k` flag.

## Procedure

1. Port forward the catalog server service:

    ``` terminal
    kubectl -n olmv1-system port-forward svc/catalogd-service 8443:443
    ```

2. Return a list of all the extensions in a catalog via the v1 API endpoint:
    ``` terminal
    curl -k https://localhost:8443/catalogs/operatorhubio/api/v1/all | jq -s '.[] | select(.schema == "olm.package") | .name'
    ```

    ??? success
        ``` text title="Example output"
        "ack-acm-controller"
        "ack-acmpca-controller"
        "ack-apigatewayv2-controller"
        "ack-applicationautoscaling-controller"
        "ack-cloudfront-controller"
        "ack-cloudtrail-controller"
        "ack-cloudwatch-controller"
        "ack-cloudwatchlogs-controller"
        "ack-dynamodb-controller"
        "ack-ec2-controller"
        "ack-ecr-controller"
        "ack-ecs-controller"
        "ack-efs-controller"
        "ack-eks-controller"
        "ack-elasticache-controller"
        "ack-emrcontainers-controller"
        "ack-eventbridge-controller"
        "ack-iam-controller"
        "ack-kafka-controller"
        "ack-keyspaces-controller"
        "ack-kinesis-controller"
        "ack-kms-controller"
        "ack-lambda-controller"
        "ack-memorydb-controller"
        "ack-mq-controller"
        "ack-networkfirewall-controller"
        "ack-opensearchservice-controller"
        "ack-pipes-controller"
        "ack-prometheusservice-controller"
        "ack-rds-controller"
        "ack-route53-controller"
        "ack-route53resolver-controller"
        "ack-s3-controller"
        "ack-sagemaker-controller"
        "ack-secretsmanager-controller"
        "ack-sfn-controller"
        "ack-sns-controller"
        "ack-sqs-controller"
        "aerospike-kubernetes-operator"
        "airflow-helm-operator"
        "aiven-operator"
        "akka-cluster-operator"
        "alvearie-imaging-ingestion"
        "anchore-engine"
        "apch-operator"
        "api-operator"
        "api-testing-operator"
        "apicast-community-operator"
        "apicurio-registry"
        "apimatic-kubernetes-operator"
        "app-director-operator"
        "appdynamics-operator"
        "application-services-metering-operator"
        "appranix"
        "aqua"
        "argocd-operator"
        ...
        ```

    !!! important
        OLM 1.0 supports installing extensions that define webhooks. Targeting a single or specified set of namespaces requires enabling the `SingleOwnNamespaceInstallSupport` feature-gate.

3. Return list of packages that support `AllNamespaces` install mode and do not use webhooks:

    ``` terminal
    curl -k https://localhost:8443/catalogs/operatorhubio/api/v1/all | jq -cs '[.[] | select(.schema == "olm.bundle" and (.properties[] | select(.type == "olm.csv.metadata").value.installModes[] | select(.type == "AllNamespaces" and .supported == true)) and .spec.webhookdefinitions == null) | .package] | unique[]'
    ```

    ??? success
        ``` text title="Example output"
        {"package":"ack-acm-controller","version":"0.0.12"}
        {"package":"ack-acmpca-controller","version":"0.0.5"}
        {"package":"ack-apigatewayv2-controller","version":"1.0.7"}
        {"package":"ack-applicationautoscaling-controller","version":"1.0.11"}
        {"package":"ack-cloudfront-controller","version":"0.0.9"}
        {"package":"ack-cloudtrail-controller","version":"1.0.8"}
        {"package":"ack-cloudwatch-controller","version":"0.0.3"}
        {"package":"ack-cloudwatchlogs-controller","version":"0.0.4"}
        {"package":"ack-dynamodb-controller","version":"1.2.9"}
        {"package":"ack-ec2-controller","version":"1.2.4"}
        {"package":"ack-ecr-controller","version":"1.0.12"}
        {"package":"ack-ecs-controller","version":"0.0.4"}
        {"package":"ack-efs-controller","version":"0.0.5"}
        {"package":"ack-eks-controller","version":"1.3.3"}
        {"package":"ack-elasticache-controller","version":"0.0.29"}
        {"package":"ack-emrcontainers-controller","version":"1.0.8"}
        {"package":"ack-eventbridge-controller","version":"1.0.6"}
        {"package":"ack-iam-controller","version":"1.3.6"}
        {"package":"ack-kafka-controller","version":"0.0.4"}
        {"package":"ack-keyspaces-controller","version":"0.0.11"}
        ...
        ```

4. Inspect the contents of an extension's metadata:

    ``` terminal
    curl -k https://localhost:8443/catalogs/operatorhubio/api/v1/all | jq -s '.[] | select( .schema == "olm.package") | select( .name == "<package_name>")'
    ```

    `package_name`
    :   Specifies the name of the package you want to inspect.

    ??? success
        ``` text title="Example output"
        {
          "defaultChannel": "stable-v6.x",
          "icon": {
            "base64data": "PHN2ZyB4bWxucz0ia...
            "mediatype": "image/svg+xml"
          },
          "name": "cockroachdb",
          "schema": "olm.package"
        }
        ```

### Additional resources

* [Catalog queries](../howto/catalog-queries.md)
