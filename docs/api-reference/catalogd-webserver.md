# Catalogd web server

[Catalogd](https://github.com/operator-framework/catalogd), the OLM v1 component for making catalog contents available on cluster, includes
a web server that serves catalog contents to clients via an HTTP(S) endpoint.

The endpoint to retrieve this information is provided in the `.status.contentURL` of a `ClusterCatalog` resource.
As an example:

```yaml
    contentURL: https://catalogd-service.olmv1-system.svc/catalogs/operatorhubio/all.json
```

!!! note

    The value of the `.status.contentURL` field in a `ClusterCatalog` resource is an arbitrary string value and can change at any time.
    While there are no guarantees on the exact value of this field, it will always be a URL that resolves successfully for clients using
    it to make a request from within the cluster.

## Interacting With the Server

### Supported HTTP Methods

The HTTP request methods supported by the catalogd web server are:

- [GET](https://developer.mozilla.org/en-US/docs/Web/HTTP/Methods/GET)
- [HEAD](https://developer.mozilla.org/en-US/docs/Web/HTTP/Methods/HEAD)

### Response Format

Responses are encoded as a [JSON Lines](https://jsonlines.org/) stream of [File-Based Catalog](https://olm.operatorframework.io/docs/reference/file-based-catalogs) (FBC) [Meta](https://olm.operatorframework.io/docs/reference/file-based-catalogs/#schema) objects delimited by newlines.

??? example "Example JSON-encoded FBC snippet"

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

    Corresponding JSON lines response:
    ```jsonlines
    {"schema":"olm.package","name":"cockroachdb","defaultChannel":"stable-v6.x"}
    {"schema":"olm.channel","name":"stable-v6.x","package":"cockroachdb","entries":[{"name":"cockroachdb.v6.0.0","skipRange":"<6.0.0"}]}
    {"schema":"olm.bundle","name":"cockroachdb.v6.0.0","package":"cockroachdb","image":"quay.io/openshift-community-operators/cockroachdb@sha256:d3016b1507515fc7712f9c47fd9082baf9ccb070aaab58ed0ef6e5abdedde8ba","properties":[{"type":"olm.package","value":{"packageName":"cockroachdb","version":"6.0.0"}}]}
    ```

### Compression Support

The `catalogd` web server supports gzip compression of responses, which can significantly reduce associated network traffic.  In order to signal that the client handles compressed responses, the client must include `Accept-Encoding: gzip` as a header in the HTTP request.

The web server will include a `Content-Encoding: gzip` header in compressed responses.

!!! note

    Only catalogs that result in a response size greater than 1400 bytes will be compressed.

### Cache Header Support

For clients interested in caching the information returned from the `catalogd` web server, the `Last-Modified` header is set
on responses and the `If-Modified-Since` header is supported for requests.
