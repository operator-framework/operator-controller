Uninstalling an extension is as simple as deleting an existing ClusterExtension CR: 

```bash
$ kubectl get clusterextensions
NAME                          AGE
operatorhubio-argocd-operator   53s

$ kubectl delete clusterextension argocd-operator 
clusterextension.olm.operatorframework.io "argocd-operator" deleted
$ kubectl get namespaces | grep argocd
$
$ kubectl get crds | grep argocd-operator 
$
```
