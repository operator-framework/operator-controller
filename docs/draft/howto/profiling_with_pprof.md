# Profiling with Pprof 

!!! warning
Pprof bind port flag are available as an alpha release and are subject to change in future versions.

[Pprof][pprof] is a useful tool for analyzing memory and CPU usage profiles. However, it is not
recommended to enable it by default in production environments. While it is great for troubleshooting,
keeping it enabled can introduce performance concerns and potential information leaks.

Both components allow you to enable pprof by specifying the port it should bind to using the
`pprof-bind-address` flag. However, you must ensure that each component uses a unique port—using
the same port for multiple components is not allowed. Additionally, you need to export the corresponding
port in the service configuration for each component.

The following steps are examples to demonstrate the required changes to enable Pprof for Operator-Controller and CatalogD.

## Enabling Pprof for gathering the data

### For Operator-Controller

1. Run the following command to patch the Deployment and add the `--pprof-bind-address=:8082` flag:

```shell
kubectl patch deployment $(kubectl get deployments -n olmv1-system -l apps.kubernetes.io/name=operator-controller -o jsonpath='{.items[0].metadata.name}') \
-n olmv1-system --type='json' -p='[
  {
    "op": "add",
    "path": "/spec/template/spec/containers/0/args/-",
    "value": "--pprof-bind-address=:8082"
  }
]'
```

2. Once Pprof is enabled, you need to export port `8082` in the Service to make it accessible:

```shell
kubectl patch service operator-controller-service -n olmv1-system --type='json' -p='[
  {
    "op": "add",
    "path": "/spec/ports/-",
    "value": {
      "name": "pprof",
      "port": 8082,
      "targetPort": 8082,
      "protocol": "TCP"
    }
  }
]'
```

3. Create the Pod with `curl` to allow to generate the report:

```shell
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: curl-oper-con-pprof
  namespace: olmv1-system
spec:
  serviceAccountName: operator-controller-controller-manager
  securityContext:
    seccompProfile:
      type: RuntimeDefault
  containers:
  - name: curl
    image: curlimages/curl:latest
    command:
    - sh
    - -c
    - sleep 3600
    securityContext:
      runAsNonRoot: true
      readOnlyRootFilesystem: false
      runAsUser: 1000
      runAsGroup: 1000
      allowPrivilegeEscalation: false
      capabilities:
        drop:
        - ALL
    volumeMounts:
    - mountPath: /tmp
      name: tmp-volume
  restartPolicy: Never
  volumes:
  - name: tmp-volume
    emptyDir: {}
EOF
```

4. Run the following command to generate the token for authentication:

```shell
TOKEN=$(kubectl create token operator-controller-controller-manager -n olmv1-system)
echo $TOKEN
```

5. Run the following command to generate the report in the Pod:

```shell
kubectl exec -it curl-oper-con-pprof -n olmv1-system -- sh -c \
"curl -s -k -H \"Authorization: Bearer $TOKEN\" \
http://operator-controller-service.olmv1-system.svc.cluster.local:8082/debug/pprof/profile > /tmp/operator-controller-profile.pprof"
```

6. Now, we can verify that the report was successfully created:

```shell
kubectl exec -it curl-oper-con-pprof -n olmv1-system -- ls -lh /tmp/
```

7. Then, we can copy the result for your local environment:

```shell
kubectl cp olmv1-system/curl-oper-con-pprof:/tmp/operator-controller-profile.pprof ./operator-controller-profile.pprof
tar: removing leading '/' from member names
```

8. By last, we can use pprof to analyse the result:

```shell
go tool pprof -http=:8080 ./operator-controller-profile.pprof
```

### For the CatalogD

1. Run the following command to patch the Deployment and add the `--pprof-bind-address=:8083` flag:

```shell
kubectl patch deployment $(kubectl get deployments -n olmv1-system -l apps.kubernetes.io/name=catalogd -o jsonpath='{.items[0].metadata.name}') \
-n olmv1-system --type='json' -p='[
  {
    "op": "add",
    "path": "/spec/template/spec/containers/0/args/-",
    "value": "--pprof-bind-address=:8083"
  }
]'
```

2. Once Pprof is enabled, you need to export port `8083` in the `Service` to make it accessible:

```shell
kubectl patch service $(kubectl get service -n olmv1-system -l app.kubernetes.io/part-of=olm,app.kubernetes.io/name=catalogd -o jsonpath='{.items[0].metadata.name}') \
-n olmv1-system --type='json' -p='[
  {
    "op": "add",
    "path": "/spec/ports/-",
    "value": {
      "name": "pprof",
      "port": 8083,
      "targetPort": 8083,
      "protocol": "TCP"
    }
  }
]'
```

3. Create the Pod with `curl` to allow to generate the report:

```shell
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: curl-catalogd-pprof
  namespace: olmv1-system
spec:
  serviceAccountName: catalogd-controller-manager
  securityContext:
    seccompProfile:
      type: RuntimeDefault
  containers:
  - name: curl
    image: curlimages/curl:latest
    command:
    - sh
    - -c
    - sleep 3600
    securityContext:
      runAsNonRoot: true
      readOnlyRootFilesystem: false
      runAsUser: 1000
      runAsGroup: 1000
      allowPrivilegeEscalation: false
      capabilities:
        drop:
        - ALL
    volumeMounts:
    - mountPath: /tmp
      name: tmp-volume
  restartPolicy: Never
  volumes:
  - name: tmp-volume
    emptyDir: {}
EOF
```

4. Run the following command to generate the token for authentication:

```shell
TOKEN=$(kubectl create token catalogd-controller-manager -n olmv1-system)
echo $TOKEN
```

5. Run the following command to generate the report in the Pod:

```shell
kubectl exec -it curl-catalogd-pprof -n olmv1-system -- sh -c \
"curl -s -k -H \"Authorization: Bearer $TOKEN\" \
http://catalogd-service.olmv1-system.svc.cluster.local:8083/debug/pprof/profile > /tmp/catalogd-profile.pprof"
```

6. Now, we can verify that the report was successfully created:

```shell
kubectl exec -it curl-catalogd-pprof -n olmv1-system -- ls -lh /tmp/
```

7. Then, we can copy the result for your local environment:

```shell
kubectl cp olmv1-system/curl-catalogd-pprof:/tmp/catalogd-profile.pprof ./catalogd-profile.pprof
```

8. By last, we can use pprof to analyse the result:

```shell
go tool pprof -http=:8080 ./catalogd-profile.pprof
```

## Disabling pprof after gathering the data

### For Operator-Controller

1. Run the following command to bind to `--pprof-bind-address` the value `0` in order to disable the endpoint.

```shell
kubectl patch deployment $(kubectl get deployments -n olmv1-system -l apps.kubernetes.io/name=operator-controller -o jsonpath='{.items[0].metadata.name}') \
-n olmv1-system --type='json' -p='[
  {
    "op": "replace",
    "path": "/spec/template/spec/containers/0/args",
    "value": ["--pprof-bind-address=0"]
  }
]'
```

2. Try to generate the report as done previously. The connection should now be refused:

```shell
kubectl exec -it curl-pprof -n olmv1-system -- sh -c \
"curl -s -k -H \"Authorization: Bearer $TOKEN\" \
http://operator-controller-service.olmv1-system.svc.cluster.local:8082/debug/pprof/profile > /tmp/operator-controller-profile.pprof"
```

**NOTE:** if you wish you can delete the service port added to allow use pprof and
re-start the deployment `kubectl rollout restart deployment -n olmv1-system operator-controller-controller-manager`

3. We can remove the Pod created to generate the report:

```shell
kubectl delete pod curl-oper-con-pprof -n olmv1-system
```

### For CatalogD

1. Run the following command to bind to `--pprof-bind-address` the value `0` in order to disable the endpoint.
```shell
kubectl patch deployment $(kubectl get deployments -n olmv1-system -l apps.kubernetes.io/name=catalogd -o jsonpath='{.items[0].metadata.name}') \
-n olmv1-system --type='json' -p='[
  {
    "op": "replace",
    "path": "/spec/template/spec/containers/0/args",
    "value": ["--pprof-bind-address=0"]
  }
]'
```

2. To ensure we can try to generate the report as done above. Note that the connection
should be refused:

```shell
kubectl exec -it curl-pprof -n olmv1-system -- sh -c \
"curl -s -k -H \"Authorization: Bearer $TOKEN\" \
http://catalogd-service.olmv1-system.svc.cluster.local:8083/debug/pprof/profile > /tmp/catalogd-profile.pprof"
```

**NOTE:** if you wish you can delete the service port added to allow use pprof and 
re-start the deployment `kubectl rollout restart deployment -n olmv1-system catalogd-controller-manager`

3. We can remove the Pod created to generate the report:

```shell
kubectl delete pod curl-catalogd-pprof -n olmv1-system
```

[pprof]: https://github.com/google/pprof/blob/main/doc/README.md
