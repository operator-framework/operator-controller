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
$ kubectl apply -f config/samples/catalogsource.yaml

$ kubectl get catalogsource -n test 
NAME                   AGE
catalogsource-sample   98s

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

 
