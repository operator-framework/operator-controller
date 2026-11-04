package steps

import (
	"context"
	"os"
	"strings"

	"github.com/cucumber/godog"
)

const demoTag = "@demo"

var demoOutputDir = "docs/demos"

func init() {
	if dir := os.Getenv("DEMO_OUTPUT_DIR"); dir != "" {
		demoOutputDir = dir
	}
}

func RegisterAsciiCastHooks(sc *godog.ScenarioContext) {
	sc.Before(startRecordingIfDemo)
	sc.StepContext().Before(beforeStep)
	sc.StepContext().After(afterStep)
	sc.After(stopRecordingIfDemo)
}

func hasTag(sc *godog.Scenario, tag string) bool {
	for _, t := range sc.Tags {
		if t.Name == tag {
			return true
		}
	}
	return false
}

func startRecordingIfDemo(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
	if !hasTag(sc, demoTag) {
		return ctx, nil
	}
	rec := NewAsciiCastRecorder(sc.Name, demoOutputDir)
	logger.Info("Starting asciicast recording", "scenario", sc.Name, "output", rec.castPath)
	return WithRecorder(ctx, rec), nil
}

func beforeStep(ctx context.Context, st *godog.Step) (context.Context, error) {
	if rec := RecorderFromContext(ctx); rec != nil {
		rec.BeginStep(st.Text)
	}
	return ctx, nil
}

func afterStep(ctx context.Context, st *godog.Step, status godog.StepResultStatus, err error) (context.Context, error) {
	rec := RecorderFromContext(ctx)
	if rec == nil {
		return ctx, nil
	}
	if status == godog.StepPassed {
		rec.CommitStep()
	} else {
		rec.DiscardStep()
	}
	return ctx, nil
}

func stopRecordingIfDemo(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
	rec := RecorderFromContext(ctx)
	if rec == nil {
		return ctx, nil
	}
	if stopErr := rec.Stop(); stopErr != nil {
		logger.Info("Failed to write asciicast file", "error", stopErr)
		if err == nil {
			return ctx, stopErr
		}
	} else {
		// Slugify scenario name the same way as the recorder
		slug := strings.ToLower(strings.ReplaceAll(sc.Name, " ", "-"))
		logger.Info("Asciicast recording saved", "file", demoOutputDir+"/"+slug+".cast")
	}
	return ctx, nil
}
