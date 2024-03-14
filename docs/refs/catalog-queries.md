# Catalog queries

You can use the `curl` command with `jq` to query catalogs that are installed on your cluster.

``` terminal title="Query syntax"
$ curl http://localhost:8080/catalogs/operatorhubio/all.json | <query>
```

## Package queries

Available packages in a catalog
: `jq -s '.[] | select( .schema == "olm.package")`

Packages that support `AllNamespaces` install mode and do not use webhooks

: `jq -c 'select(.schema == "olm.bundle") | {"package":.package, "version":.properties[] | select(.type == "olm.bundle.object").value.data |  @base64d | fromjson | select(.kind == "ClusterServiceVersion" and (.spec.installModes[] | select(.type == "AllNamespaces" and .supported == true) != null) and .spec.webhookdefinitions == null).spec.version}'`

Package metadata
: `jq -s '.[] | select( .schema == "olm.package") | select( .name == "<package_name>")'`

Catalog blobs in a package
: `jq -s '.[] | select( .package == "<package_name>")'`

## Channel queries

Channels in a package
: `jq -s '.[] | select( .schema == "olm.channel" ) | select( .package == "<package_name>") \| .name'`

Versions in a channel
: `jq -s '.[] | select( .package == "<package_name>" ) | select( .schema == "olm.channel" ) | select( .name == "<channel_name>" ) | .entries | .[] | .name'`

Latest version in a channel and upgrade path
: `jq -s '.[] | select( .schema == "olm.channel" ) | select ( .name == "<channel>") | select( .package == "<package_name>")'`

## Bundle queries

Bundles in a package
: `jq -s '.[] | select( .schema == "olm.bundle" ) | select( .package == "<package_name>") | .name'`

Bundle dependencies and available APIs
: `jq -s '.[] | select( .schema == "olm.bundle" ) | select ( .name == "<bundle_name>") | select( .package == "<package_name>")'`
