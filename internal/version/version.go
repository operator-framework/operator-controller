package version

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/blang/semver/v4"
	genericversion "k8s.io/apimachinery/pkg/version"
)

var (
	gitVersion   = "unknown"
	gitCommit    = "unknown" // sha1 from git, output of $(git rev-parse HEAD)
	gitTreeState = "unknown" // state of git tree, either "clean" or "dirty"
	commitDate   = "unknown" // build date in ISO8601 format, output of $(date -u +'%Y-%m-%dT%H:%M:%SZ')
)

// Version returns a version struct for the build
func Version() genericversion.Info {
	info := genericversion.Info{
		GitVersion:   gitVersion,
		GitCommit:    gitCommit,
		GitTreeState: gitTreeState,
		BuildDate:    commitDate,
		GoVersion:    runtime.Version(),
		Compiler:     runtime.Compiler,
		Platform:     fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
	v, err := semver.Parse(strings.TrimPrefix(gitVersion, "v"))
	if err == nil {
		info.Major = fmt.Sprintf("%d", v.Major)
		info.Minor = fmt.Sprintf("%d", v.Minor)
	}
	return info
}
