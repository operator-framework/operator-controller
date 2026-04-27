# Catalog queries using GraphQL

!!! warning "Alpha Feature"
    The GraphQL endpoint is an **alpha feature** controlled by the `GraphQLCatalogQueries` feature gate.
    The API and behavior may change in future releases.

After you [add a catalog of extensions](../../tutorials/add-catalog.md) to your cluster, you can query the catalog using GraphQL for flexible, structured queries with precise field selection.

## Prerequisites

* You have added a ClusterCatalog of extensions, such as [OperatorHub.io](https://operatorhub.io), to your cluster.
* The `GraphQLCatalogQueries` feature gate is enabled in catalogd.

!!! note
    By default, Catalogd is installed with TLS enabled for the catalog webserver.
    The following examples will show this default behavior, but for simplicity's sake will ignore TLS verification in the curl commands using the `-k` flag.

You also need to port forward the catalog server service:

``` terminal
kubectl -n olmv1-system port-forward svc/catalogd-service 8443:443
```

## GraphQL Endpoint

The GraphQL endpoint is available at:

```
https://localhost:8443/catalogs/<catalog-name>/api/v1/graphql
```

All queries must be sent as **HTTP POST** requests with a JSON body containing a `query` field.

## Understanding GraphQL Field Names

**IMPORTANT**: GraphQL field names are automatically generated from catalog schema names.

### Naming Convention

Schema names are converted to GraphQL field names using this process:

1. Remove dots and special characters: `olm.bundle` → `olmbundle`
2. Convert to lowercase: `OLM.Bundle` → `olmbundle`  
3. Append 's' for pluralization: `olmbundle` → `olmbundles`

**Examples:**

| Schema Name | GraphQL Field Name |
|-------------|-------------------|
| `olm.bundle` | `olmbundles` |
| `olm.package` | `olmpackages` |
| `olm.channel` | `olmchannels` |
| `helm.chart` | `helmcharts` |

### Discovering Available Fields

To find the exact field names available for your catalog, use GraphQL introspection:

``` terminal
curl -k -X POST 'https://localhost:8443/catalogs/operatorhubio/api/v1/graphql' \
  -H "Content-Type: application/json" \
  -d '{
    "query": "{ __schema { queryType { fields { name description } } } }"
  }' | jq
```

This returns all available query fields for the catalog, including the automatically generated schema-based fields.

!!! warning "Pluralization Limitations"
    The current implementation appends 's' to schema names for pluralization. This may not produce grammatically correct English plurals in all cases (e.g., `index` → `indexs` instead of `indices`). When creating custom schemas, use singular nouns that pluralize well with a simple 's' suffix.

## Basic Queries

### Catalog Summary

Get an overview of schemas and object counts in the catalog:

``` terminal
curl -k -X POST 'https://localhost:8443/catalogs/operatorhubio/api/v1/graphql' \
  -H "Content-Type: application/json" \
  -d '{
    "query": "{ summary { totalSchemas schemas { name totalObjects totalFields } } }"
  }' | jq
```

### Query Bundles

List bundles with specific fields:

``` terminal
curl -k -X POST 'https://localhost:8443/catalogs/operatorhubio/api/v1/graphql' \
  -H "Content-Type: application/json" \
  -d '{
    "query": "{ olmbundles(limit: 5, offset: 0) { name package image } }"
  }' | jq
```

### Query Packages

List packages with metadata:

``` terminal
curl -k -X POST 'https://localhost:8443/catalogs/operatorhubio/api/v1/graphql' \
  -H "Content-Type: application/json" \
  -d '{
    "query": "{ olmpackages(limit: 10) { name description defaultChannel } }"
  }' | jq
```

### Query Channels

List channels:

``` terminal
curl -k -X POST 'https://localhost:8443/catalogs/operatorhubio/api/v1/graphql' \
  -H "Content-Type: application/json" \
  -d '{
    "query": "{ olmchannels { name package entries } }"
  }' | jq
```

## Advanced Queries

### Pagination

All schema-based queries support pagination via `limit` and `offset` arguments:

``` terminal
curl -k -X POST 'https://localhost:8443/catalogs/operatorhubio/api/v1/graphql' \
  -H "Content-Type: application/json" \
  -d '{
    "query": "{ olmbundles(limit: 10, offset: 20) { name } }"
  }' | jq
```

### Nested Field Selection

Select only the fields you need, including nested objects:

``` terminal
curl -k -X POST 'https://localhost:8443/catalogs/operatorhubio/api/v1/graphql' \
  -H "Content-Type: application/json" \
  -d '{
    "query": "{ olmpackages { name icon { mediatype base64data } } }"
  }' | jq
```

### Complex Bundle Properties

Query bundle properties with their type and value fields:

``` terminal
curl -k -X POST 'https://localhost:8443/catalogs/operatorhubio/api/v1/graphql' \
  -H "Content-Type: application/json" \
  -d '{
    "query": "{ olmbundles(limit: 5) { name properties { type value } } }"
  }' | jq
```

**Note:** The `properties` field contains an array of objects, each with a `type` string and a `value` field that can contain complex nested data. GraphQL will return the full JSON structure for the `value` field.

## Comparing GraphQL vs Metas Endpoint

| Feature | GraphQL (`/api/v1/graphql`) | Metas (`/api/v1/metas`) |
|---------|---------------------------|------------------------|
| Field selection | Precise - request only needed fields | All fields always returned |
| Query complexity | Rich queries with nested objects | Simple parameter-based filtering |
| Response size | Minimal - only requested data | Full objects always returned |
| Schema discovery | Introspection built-in | External documentation needed |
| Pagination | Built-in `limit` and `offset` | Manual implementation required |
| HTTP Method | POST only | GET supported |
| Feature status | Alpha (feature gate required) | Stable |

**When to use GraphQL:**
- You need specific fields from large objects
- You want to query related data in a single request
- You need structured, typed responses
- You're building a UI or client that benefits from precise data fetching

**When to use Metas endpoint:**
- You need simple, stable API
- You're doing basic filtering by schema/package/name
- You want to use GET requests for caching
- You need guaranteed API stability

## Limitations

1. **Pluralization**: Schema names are pluralized by appending 's', which may not be grammatically correct for all words
2. **Schema naming**: Full schema names (including namespace/prefix) are preserved in field names (`olm.bundle` → `olmbundles`, not `bundles`)
3. **POST only**: GraphQL endpoint only accepts POST requests, unlike the metas endpoint which supports GET
4. **Alpha stability**: API may change in future releases while in alpha

## Enabling the GraphQL Feature

The GraphQL endpoint is controlled by the `GraphQLCatalogQueries` feature gate. To enable it:

``` yaml
args:
  - --feature-gates=GraphQLCatalogQueries=true
```

See [enable webhook support](enable-webhook-support.md) for more details on configuring feature gates.
