# GraphQL Integration

This package provides dynamic GraphQL schema generation for operator catalog data, integrated into the catalogd storage server.

⚠️ **Alpha Feature**: This is an experimental feature controlled by the `GraphQLCatalogQueries` feature gate. See user documentation at `docs/draft/howto/catalog-queries-graphql-endpoint.md`.

## Usage

The GraphQL endpoint is now available as part of the catalogd storage server at:

```
/catalogs/{catalog}/api/v1/graphql
```

Where `{catalog}` is replaced by the actual catalog name at runtime.

## Example Usage

### Making a GraphQL Request

```bash
curl -X POST http://localhost:8080/catalogs/my-catalog/api/v1/graphql \
  -H "Content-Type: application/json" \
  -d '{
    "query": "{ summary { totalSchemas schemas { name totalObjects totalFields } } }"
  }'
```

### Sample Queries

#### Get catalog summary:
```graphql
{
  summary {
    totalSchemas
    schemas {
      name
      totalObjects
      totalFields
    }
  }
}
```

#### Get bundles with pagination:
```graphql
{
  olmbundles(limit: 5, offset: 0) {
    name
    package
    version
  }
}
```

#### Get packages:
```graphql
{
  olmpackages(limit: 10) {
    name
    description
  }
}
```

#### Get channels:
```graphql
{
  olmchannels(limit: 10) {
    name
    package
    entries
  }
}
```

## Features

- **Dynamic Schema Generation**: Automatically discovers schema structure from catalog metadata
- **Nested Object Support**: Handles complex nested structures like bundle properties and related images
- **Pagination**: Built-in limit/offset pagination for all queries
- **Field Name Sanitization**: Converts JSON field names to valid GraphQL identifiers
- **Catalog-Specific**: Each catalog gets its own dynamically generated schema
- **Query Performance**: Pre-parsed objects cached during schema build eliminate JSON unmarshaling overhead

## Integration

The GraphQL functionality is integrated across multiple packages:

- `internal/catalogd/server/handlers.go`: `CatalogHandlers.handleV1GraphQL()` handles POST requests to the GraphQL endpoint
- `internal/catalogd/storage/localdir.go`: `LocalDirV1.GetCatalogFS()` creates filesystem interface for catalog data
- `internal/catalogd/service/graphql_service.go`: `GraphQLService.GetSchema()` and `buildSchemaFromFS()` build dynamic GraphQL schemas for specific catalogs

## Technical Details

- Uses `declcfg.WalkMetasFS` to discover schema structure from catalog metadata
- Generates GraphQL object types dynamically from discovered fields
- Handles nested objects (arrays of objects) by creating dynamic nested types
- Pre-parses all catalog objects during schema build and caches them for fast query execution
- Supports all standard GraphQL features including introspection

## Field Naming Conventions

### Schema to GraphQL Field Name Mapping

**IMPORTANT**: GraphQL field names are automatically generated from schema names using the following convention:

1. **Remove dots and special characters** - `olm.bundle` becomes `olmbundle`
2. **Convert to lowercase** - `OLM.Bundle` becomes `olmbundle`
3. **Append 's' for pluralization** - `olmbundle` becomes `olmbundles`

**Examples:**
- `olm.bundle` → `olmbundles`
- `olm.package` → `olmpackages`
- `olm.channel` → `olmchannels`
- `helm.chart` → `helmcharts`
- `custom.operator` → `customoperators`

### Limitations and Considerations

⚠️ **Pluralization Limitations**: The current implementation blindly appends 's' to create plural field names. This approach has known limitations:

1. **English grammar rules not applied**: Words ending in 's', 'x', 'z', 'ch', 'sh' should use 'es', but currently just get 's' appended
2. **Irregular plurals not supported**: Schema names like `person`, `child`, `index` will become `persons`, `childs`, `indexs` instead of proper English plurals
3. **Non-English schema names**: Schemas using non-English words will not follow appropriate pluralization rules for their language
4. **Already-plural names**: If a schema name is already plural, it will still get 's' appended

**Recommendations for schema naming:**
- Use schema names that work well with simple 's' pluralization (e.g., `bundle`, `package`, `chart`)
- Avoid schema names that are already plural or have irregular plural forms
- Document the expected GraphQL field names in your catalog documentation
- Use GraphQL introspection to discover actual field names: `{ __schema { queryType { fields { name } } } }`

### Field Name Sanitization

All field names within objects are sanitized to be valid GraphQL identifiers:

- Special characters (dots, hyphens, etc.) are replaced with underscores
- CamelCase conversion: `package-name` → `packageName`, `default_channel` → `defaultChannel`
- Names starting with numbers get `field_` prefix: `123invalid` → `field_123invalid`
- Empty or invalid names default to `value`

See `remapFieldName()` function for complete logic.