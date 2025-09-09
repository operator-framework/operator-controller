# Consuming Metrics

!!! warning
Metrics endpoints and ports are available as an alpha release and are subject to change in future versions.
The following procedure is provided as an example for testing purposes. Do not depend on alpha features in production clusters.

In OLM v1, you can use the provided metrics with tools such as the [Prometheus Operator][prometheus-operator]. By default, Operator Controller and catalogd export metrics to the `/metrics` endpoint of each service.

You must grant the necessary permissions to access the metrics by using [role-based access control (RBAC) polices][rbac-k8s-docs]. You will also need to create a `NetworkPolicy` to allow egress traffic from your scraper pod, as the OLM namespace by default allows only `catalogd` and `operator-controller` to send and receive traffic.
Because the metrics are exposed over HTTPS by default, you need valid certificates to use the metrics with services such as Prometheus.
The following sections cover enabling metrics, validating access, and provide a reference of a `ServiceMonitor`
to illustrate how you might integrate the metrics with the [Prometheus Operator][prometheus-operator] or other third-part solutions.

---

## Enabling metrics for the Operator Controller

1. To enable access to the Operator controller metrics, create a `ClusterRoleBinding` resource by running the following command:

```shell
kubectl create clusterrolebinding operator-controller-metrics-binding \
   --clusterrole=operator-controller-metrics-reader \
   --serviceaccount=olmv1-system:operator-controller-controller-manager
```

2. Next, create a `NetworkPolicy` to allow the scraper pods to send their scrape requests:

```shell
kubectl apply -f - << EOF
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: scraper-policy
  namespace: olmv1-system
spec:
  podSelector:
    matchLabels:
      metrics: scraper
  policyTypes:
    - Egress
  egress:
    - {}  # Allows all egress traffic for metrics requests
EOF
```
### Validating Access Manually

1. Generate a token for the service account and extract the required certificates:

```shell
TOKEN=$(kubectl create token operator-controller-controller-manager -n olmv1-system)
echo $TOKEN
```

2. Apply the following YAML to deploy a pod in a namespace to consume the metrics:

```shell
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: curl-metrics
  namespace: olmv1-system
  labels:
    metrics: scraper
spec:
  serviceAccountName: operator-controller-controller-manager
  containers:
  - name: curl
    image: curlimages/curl:latest
    command:
    - sh
    - -c
    - sleep 3600
    securityContext:
      runAsNonRoot: true
      readOnlyRootFilesystem: true
      runAsUser: 1000
      runAsGroup: 1000
      allowPrivilegeEscalation: false
      capabilities:
        drop:
        - ALL
    volumeMounts:
    - mountPath: /tmp/cert
      name: olm-cert
      readOnly: true
  volumes:
  - name: olm-cert
    secret:
      secretName: olmv1-cert
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    seccompProfile:
        type: RuntimeDefault
  restartPolicy: Never
EOF
```

3. Run the following command using the `TOKEN` value obtained above to check the metrics:

```shell
kubectl exec -it curl-metrics -n olmv1-system -- \
curl -v -k -H "Authorization: Bearer ${TOKEN}" \
https://operator-controller-service.olmv1-system.svc.cluster.local:8443/metrics
```

4. Run the following command to validate the certificates and token:

```shell
kubectl exec -it curl-metrics -n olmv1-system -- \
curl -v --cacert /tmp/cert/ca.crt --cert /tmp/cert/tls.crt --key /tmp/cert/tls.key \
-H "Authorization: Bearer ${TOKEN}" \
https://operator-controller-service.olmv1-system.svc.cluster.local:8443/metrics
```

---

## Enabling metrics for the Operator CatalogD

1. To enable access to the CatalogD metrics, create a `ClusterRoleBinding` for the CatalogD service account:

```shell
kubectl create clusterrolebinding catalogd-metrics-binding \
   --clusterrole=catalogd-metrics-reader \
   --serviceaccount=olmv1-system:catalogd-controller-manager
```

### Validating Access Manually

1. Generate a token and get the required certificates:

```shell
TOKEN=$(kubectl create token catalogd-controller-manager -n olmv1-system)
echo $TOKEN
```

2. Run the following command to obtain the name of the secret which store the certificates:

```shell
OLM_SECRET=$(kubectl get secret -n olmv1-system -o jsonpath="{.items[*].metadata.name}" | tr ' ' '\n' | grep '^catalogd-service-cert')
echo $OLM_SECRET
```

3. Apply the following YAML to deploy a pod in a namespace to consume the metrics:

```shell
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: curl-metrics-catalogd
  namespace: olmv1-system
  labels:
    metrics: scraper
spec:
  serviceAccountName: catalogd-controller-manager
  containers:
  - name: curl
    image: curlimages/curl:latest
    command:
    - sh
    - -c
    - sleep 3600
    securityContext:
      runAsNonRoot: true
      readOnlyRootFilesystem: true
      runAsUser: 1000
      runAsGroup: 1000
      allowPrivilegeEscalation: false
      capabilities:
        drop:
        - ALL
    volumeMounts:
    - mountPath: /tmp/cert
      name: catalogd-cert
      readOnly: true
  volumes:
  - name: catalogd-cert
    secret:
      secretName: $OLM_SECRET
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    seccompProfile:
        type: RuntimeDefault
  restartPolicy: Never
EOF
```

4. Run the following command using the `TOKEN` value obtained above to check the metrics:

```shell
kubectl exec -it curl-metrics -n olmv1-system -- \
curl -v -k -H "Authorization: Bearer ${TOKEN}" \
https://catalogd-service.olmv1-system.svc.cluster.local:7443/metrics
```

5. Run the following command to validate the certificates and token:
```shell
kubectl exec -it curl-metrics -n olmv1-system -- \
curl -v --cacert /tmp/cert/ca.crt --cert /tmp/cert/tls.crt --key /tmp/cert/tls.key \
-H "Authorization: Bearer ${TOKEN}" \
https://catalogd-service.olmv1-system.svc.cluster.local:7443/metrics
```

---

## Integrating the metrics endpoints with third-party solutions

In many cases, you must provide the certificates and the `ServiceName` resources to integrate metrics endpoints with third-party solutions.
The following example illustrates how to create a `ServiceMonitor` resource to scrape metrics for the [Prometheus Operator][prometheus-operator] in OLM v1.

!!! note
The following manifests are provided as a reference mainly to let you know how to configure the certificates.
The following procedure is not a complete guide to configuring the Prometheus Operator or how to integrate within.
To integrate with [Prometheus Operator][prometheus-operator] you might need to adjust your
configuration settings, such as the `serviceMonitorSelector` resource, and the namespace
where you apply the `ServiceMonitor` resource to ensure that metrics are properly scraped.

**Example for Operator-Controller**

```shell
kubectl apply -f - <<EOF
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  labels:
    apps.kubernetes.io/name: operator-controller
  name: controller-manager-metrics-monitor
  namespace: olmv1-system
spec:
  endpoints:
    - path: /metrics
      port: https
      scheme: https
      bearerTokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token
      tlsConfig:
        insecureSkipVerify: false 
        serverName: operator-controller-service.olmv1-system.svc
        ca:
          secret:
            name: olmv1-cert
            key: ca.crt
        cert:
          secret:
            name: olmv1-cert
            key: tls.crt
        keySecret:
          name: olmv1-cert
          key: tls.key
  selector:
    matchLabels:
      apps.kubernetes.io/name: operator-controller
EOF
```

**Example for CatalogD**

```shell
OLM_SECRET=$(kubectl get secret -n olmv1-system -o jsonpath="{.items[*].metadata.name}" | tr ' ' '\n' | grep '^catalogd-service-cert')
echo $OLM_SECRET
```

```shell
kubectl apply -f - <<EOF
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  labels:
    apps.kubernetes.io/name: catalogd
  name: catalogd-metrics-monitor
  namespace: olmv1-system
spec:
  endpoints:
    - path: /metrics
      port: metrics
      scheme: https
      bearerTokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token
      tlsConfig:
        serverName: catalogd-service.olmv1-system.svc
        insecureSkipVerify: false
        ca:
          secret:
            name: $OLM_SECRET
            key: ca.crt
        cert:
          secret:
            name: $OLM_SECRET
            key: tls.crt
        keySecret:
          name: $OLM_SECRET
          key: tls.key
  selector:
    matchLabels:
      app.kubernetes.io/name: catalogd
EOF
```

[prometheus-operator]: https://github.com/prometheus-operator/kube-prometheus
[rbac-k8s-docs]: https://kubernetes.io/docs/reference/access-authn-authz/rbac/
