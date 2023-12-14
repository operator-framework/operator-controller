Creating a ClusterExtension CR installs the extension on cluster:  

```bash
$ kubectl get packages | grep argocd 
operatorhubio-argocd-operator                            5m19s
operatorhubio-argocd-operator-helm                       5m19s

$ kubectl apply -f - <<EOF
apiVersion: olm.operatorframework.io/v1alpha1
kind: ClusterExtension
metadata:
  name: argocd-operator
spec:
  packageName: operatorhubio-argocd-operator
EOF

$ kubectl get clusterextensions
NAME                          AGE
operatorhubio-argocd-operator   53s

$  kubectl get clusterextension argocd-operator -o yaml 
apiVersion: olm.operatorframework.io/v1alpha1
kind: ClusterExtension
metadata:
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"olm.operatorframework.io/v1alpha1","kind":"ClusterExtension","metadata":{"annotations":{},"name":"argocd-operator"},"spec":{"packageName":"argocd-operator"}}
  creationTimestamp: "2023-06-21T14:57:50Z"
  generation: 1
  name: argocd-operator
  resourceVersion: "10690"
  uid: 6e0c67a5-eb9c-41c6-a455-140b28714d34
spec:
  packageName: operatorhubio-argocd-operator
status:
  conditions:
  - lastTransitionTime: "2023-06-21T14:57:51Z"
    message: resolved to "quay.io/operatorhubio/argocd-operator@sha256:1a9b3c8072f2d7f4d6528fa32905634d97b7b4c239ef9887e3fb821ff033fef6"
    observedGeneration: 1
    reason: Success
    status: "True"
    type: Resolved
  - lastTransitionTime: "2023-06-21T14:57:57Z"
    message: installed from "quay.io/operatorhubio/argocd-operator@sha256:1a9b3c8072f2d7f4d6528fa32905634d97b7b4c239ef9887e3fb821ff033fef6"
    observedGeneration: 1
    reason: Success
    status: "True"
    type: Installed
  installedBundleResource: quay.io/operatorhubio/argocd-operator@sha256:1a9b3c8072f2d7f4d6528fa32905634d97b7b4c239ef9887e3fb821ff033fef6
  resolvedBundleResource: quay.io/operatorhubio/argocd-operator@sha256:1a9b3c8072f2d7f4d6528fa32905634d97b7b4c239ef9887e3fb821ff033fef6
```

The status condition type `Installed`:`true` indicates that the extension was installed successfully. We can confirm this by looking at the workloads that were created as a result of this extension installation: 

```bash 
$ kubectl get namespaces | grep argocd 
argocd-operator-system       Active   4m17s

$ kubectl get pods -n argocd-operator-system 
NAME                                                 READY   STATUS    RESTARTS   AGE
argocd-operator-controller-manager-bb496c545-ljbbr   2/2     Running   0          4m32s
```
