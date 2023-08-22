Operator authors have the mechanisms to offer their product as part of a curated catalog of operators, that they can push updates to over-the-air (eg publish new versions, publish patched versions with CVEs, etc). Cluster admins can sign up to receive these updates on clusters, by adding the catalog to the cluster. When a catalog is added to a cluster, the kubernetes extension packages (operators, or any other extension package) in that catalog become available on cluster for installation and receiving updates.  

For example, the [k8s-operatorhub/community-operators](https://github.com/k8s-operatorhub/community-operators) is a catalog of curated operators that contains a list of operators being developed by the community. The list of operators can be viewed in [Operatorhub.io](https://operatorhub.io). This catalog is distributed as an image [quay.io/operatorhubio/catalog](https://quay.io/repository/operatorhubio/catalog?tag=latest&tab=tags) for consumption on clusters. 

To consume this catalog on cluster, create a `Catalog` Custom Resource(CR) with the image specified in the `spec.source.image` field: 

```bash
$ kubectl apply -f - <<EOF
apiVersion: catalogd.operatorframework.io/v1alpha1
kind: Catalog
metadata:
  name: operatorhubio
spec:
  source:
    type: image
    image:
      ref: quay.io/operatorhubio/catalog:latest
EOF
```

The packages made available for installation/receiving updates on cluster can then be explored by querying the `Package` and `BundleMetadata` CRs: 

```bash
$ kubectl get packages 
NAME                                                     AGE
operatorhubio-ack-acm-controller                         3m12s
operatorhubio-ack-apigatewayv2-controller                3m12s
operatorhubio-ack-applicationautoscaling-controller      3m12s
operatorhubio-ack-cloudtrail-controller                  3m12s
operatorhubio-ack-dynamodb-controller                    3m12s
operatorhubio-ack-ec2-controller                         3m12s
operatorhubio-ack-ecr-controller                         3m12s
operatorhubio-ack-eks-controller                         3m12s
operatorhubio-ack-elasticache-controller                 3m12s
operatorhubio-ack-emrcontainers-controller               3m12s
operatorhubio-ack-eventbridge-controller                 3m12s
operatorhubio-ack-iam-controller                         3m12s
operatorhubio-ack-kinesis-controller                     3m12s
operatorhubio-ack-kms-controller                         3m12s
operatorhubio-ack-lambda-controller                      3m12s
operatorhubio-ack-memorydb-controller                    3m12s
operatorhubio-ack-mq-controller                          3m12s
operatorhubio-ack-opensearchservice-controller           3m12s
.
.
.

$ kubectl get bundlemetadata 
NAME                                                            AGE
operatorhubio-ack-acm-controller.v0.0.1                         3m58s
operatorhubio-ack-acm-controller.v0.0.2                         3m58s
operatorhubio-ack-acm-controller.v0.0.4                         3m58s
operatorhubio-ack-acm-controller.v0.0.5                         3m58s
operatorhubio-ack-acm-controller.v0.0.6                         3m58s
operatorhubio-ack-apigatewayv2-controller.v0.0.10               3m58s
operatorhubio-ack-apigatewayv2-controller.v0.0.11               3m58s
operatorhubio-ack-apigatewayv2-controller.v0.0.12               3m58s
operatorhubio-ack-apigatewayv2-controller.v0.0.13               3m58s
operatorhubio-ack-apigatewayv2-controller.v0.0.14               3m58s
operatorhubio-ack-apigatewayv2-controller.v0.0.15               3m58s
operatorhubio-ack-apigatewayv2-controller.v0.0.16               3m58s
operatorhubio-ack-apigatewayv2-controller.v0.0.17               3m58s
operatorhubio-ack-apigatewayv2-controller.v0.0.18               3m58s
operatorhubio-ack-apigatewayv2-controller.v0.0.19               3m58s
operatorhubio-ack-apigatewayv2-controller.v0.0.20               3m58s
operatorhubio-ack-apigatewayv2-controller.v0.0.21               3m58s
operatorhubio-ack-apigatewayv2-controller.v0.0.22               3m58s
operatorhubio-ack-apigatewayv2-controller.v0.0.9                3m58s
operatorhubio-ack-apigatewayv2-controller.v0.1.0                3m58s
operatorhubio-ack-apigatewayv2-controller.v0.1.1                3m58s
operatorhubio-ack-apigatewayv2-controller.v0.1.2                3m58s
operatorhubio-ack-apigatewayv2-controller.v0.1.3                3m58s
.
.
.
```


