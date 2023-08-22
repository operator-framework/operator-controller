Uninstalling an operator is as simple as deleting an existing Operator CR: 

```bash
$ kubectl get operators 
NAME                          AGE
operatorhubio-argocd-operator   53s

$ kubectl delete operator argocd-operator 
operator.operators.operatorframework.io "argocd-operator" deleted
$ kubectl get namespaces | grep argocd
$
$ kubectl get crds | grep argocd-operator 
$
```