#!/bin/bash

set -euo pipefail

help="setup-monitoring.sh is used to set up prometheus monitoring for e2e testing.

Usage:
  setup-monitoring.sh [PROMETHEUS_NAMESPACE] [PROMETHEUS_VERSION] [KUSTOMIZE]
"

if [[ "$#" -ne 3 ]]; then
  echo "Illegal number of arguments passed"
  echo "${help}"
  exit 1
fi

NAMESPACE=$1
PROMETHEUS_VERSION=$2
KUSTOMIZE=$3

TMPDIR=$(mktemp -d)
trap 'echo "Cleaning up ${TMPDIR}"; rm -rf "${TMPDIR}"' EXIT
curl -s "https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/refs/tags/${PROMETHEUS_VERSION}/kustomization.yaml" > "${TMPDIR}/kustomization.yaml"
curl -s "https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/refs/tags/${PROMETHEUS_VERSION}/bundle.yaml" > "${TMPDIR}/bundle.yaml"
(cd ${TMPDIR} && ${KUSTOMIZE} edit set namespace ${NAMESPACE}) && kubectl create -k "${TMPDIR}"
kubectl wait --for=condition=Ready pods -n ${NAMESPACE} -l app.kubernetes.io/name=prometheus-operator

kubectl apply -f - << EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: prometheus
  namespace: ${NAMESPACE}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: prometheus
rules:
- apiGroups: [""]
  resources:
  - nodes
  - nodes/metrics
  - services
  - endpoints
  - pods
  verbs: ["get", "list", "watch"]
- apiGroups: [""]
  resources:
  - configmaps
  verbs: ["get"]
- apiGroups:
  - discovery.k8s.io
  resources:
  - endpointslices
  verbs: ["get", "list", "watch"]
- apiGroups:
  - networking.k8s.io
  resources:
  - ingresses
  verbs: ["get", "list", "watch"]
- nonResourceURLs: ["/metrics"]
  verbs: ["get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: prometheus
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: prometheus
subjects:
- kind: ServiceAccount
  name: prometheus
  namespace: ${NAMESPACE}
EOF

kubectl apply -f - << EOF
apiVersion: monitoring.coreos.com/v1
kind: Prometheus
metadata:
  name: prometheus
  namespace: ${NAMESPACE}
spec:
  logLevel: debug
  serviceAccountName: prometheus
  scrapeTimeout: 30s
  scrapeInterval: 1m
  securityContext:
    runAsNonRoot: true
    runAsUser: 65534
    seccompProfile:
        type: RuntimeDefault
  serviceDiscoveryRole: EndpointSlice
  serviceMonitorSelector: {}
EOF

kubectl apply -f - << EOF
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: prometheus
  namespace: ${NAMESPACE}
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: prometheus
  policyTypes:
    - Egress
    - Ingress
  egress:
    - {}  # Allows all egress traffic for metrics requests
  ingress:
    - {}  # Allows us to query prometheus
EOF

# Give the operator time to create the pod
kubectl wait --for=create pods -n ${NAMESPACE} prometheus-prometheus-0 --timeout=60s
kubectl wait --for=condition=Ready pods -n ${NAMESPACE} prometheus-prometheus-0 --timeout=120s

# Authentication token for the scrape requests
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
type: kubernetes.io/service-account-token
metadata:
  name: prometheus-metrics-token
  namespace: ${NAMESPACE}
  annotations:
    kubernetes.io/service-account.name: prometheus
EOF

# ServiceMonitors for operator-controller and catalogd
kubectl apply -f - <<EOF
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: operator-controller-controller-manager-metrics-monitor
  namespace: ${NAMESPACE}
spec:
  endpoints:
    - path: /metrics
      port: https
      scheme: https
      authorization:
        credentials:
          name: prometheus-metrics-token
          key: token
      tlsConfig:
        serverName: operator-controller-service.${NAMESPACE}.svc
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

CATD_SECRET=$(kubectl get secret -n ${NAMESPACE} -o jsonpath="{.items[*].metadata.name}" | tr ' ' '\n' | grep '^catalogd-service-cert')

kubectl apply -f - <<EOF
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: catalogd-controller-manager-metrics-monitor
  namespace: ${NAMESPACE}
spec:
  endpoints:
    - path: /metrics
      port: metrics
      scheme: https
      authorization:
        credentials:
          name: prometheus-metrics-token
          key: token
      tlsConfig:
        serverName: catalogd-service.${NAMESPACE}.svc
        insecureSkipVerify: false
        ca:
          secret:
            name: ${CATD_SECRET}
            key: ca.crt
        cert:
          secret:
            name: ${CATD_SECRET}
            key: tls.crt
        keySecret:
          name: ${CATD_SECRET}
          key: tls.key
  selector:
    matchLabels:
      app.kubernetes.io/name: catalogd
EOF

# NodePort service to allow querying prometheus from outside the cluster
# NOTE: This NodePort must also be configured in kind-config.yaml
kubectl apply -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: prometheus-service
  namespace: ${NAMESPACE}
spec:
  type: NodePort
  ports:
  - name: web
    nodePort: 30900
    port: 9090
    protocol: TCP
    targetPort: web
  selector:
    prometheus: prometheus
EOF
