---
description: Validate API issues using kube-api-linter with diff-aware analysis
---

# API Lint Diff

Validates API issues in `api/` directory using kube-api-linter with diff-aware analysis that distinguishes between FIXED, NEW, and PRE-EXISTING issues.

## Instructions for Claude AI

When this command is invoked, you MUST:

**CRITICAL:** The final output MUST be a comprehensive analysis report displayed directly in the conversation for the user to read. Do NOT just create temp files - output the full report as your response.

1. **Execute the shell script**
   ```bash
   bash hack/api-lint-diff/run.sh
   ```

2. **Understand the shell script's output**:
   - **False positives (IGNORED)**: Standard CRD scaffolding patterns that kube-api-linter incorrectly flags
   - **FIXED issues (SUCCESS)**: Issues that existed in baseline but were resolved in current branch → Celebrate! 🎉
   - **NEW issues (ERRORS)**: Introduced in current branch → MUST fix
   - **PRE-EXISTING issues (WARNINGS)**: Existed before changes → Can fix separately

3. **Filter false positives** - Operator projects scaffold standard Kubernetes CRD patterns that kube-api-linter incorrectly flags as errors, even with WhenRequired configuration.

   **Scenario 1: optionalfields on Status field**
   ```go
   Status MyResourceStatus `json:"status,omitzero"`
   ```
   **Error reported:**
   ```
   optionalfields: field Status has a valid zero value ({}), but the validation
   is not complete (e.g. min properties/adding required fields). The field should
   be a pointer to allow the zero value to be set. If the zero value is not a
   valid use case, complete the validation and remove the pointer.
   ```
   **Why it's a FALSE POSITIVE:**
   - Status is NEVER a pointer in any Kubernetes API
   - Works correctly with `omitzero` tag
   - Validation incompleteness is expected - Status is controller-managed, not user-provided
   - **ACTION: IGNORE this error**

   **Scenario 2: nonpointerstructs on Spec field**
   ```go
   Spec MyResourceSpec `json:"spec"`
   ```
   **Error reported:**
   ```
   requiredfields: field Spec has a valid zero value ({}), but the validation is
   not complete (e.g. min properties/adding required fields). The field should be
   a pointer to allow the zero value to be set. If the zero value is not a valid
   use case, complete the validation and remove the pointer.
   ```
   **Why it's a FALSE POSITIVE:**
   - Spec is NEVER a pointer in Kubernetes APIs
   - Scaffolds are starting points - users add validation when they implement their business logic
   - **ACTION: IGNORE this error**

   **Scenario 3: conditions markers on metav1.Condition**
   ```go
   Conditions []metav1.Condition `json:"conditions,omitempty"`
   ```
   **Error reported:**
   ```
   conditions: Conditions field is missing the following markers:
   patchStrategy=merge, patchMergeKey=type
   ```
   **Why it's a FALSE POSITIVE:**
   - `metav1.Condition` already handles patches correctly
   - Adding these markers is redundant for this standard Kubernetes type
   - **ACTION: IGNORE this error**

4. **For reported issues, provide intelligent analysis**:

   a. **Categorize by fix complexity**:
      - NON-BREAKING: Marker replacements, adding listType, adding +required/+optional
      - BREAKING: Pointer conversions, type changes (int→int32)

   b. **Search for actual usage** (REQUIRED FOR ALL ISSUES - NOT OPTIONAL):
      - **CRITICAL:** Do NOT just look at JSON tags - analyze actual code usage patterns
      - **Exception:** Deprecated marker replacements (`+kubebuilder:validation:Required` → `+required`) are straightforward - no usage analysis needed
      - **For all other issues:** MUST analyze actual usage before making recommendations
      - Use grep to find ALL occurrences where each field is:
        * **Read/accessed**: `obj.Spec.FieldName`, `cat.Spec.Priority`
        * **Written/set**: `obj.Spec.FieldName = value`
        * **Checked for zero/nil**: `if obj.Spec.FieldName == ""`, `if ptr != nil`
        * **Used in conditionals**: Understand semantic meaning of zero values
      - Search in: controllers, reconcilers, internal packages, tests, examples
      - **Smart assessment based on usage patterns**:
        * Field ALWAYS set in code? → Should be **required**, no omitempty
        * Field SOMETIMES set? → Should be **optional** with omitempty
        * Code checks `if field == zero`? → May need **pointer** to distinguish zero vs unset
        * Zero value semantically valid? → Keep as value type with omitempty
        * Zero value semantically invalid? → Use pointer OR mark required
        * Field never read but only set by controller? → Likely Status field
      - **Example analysis workflow for a field**:
        ```
        1. Grep for field usage: `CatalogFilter.Version`
        2. Found 5 occurrences:
           - controllers/extension.go:123: if filter.Version != "" { ... }
           - controllers/extension.go:456: result.Version = bundle.Version
           - tests/filter_test.go:89: Version: "1.2.3"
        3. Analysis: Version is checked for empty, sometimes set, sometimes omitted
        4. Recommendation: Optional with omitempty (current usage supports this)
        ```

   c. **Generate EXACT code fixes** grouped by file:
      - Show current code
      - Show replacement code, ready to copy and paste
      - **Explain why based on actual usage analysis** (not just JSON tags):
        * Include usage summary: "Found N occurrences"
        * Cite specific examples: "Used in resolve/catalog.go:163 as direct int32"
        * Explain semantic meaning: "Field distinguishes priority 0 vs unset"
        * Justify recommendation: "Since code checks for empty, should be optional"
      - Note breaking change impact with reasoning
      - **Each fix MUST include evidence from code usage**

   d. **Prioritize recommendations**:
      - NEW issues first (must fix)
      - Group PRE-EXISTING by NON-BREAKING vs BREAKING

5. **Present actionable report directly to user**:
   - **IMPORTANT:** Output the full comprehensive analysis in the conversation (not just to a temp file)
   - Summary: False positives filtered, NEW count, PRE-EXISTING count
   - Group issues by file and fix type
   - Provide code snippets ready to apply (current code → fixed code)
   - **DO NOT include "Next Steps" or "Conclusion" sections** - just present the analysis

   **Report Structure:**
   ```
   # API Lint Diff Analysis Report

   **Generated:** [date]
   **Baseline:** main branch (X issues)
   **Current:** [branch name] (Y issues)
   **Status:** [status icon and message based on logic below]

   **Status Logic:**
   - ✅ PASSED: 0 new issues (fixed issues are OK)
   - ⚠️ WARN: 0 new issues but has pre-existing issues
   - ❌ FAIL: Has new issues that must be fixed

   ## Executive Summary
   - Baseline issues: X
   - Current issues: Y
   - **FIXED**: F (issues resolved in this branch)
   - **NEW**: N (issues introduced in this branch)
   - **PRE-EXISTING**: P (issues that still remain)
   - False positives (IGNORED): Z

   ## FIXED ISSUES (F issues)

   [List of issues that were fixed in this branch - show the baseline line numbers]

   ## NEW ISSUES (N issues)

   [List of issues introduced in this branch - these MUST be fixed]

   ## PRE-EXISTING ISSUES (P issues)

   [List of issues that existed before and still exist - can be fixed separately]

   ---

   ## DETAILED ANALYSIS FOR ISSUES NEEDING FIXES

   ### Category 1: [Issue Type] (N issues) - [BREAKING/NON-BREAKING]

   #### File: [filename]

   **[Issue #]. Line X - [Field Name]**
   ```go
   // CURRENT:
   [current code]

   // FIX:
   [fixed code]
   ```
   **Usage Analysis:**
   - Found N occurrences in codebase
   - [Specific usage example 1]: path/file.go:123
   - [Specific usage example 2]: path/file.go:456
   - Pattern: [always set / sometimes set / checked for zero / etc.]

   **Why:** [Recommendation based on usage analysis with evidence]
   **Breaking:** [YES/NO] ([detailed reason with impact])

   [Repeat for all issues]

   ## Summary of Breaking Changes
   [Table of breaking changes if any]
   ```

## Related Documentation

- [Kubernetes API Conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md)
- [kube-api-linter](https://github.com/kubernetes/kubernetes/tree/master/staging/src/k8s.io/code-generator/cmd/kube-api-linter)
- AGENTS.md in this repository for understanding operator patterns
