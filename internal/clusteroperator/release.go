package clusteroperator

import "os"

const (
	defaultVersionValue    = "0.0.1-snapshot"
	releaseVersionVariable = "RELEASE_VERSION" // OpenShift's env variable for defining the current release
)

func GetReleaseVariable() string {
	v := os.Getenv(releaseVersionVariable)
	if v == "" {
		return defaultVersionValue
	}
	return v
}
