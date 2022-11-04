# rukpak-packageserver

This repository is a prototype for a custom apiserver that uses a (dedicated ectd instance)[configs/etcd] to serve [FBC](https://olm.operatorframework.io/docs/reference/file-based-catalogs/#docs) content on cluster in a Kubernetes native way on cluster.


## Enhacement 

https://hackmd.io/@i2YBW1rSQ8GcKcTIHn9CCA/B1cMe1kHj

## Quickstart. 

```
$ kind create cluster
$ kubectl apply -f https://github.com/anik120/rukpak-packageserver/config/crd/bases/
$ kubectl apply -f https://github.com/anik120/rukpak-packageserver/config/
$ kubectl create ns test
$ kubectl apply -f config/samples/rukpak_catalogsource.yaml

$ kubectl get catalogsource -n test 
NAME                   AGE
catalogsource-sample   98m

$ kubectl get catalogcache -n test 
NAME                   AGE
catalogsource-sample   77m

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

 ## Demo 

 The rukpak-packageserver can be thought of as packageserver 2.0, which takes advantage of the FBC format (and the underlying library https://github.com/operator-framework/operator-registry/tree/master/alpha/declcfg) with a dedicated etcd instance, to efficiently expose the content of an index image inside a cluster (eliminating the need to connect to expensive services or build caches expanding memory surface for clients.)

![DEMO](./docs/static_includes/demo.gif)