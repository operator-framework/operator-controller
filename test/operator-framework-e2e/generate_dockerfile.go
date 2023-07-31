package operatore2e

import (
	"os"
	"path/filepath"
	"text/template"
)

// generates Dockerfile and its contents for a given set of yaml files
func generateDockerFile(dockerFilePath, yamlFolderName, dockerFileName string) error {
	t, err := template.New("dockerfile").Parse(dockerfileTmpl)
	if err != nil {
		panic(err)
	}

	dockerFilePath = filepath.Join(dockerFilePath, dockerFileName)
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

const dockerfileTmpl = "ADD {{.YamlDir}} /configs/{{.YamlDir}}\n"
