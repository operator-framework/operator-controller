# Catalog queries

**Note:** By default, Catalogd is installed with TLS enabled for the catalog webserver.
The following examples will show this default behavior, but for simplicity's sake will ignore TLS verification in the curl commands using the `-k` flag.


You can use the `curl` command with `jq` to query catalogs that are installed on your cluster.

``` terminal title="Query syntax"
curl -k https://localhost:8443/catalogs/operatorhubio/all.json | <query>
```

## Package queries

Available packages in a catalog
: 
``` terminal
jq -s '.[] | select( .schema == "olm.package")
```

Packages that support `AllNamespaces` install mode and do not use webhooks

: 
``` terminal
jq -c 'select(.schema == "olm.bundle") | {"package":.package, "version":.properties[] | select(.type == "olm.bundle.object").value.data |  @base64d | fromjson | select(.kind == "ClusterServiceVersion" and (.spec.installModes[] | select(.type == "AllNamespaces" and .supported == true) != null) and .spec.webhookdefinitions == null).spec.version}'
```

Package metadata
: 
``` terminal
jq -s '.[] | select( .schema == "olm.package") | select( .name == "<package_name>")'
```

Catalog blobs in a package
: 
``` terminal
jq -s '.[] | select( .package == "<package_name>")'
```

## Channel queries

Channels in a package
: 
``` terminal
jq -s '.[] | select( .schema == "olm.channel" ) | select( .package == "<package_name>") | .name'
```

Versions in a channel
: 
``` terminal
jq -s '.[] | select( .package == "<package_name>" ) | select( .schema == "olm.channel" ) | select( .name == "<channel_name>" ) | .entries | .[] | .name'
```

Latest version in a channel and upgrade path
: 
``` terminal 
jq -s '.[] | select( .schema == "olm.channel" ) | select ( .name == "<channel>") | select( .package == "<package_name>")'
```

## Bundle queries

Bundles in a package
: 
``` terminal
jq -s '.[] | select( .schema == "olm.bundle" ) | select( .package == "<package_name>") | .name'
```

Bundle dependencies and available APIs
: 
``` terminal
jq -s '.[] | select( .schema == "olm.bundle" ) | select ( .name == "<bundle_name>") | select( .package == "<package_name>")'
```
