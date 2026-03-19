package main

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/operator-framework/operator-controller/internal/operator-controller/migration"
)

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"

	clearLine = "\033[2K\r"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinner runs an animated spinner in a background goroutine.
// The message can be updated via the progress callback, and the
// spinner frame advances every 100ms independently of the poll interval.
type spinner struct {
	mu      sync.Mutex
	msg     string
	start   time.Time
	stop    chan struct{}
	stopped chan struct{}
}

var activeSpinner *spinner

func stepHeader(num int, title string) {
	fmt.Printf("\n%s%s━━━ Step %d: %s ━━━%s\n", colorBold, colorCyan, num, title, colorReset)
}

func success(msg string) {
	fmt.Printf("  %s✅ %s%s\n", colorGreen, msg, colorReset)
}

func warn(msg string) {
	fmt.Printf("  %s⚠️  %s%s\n", colorYellow, msg, colorReset)
}

func fail(msg string) {
	fmt.Printf("  %s❌ %s%s\n", colorRed, msg, colorReset)
}

func info(msg string) {
	fmt.Printf("  %s%s%s\n", colorDim, msg, colorReset)
}

func detail(label, value string) {
	fmt.Printf("  %s%-16s%s %s\n", colorBlue, label, colorReset, value)
}

func resource(kind, location, name string) {
	fmt.Printf("    %s📦 %s%s %s%s/%s%s\n", colorDim, colorReset, kind, colorDim, location, name, colorReset)
}

func banner(msg string) {
	fmt.Printf("\n%s%s🎉 %s%s\n\n", colorBold, colorGreen, msg, colorReset)
}

func sectionHeader(title string) {
	fmt.Printf("\n%s%s── %s ──%s\n", colorBold, colorBlue, title, colorReset)
}

func printCheckResults(checks []migration.CheckResult) {
	for _, c := range checks {
		if c.Passed {
			fmt.Printf("  %s✅ %-28s%s %s%s%s\n", colorGreen, c.Name, colorReset, colorDim, c.Message, colorReset)
		} else {
			fmt.Printf("  %s❌ %-28s%s %s\n", colorRed, c.Name, colorReset, c.Message)
		}
	}
}

// startProgress starts the background spinner animation.
func startProgress() {
	s := &spinner{
		msg:     "Working...",
		start:   time.Now(),
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	activeSpinner = s

	go func() {
		defer close(s.stopped)
		idx := 0
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-s.stop:
				return
			case <-ticker.C:
				s.mu.Lock()
				msg := s.msg
				elapsed := time.Since(s.start).Truncate(time.Second)
				s.mu.Unlock()

				frame := spinnerFrames[idx%len(spinnerFrames)]
				idx++
				fmt.Fprintf(os.Stderr, "%s  %s%s %s%s (%s)%s",
					clearLine, colorYellow, frame, msg, colorDim, elapsed, colorReset)
			}
		}
	}()
}

// clearProgress stops the spinner and clears its line.
func clearProgress() {
	if activeSpinner == nil {
		return
	}
	close(activeSpinner.stop)
	<-activeSpinner.stopped
	fmt.Fprint(os.Stderr, clearLine)
	activeSpinner = nil
}

// progressFunc is the migration.ProgressFunc callback that updates the spinner message.
// The spinner goroutine handles the animation; this just updates the text.
func progressFunc(msg string) {
	if activeSpinner == nil {
		return
	}
	activeSpinner.mu.Lock()
	activeSpinner.msg = msg
	activeSpinner.mu.Unlock()
}
