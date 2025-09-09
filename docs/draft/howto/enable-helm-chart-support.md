# How to Enable Helm Chart Support Feature Gate

## Description

This document outlines the steps to enable the Helm Chart support feature gate in the OLMv1 and subsequently deploy a Helm Chart to a Kubernetes cluster. It involves patching the `operator-controller-controller-manager` deployment to enable the `HelmChartSupport` feature, setting up a network policy for the registry, deploying an OCI registry, and finally creating a ClusterExtension to deploy the metrics server helm chart.

The feature allows developers and end-users to deploy Helm charts from OCI registries through the `ClusterExtension` API.

## Demos

[![Helm Chart Support Demo](https://asciinema.org/a/wEzsqXLDAflJvzSX7QP47GvLw.svg)](https://asciinema.org/a/wEzsqXLDAflJvzSX7QP47GvLw)


## Enabling the Feature Gate

To enable the Helm Chart support feature gate, you need to patch the `operator-controller-controller-manager` deployment in the `olmv1-system` namespace. This will add the `--feature-gates=HelmChartSupport=true` argument to the manager container.

1.  **Create a patch file:**

    ```bash
    $ kubectl patch deployment -n olmv1-system operator-controller-controller-manager --type='json' -p='[{"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--feature-gates=HelmChartSupport=true"}]'
    ```

2.  **Wait for the controller manager pods to be ready:**

    ```bash
    $ kubectl -n olmv1-system wait --for condition=ready pods -l apps.kubernetes.io/name=operator-controller
    ```

Once the above wait condition is met, the `HelmChartSupport` feature gate should be enabled in operator controller.

## Deploy an OCI Chart registry for testing

With the operator-controller pod running with the `HelmChartSupport` feature gate enabled, you would need access to a Helm charts 
hosted in an OCI registry. For this demo, the instructions will walk you through steps to deploy a registry in the `olmv1-system`
project.

In addition to the OCI registry, you will need a ClusterCatalog in the Kubernetes cluster which will reference Helm charts in the OCI registry.

1.  **Configure network policy for the registry:**

    ```bash
    $ cat << EOF | kubectl -n olmv1-system apply -f -
    apiVersion: networking.k8s.io/v1
    kind: NetworkPolicy
    metadata:
      name: registry
    spec:
      egress:
      - {}
      ingress:
      - ports:
        - port: 8443
          protocol: TCP
      podSelector:
        matchLabels:
          app: registry
      policyTypes:
      - Ingress
      - Egress
    EOF
    ```

2. **Create certificates for the OCI registry:**

   ```bash
   $ cat << EOF | kubectl -n olmv1-system apply -f -
   ---
   apiVersion: cert-manager.io/v1
   kind: Certificate
   metadata:
     name: registry-cert
     namespace: olmv1-system
   spec:
     dnsNames:
       - registry.olmv1-system.svc
       - registry.olmv1-system.svc.cluster.local
     issuerRef:
       group: cert-manager.io
       kind: ClusterIssuer
       name: olmv1-ca
     privateKey:
       algorithm: RSA
       encoding: PKCS1
       size: 2048
     secretName: registry-cert
   status: {}
   EOF
   ```

3.  **Deploy an OCI registry:**

    ```bash
    $ cat << EOF | kubectl -n olmv1-system apply -f -
    ---
    apiVersion: apps/v1
    kind: Deployment
    metadata:
      creationTimestamp: null
      labels:
        app: registry
      name: registry
    spec:
      replicas: 1
      selector:
        matchLabels:
          app: registry
      strategy: {}
      template:
        metadata:
          creationTimestamp: null
          labels:
            app: registry
        spec:
          containers:
            - name: registry
              image: docker.io/library/registry:3.0.0
              env:
                - name: REGISTRY_HTTP_ADDR
                  value: "0.0.0.0:8443"
                - name: REGISTRY_HTTP_TLS_CERTIFICATE
                  value: "/certs/tls.crt"
                - name: REGISTRY_HTTP_TLS_KEY
                  value: "/certs/tls.key"
                - name: OTEL_TRACES_EXPORTER
                  value: "none"
              ports:
                - name: registry
                  protocol: TCP
                  containerPort: 8443
              securityContext:
                runAsUser: 999
                allowPrivilegeEscalation: false
                runAsNonRoot: true
                seccompProfile:
                  type: "RuntimeDefault"
                capabilities:
                  drop:
                    - ALL
              volumeMounts:
                - name: blobs
                  mountPath: /var/lib/registry/docker
                - name: certs
                  mountPath: /certs
              resources: {}
          volumes:
            - name: blobs
              emptyDir: {}
            - name: certs
              secret:
                secretName: registry-cert
    status: {}
    EOF
    ```

4. **Expose the registry container:**

   ```bash
   $ cat << EOF | kubectl -n olmv1-system apply -f -
   ---
   apiVersion: v1
   kind: Service
   metadata:
     creationTimestamp: null
     labels:
       app: registry
     name: registry
     namespace: olmv1-system
   spec:
     ports:
       - port: 443
         protocol: TCP
         targetPort: 8443
     selector:
       app: registry
   status:
     loadBalancer: {}
   EOF
   ```

5.  **Wait for the registry pod to be in a Running phase:**

    ```bash
    $ kubectl -n olmv1-system wait --for=jsonpath='{.status.phase}'=Running pod -l app=registry
    ```

6.  **Deploy the cluster catalog:**

    ```bash
    $ cat << EOF | kubectl apply -f -
    ---
    apiVersion: olm.operatorframework.io/v1
    kind: ClusterCatalog
    metadata:
      name: metrics-server-operators
      namespace: olmv1-system
    spec:
      priority: -100
      source:
        image:
          pollIntervalMinutes: 5
          ref: quay.io/eochieng/metrics-server-catalog:latest
        type: Image
    EOF
    ```

7.  **Upload charts to the registry:**

    ```bash
    $ cat << EOF | kubectl apply -f -
    ---
    apiVersion: batch/v1
    kind: Job
    metadata:
      creationTimestamp: null
      name: chart-uploader
    spec:
      template:
        metadata:
          creationTimestamp: null
        spec:
          containers:
          - image: quay.io/eochieng/uploader:latest
            name: chart-uploader
            resources: {}
          restartPolicy: Never
    status: {}
    EOF
    ```

8.  **Deploy metrics server RBAC and metrics server:**
    
    ```bash
    $ cat << EOF | kubectl apply -f -
    ---
    apiVersion: v1
    kind: Namespace
    metadata:
      creationTimestamp: null
      name: metrics-server-system
    ---
    apiVersion: v1
    kind: ServiceAccount
    metadata:
      creationTimestamp: null
      name: metrics-server-installer
      namespace: metrics-server-system
    ---
    apiVersion: rbac.authorization.k8s.io/v1
    kind: ClusterRoleBinding
    metadata:
      creationTimestamp: null
      name: metrics-server-crb
    roleRef:
      apiGroup: rbac.authorization.k8s.io
      kind: ClusterRole
      name: metrics-server-cr
    subjects:
    - kind: ServiceAccount
      name: metrics-server-installer
      namespace: metrics-server-system
    ---
    apiVersion: rbac.authorization.k8s.io/v1
    kind: ClusterRole
    metadata:
      creationTimestamp: null
      name: metrics-server-cr
    rules:
    - apiGroups:
      - ""
      resources:
      - serviceaccounts
      verbs:
      - create
      - delete
      - list
      - watch
      - get
      - patch
      - update
    - apiGroups:
      - rbac.authorization.k8s.io
      resources:
      - clusterroles
      - clusterrolebindings
      - rolebindings
      verbs:
      - create
      - delete
      - list
      - watch
      - get
      - patch
      - update
    - apiGroups:
      - ""
      resources:
      - services
      - secrets
      verbs:
      - get
      - list
      - watch
      - create
      - delete
      - patch
      - update
    - apiGroups:
      - apps
      resources:
      - deployments
      - deployments/finalizers
      verbs:
      - get
      - list
      - watch
      - create
      - delete
      - patch
      - update
    - apiGroups:
      - apiregistration.k8s.io
      resources:
      - apiservices
      verbs:
      - get
      - list
      - watch
      - create
      - delete
      - patch
      - update
    - apiGroups:
      - olm.operatorframework.io
      resources:
      - clusterextensions
      - clusterextensions/finalizers
      verbs:
      - get
      - list
      - watch
      - create
      - delete
      - update
      - patch
    - apiGroups:
      - metrics.k8s.io
      resources:
      - nodes
      - pods
      verbs:
      - get
      - list
      - watch
    - apiGroups:
      - ""
      resources:
      - configmaps
      - namespaces
      - nodes
      - pods
      verbs:
      - get
      - list
      - watch
    - apiGroups:
      - ""
      resources:
      - nodes/metrics
      verbs:
      - get
    - apiGroups:
      - authentication.k8s.io
      resources:
      - tokenreviews
      verbs:
      - create
    - apiGroups:
      - authorization.k8s.io
      resources:
      - subjectaccessreviews
      verbs:
      - create
    EOF
    ```

9.  **Deploy metrics server cluster extension:**

    ```bash
    $ cat << EOF | kubectl apply -f -
    ---
    apiVersion: olm.operatorframework.io/v1
    kind: ClusterExtension
    metadata:
      name: metrics-server
      namespace: metrics-server-system
    spec:
      namespace: metrics-server-system
      serviceAccount:
        name: metrics-server-installer
      source:
        sourceType: Catalog
        catalog:
          packageName: metrics-server
          version: 3.12.0
    EOF
    ```

10. **Confirm the Helm chart has been deployed:**

   ```bash
   $ kubectl get clusterextensions metrics-server
   NAME             INSTALLED BUNDLE         VERSION   INSTALLED   PROGRESSING   AGE
   metrics-server   metrics-server.v3.12.0   3.12.0    True        True          4m40s
   ```
