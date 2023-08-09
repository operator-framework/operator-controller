package operatore2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"
)

const (
	SchemaBundleMediatype = "olm.bundle.mediatype"
	BundleMediatype       = "plain+v0"
)

// Forms the FBC declartive config and creates the FBC by calling functions for forming the package, channel and bundles.
func CreateFBC(operatorName, channelName string, bundleRefsVersions map[string]string) *declcfg.DeclarativeConfig {
	dPackage := formPackage(operatorName)
	bundleVersions := make([]string, 0)
	for _, bundleVersion := range bundleRefsVersions {
		bundleVersions = append(bundleVersions, bundleVersion)
	}
	dChannel := formChannel(operatorName, channelName, bundleVersions)

	dBundle := formBundle(operatorName, bundleRefsVersions)

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
		Schema: declcfg.SchemaPackage,
		Name:   pkgName,
	}
	return packageFormed
}

// Forms channel schema for the FBC
func formChannel(pkgName, channelName string, bundleVersions []string) declcfg.Channel {
	channelEntries := formChannelEntries(pkgName, bundleVersions)
	channelFormed := declcfg.Channel{
		Schema:  declcfg.SchemaChannel,
		Name:    channelName,
		Package: pkgName,
		Entries: channelEntries,
	}
	return channelFormed
}

// Forms the uprade graph for the FBC. Only forms replaces edge. For forming replaces edge,
// bundleVersions are assumed to be in increasing version number order.
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
func formBundle(pkgName string, imgRefsVersions map[string]string) []declcfg.Bundle {
	bundleFormed := make([]declcfg.Bundle, 0, len(imgRefsVersions))
	for imgRef, version := range imgRefsVersions {
		var properties []property.Property
		properties = append(properties, property.Property{
			Type:  declcfg.SchemaPackage,
			Value: json.RawMessage(fmt.Sprintf(`{"packageName": "%s", "version": "%s"}`, pkgName, version)),
		})
		properties = append(properties, property.Property{
			Type:  SchemaBundleMediatype,
			Value: json.RawMessage(fmt.Sprintf(`"%s"`, BundleMediatype)),
		})

		bundleFormed = append(bundleFormed, declcfg.Bundle{
			Schema:     declcfg.SchemaBundle,
			Name:       pkgName + ".v" + version,
			Package:    pkgName,
			Image:      imgRef,
			Properties: properties,
		})
	}
	return bundleFormed
}

// Writes the formed FBC into catalog.yaml file in the path fbcFilePath
func WriteFBC(fbc declcfg.DeclarativeConfig, fbcFilePath, fbcFileName string) error {
	var buf bytes.Buffer
	if err := declcfg.WriteYAML(fbc, &buf); err != nil {
		return err
	}

	if _, err := os.Stat(fbcFilePath); os.IsNotExist(err) {
		if err := os.MkdirAll(fbcFilePath, 0755); err != nil {
			return err
		}
	}

	file, err := os.Create(filepath.Join(fbcFilePath, fbcFileName))
	if err != nil {
		return err
	}
	defer file.Close()

	err = os.WriteFile(filepath.Join(fbcFilePath, fbcFileName), buf.Bytes(), 0600)
	return err
}

// Forms the semver template using the bundle images passed
func formOLMSemverTemplateFile(semverFileName string, bundleImages []string) error {
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

	if _, err = file.WriteString(fileContent); err != nil {
		return fmt.Errorf("error forming the semver yaml file %v : %v", semverFileName, err)
	}

	return nil
}
