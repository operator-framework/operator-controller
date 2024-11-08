# `ClusterCatalog` Interface
`catalogd` serves catalog content via a catalog-specific, versioned HTTP(S) endpoint. Clients access catalog information via this API endpoint and a versioned reference of the desired format. Current support includes only a complete catalog download, indicated by the path "api/v1/all", for example if `status.urls.base` is `https://catalogd-service.olmv1-system.svc/catalogs/operatorhubio` then `https://catalogd-service.olmv1-system.svc/catalogs/operatorhubio/api/vi/all` would receive the complete FBC for the catalog `operatorhubio`.


## Response Format
`catalogd` responses retrieved via the catalog-specific v1 API are encoded as a [JSON Lines](https://jsonlines.org/) stream of File-Based Catalog (FBC) [Meta](https://olm.operatorframework.io/docs/reference/file-based-catalogs/#schema) objects delimited by newlines.

### Example
For an example JSON-encoded FBC snippet
```json
{
  "schema": "olm.package",
  "name": "cockroachdb",
  "defaultChannel": "stable-v6.x",
}
{
  "schema": "olm.channel",
  "name": "stable-v6.x",
  "package": "cockroachdb",
  "entries": [
    {
      "name": "cockroachdb.v6.0.0",
      "skipRange": "<6.0.0"
    }
  ]
}
{
  "schema": "olm.bundle",
  "name": "cockroachdb.v6.0.0",
  "package": "cockroachdb",
  "image": "quay.io/openshift-community-operators/cockroachdb@sha256:d3016b1507515fc7712f9c47fd9082baf9ccb070aaab58ed0ef6e5abdedde8ba",
  "properties": [
    {
      "type": "olm.package",
      "value": {
        "packageName": "cockroachdb",
        "version": "6.0.0"
      }
    },
  ],
}
```
the corresponding JSON Lines formatted response would be
```json
{"schema":"olm.package","name":"cockroachdb","defaultChannel":"stable-v6.x"}
{"schema":"olm.channel","name":"stable-v6.x","package":"cockroachdb","entries":[{"name":"cockroachdb.v6.0.0","skipRange":"<6.0.0"}]}
{"schema":"olm.bundle","name":"cockroachdb.v6.0.0","package":"cockroachdb","image":"quay.io/openshift-community-operators/cockroachdb@sha256:d3016b1507515fc7712f9c47fd9082baf9ccb070aaab58ed0ef6e5abdedde8ba","properties":[{"type":"olm.package","value":{"packageName":"cockroachdb","version":"6.0.0"}}]}
```

## Compression Support

`catalogd` supports gzip compression of responses, which can significantly reduce associated network traffic.  In order to signal to `catalogd` that the client handles compressed responses, the client must include `Accept-Encoding: gzip` as a header in the HTTP request.

`catalogd` will include a `Content-Encoding: gzip` header in compressed responses.  

Note that `catalogd` will only compress catalogs larger than 1400 bytes.

### Example

The demo below
1. retrieves plaintext catalog content (and saves to file 1)
2. adds the `Accept-Encoding` header and retrieves compressed content
3. adds the `Accept-Encofing` header and uses curl to decompress the response (and saves to file 2)
4. uses diff to demonstrate that there is no difference between the contents of files 1 and 2


[![asciicast](https://asciinema.org/a/668823.svg)](https://asciinema.org/a/668823)



# Fetching `ClusterCatalog` contents from the Catalogd HTTP Server
This section covers how to fetch the contents for a `ClusterCatalog` from the
Catalogd HTTP(S) Server.

For example purposes we make the following assumption:
- A `ClusterCatalog` named `operatorhubio` has been created and successfully unpacked
(denoted in the `ClusterCatalog.Status`)

**NOTE:** By default, Catalogd is configured to use TLS with self-signed certificates.
For local development, consider skipping TLS verification, such as `curl -k`, or reference external material
on self-signed certificate verification.

`ClusterCatalog` CRs have a `status.urls.base` field which identifies the catalog-specific API to access the catalog content:

```yaml
  status:
    .
    .
    urls:
        base: https://catalogd-service.olmv1-system.svc/catalogs/operatorhubio
    resolvedSource:
      image:
        ref: quay.io/operatorhubio/catalog@sha256:e53267559addc85227c2a7901ca54b980bc900276fc24d3f4db0549cb38ecf76
      type: Image
```

## On cluster

When making a request for the complete contents of the `operatorhubio` `ClusterCatalog` from within
the cluster, clients would combine `status.urls.base` with the desired API service and issue an HTTP GET request for the URL.

For example, to receive the complete catalog data for the `operatorhubio` catalog indicated above, the client would append the service point designator `api/v1/all`, like:

`https://catalogd-service.olmv1-system.svc/catalogs/operatorhubio/api/v1/all`.

An example command to run a `Pod` to `curl` the catalog contents:
```sh
kubectl run fetcher --image=curlimages/curl:latest -- curl https://catalogd-service.olmv1-system.svc/catalogs/operatorhubio/api/v1/all
```

## Off cluster

When making a request for the contents of the `operatorhubio` `ClusterCatalog` from outside
the cluster, we have to perform an extra step:
1. Port forward the `catalogd-service` service in the `olmv1-system` namespace:
```sh
kubectl -n olmv1-system port-forward svc/catalogd-service 8080:443
```

Once the service has been successfully forwarded to a localhost port, issue a HTTP `GET`
request to `https://localhost:8080/catalogs/operatorhubio/api/v1/all`

An example `curl` request that assumes the port-forwarding is mapped to port 8080 on the local machine:
```sh
curl http://localhost:8080/catalogs/operatorhubio/api/v1/all
```

# Fetching `ClusterCatalog` contents from the `Catalogd` Service outside of the cluster

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

1. Once the prerequisites are satisfied, create a `ClusterCatalog` object that points to the OperatorHub Community catalog by running the following command:

    ```sh
      $ kubectl apply -f - << EOF
      apiVersion: olm.operatorframework.io/v1
      kind: ClusterCatalog
        metadata:
          name: operatorhubio
        spec:
          source:
            type: Image
            image:
              ref: quay.io/operatorhubio/catalog:latest
        EOF
    ```

1. Before proceeding further, let's verify that the `ClusterCatalog` object was created successfully by running the below command: 

    ```sh
      $ kubectl describe catalog/operatorhubio
    ```

1. At this point the `ClusterCatalog` object exists and `Ingress` controller is ready to process requests. The sample `Ingress` Resource that was created during Step 4 of Prerequisites is shown as below: 

    ```yaml
      apiVersion: networking.k8s.io/v1
      kind: Ingress
      metadata:
        name: catalogd-nginx-ingress
        namespace: olmv1-system
      spec:
        ingressClassName: nginx
        rules:
        - http:
            paths:
            - path: /
              pathType: Prefix
              backend:
                service:
                  name: catalogd-service
                  port:
                    number: 80
      ```
    Let's verify that the `Ingress` object got created successfully from the sample by running the following command:

      ```sh
        $ kubectl describe ingress/catalogd-ingress -n olmv1-system
      ```

1. Run the below example `curl` request to retrieve all of the catalog contents:

    ```sh
      $ curl https://<address>/catalogs/operatorhubio/api/v1/all
    ```
    
    To obtain `address` of the ingress object, you can run the below command and look for the value in the `ADDRESS` field from output: 
    ```sh
      $ kubectl -n olmv1-system get ingress
    ```
   
    You can further use the `curl` commands outlined in the [Catalogd README](https://github.com/operator-framework/catalogd/blob/main/README.md) to filter out the JSON content by list of bundles, channels & packages.
