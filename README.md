# catalogd

This repository is a prototype for a custom apiserver that uses a (dedicated ectd instance)[configs/etcd] to serve [FBC](https://olm.operatorframework.io/docs/reference/file-based-catalogs/#docs) content on cluster in a Kubernetes native way on cluster.


## Enhacement 

https://hackmd.io/@i2YBW1rSQ8GcKcTIHn9CCA/B1cMe1kHj

## Quickstart. 

```
$ kind create cluster
$ kubectl apply -f https://github.com/operator-framework/catalogd/config/crd/bases/
$ kubectl apply -f https://github.com/operator-framework/catalogd/config/
$ kubectl create ns test
$ kubectl apply -f config/samples/core_v1alpha1_catalog.yaml

$ kubectl get catalog -n test 
NAME                   AGE
catalog-sample   98s

$ kubectl get bundlemetadata -n test 
NAME                                               AGE
3scale-community-operator.v0.7.0                   28s
3scale-community-operator.v0.8.2                   28s
3scale-community-operator.v0.9.0                   28s
falcon-operator.v0.5.1                             2s
falcon-operator.v0.5.2                             2s
falcon-operator.v0.5.3                             1s
falcon-operator.v0.5.4                             1s
falcon-operator.v0.5.5                             1s
flux.v0.13.4                                       1s
flux.v0.14.0                                       1s
flux.v0.14.1                                       1s
flux.v0.14.2                                       1s
flux.v0.15.2                                       1s
flux.v0.15.3                                       1s
.
.
.

$ kubectl get packages -n test 
NAME                                        AGE
3scale-community-operator                   77m
ack-apigatewayv2-controller                 77m
ack-applicationautoscaling-controller       77m
ack-dynamodb-controller                     77m
ack-ec2-controller                          77m
ack-ecr-controller                          77m
ack-eks-controller                          77m
ack-elasticache-controller                  77m
ack-emrcontainers-controller                77m
ack-iam-controller                          77m
ack-kms-controller                          77m
ack-lambda-controller                       77m
ack-mq-controller                           77m
ack-opensearchservice-controller            77m
.
.
.
```

## Contributing
Thanks for your interest in contributing to `catalogd`!

`catalogd` is in the very early stages of development and a more in depth contributing guide will come in the near future.

In the mean time, it is assumed you know how to make contributions to open source projects in general and this guide will only focus on how to manually test your changes (no automated testing yet).

If you have any questions, feel free to reach out to us on the Kubernetes Slack channel [#olm-dev](https://kubernetes.slack.com/archives/C0181L6JYQ2) or [create an issue](https://github.com/operator-framework/catalogd/issues/new)
### Testing Local Changes
**Prerequisites**
- [Install kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)

**Local (not on cluster)**
> **Note**: This will work *only* for the controller
- Create a cluster:
```sh
kind create cluster
```
- Install CRDs and run the controller locally:
```sh
kubectl apply -f config/crd/bases/ && make run
```

**On Cluster**
- Build the images locally:
```sh
make docker-build-controller && make docker-build-server
```
- Create a cluster:
```sh
kind create cluster
```
- Load the images onto the cluster:
```sh
kind load docker-image quay.io/operator-framework/catalogd-controller:latest && kind load docker-image quay.io/operator-framework/catalogd-server:latest
``` 
- Install cert-manager:
```sh
 make cert-manager
```
- Install the CRDs
```sh
kubectl apply -f config/crd/bases/
```
- Deploy the apiserver, etcd, and controller: 
```sh
kubectl apply -f config/
```
- Create the sample Catalog (this will trigger the reconciliation loop): 
```sh
kubectl apply -f config/samples/core_v1alpha1_catalog.yaml
```
