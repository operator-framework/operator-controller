---
tags:
  - alpha
---

# Configure global pull secrets for allowing components to pull private images

**Note: The UX for how auth info for using private images is provided is an active work in progress.**  

To configure `catalogd` and `operator-controller` to use authentication information for pulling private images (catalog/bundle images etc), the components can be informed about a kubernetes `Secret` object that contains the relevant auth information. The `Secret` must be of type `kubernetes.io/dockerconfigjson`. 

Once the `Secret` is created, `catalogd` and `operator-controller` needs to be redeployed with an additional field, `--global-pull-secret=<secret-namespace>/<secret-name>` passed to the respective binaries.

For eg, create a `Secret` using locally available `config.json`: 

```sh
$ kubectl create secret docker-registry test-secret \
  --from-file=.dockerconfigjson=$HOME/.docker/config.json \
  --namespace olmv1-system
secret/test-secret created
```

Verify that the Secret is created: 

```sh
$ kubectl get secret test-secret -n olmv1-system -o yaml 
apiVersion: v1
data:
  .dockerconfigjson: ewogICJh....
kind: Secret
metadata:
  creationTimestamp: "2024-10-25T12:05:46Z"
  name: test-secret
  namespace: olmv1-system
  resourceVersion: "237734"
  uid: 880138f1-5d98-4bb0-9e45-45e8ebaff647
type: kubernetes.io/dockerconfigjson
```

Modify the `config/base/manager/manager.yaml` file for `catalogd` and `operator-controller` to include the new field in the binary args: 

```yaml
 - command:
        - ./manager
        args:
        - ...
        - ...
        - ...
        - --global-pull-secret=olmv1-system/test-secret
```

With the above configuration, creating a `ClusterCatalog` or a `ClusterExention` whose content is packaged in a private container image hosted in an image registry, will become possible.
 