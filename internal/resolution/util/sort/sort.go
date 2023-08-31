package sort

import (
	"strings"

	"github.com/operator-framework/operator-controller/internal/resolution/variables"
)

// ByChannelAndVersion is an entity sort function that orders the entities in
// package, channel (default channel at the head), and inverse version (higher versions on top)
// if a property does not exist for one of the entities, the one missing the property is pushed down
// if both entities are missing the same property they are ordered by id
func ByChannelAndVersion(variable1 *variables.BundleVariable, variable2 *variables.BundleVariable) bool {
	// first sort package lexical order
	pkgOrder := packageOrder(variable1, variable2)
	if pkgOrder != 0 {
		return pkgOrder < 0
	}

	// todo(perdasilva): handle default channel in ordering once it is being exposed by the entity
	channelOrder := channelOrder(variable1, variable2)
	if channelOrder != 0 {
		return channelOrder < 0
	}

	// order version from highest to lowest (favor the latest release)
	versionOrder := versionOrder(variable1, variable2)
	return versionOrder > 0
}

func compareErrors(err1 error, err2 error) int {
	if err1 != nil && err2 == nil {
		return 1
	}

	if err1 == nil && err2 != nil {
		return -1
	}
	return 0
}

func packageOrder(var1, var2 *variables.BundleVariable) int {
	name1, err1 := var1.PackageName()
	name2, err2 := var2.PackageName()
	errComp := compareErrors(err1, err2)
	if errComp != 0 {
		return errComp
	}
	return strings.Compare(name1, name2)
}

func channelOrder(var1, var2 *variables.BundleVariable) int {
	channelProperties1, err1 := var1.Channel()
	channelProperties2, err2 := var2.Channel()
	errComp := compareErrors(err1, err2)
	if errComp != 0 {
		return errComp
	}
	if channelProperties1.Priority != channelProperties2.Priority {
		return channelProperties1.Priority - channelProperties2.Priority
	}
	return strings.Compare(channelProperties1.ChannelName, channelProperties2.ChannelName)
}

func versionOrder(var1, var2 *variables.BundleVariable) int {
	ver1, err1 := var1.Version()
	ver2, err2 := var2.Version()
	errComp := compareErrors(err1, err2)
	if errComp != 0 {
		// the sign gets inverted because version is sorted
		// from highest to lowest
		return -1 * errComp
	}
	return ver1.Compare(*ver2)
}
