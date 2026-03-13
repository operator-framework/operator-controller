# GraphQL Integration

This package provides dynamic GraphQL schema generation for operator catalog data, integrated into the catalogd storage server.

## Usage

The GraphQL endpoint is now available as part of the catalogd storage server at:

```
{catalog}/api/v1/graphql
```

Where `{catalog}` is replaced by the actual catalog name at runtime.

## Example Usage

### Making a GraphQL Request

```bash
curl -X POST http://localhost:8080/my-catalog/api/v1/graphql \
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
  bundles(limit: 5, offset: 0) {
    name
    package
    version
  }
}
```

#### Get packages:
```graphql
{
  packages(limit: 10) {
    name
    description
  }
}
```

#### Get bundle properties (union types):
```graphql
{
  bundles(limit: 5) {
    name
    properties {
      type
      value {
        ... on PropertyValueFeaturesOperatorsOpenshiftIo {
          disconnected
          cnf
          cni
          csi
          fips
        }
      }
    }
  }
}
```

## Features

- **Dynamic Schema Generation**: Automatically discovers schema structure from catalog metadata
- **Union Types**: Supports complex bundle properties with variable structures
- **Pagination**: Built-in limit/offset pagination for all queries
- **Field Name Sanitization**: Converts JSON field names to valid GraphQL identifiers
- **Catalog-Specific**: Each catalog gets its own dynamically generated schema

## Integration

The GraphQL functionality is integrated into the `LocalDirV1` storage handler in `internal/catalogd/storage/localdir.go`:

- `handleV1GraphQL()`: Handles POST requests to the GraphQL endpoint
- `createCatalogFS()`: Creates filesystem interface for catalog data
- `buildCatalogGraphQLSchema()`: Builds dynamic GraphQL schema for specific catalogs

## Technical Details

- Uses `declcfg.WalkMetasFS` to discover schema structure
- Generates GraphQL object types dynamically from discovered fields
- Creates union types for bundle properties with variable structures
- Supports all standard GraphQL features including introspection 