# Fetching `Catalog` contents from the Catalogd HTTP Server
This document covers how to fetch the contents for a `Catalog` from the
Catalogd HTTP Server that runs when the `HTTPServer` feature-gate is enabled
(enabled by default).

All `Catalog`s currently have their contents served via the following endpoint pattern:
`http://{httpServerBaseUrl}/catalogs/{Catalog.Name}/all.json`

All responses will be a JSON stream where each JSON object is a File-Based Catalog (FBC)
object.

For example purposes we make the following assumption:
- A `Catalog` named `operatorhubio` has been created and successfully unpacked
(denoted in the `Catalog.Status`)

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
kubectl -n catalogd-system port-forward svc/catalogd-catalogserver <port>:80
```

Once the service has been successfully forwarded to a localhost port, issue a HTTP `GET`
request to `http://localhost:<port>/catalogs/operatorhubio/all.json`

An example `curl` request that assumes the port-forwarding is mapped to port 8080 on the local machine:
```sh
curl http://localhost:8080/catalogs/operatorhubio/all.json
```
