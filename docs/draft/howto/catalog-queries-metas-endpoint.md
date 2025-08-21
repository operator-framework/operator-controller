# Catalog queries

After you [add a catalog of extensions](../../tutorials/add-catalog.md) to your cluster, you must port forward your catalog as a service.
Then you can query the catalog by using `curl` commands and the `jq` CLI tool to find extensions to install.

## Prerequisites

* You have added a ClusterCatalog of extensions, such as [OperatorHub.io](https://operatorhub.io), to your cluster.
* You have installed the `jq` CLI tool.

!!! note
    By default, Catalogd is installed with TLS enabled for the catalog webserver.
    The following examples will show this default behavior, but for simplicity's sake will ignore TLS verification in the curl commands using the `-k` flag.

!!! note 
    While using the `/api/v1/metas` endpoint shown in the below examples, it is important to note that the metas endpoint accepts parameters which are one of the sub-types of the `Meta` [definition](https://github.com/operator-framework/operator-registry/blob/e15668c933c03e229b6c80025fdadb040ab834e0/alpha/declcfg/declcfg.go#L111-L114), following the pattern `/api/v1/metas?<parameter>[&<parameter>...]`. e.g. `schema=<schema_name>&package=<package_name>`, `schema=<schema_name>&name=<name>`, and `package=<package_name>&name=<name>` are all valid parameter combinations. However `schema=<schema_name>&version=<version_string>` is not a valid parameter combination, since version is not a first class FBC meta field. 
    
You also need to port forward the catalog server service:

``` terminal
kubectl -n olmv1-system port-forward svc/catalogd-service 8443:443
```

Now you can use the `curl` command with `jq` to query catalogs that are installed on your cluster.

## Package queries

* Available packages in a catalog:
    ``` terminal
    curl -k 'https://localhost:8443/catalogs/operatorhubio/api/v1/metas?schema=olm.package'
    ```

* Packages that support `AllNamespaces` install mode and do not use webhooks:
    ``` terminal
    jq -cs '[.[] | select(.schema == "olm.bundle" and (.properties[] | select(.type == "olm.csv.metadata").value.installModes[] | select(.type == "AllNamespaces" and .supported == true)) and .spec.webhookdefinitions == null) | .package] | unique[]'
    ```

* Package metadata:
    ``` terminal
    curl -k 'https://localhost:8443/catalogs/operatorhubio/api/v1/metas?schema=olm.package&name=<package_name>'
    ```

    `<package_name>`
    : Name of the package from the catalog you are querying.

* Blobs that belong to a package (that are not schema=olm.package):
    ``` terminal
    curl -k 'https://localhost:8443/catalogs/operatorhubio/api/v1/metas?package=<package_name>'
    ```

    `<package_name>`
    : Name of the package from the catalog you are querying.

Note: the `olm.package` schema blob does not have the `package` field set. In other words, to get all the blobs that belong to a package, along with the olm.package blob for that package, a combination of both of the above queries need to be used. 

## Channel queries

* Channels in a package:
    ``` terminal
    curl -k 'https://localhost:8443/catalogs/operatorhubio/api/v1/metas?schema=olm.channel&package=<package_name>'
    ```

    `<package_name>`
    : Name of the package from the catalog you are querying.

* Versions in a channel:
    ``` terminal
    curl -k 'https://localhost:8443/catalogs/operatorhubio/api/v1/metas?schema=olm.channel&package=zoperator&name=alpha' | jq -s '.[] | .entries | .[] | .name'
    ```

    `<package_name>`
    : Name of the package from the catalog you are querying.

    `<channel_name>`
    : Name of the channel for a given package.

## Bundle queries

* Bundles in a package:
    ``` terminal
    curl -k 'https://localhost:8443/catalogs/operatorhubio/api/v1/metas?schema=olm.bundle&package=<package_name>'
    ```

    `<package_name>`
    : Name of the package from the catalog you are querying.

* Bundle dependencies and available APIs:
    ``` terminal
    curl -k 'https://localhost:8443/catalogs/operatorhubio/api/v1/metas?schema=olm.bundle&name=<bundle_name>' | jq -s '.[] | .properties[] | select(.type=="olm.gvk")'
    ```

    `<bundle_name>`
    : Name of the bundle for a given package.
