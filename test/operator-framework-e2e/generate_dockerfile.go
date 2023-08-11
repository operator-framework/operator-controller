package operatore2e

import (
	"os"
	"text/template"
)

// GenerateDockerFile generates Dockerfile and its contents for the data in yamlFolderName
func generateDockerFile(dockerFilePath, yamlFolderName, dockerfileTmpl string) error {
	t, err := template.New("dockerfile").Parse(dockerfileTmpl)
	if err != nil {
		panic(err)
	}

	file, err := os.Create(dockerFilePath)
	if err != nil {
		return err
	}
	defer file.Close()

	if _, err = file.WriteString("FROM scratch\n"); err != nil {
		return err
	}

	err = t.Execute(file, struct{ YamlDir string }{yamlFolderName})
	return err
}

// GenerateCatalogDockerFile generates Dockerfile for the catalog content in catalogFolderName
func generateCatalogDockerFile(dockerFilePath, catalogFolderName string) error {
	return generateDockerFile(dockerFilePath, catalogFolderName, catalogDockerfileTmpl)
}

// GenerateBundleDockerFile generates Dockerfile for the bundle content in bundleFolderName
func generateBundleDockerFile(dockerFilePath, bundleFolderName string) error {
	return generateDockerFile(dockerFilePath, bundleFolderName, bundleDockerfileTmpl)
}

// Dockerfile templates
const catalogDockerfileTmpl = "ADD {{.YamlDir}} /configs/{{.YamlDir}}\n"
const bundleDockerfileTmpl = "ADD manifests /manifests\n"
