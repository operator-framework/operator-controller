package operatore2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	"github.com/operator-framework/operator-controller/test/operator-e2e/config"
)

const (
	SchemaPackage         = "olm.package"
	SchemaChannel         = "olm.channel"
	SchemaBundle          = "olm.bundle"
	SchemaBundleMediatype = "olm.bundle.mediatype"
	BundleMediatype       = "plain+v0"
)

func CreateFBC(configFilePath string) (*declcfg.DeclarativeConfig, error) {
	config, err := config.ReadFile(configFilePath)
	if err != nil {
		return nil, err
	}

	dPackage := formPackage(*config)
	dChannel := formChannel(*config)
	dBundle := formBundle(*config)

	fbc := declcfg.DeclarativeConfig{
		Packages: []declcfg.Package{dPackage},
		Channels: dChannel,
		Bundles:  dBundle,
	}
	return &fbc, nil
}

func formPackage(config config.Config) declcfg.Package {
	packageFormed := declcfg.Package{
		Schema:         SchemaPackage,
		Name:           config.PackageName,
		DefaultChannel: config.ChannelData[0].ChannelName,
	}
	return packageFormed
}

func formChannel(config config.Config) []declcfg.Channel {
	channelFormed := make([]declcfg.Channel, 0, len(config.ChannelData))
	for _, channel := range config.ChannelData {
		channelEntries := formChannelEntries(config.PackageName, channel)
		channelFormed = append(channelFormed, declcfg.Channel{
			Schema:  SchemaChannel,
			Name:    channel.ChannelName,
			Package: config.PackageName,
			Entries: channelEntries,
		})
	}
	return channelFormed
}

func formChannelEntries(pkgName string, channel config.ChannelData) []declcfg.ChannelEntry {
	channelEntries := make([]declcfg.ChannelEntry, 0, len(channel.ChannelEntries))
	for _, channelEntry := range channel.ChannelEntries {
		replace := ""
		if channelEntry.Replaces != "" {
			replace = pkgName + "." + channelEntry.Replaces
		}

		skip := []string{}
		if len(channelEntry.Skips) != 0 {
			for _, s := range channelEntry.Skips {
				if s != "" {
					skip = append(skip, s)
				}
			}
		}
		channelEntries = append(channelEntries, declcfg.ChannelEntry{
			Name:      pkgName + "." + channelEntry.EntryVersion,
			Replaces:  replace,
			Skips:     skip,
			SkipRange: channelEntry.SkipRange,
		})
	}
	return channelEntries
}

func formBundle(config config.Config) []declcfg.Bundle {
	bundleFormed := make([]declcfg.Bundle, 0, len(config.BundleData))
	for _, bundle := range config.BundleData {
		var properties []property.Property
		properties = append(properties, property.Property{
			Type:  SchemaPackage,
			Value: json.RawMessage(fmt.Sprintf(`{"packageName": "%s", "version": "%s"}`, config.PackageName, bundle.BundleVersion)),
		})
		properties = append(properties, property.Property{
			Type:  SchemaBundleMediatype,
			Value: json.RawMessage(fmt.Sprintf(`"%s"`, BundleMediatype)),
		})

		bundleFormed = append(bundleFormed, declcfg.Bundle{
			Schema:     SchemaBundle,
			Name:       config.PackageName + "." + bundle.BundleVersion,
			Package:    config.PackageName,
			Image:      bundle.BundleImage,
			Properties: properties,
		})
	}
	return bundleFormed
}

func WriteFBC(fbc declcfg.DeclarativeConfig, fbcFilePath string, fbcFileName string) error {
	var buf bytes.Buffer
	err := declcfg.WriteYAML(fbc, &buf)
	if err != nil {
		return err
	}

	_, err = os.Stat(fbcFilePath)
	if os.IsNotExist(err) {
		err := os.MkdirAll(fbcFilePath, 0755)
		if err != nil {
			fmt.Printf("Failed to create directory: %v\n", err)
			return err
		}
	}
	file, err := os.Create(fbcFilePath + "/" + fbcFileName)
	if err != nil {
		fmt.Printf("Failed to create file: %v\n", err)
		return err
	}
	defer file.Close()

	err = os.WriteFile(fbcFilePath+"/"+fbcFileName, buf.Bytes(), 0600)
	if err != nil {
		return err
	}

	return nil
}
