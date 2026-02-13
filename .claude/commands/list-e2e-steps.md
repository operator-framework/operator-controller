---
description: List all available Godog e2e step definitions with categories, parameters, and descriptions
---

# List E2E Steps

Generate a categorized reference of all Godog (Cucumber BDD) step definitions available for writing e2e feature files.

## Instructions for Claude AI

When this command is invoked, you MUST:

**CRITICAL:** The final output MUST be a comprehensive categorized reference displayed directly in the conversation. Do NOT just create files - output the full reference as your response.

1. **Get the authoritative list of registered step patterns**

   Run:
   ```bash
   go test ./test/e2e/features_test.go -d || true 2>&1
   ```
   The `-d` flag prints all registered step definitions without running tests. Parse each step regex pattern from the output.

2. **Read the step registration and handler implementations**

   Read `test/e2e/steps/steps.go` and locate the `RegisterSteps()` function. For each regex pattern from step 1, find its corresponding `sc.Step()` call and the handler function it references.

   Then read each handler function's signature to determine:
   - What parameters it accepts (string, int, `*godog.DocString`, etc.)
   - Whether it expects a DocString (multi-line YAML block in the feature file)
   
   Consult the handler documentation to understand what the step does. If the documentation is missing,
   check what the handler actually does (kubectl commands, polling, assertions, etc.)

3. **Output a categorized reference**

   Organize all steps into the following categories and format. Use the information gathered from the handler implementations to write accurate descriptions.

### Output Format

Start with a variable substitution reference, then list steps by category.

---

#### Variable Substitution

These variables are automatically replaced in DocString content and some string parameters:

| Variable | Replaced With | Example                                       |
|---|---|-----------------------------------------------|
| `${TEST_NAMESPACE}` | Scenario's test namespace | `olmv1-e2e-abc123`                            |
| `${NAME}` | Current ClusterExtension name | `ce-123`                                      |

---

#### Categories

Organize steps into these 10 categories. For each step, document:
- **Pattern**: The human-readable Gherkin step text (with capture groups shown as `<arg>`)
- **Parameters**: Each captured parameter with its Go type
- **DocString**: Whether the step expects a `"""` YAML block (and what it should contain)
- **Description**: What the handler function does, based on reading its documentation

**Categories:**

1. **Setup & Prerequisites** - OLM availability, ServiceAccount setup, permissions
2. **Catalog Management** - ClusterCatalog creation, updates, image tagging, deletion
3. **ClusterExtension Lifecycle** - Apply, update, remove ClusterExtension resources
4. **ClusterExtension Status & Conditions** - Condition checks, transition times, reconciliation
5. **ClusterExtensionRevision** - Revision-specific condition checks, archival, annotations, labels, active revisions
6. **Generic Resource Operations** - Get, delete, restore, match arbitrary resources
7. **Test Operator Control** - Marking test-operator deployment ready/not-ready
8. **Metrics** - Fetching and validating Prometheus metrics
9. **CRD Patching** - Setting minimum values on CRD fields

Group steps by category.

For each step entry, use this format:

```
### <N>. <Category Name>

#### `<human-readable pattern>`
- **Parameters:** `<name>` (string), ...
- **DocString:** Yes/No — <what to include if yes>
- **Description:** <what the handler does>
- **Handler:** `<FunctionName>` in steps.go:<line>
- **Polling:** Yes/No - <specify the timeout if yes>
```

---

#### Notes Section

After all categories, include a **Notes** section covering:

- **Case insensitivity**: Most patterns use `(?i)` — step text is case-insensitive
- **Resource format**: Steps accepting a `"resource"` parameter expect `kind/name` format (e.g., `"deployment/my-deploy"`)
- **Scenario context**: The ClusterExtension name is auto-captured when a ClusterExtension is applied — subsequent steps reference it implicitly
- **Duplicate aliases**: Some steps have multiple patterns mapping to the same handler (e.g., `resource "X" is installed` / `resource "X" is available` / `resource "X" exists` all map to `ResourceAvailable`). Note which are aliases.
- **Catalog naming convention**: Catalog steps automatically append `-catalog` to the name parameter (e.g., `"test"` becomes `"test-catalog"`)
