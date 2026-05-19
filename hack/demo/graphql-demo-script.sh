#!/usr/bin/env bash

#
# Catalogd Dynamic GraphQL API Demo
#
# This demo showcases the dynamic GraphQL endpoint for querying
# File-Based Catalog (FBC) content. The schema is automatically
# discovered from catalog data -- no manual type definitions needed.
#

set -euo pipefail
trap cleanup SIGINT SIGTERM EXIT

SCRIPTPATH="$(cd -- "$(dirname "$0")" > /dev/null 2>&1; pwd -P)"
SERVER_PID=""
PORT=9376
BASE="http://localhost:${PORT}/catalogs/example-catalog/api/v1/graphql"

cleanup() {
    if [[ -n "${SERVER_PID}" ]]; then
        kill "${SERVER_PID}" 2>/dev/null || true
        wait "${SERVER_PID}" 2>/dev/null || true
    fi
    if [[ -n "${TMPBIN:-}" && -f "${TMPBIN}" ]]; then
        rm -f "${TMPBIN}"
    fi
}

gql() {
    local query="$1"
    curl -s -X POST "${BASE}" \
        -H "Content-Type: application/json" \
        -d "{\"query\": \"$query\"}" | jq .
}

banner() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "  $1"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
}

# -- Build and start the demo server --
banner "Building GraphQL demo server"
TMPBIN=$(mktemp /tmp/graphql-demo.XXXXXX)
REPOROOT="$(cd "${SCRIPTPATH}/../.."; pwd)"
(cd "${REPOROOT}" && go build -o "${TMPBIN}" ./hack/demo/graphql-demo-server/)
echo "Built successfully."

echo ""
echo "Starting server on port ${PORT}..."
"${TMPBIN}" 2>/dev/null &
SERVER_PID=$!
sleep 1
echo "Server ready. Catalog loaded: example-catalog (5 packages, 11 bundles)"

# -- 1. Catalog Summary --
banner "1. Discover catalog contents (summary query)"
echo '$ curl ... -d '\''{"query": "{ summary { totalSchemas schemas { name totalObjects totalFields } } }"}'\'''
echo ""
gql "{ summary { totalSchemas schemas { name totalObjects totalFields } } }"
sleep 2

# -- 2. List packages --
banner "2. List packages (field selection: name + defaultChannel only)"
echo '$ curl ... -d '\''{"query": "{ olmpackages { name defaultChannel } }"}'\'''
echo ""
gql "{ olmpackages { name defaultChannel } }"
sleep 2

# -- 3. List bundles with pagination --
banner "3. Browse bundles with pagination (offset 3, limit 4)"
echo '$ curl ... -d '\''{"query": "{ olmbundles(limit: 4, offset: 3) { name package } }"}'\'''
echo ""
gql "{ olmbundles(limit: 4, offset: 3) { name package } }"
sleep 2

# -- 4. Nested properties --
banner "4. Query bundle properties (nested objects)"
echo '$ curl ... -d '\''{"query": "{ olmbundles(limit: 2) { name package properties { type value } } }"}'\'''
echo ""
gql "{ olmbundles(limit: 2) { name package properties { type value } } }"
sleep 2

# -- 5. Related images --
banner "5. Query related images (nested array)"
echo '$ curl ... -d '\''{"query": "{ olmbundles(limit: 1) { name relatedImages { name image } } }"}'\'''
echo ""
gql "{ olmbundles(limit: 1) { name relatedImages { name image } } }"
sleep 2

# -- 6. Channel upgrade graph --
banner "6. Explore channel upgrade graph"
echo '$ curl ... -d '\''{"query": "{ olmchannels(limit: 2) { name package entries { name skipRange } } }"}'\'''
echo ""
gql "{ olmchannels(limit: 2) { name package entries { name skipRange } } }"
sleep 2

# -- 7. Cross-schema query --
banner "7. Multi-schema query in one request"
echo '$ curl ... -d '\''{"query": "{ olmpackages(limit: 3) { name defaultChannel } olmbundles(limit: 3) { name package } }"}'\'''
echo ""
gql "{ olmpackages(limit: 3) { name defaultChannel } olmbundles(limit: 3) { name package } }"
sleep 2

# -- 8. GraphQL introspection --
banner "8. Standard GraphQL introspection (auto-generated types)"
echo '$ curl ... -d '\''{"query": "{ __schema { types { name kind } } }"}'\'' | jq ...'
echo ""
curl -s -X POST "${BASE}" \
    -H "Content-Type: application/json" \
    -d '{"query": "{ __schema { types { name kind } } }"}' \
    | jq '[.data.__schema.types[] | select(.name | startswith("__") | not) | select(.kind == "OBJECT")]'
sleep 2

# -- Done --
banner "Demo complete"
echo ""
echo "Key takeaways:"
echo "  - GraphQL schema is auto-discovered from FBC data"
echo "  - No code changes needed when FBC schemas evolve"
echo "  - Supports field selection, pagination, nested queries"
echo "  - Full GraphQL introspection for tooling support"
echo ""
