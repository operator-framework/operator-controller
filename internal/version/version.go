package version

import (
	"fmt"
	"runtime"

	genericversion "k8s.io/apimachinery/pkg/version"
)

var (
	gitVersion   = "unknown"
	gitCommit    = "unknown" // sha1 from git, output of $(git rev-parse HEAD)
	gitTreeState = "unknown" // state of git tree, either "clean" or "dirty"
	commitDate   = "unknown" // build date in ISO8601 format, output of $(date -u +'%Y-%m-%dT%H:%M:%SZ')
)

// ControllerVersion returns a version string for the controller
func ControllerVersion() string {
	return gitVersion
}

// ApiserverVersion returns a version.Info object for the apiserver
func ApiserverVersion() *genericversion.Info {
	return &genericversion.Info{
		GitVersion:   gitVersion,
		GitCommit:    gitCommit,
		GitTreeState: gitTreeState,
		BuildDate:    commitDate,
		GoVersion:    runtime.Version(),
		Compiler:     runtime.Compiler,
		Platform:     fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
}
