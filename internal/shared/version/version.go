package version

import (
	"fmt"
	"runtime/debug"
)

var (
	gitCommit  = "unknown"
	commitDate = "unknown"
	repoState  = "unknown"
	version    = "unknown"

	stateMap = map[string]string{
		"true":  "dirty",
		"false": "clean",
	}
)

func String() string {
	return fmt.Sprintf("version: %q, commit: %q, date: %q, state: %q", version, gitCommit, commitDate, repoState)
}

func init() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			gitCommit = setting.Value
		case "vcs.time":
			commitDate = setting.Value
		case "vcs.modified":
			if v, ok := stateMap[setting.Value]; ok {
				repoState = v
			}
		}
	}
	if version == "unknown" {
		version = info.Main.Version
	}
}
