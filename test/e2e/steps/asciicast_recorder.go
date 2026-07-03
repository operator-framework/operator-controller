package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type recorderContextKey struct{}

func WithRecorder(ctx context.Context, rec *AsciiCastRecorder) context.Context {
	return context.WithValue(ctx, recorderContextKey{}, rec)
}

func RecorderFromContext(ctx context.Context) *AsciiCastRecorder {
	rec, _ := ctx.Value(recorderContextKey{}).(*AsciiCastRecorder)
	return rec
}

type asciicastHeader struct {
	Version int               `json:"version"`
	Width   int               `json:"width"`
	Height  int               `json:"height"`
	Title   string            `json:"title,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

type recordedEntry struct {
	command   string
	stdin     string
	stdout    string
	stderr    string
	timestamp time.Time
	duration  time.Duration
}

// AsciiCastRecorder generates asciicast v2 recordings from demo scenario execution.
// Format spec: https://docs.asciinema.org/manual/asciicast/v2/
type AsciiCastRecorder struct {
	entries    []recordedEntry
	stepBuffer []recordedEntry
	stepIndex  map[string]int
	stepText   string
	startTime  time.Time
	castPath   string
}

func NewAsciiCastRecorder(scenarioName, outputDir string) *AsciiCastRecorder {
	slug := strings.ToLower(strings.ReplaceAll(scenarioName, " ", "-"))
	return &AsciiCastRecorder{
		castPath:  filepath.Join(outputDir, slug+".cast"),
		startTime: time.Now(),
	}
}

func (r *AsciiCastRecorder) BeginStep(stepText string) {
	r.stepBuffer = nil
	r.stepIndex = make(map[string]int)
	r.stepText = stepText
}

func (r *AsciiCastRecorder) CommitStep() {
	if len(r.stepBuffer) == 0 {
		return
	}
	comment := recordedEntry{
		command:   "",
		stdout:    "\033[34m# " + r.stepText + "\033[0m", // ANSI blue for step comments
		timestamp: r.stepBuffer[0].timestamp,
	}
	r.entries = append(r.entries, comment)
	r.entries = append(r.entries, r.stepBuffer...)
	r.stepBuffer = nil
	r.stepIndex = nil
}

func (r *AsciiCastRecorder) DiscardStep() {
	r.stepBuffer = nil
	r.stepIndex = nil
}

func (r *AsciiCastRecorder) RecordCommand(command, stdout, stderr string, duration time.Duration) {
	if r.stepIndex == nil {
		return
	}
	if idx, ok := r.stepIndex[command]; ok {
		r.stepBuffer[idx].stdout = stdout
		r.stepBuffer[idx].stderr = stderr
		r.stepBuffer[idx].duration = duration
		return
	}
	r.stepIndex[command] = len(r.stepBuffer)
	r.stepBuffer = append(r.stepBuffer, recordedEntry{
		command:   command,
		stdout:    stdout,
		stderr:    stderr,
		timestamp: time.Now(),
		duration:  duration,
	})
}

func (r *AsciiCastRecorder) RecordCommandWithInput(command, stdin, stdout, stderr string, duration time.Duration) {
	if r.stepIndex == nil {
		return
	}
	key := command + "\x00" + stdin
	if idx, ok := r.stepIndex[key]; ok {
		r.stepBuffer[idx].stdout = stdout
		r.stepBuffer[idx].stderr = stderr
		r.stepBuffer[idx].duration = duration
		return
	}
	r.stepIndex[key] = len(r.stepBuffer)
	r.stepBuffer = append(r.stepBuffer, recordedEntry{
		command:   command,
		stdin:     stdin,
		stdout:    stdout,
		stderr:    stderr,
		timestamp: time.Now(),
		duration:  duration,
	})
}

func (r *AsciiCastRecorder) RecordCustom(displayCommand, stdout, stderr string) {
	r.stepBuffer = r.stepBuffer[:0]
	r.stepIndex = make(map[string]int)
	r.stepBuffer = append(r.stepBuffer, recordedEntry{
		command:   displayCommand,
		stdout:    stdout,
		stderr:    stderr,
		timestamp: time.Now(),
	})
}

func (r *AsciiCastRecorder) Stop() error {
	if err := os.MkdirAll(filepath.Dir(r.castPath), 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	f, err := os.Create(r.castPath)
	if err != nil {
		return fmt.Errorf("failed to create cast file %s: %w", r.castPath, err)
	}
	defer f.Close()

	header := asciicastHeader{
		Version: 2,
		Width:   120,
		Height:  40,
		Env:     map[string]string{"TERM": "xterm-256color", "SHELL": "/bin/bash"},
	}
	headerBytes, err := json.Marshal(header)
	if err != nil {
		return fmt.Errorf("failed to marshal header: %w", err)
	}
	if _, err := fmt.Fprintf(f, "%s\n", headerBytes); err != nil {
		return err
	}

	for _, entry := range r.entries {
		elapsed := entry.timestamp.Sub(r.startTime).Seconds()

		if entry.command == "" {
			if entry.stdout != "" {
				if err := writeEvent(f, elapsed, "\r\n"+toTerminalLines(entry.stdout)); err != nil {
					return err
				}
			}
			continue
		}

		cmdText := "\033[1;32m$ " + entry.command + "\033[0m\r\n" // ANSI bold green for prompt and command
		if err := writeEvent(f, elapsed, cmdText); err != nil {
			return err
		}

		if entry.stdin != "" {
			heredoc := "<<EOF\r\n" + toTerminalLines(entry.stdin) + "EOF\r\n"
			if err := writeEvent(f, elapsed+0.1, heredoc); err != nil {
				return err
			}
		}

		outputTime := elapsed + entry.duration.Seconds()

		if entry.stdout != "" {
			if err := writeEvent(f, outputTime, toTerminalLines(entry.stdout)); err != nil {
				return err
			}
		}

		if entry.stderr != "" {
			stderrText := "\033[31m" + toTerminalLines(entry.stderr) + "\033[0m" // ANSI red for stderr
			if err := writeEvent(f, outputTime+0.01, stderrText); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeEvent(f *os.File, elapsed float64, data string) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(f, "[%.6f, \"o\", %s]\n", elapsed, jsonData)
	return err
}

func toTerminalLines(s string) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	return strings.ReplaceAll(s, "\n", "\r\n") + "\r\n"
}
