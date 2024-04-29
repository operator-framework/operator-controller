package packageerrors

import (
	"fmt"
)

func GenerateError(packageName string) error {
	return GenerateFullError(packageName, "", "", "")
}

func GenerateVersionError(packageName, versionRange string) error {
	return GenerateFullError(packageName, versionRange, "", "")
}

func GenerateChannelError(packageName, channelName string) error {
	return GenerateFullError(packageName, "", channelName, "")
}

func GenerateVersionChannelError(packageName, versionRange, channelName string) error {
	return GenerateFullError(packageName, versionRange, channelName, "")
}

func GenerateFullError(packageName, versionRange, channelName, installedVersion string) error {
	var versionError, channelError, existingVersionError string
	if versionRange != "" {
		versionError = fmt.Sprintf(" matching version %q", versionRange)
	}
	if channelName != "" {
		channelError = fmt.Sprintf(" in channel %q", channelName)
	}
	if installedVersion != "" {
		existingVersionError = fmt.Sprintf(" which upgrades currently installed version %q", installedVersion)
	}
	return fmt.Errorf("no package %q%s%s%s found", packageName, versionError, channelError, existingVersionError)
}
