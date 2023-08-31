package sort

import (
	"strings"

	"github.com/operator-framework/deppy/pkg/deppy/input"

	"github.com/operator-framework/operator-controller/internal/resolution/entities"
)

// ByChannelAndVersion is an entity sort function that orders the entities in
// package, channel (default channel at the head), and inverse version (higher versions on top)
// if a property does not exist for one of the entities, the one missing the property is pushed down
// if both entities are missing the same property they are ordered by id
func ByChannelAndVersion(entity1 *input.Entity, entity2 *input.Entity) bool {
	e1 := entities.NewBundleEntity(entity1)
	e2 := entities.NewBundleEntity(entity2)

	// first sort package lexical order
	pkgOrder := packageOrder(e1, e2)
	if pkgOrder != 0 {
		return pkgOrder < 0
	}

	// todo(perdasilva): handle default channel in ordering once it is being exposed by the entity
	channelOrder := channelOrder(e1, e2)
	if channelOrder != 0 {
		return channelOrder < 0
	}

	// order version from highest to lowest (favor the latest release)
	versionOrder := versionOrder(e1, e2)
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

func packageOrder(e1, e2 *entities.BundleEntity) int {
	name1, err1 := e1.PackageName()
	name2, err2 := e2.PackageName()
	errComp := compareErrors(err1, err2)
	if errComp != 0 {
		return errComp
	}
	return strings.Compare(name1, name2)
}

func channelOrder(e1, e2 *entities.BundleEntity) int {
	channelProperties1, err1 := e1.Channel()
	channelProperties2, err2 := e2.Channel()
	errComp := compareErrors(err1, err2)
	if errComp != 0 {
		return errComp
	}
	if channelProperties1.Priority != channelProperties2.Priority {
		return channelProperties1.Priority - channelProperties2.Priority
	}
	return strings.Compare(channelProperties1.ChannelName, channelProperties2.ChannelName)
}

func versionOrder(e1, e2 *entities.BundleEntity) int {
	ver1, err1 := e1.Version()
	ver2, err2 := e2.Version()
	errComp := compareErrors(err1, err2)
	if errComp != 0 {
		// the sign gets inverted because version is sorted
		// from highest to lowest
		return -1 * errComp
	}
	return ver1.Compare(*ver2)
}
