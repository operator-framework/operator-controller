package version

// GitCommit indicates which commit the rukpak binaries were built from
var GitCommit string

func String() string {
	return GitCommit
}
