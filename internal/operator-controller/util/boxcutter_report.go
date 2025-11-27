package util

import (
	"fmt"
	"strings"

	"pkg.package-operator.run/boxcutter/machinery"
)

// SummarizeRevisionResult creates a concise, human-readable summary of a boxcutter
// RevisionResult, extracting key information without the verbose details of String().
// This is similar to how crdupgradesafety.conciseUnhandledMessage works for CRD diffs.
func SummarizeRevisionResult(result machinery.RevisionResult) string {
	if result == nil {
		return ""
	}

	var parts []string

	// Check for validation errors first (using error interface)
	if verr := result.GetValidationError(); verr != nil {
		parts = append(parts, fmt.Sprintf("validation error: %s", verr.Error()))
	}

	// Summarize phase information
	phases := result.GetPhases()
	if len(phases) > 0 {
		phaseSummary := summarizePhases(phases)
		if phaseSummary != "" {
			parts = append(parts, phaseSummary)
		}
	}

	// Add completion status
	if !result.IsComplete() {
		if result.InTransistion() {
			parts = append(parts, "status: in transition")
		} else {
			parts = append(parts, "status: incomplete")
		}
	}

	if len(parts) == 0 {
		return "reconcile completed successfully"
	}

	return strings.Join(parts, "; ")
}

// summarizePhases creates a summary of phase results, focusing on problems
func summarizePhases(phases []machinery.PhaseResult) string {
	var problemPhases []string
	var successfulPhases []string

	for _, phase := range phases {
		phaseName := phase.GetName()
		if phaseName == "" {
			phaseName = "unnamed"
		}

		// Check for validation errors (using error interface)
		if verr := phase.GetValidationError(); verr != nil {
			problemPhases = append(problemPhases, fmt.Sprintf("%s: validation error", phaseName))
			continue
		}

		// Check for object issues
		objects := phase.GetObjects()
		if len(objects) > 0 {
			objectSummary := summarizeObjects(objects)
			if objectSummary.hasIssues {
				problemPhases = append(problemPhases, fmt.Sprintf("%s: %s", phaseName, objectSummary.summary))
			} else if phase.IsComplete() {
				successfulPhases = append(successfulPhases, phaseName)
			}
		}

		// Check phase completion status
		if !phase.IsComplete() && len(objects) == 0 {
			problemPhases = append(problemPhases, fmt.Sprintf("%s: incomplete", phaseName))
		}
	}

	var parts []string
	if len(problemPhases) > 0 {
		parts = append(parts, fmt.Sprintf("phases with issues: %s", strings.Join(problemPhases, ", ")))
	}
	if len(successfulPhases) > 0 && len(problemPhases) == 0 {
		parts = append(parts, fmt.Sprintf("%d phase(s) successful", len(successfulPhases)))
	}

	return strings.Join(parts, "; ")
}

type objectSummary struct {
	hasIssues bool
	summary   string
}

// summarizeObjects creates a summary of object results
func summarizeObjects(objects []machinery.ObjectResult) objectSummary {
	var collisions []string
	var failures []string
	var probeFailures []string
	successCount := 0

	for _, obj := range objects {
		action := obj.Action()
		success := obj.Success()
		objInfo := getObjectInfo(obj.Object())

		switch action {
		case machinery.ActionCollision:
			collisions = append(collisions, objInfo)
		default:
			if !success {
				failures = append(failures, fmt.Sprintf("%s (action: %s)", objInfo, action))
			} else {
				// Check probe results
				probes := obj.Probes()
				for probeName, probeResult := range probes {
					if !probeResult.Success {
						// Only include the probe name and status, as probeResult.Message doesn't exist
						probeFailures = append(probeFailures, fmt.Sprintf("%s probe '%s' failed", objInfo, probeName))
					}
				}
				if len(probes) == 0 || allProbesSuccessful(probes) {
					successCount++
				}
			}
		}
	}

	var parts []string
	if len(collisions) > 0 {
		// Limit to first 3 collisions to avoid verbose output
		displayed := collisions
		if len(collisions) > 3 {
			displayed = collisions[:3]
			parts = append(parts, fmt.Sprintf("%d collision(s) [showing first 3: %s]", len(collisions), strings.Join(displayed, ", ")))
		} else {
			parts = append(parts, fmt.Sprintf("%d collision(s): %s", len(collisions), strings.Join(displayed, ", ")))
		}
	}
	if len(failures) > 0 {
		// Limit to first 3 failures
		displayed := failures
		if len(failures) > 3 {
			displayed = failures[:3]
			parts = append(parts, fmt.Sprintf("%d failed object(s) [showing first 3: %s]", len(failures), strings.Join(displayed, ", ")))
		} else {
			parts = append(parts, fmt.Sprintf("%d failed object(s): %s", len(failures), strings.Join(displayed, ", ")))
		}
	}
	if len(probeFailures) > 0 {
		// Limit to first 3 probe failures
		displayed := probeFailures
		if len(probeFailures) > 3 {
			displayed = probeFailures[:3]
			parts = append(parts, fmt.Sprintf("%d probe failure(s) [showing first 3: %s]", len(probeFailures), strings.Join(displayed, ", ")))
		} else {
			parts = append(parts, fmt.Sprintf("%d probe failure(s): %s", len(probeFailures), strings.Join(displayed, ", ")))
		}
	}

	hasIssues := len(collisions) > 0 || len(failures) > 0 || len(probeFailures) > 0
	summary := strings.Join(parts, "; ")

	if !hasIssues && successCount > 0 {
		summary = fmt.Sprintf("%d object(s) applied successfully", successCount)
	}

	return objectSummary{
		hasIssues: hasIssues,
		summary:   summary,
	}
}

// getObjectInfo extracts a human-readable identifier from an object
func getObjectInfo(obj machinery.Object) string {
	if obj == nil {
		return "unknown object"
	}

	gvk := obj.GetObjectKind().GroupVersionKind()
	name := obj.GetName()
	namespace := obj.GetNamespace()

	kind := gvk.Kind
	if kind == "" {
		kind = "unknown"
	}

	if namespace != "" {
		return fmt.Sprintf("%s %s/%s", kind, namespace, name)
	}
	return fmt.Sprintf("%s %s", kind, name)
}

// allProbesSuccessful checks if all probes passed
func allProbesSuccessful(probes map[string]machinery.ObjectProbeResult) bool {
	for _, result := range probes {
		if !result.Success {
			return false
		}
	}
	return true
}

// SummarizeRevisionTeardownResult creates a concise summary of a teardown result
func SummarizeRevisionTeardownResult(result machinery.RevisionTeardownResult) string {
	if result == nil {
		return ""
	}

	if result.IsComplete() {
		return "teardown completed successfully"
	}

	var parts []string

	// Check waiting phases
	waitingPhases := result.GetWaitingPhaseNames()
	if len(waitingPhases) > 0 {
		parts = append(parts, fmt.Sprintf("waiting on phases: %s", strings.Join(waitingPhases, ", ")))
	}

	// Summarize phase teardown
	phases := result.GetPhases()
	if len(phases) > 0 {
		phaseSummary := summarizeTeardownPhases(phases)
		if phaseSummary != "" {
			parts = append(parts, phaseSummary)
		}
	}

	if len(parts) == 0 {
		return "teardown in progress"
	}

	return strings.Join(parts, "; ")
}

// summarizeTeardownPhases creates a summary of phase teardown results
func summarizeTeardownPhases(phases []machinery.PhaseTeardownResult) string {
	var incompletePhases []string
	completedCount := 0

	for _, phase := range phases {
		phaseName := phase.GetName()
		if phaseName == "" {
			phaseName = "unnamed"
		}

		if !phase.IsComplete() {
			incompletePhases = append(incompletePhases, phaseName)
		} else {
			completedCount++
		}
	}

	var parts []string
	if len(incompletePhases) > 0 {
		parts = append(parts, fmt.Sprintf("incomplete phases: %s", strings.Join(incompletePhases, ", ")))
	}
	if completedCount > 0 {
		parts = append(parts, fmt.Sprintf("%d phase(s) completed", completedCount))
	}

	return strings.Join(parts, "; ")
}
