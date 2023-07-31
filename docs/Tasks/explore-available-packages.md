---
layout: default
title: Exploring packages available for installation on cluster
nav_order: 2
parent: Tasks
---

The packages available for installation/receiving updates on cluster can be explored by querying the `Package` and `BundleMetadata` CRs: 

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

Individual `Package`/`BundleMetadata` CRs can then be explored more by retrieving their yamls. Eg the `operatorhubio-argocd-operator` CR has more detailed information about the `argocd-operator`: 

```bash
$ kubectl get packages | grep argocd 
operatorhubio-argocd-operator                            5m19s
operatorhubio-argocd-operator-helm                       5m19s

$ kubectl get package operatorhubio-argocd-operator -o yaml 
apiVersion: catalogd.operatorframework.io/v1alpha1
kind: Package
metadata:
  creationTimestamp: "2023-06-16T14:34:04Z"
  generation: 1
  labels:
    catalog: operatorhubio
  name: operatorhubio-argocd-operator
  ownerReferences:
  - apiVersion: catalogd.operatorframework.io/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: Catalog
    name: operatorhubio
    uid: 9a949664-9069-4376-9a66-a9921f7488e2
  resourceVersion: "3765"
  uid: 43396920-4af4-4daf-a069-be68b8a0631e
spec:
  catalog:
    name: operatorhubio
  channels:
  - entries:
    - name: argocd-operator.v0.0.11
      replaces: argocd-operator.v0.0.9
    - name: argocd-operator.v0.0.12
      replaces: argocd-operator.v0.0.11
    - name: argocd-operator.v0.0.13
      replaces: argocd-operator.v0.0.12
    - name: argocd-operator.v0.0.14
      replaces: argocd-operator.v0.0.13
    - name: argocd-operator.v0.0.15
      replaces: argocd-operator.v0.0.14
    - name: argocd-operator.v0.0.2
    - name: argocd-operator.v0.0.3
      replaces: argocd-operator.v0.0.2
    - name: argocd-operator.v0.0.4
      replaces: argocd-operator.v0.0.3
    - name: argocd-operator.v0.0.5
      replaces: argocd-operator.v0.0.4
    - name: argocd-operator.v0.0.6
      replaces: argocd-operator.v0.0.5
    - name: argocd-operator.v0.0.8
      replaces: argocd-operator.v0.0.6
    - name: argocd-operator.v0.0.9
      replaces: argocd-operator.v0.0.8
    - name: argocd-operator.v0.1.0
      replaces: argocd-operator.v0.0.15
    - name: argocd-operator.v0.2.0
      replaces: argocd-operator.v0.1.0
    - name: argocd-operator.v0.2.1
      replaces: argocd-operator.v0.2.0
    - name: argocd-operator.v0.3.0
      replaces: argocd-operator.v0.2.1
    - name: argocd-operator.v0.4.0
      replaces: argocd-operator.v0.3.0
    - name: argocd-operator.v0.5.0
      replaces: argocd-operator.v0.4.0
    - name: argocd-operator.v0.6.0
      replaces: argocd-operator.v0.5.0
    name: ""
  defaultChannel: ""
  description: ""
  icon:
    data: PD94bWwgdmVyc2lvbj0iMS4wIiBlbmNvZGluZz0iVVRGLTgiIHN0YW5kYWxvbmU9Im5vIj8+CjwhRE9DVFlQRSBzdmcgUFVCTElDICItLy9XM0MvL0RURCBTVkcgMS4xLy9FTiIgImh0dHA6Ly93d3cudzMub3JnL0dyYXBoaWNzL1NWRy8xLjEvRFREL3N2Z==
    mediatype: image/svg+xml
  packageName: argocd-operator
status: {}
```

**This CR is most helpful when exploring the versions of a package that are available for installation on cluster, and the upgrade graph of versions** (eg if v0.5.0 of `argocd-operator` is installed on cluster, what is the next upgrade available? The answer is v0.6.0).