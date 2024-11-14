# Catalog queries

After you [add a catalog of extensions](../tutorials/add-catalog.md) to your cluster, you must port forward your catalog as a service.
Then you can query the catalog by using `curl` commands and the `jq` CLI tool to find extensions to install.

## Prerequisites

* You have added a ClusterCatalog of extensions, such as [OperatorHub.io](https://operatorhub.io), to your cluster.
* You have installed the `jq` CLI tool.

!!! note
    By default, Catalogd is installed with TLS enabled for the catalog webserver.
    The following examples will show this default behavior, but for simplicity's sake will ignore TLS verification in the curl commands using the `-k` flag.

You also need to port forward the catalog server service:

``` terminal
kubectl -n olmv1-system port-forward svc/catalogd-service 8443:443
```

Now you can use the `curl` command with `jq` to query catalogs that are installed on your cluster.

``` terminal title="Query syntax"
curl -k https://localhost:8443/catalogs/operatorhubio/api/v1/all | <query>
```
`<query>`
: One of the `jq` queries from this document

## Package queries

* Available packages in a catalog:
    ``` terminal
    jq -s '.[] | select( .schema == "olm.package")'
    ```

* Packages that support `AllNamespaces` install mode and do not use webhooks:
    ``` terminal
    jq -cs '[.[] | select(.schema == "olm.bundle" and (.properties[] | select(.type == "olm.csv.metadata").value.installModes[] | select(.type == "AllNamespaces" and .supported == true)) and .spec.webhookdefinitions == null) | .package] | unique[]'
    ```

* Package metadata:
    ``` terminal
    jq -s '.[] | select( .schema == "olm.package") | select( .name == "<package_name>")'
    ```

    `<package_name>`
    : Name of the package from the catalog you are querying.

* Catalog blobs in a package:
    ``` terminal
    jq -s '.[] | select( .package == "<package_name>")'
    ```

    `<package_name>`
    : Name of the package from the catalog you are querying.

## Channel queries

* Channels in a package:
    ``` terminal
    jq -s '.[] | select( .schema == "olm.channel" ) | select( .package == "<package_name>") | .name'
    ```

    `<package_name>`
    : Name of the package from the catalog you are querying.

* Versions in a channel:
    ``` terminal
    jq -s '.[] | select( .package == "<package_name>" ) | select( .schema == "olm.channel" ) | select( .name == "<channel_name>" ) | .entries | .[] | .name'
    ```

    `<package_name>`
    : Name of the package from the catalog you are querying.

    `<channel_name>`
    : Name of the channel for a given package.

* Latest version in a channel and upgrade path:
    ``` terminal
    jq -s '.[] | select( .schema == "olm.channel" ) | select ( .name == "<channel_name>") | select( .package == "<package_name>")'
    ```

    `<package_name>`
    : Name of the package from the catalog you are querying.

    `<channel_name>`
    : Name of the channel for a given package.

## Bundle queries

* Bundles in a package:
    ``` terminal
    jq -s '.[] | select( .schema == "olm.bundle" ) | select( .package == "<package_name>") | .name'
    ```

    `<package_name>`
    : Name of the package from the catalog you are querying.

* Bundle dependencies and available APIs:
    ``` terminal
    jq -s '.[] | select( .schema == "olm.bundle" ) | select ( .name == "<bundle_name>") | select( .package == "<package_name>")'
    ```

    `<package_name>`
    : Name of the package from the catalog you are querying.

    `<bundle_name>`
    : Name of the bundle for a given package.
