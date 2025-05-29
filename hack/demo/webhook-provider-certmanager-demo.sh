#!/usr/bin/env bash

#
# Welcome to the webhook support with CertManager demo
#
trap "trap - SIGTERM && kill -- -$$" SIGINT SIGTERM EXIT

# enable 'WebhookProviderCertManager' feature
kubectl kustomize config/overlays/featuregate/webhook-provider-certmanager | kubectl apply -f -

# wait for operator-controller to become available
kubectl rollout status -n olmv1-system deployment/operator-controller-controller-manager

# create webhook-operator catalog
cat ${DEMO_RESOURCE_DIR}/webhook-provider-certmanager/webhook-operator-catalog.yaml
kubectl apply -f ${DEMO_RESOURCE_DIR}/webhook-provider-certmanager/webhook-operator-catalog.yaml

# wait for catalog to be serving
kubectl wait --for=condition=Serving clustercatalog/webhook-operator-catalog --timeout="60s"

# create install namespace
kubectl create ns webhook-operator

# create installer service account
kubectl create serviceaccount -n webhook-operator webhook-operator-installer

# give installer service account admin privileges
kubectl create clusterrolebinding webhook-operator-installer-crb --clusterrole=cluster-admin --serviceaccount=webhook-operator:webhook-operator-installer

# install webhook operator clusterextension
cat ${DEMO_RESOURCE_DIR}/webhook-provider-certmanager/webhook-operator-extension.yaml

# apply cluster extension
kubectl apply -f ${DEMO_RESOURCE_DIR}/webhook-provider-certmanager/webhook-operator-extension.yaml

# wait for cluster extension installation to succeed
kubectl wait --for=condition=Installed clusterextension/webhook-operator --timeout="60s"

# wait for webhook-operator deployment to become available and back the webhook service
kubectl wait --for=condition=Available -n webhook-operator deployments/webhook-operator-webhook

# demonstrate working validating webhook
cat ${DEMO_RESOURCE_DIR}/webhook-provider-certmanager/validating-webhook-test.yaml

# resource creation should be rejected by the validating webhook due to bad attribute value
kubectl apply -f ${DEMO_RESOURCE_DIR}/webhook-provider-certmanager/validating-webhook-test.yaml

# demonstrate working mutating webhook
cat ${DEMO_RESOURCE_DIR}/webhook-provider-certmanager/mutating-webhook-test.yaml

# apply resource
kubectl apply -f ${DEMO_RESOURCE_DIR}/webhook-provider-certmanager/mutating-webhook-test.yaml

# get webhooktest resource in v1 schema - resource should have new .spec.mutate attribute
kubectl get webhooktest.v1.webhook.operators.coreos.io -n webhook-operator mutating-webhook-test -o yaml

# demonstrate working conversion webhook by getting webhook test resource in v2 schema - the .spec attributes should now be under the .spec.conversion stanza
kubectl get webhooktest.v2.webhook.operators.coreos.io -n webhook-operator mutating-webhook-test -o yaml

# this concludes the webhook support demo - Thank you!
