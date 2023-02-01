#!/bin/bash

kind delete cluster && kind create cluster

kubectl apply -f config/crd/bases
kubectl apply -f config/