package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v2"
)

const (
	bundlesSubPath  string = "bundles"
	catalogsSubPath string = "catalogs"
)

func main() {
	var (
		registryAddr string
		imagesPath   string
	)
	flag.StringVar(&registryAddr, "registry-address", ":12345", "The address the registry binds to.")
	flag.StringVar(&imagesPath, "images-path", "/images", "Image directory path")
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	log.Printf("push operation configured with images path %s and destination %s", imagesPath, registryAddr)

	bundlesFullPath := fmt.Sprintf("%s/%s", imagesPath, bundlesSubPath)
	catalogsFullPath := fmt.Sprintf("%s/%s", imagesPath, catalogsSubPath)

	bundles, err := buildBundles(bundlesFullPath)
	if err != nil {
		log.Fatalf("failed to build bundles: %s", err.Error())
	}
	catalogs, err := buildCatalogs(catalogsFullPath)
	if err != nil {
		log.Fatalf("failed to build catalogs: %s", err.Error())
	}
	// Push the images
	for name, image := range bundles {
		ref := fmt.Sprintf("%s/%s", registryAddr, name)
		log.Printf("pushing bundle %q", ref)
		if err := crane.Push(image, ref, crane.Insecure); err != nil {
			log.Fatalf("failed to push bundle images: %s", err.Error())
		}
	}
	for name, image := range catalogs {
		ref := fmt.Sprintf("%s/%s", registryAddr, name)
		log.Printf("pushing catalog %q", ref)
		if err := crane.Push(image, fmt.Sprintf("%s/%s", registryAddr, name), crane.Insecure); err != nil {
			log.Fatalf("failed to push catalog images: %s", err.Error())
		}
	}
	log.Printf("finished")
	os.Exit(0)
}

func buildBundles(path string) (map[string]v1.Image, error) {
	bundles, err := processImageDirTree(path)
	if err != nil {
		return nil, err
	}
	mutatedMap := make(map[string]v1.Image, 0)
	// Apply required bundle labels
	for key, img := range bundles {
		// Replace ':' between image name and image tag for file path
		metadataPath := strings.Replace(key, ":", "/", 1)
		labels, err := getBundleLabels(fmt.Sprintf("%s/%s/%s", path, metadataPath, "metadata/annotations.yaml"))
		if err != nil {
			return nil, err
		}
		mutatedMap[fmt.Sprintf("bundles/registry-v1/%s", key)], err = mutate.Config(img, v1.Config{Labels: labels})
		if err != nil {
			return nil, fmt.Errorf("failed to apply image labels: %w", err)
		}
	}
	return mutatedMap, nil
}

type bundleAnnotations struct {
	annotations map[string]string
}

func getBundleLabels(path string) (map[string]string, error) {
	var metadata bundleAnnotations
	yamlFile, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	err = yaml.Unmarshal(yamlFile, metadata)
	if err != nil {
		return nil, err
	}
	return metadata.annotations, nil
}

func buildCatalogs(path string) (map[string]v1.Image, error) {
	catalogs, err := processImageDirTree(path)
	if err != nil {
		return nil, err
	}
	mutatedMap := make(map[string]v1.Image, 0)
	// Apply required catalog label
	for key, img := range catalogs {
		cfg := v1.Config{
			Labels: map[string]string{
				"operators.operatorframework.io.index.configs.v1": "/configs",
			},
		}
		mutatedMap[fmt.Sprintf("e2e/%s", key)], err = mutate.Config(img, cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to apply image labels: %w", err)
		}
	}
	return mutatedMap, nil
}

func processImageDirTree(path string) (map[string]v1.Image, error) {
	imageMap := make(map[string]v1.Image, 0)
	images, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	// Each directory in 'path' represents an image
	for _, entry := range images {
		entryFullPath := fmt.Sprintf("%s/%s", path, entry.Name())
		if !entry.IsDir() {
			continue
		}
		tags, err := os.ReadDir(entryFullPath)
		if err != nil {
			return nil, err
		}
		// Each directory in the image directory represents a separate tag
		for _, tag := range tags {
			if !tag.IsDir() {
				continue
			}
			tagFullPath := fmt.Sprintf("%s/%s", entryFullPath, tag.Name())

			var fileMap map[string][]byte
			fileMap, err = createFileMap(tagFullPath)
			if err != nil {
				return nil, fmt.Errorf("failed to read files for image: %w", err)
			}

			image, err := crane.Image(fileMap)
			if err != nil {
				return nil, fmt.Errorf("failed to generate image: %w", err)
			}
			imageMap[fmt.Sprintf("%s:%s", entry.Name(), tag.Name())] = image
		}
	}
	return imageMap, nil
}

func createFileMap(originPath string) (map[string][]byte, error) {
	fileMap := make(map[string][]byte)
	if err := fs.WalkDir(os.DirFS(originPath), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d != nil && !d.IsDir() {
			fileMap[path], err = os.ReadFile(fmt.Sprintf("%s/%s", originPath, path))
			if err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return fileMap, nil
}
