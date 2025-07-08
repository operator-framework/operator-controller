package main

import (
	"archive/tar"
	"bytes"
	"flag"
	"fmt"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v2"
)

const (
	controllersSubPath string = "controllers"
	bundlesSubPath     string = "bundles"
	catalogsSubPath    string = "catalogs"
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
	controllersFullPath := fmt.Sprintf("%s/%s", imagesPath, controllersSubPath)

	bundles, err := buildBundles(bundlesFullPath)
	if err != nil {
		log.Fatalf("failed to build bundles: %s", err.Error())
	}
	catalogs, err := buildCatalogs(catalogsFullPath)
	if err != nil {
		log.Fatalf("failed to build catalogs: %s", err.Error())
	}
	controllers, err := buildControllers(controllersFullPath)
	if err != nil {
		log.Fatalf("failed to build controllers: %s", err.Error())
	}
	// Push the images
	for name, image := range bundles {
		dest := fmt.Sprintf("%s/%s", registryAddr, name)
		log.Printf("pushing bundle %s to %s", name, dest)
		if err := crane.Push(image, dest); err != nil {
			log.Fatalf("failed to push bundle images: %s", err.Error())
		}
	}
	for name, image := range catalogs {
		dest := fmt.Sprintf("%s/%s", registryAddr, name)
		log.Printf("pushing catalog %s to %s", name, dest)
		if err := crane.Push(image, fmt.Sprintf("%s/%s", registryAddr, name)); err != nil {
			log.Fatalf("failed to push catalog images: %s", err.Error())
		}
	}
	for name, image := range controllers {
		dest := fmt.Sprintf("%s/%s", registryAddr, name)
		log.Printf("pushing controller %s to %s", name, dest)
		if err := crane.Push(image, dest); err != nil {
			log.Fatalf("failed to push controller images: %s", err.Error())
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

func buildControllers(path string) (map[string]v1.Image, error) {
	controllers, err := processImageDirTree(path)
	if err != nil {
		return nil, err
	}
	mutatedMap := make(map[string]v1.Image, 0)
	// Apply required catalog label
	for key, img := range controllers {
		cfg := v1.Config{
			WorkingDir: "/",
			Entrypoint: []string{"/manager"},
			User:       "65532:65532",
		}
		mutatedMap[fmt.Sprintf("controllers/%s", key)], err = mutate.Config(img, cfg)
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
			b := &bytes.Buffer{}
			w := tar.NewWriter(b)

			files, err := collectFiles(tagFullPath)
			if err != nil {
				return nil, fmt.Errorf("failed to read files for image: %w", err)
			}
			sort.Strings(files)

			for _, f := range files {
				filePath := filepath.Join(tagFullPath, f)
				fileBytes, err := os.ReadFile(filePath)
				if err != nil {
					return nil, fmt.Errorf("failed to read file %q for image: %w", filePath, err)
				}
				if err := w.WriteHeader(&tar.Header{
					Name: f,
					Mode: 0755,
					Size: int64(len(fileBytes)),
				}); err != nil {
					return nil, err
				}
				if _, err := w.Write(fileBytes); err != nil {
					return nil, err
				}
			}
			if err := w.Close(); err != nil {
				return nil, err
			}

			// Return a new copy of the buffer each time it's opened.
			layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewBuffer(b.Bytes())), nil
			})
			if err != nil {
				return nil, fmt.Errorf("failed to create image layer: %w", err)
			}

			image, err := mutate.AppendLayers(empty.Image, layer)
			if err != nil {
				return nil, fmt.Errorf("failed to append layer to image: %w", err)
			}
			imageMap[fmt.Sprintf("%s:%s", entry.Name(), tag.Name())] = image
		}
	}
	return imageMap, nil
}

func collectFiles(originPath string) ([]string, error) {
	var files []string
	if err := fs.WalkDir(os.DirFS(originPath), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d != nil && !d.IsDir() {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return files, nil
}
