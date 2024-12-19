# Consuming Metrics

Operator-Controller and CatalogD are configured to export metrics by default. The metrics are exposed on the `/metrics` endpoint of the respective services.

The metrics are protected by RBAC policies, and you need to have the appropriate permissions to access them.
By default, the metrics are exposed over HTTPS, and you need to have the appropriate certificates to access them via other services such as Prometheus.

Below, you will learn how to enable the metrics, validate access, and integrate with [Prometheus Operator][prometheus-operator].

---

## Operator-Controller Metrics

### Step 1: Enable Access

To enable access to the Operator-Controller metrics, create a `ClusterRoleBinding` to allow the Operator-Controller service account to access the metrics.

```shell
kubectl create clusterrolebinding operator-controller-metrics-binding \
   --clusterrole=operator-controller-metrics-reader \
   --serviceaccount=olmv1-system:operator-controller-controller-manager
```

### Step 2: Validate Access Manually

#### Create a Token and Extract Certificates

Generate a token for the service account:

```shell
TOKEN=$(kubectl create token operator-controller-controller-manager -n olmv1-system)
echo $TOKEN
```

#### Deploy a Pod to Consume Metrics

```shell
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: curl-metrics
  namespace: olmv1-system
  labels:
    access: restricted
spec:
  serviceAccountName: operator-controller-controller-manager
  containers:
  - name: curl
    image: curlimages/curl:7.87.0
    command:
    - sh
    - -c
    - sleep 3600
    securityContext:
      runAsNonRoot: true
      readOnlyRootFilesystem: true
    volumeMounts:
    - mountPath: /tmp/cert
      name: olm-cert
      readOnly: true
  volumes:
  - name: olm-cert
    secret:
      secretName: olmv1-cert
  restartPolicy: Never
EOF
```

#### Access the Pod and Test Metrics

Access the pod:

```shell
kubectl exec -it curl-metrics -n olmv1-system -- sh
```

From the shell:

```shell
curl -v -k -H "Authorization: Bearer $TOKEN" \
https://operator-controller-controller-manager-metrics-service.olmv1-system.svc.cluster.local:8443/metrics
```

Validate using certificates and token:

```shell
curl -v --cacert /tmp/cert/ca.crt --cert /tmp/cert/tls.crt --key /tmp/cert/tls.key \
-H "Authorization: Bearer $TOKEN" \
https://operator-controller-controller-manager-metrics-service.olmv1-system.svc.cluster.local:8443/metrics
```

---

## CatalogD Metrics

### Step 1: Enable Access

To enable access to the CatalogD metrics, create a `ClusterRoleBinding` for the CatalogD service account:

```shell
kubectl create clusterrolebinding catalogd-metrics-binding \
   --clusterrole=catalogd-metrics-reader \
   --serviceaccount=olmv1-system:catalogd-controller-manager
```

### Step 2: Validate Access Manually

#### Create a Token and Extract Certificates

Generate a token:

```shell
TOKEN=$(kubectl create token catalogd-controller-manager -n olmv1-system)
echo $TOKEN
```

#### Deploy a Pod to Consume Metrics

```shell
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: curl-metrics
  namespace: olmv1-system
  labels:
    access: restricted
spec:
  serviceAccountName: catalogd-controller-manager
  containers:
  - name: curl
    image: curlimages/curl:7.87.0
    command:
    - sh
    - -c
    - sleep 3600
    securityContext:
      runAsNonRoot: true
      readOnlyRootFilesystem: true
    volumeMounts:
    - mountPath: /tmp/cert
      name: catalogd-cert
      readOnly: true
  volumes:
  - name: catalogd-cert
    secret:
      secretName: $(kubectl get secret -n olmv1-system -o jsonpath="{.items[?(@.metadata.name | startswith('catalogd-service-cert'))].metadata.name}")
  restartPolicy: Never
EOF
```

#### Access the Pod and Test Metrics

Access the pod:

```shell
kubectl exec -it curl-metrics -n olmv1-system -- sh
```

From the shell:

```shell
curl -v -k -H "Authorization: Bearer $TOKEN" \
https://catalogd-service.olmv1-system.svc.cluster.local:7443/metrics
```

Validate using certificates and token:

```shell
curl -v --cacert /tmp/cert/ca.crt --cert /tmp/cert/tls.crt --key /tmp/cert/tls.key \
-H "Authorization: Bearer $TOKEN" \
https://catalogd-service.olmv1-system.svc.cluster.local:7443/metrics
```

---

## Enabling Integration with Prometheus

If using [Prometheus Operator][prometheus-operator], create a `ServiceMonitor` to scrape metrics:

### For Operator-Controller

```shell
kubectl apply -f - <<EOF
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  labels:
    control-plane: operator-controller-controller-manager
  name: controller-manager-metrics-monitor
  namespace: system
spec:
  endpoints:
    - path: /metrics
      port: https
      scheme: https
      bearerTokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token
      tlsConfig:
        insecureSkipVerify: false
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
      control-plane: operator-controller-controller-manager
EOF
```

### For CatalogD

```shell
kubectl apply -f - <<EOF
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  labels:
    control-plane: catalogd-controller-manager
  name: catalogd-metrics-monitor
  namespace: system
spec:
  endpoints:
    - path: /metrics
      port: https
      scheme: https
      bearerTokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token
      tlsConfig:
        insecureSkipVerify: false
        ca:
          secret:
            name: $(kubectl get secret -n olmv1-system -o jsonpath="{.items[?(@.metadata.name | startswith('catalogd-service-cert'))].metadata.name}")
            key: ca.crt
        cert:
          secret:
            name: $(kubectl get secret -n olmv1-system -o jsonpath="{.items[?(@.metadata.name | startswith('catalogd-service-cert'))].metadata.name}")
            key: tls.crt
        keySecret:
          name: $(kubectl get secret -n olmv1-system -o jsonpath="{.items[?(@.metadata.name | startswith('catalogd-service-cert'))].metadata.name}")
          key: tls.key
  selector:
    matchLabels:
      control-plane: catalogd-controller-manager
EOF
```

[prometheus-operator]: https://github.com/prometheus-operator/prometheus-operator
