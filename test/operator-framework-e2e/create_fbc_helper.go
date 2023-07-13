package operatore2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"
)

const (
	SchemaPackage         = "olm.package"
	SchemaChannel         = "olm.channel"
	SchemaBundle          = "olm.bundle"
	SchemaBundleMediatype = "olm.bundle.mediatype"
	BundleMediatype       = "plain+v0"
)

// Forms the FBC declartive config and creates the FBC by calling functions for forming the package, channel and bundles.
func CreateFBC(operatorName, defaultChannel string, bundleRef, bundleVersions []string) *declcfg.DeclarativeConfig {
	dPackage := formPackage(operatorName)
	dChannel := formChannel(operatorName, defaultChannel, bundleVersions)
	dBundle := formBundle(operatorName, bundleVersions, bundleRef)

	fbc := declcfg.DeclarativeConfig{
		Packages: []declcfg.Package{dPackage},
		Channels: []declcfg.Channel{dChannel},
		Bundles:  dBundle,
	}
	return &fbc
}

// Forms package schema for the FBC
func formPackage(pkgName string) declcfg.Package {
	packageFormed := declcfg.Package{
		Schema: SchemaPackage,
		Name:   pkgName,
	}
	return packageFormed
}

// Forms channel schema for the FBC
func formChannel(pkgName, channelName string, bundleVersions []string) declcfg.Channel {
	channelEntries := formChannelEntries(pkgName, bundleVersions)
	channelFormed := declcfg.Channel{
		Schema:  SchemaChannel,
		Name:    channelName,
		Package: pkgName,
		Entries: channelEntries,
	}
	return channelFormed
}

// Forms the uprade graph for the FBC
func formChannelEntries(pkgName string, bundleVersions []string) []declcfg.ChannelEntry {
	channelEntries := make([]declcfg.ChannelEntry, 0, len(bundleVersions))
	for i, version := range bundleVersions {
		replace := ""
		if i != 0 {
			replace = pkgName + ".v" + bundleVersions[i-1]
		}

		channelEntries = append(channelEntries, declcfg.ChannelEntry{
			Name:     pkgName + ".v" + version,
			Replaces: replace,
		})
	}
	return channelEntries
}

// Forms bundle schema for the FBC
func formBundle(pkgName string, versions, imageRefs []string) []declcfg.Bundle {
	bundleFormed := make([]declcfg.Bundle, 0, len(imageRefs))
	for i := 0; i < len(imageRefs); i++ {
		var properties []property.Property
		properties = append(properties, property.Property{
			Type:  SchemaPackage,
			Value: json.RawMessage(fmt.Sprintf(`{"packageName": "%s", "version": "%s"}`, pkgName, versions[i])),
		})
		properties = append(properties, property.Property{
			Type:  SchemaBundleMediatype,
			Value: json.RawMessage(fmt.Sprintf(`"%s"`, BundleMediatype)),
		})

		bundleFormed = append(bundleFormed, declcfg.Bundle{
			Schema:     SchemaBundle,
			Name:       pkgName + ".v" + versions[i],
			Package:    pkgName,
			Image:      imageRefs[i],
			Properties: properties,
		})
	}
	return bundleFormed
}

// Writes the formed FBC into catalog.yaml file
func WriteFBC(fbc declcfg.DeclarativeConfig, fbcFilePath, fbcFileName string) error {
	var buf bytes.Buffer
	err := declcfg.WriteYAML(fbc, &buf)
	if err != nil {
		return err
	}

	_, err = os.Stat(fbcFilePath)
	if os.IsNotExist(err) {
		err := os.MkdirAll(fbcFilePath, 0755)
		if err != nil {
			return err
		}
	}
	file, err := os.Create(fbcFilePath + "/" + fbcFileName)
	if err != nil {
		return err
	}
	defer file.Close()

	err = os.WriteFile(fbcFilePath+"/"+fbcFileName, buf.Bytes(), 0600)
	if err != nil {
		return err
	}

	return nil
}

// Generates the semver using the bundle images passed
func generateOLMSemverFile(semverFileName string, bundleImages []string) error {
	images := make([]string, 0, len(bundleImages))
	for _, bundleImage := range bundleImages {
		images = append(images, fmt.Sprintf("  - image: %s", bundleImage))
	}

	fileContent := fmt.Sprintf(`schema: olm.semver
generatemajorchannels: false
generateminorchannels: false
stable:
  bundles:
%s
`, strings.Join(images, "\n"))

	file, err := os.Create(semverFileName)
	if err != nil {
		return fmt.Errorf("error creating the semver yaml file %v : %v", semverFileName, err)
	}
	defer file.Close()

	_, err = file.WriteString(fileContent)
	if err != nil {
		return fmt.Errorf("error forming the semver yaml file %v : %v", semverFileName, err)
	}

	return nil
}
