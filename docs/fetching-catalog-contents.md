# Fetching `Catalog` contents from the Catalogd HTTP Server
This document covers how to fetch the contents for a `Catalog` from the
Catalogd HTTP Server that runs when the `HTTPServer` feature-gate is enabled
(enabled by default).

For example purposes we make the following assumption:
- A `Catalog` named `operatorhubio` has been created and successfully unpacked
(denoted in the `Catalog.Status`)

`Catalog` CRs have a status.contentURL field whose value is the location where the content 
of a catalog can be read from:

```yaml
  status:
    conditions:
    - lastTransitionTime: "2023-09-14T15:21:18Z"
      message: successfully unpacked the catalog image "quay.io/operatorhubio/catalog@sha256:e53267559addc85227c2a7901ca54b980bc900276fc24d3f4db0549cb38ecf76"
      reason: UnpackSuccessful
      status: "True"
      type: Unpacked
    contentURL: http://catalogd-catalogserver.catalogd-system.svc/catalogs/operatorhubio/all.json
    phase: Unpacked
    resolvedSource:
      image:
        ref: quay.io/operatorhubio/catalog@sha256:e53267559addc85227c2a7901ca54b980bc900276fc24d3f4db0549cb38ecf76
      type: image
```

All responses will be a JSON stream where each JSON object is a File-Based Catalog (FBC)
object.


## On cluster

When making a request for the contents of the `operatorhubio` `Catalog` from within
the cluster issue a HTTP `GET` request to 
`http://catalogd-catalogserver.catalogd-system.svc/catalogs/operatorhubio/all.json`

An example command to run a `Pod` to `curl` the catalog contents:
```sh
kubectl run fetcher --image=curlimages/curl:latest -- curl http://catalogd-catalogserver.catalogd-system.svc/catalogs/operatorhubio/all.json
```

## Off cluster

When making a request for the contents of the `operatorhubio` `Catalog` from outside
the cluster, we have to perform an extra step:
1. Port forward the `catalogd-catalogserver` service in the `catalogd-system` namespace:
```sh
kubectl -n catalogd-system port-forward svc/catalogd-catalogserver 8080:80
```

Once the service has been successfully forwarded to a localhost port, issue a HTTP `GET`
request to `http://localhost:8080/catalogs/operatorhubio/all.json`

An example `curl` request that assumes the port-forwarding is mapped to port 8080 on the local machine:
```sh
curl http://localhost:8080/catalogs/operatorhubio/all.json
```

# Fetching `Catalog` contents from the `Catalogd` Service outside of the cluster

This section outlines a way of exposing the `Catalogd` Service's endpoints outside the cluster and then accessing the catalog contents using `Ingress`. We will be using `Ingress NGINX` Controller for the sake of this example but you are welcome to use the `Ingress` Controller of your choice.

**Prerequisites**

- [Install kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- Assuming `kind` is installed, create a `kind` cluster with `extraPortMappings` and `node-labels` as shown in the [kind documentation](https://kind.sigs.k8s.io/docs/user/ingress/)
- Install latest version of `Catalogd` by navigating to the [releases page](https://github.com/operator-framework/catalogd/releases) and following the install instructions included in the release you want to install.
- Install the `Ingress NGINX` Controller by running the below command:

  ```sh
    $ kubectl apply -k  https://github.com/operator-framework/catalogd/tree/main/config/nginx-ingress
  ```
  By running that above command, the `Ingress` Controller is installed. Along with it, the `Ingress` Resource will be applied automatically as well, thereby creating an `Ingress` Object on the cluster.

1. Once the prerequisites are satisfied, create a `Catalog` object that points to the OperatorHub Community catalog by running the following command:

    ```sh
      $ kubectl apply -f - << EOF
      apiVersion: catalogd.operatorframework.io/v1alpha1
      kind: Catalog
        metadata:
          name: operatorhubio
        spec:
          source:
            type: image
            image:
              ref: quay.io/operatorhubio/catalog:latest
        EOF
    ```

1. Before proceeding further, let's verify that the `Catalog` object was created successfully by running the below command: 

    ```sh
      $ kubectl describe catalog/operatorhubio
    ```

1. At this point the `Catalog` object exists and `Ingress` controller is ready to process requests. The sample `Ingress` Resource that was created during Step 4 of Prerequisites is shown as below: 

    ```yaml
      apiVersion: networking.k8s.io/v1
      kind: Ingress
      metadata:
        name: catalogd-nginx-ingress
        namespace: catalogd-system
      spec:
        ingressClassName: nginx
        rules:
        - http:
            paths:
            - path: /
              pathType: Prefix
              backend:
                service:
                  name: catalogd-catalogserver
                  port:
                    number: 80
      ```
    Let's verify that the `Ingress` object got created successfully from the sample by running the following command:

      ```sh
        $ kubectl describe ingress/catalogd-ingress -n catalogd-system
      ```

1. Run the below example `curl` request to retrieve all of the catalog contents:

    ```sh
      $ curl http://<address>/catalogs/operatorhubio/all.json
    ```
    
    To obtain `address` of the ingress object, you can run the below command and look for the value in the `ADDRESS` field from output: 
    ```sh
      $ kubectl -n catalogd-system get ingress
    ```
   
    You can further use the `curl` commands outlined in the [Catalogd README](https://github.com/operator-framework/catalogd/blob/main/README.md) to filter out the JSON content by list of bundles, channels & packages.
