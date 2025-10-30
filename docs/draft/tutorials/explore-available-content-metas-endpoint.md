---
hide:
  - toc
---

# Explore Available Content

After you [add a catalog of extensions](../../tutorials/add-catalog.md) to your cluster, you must port forward your catalog as a service.
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
    curl -k https://localhost:8443/catalogs/operatorhubio/api/v1/metas?schema=olm.package' | jq -s '.[] | .name'
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

3. Return list of packages which support `AllNamespaces` install mode, do not use webhooks, and where the channel head version uses `olm.csv.metadata` format:

    ``` terminal
    curl -k https://localhost:8443/catalogs/operatorhubio/api/v1/metas?schema=olm.bundle | jq -cs '[.[] | select(.properties[] | select(.type == "olm.csv.metadata").value.installModes[] | select(.type == "AllNamespaces" and .supported == true) and .spec.webhookdefinitions == null) | .package] | unique[]'
    ```

    ??? success
        ``` text title="Example output"
        "ack-acm-controller"
        "ack-acmpca-controller"
        "ack-apigateway-controller"
        "ack-apigatewayv2-controller"
        "ack-applicationautoscaling-controller"
        "ack-athena-controller"
        "ack-cloudfront-controller"
        "ack-cloudtrail-controller"
        "ack-cloudwatch-controller"
        "ack-cloudwatchlogs-controller"
        "ack-documentdb-controller"
        "ack-dynamodb-controller"
        "ack-ec2-controller"
        "ack-ecr-controller"
        "ack-ecs-controller"
        ...
        ```

4. Inspect the contents of an extension's metadata:

    ``` terminal
    curl -k https://localhost:8443/catalogs/operatorhubio/api/v1/metas?schema=olm.package&name=<package_name>
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

* [Catalog queries](../../howto/catalog-queries.md)
