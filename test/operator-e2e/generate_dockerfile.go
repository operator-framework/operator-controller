package operatore2e

import (
	"os"
	"path/filepath"
	"text/template"
)

// generates Dockerfile and its contents for a given yaml file
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

	_, err = file.WriteString("FROM scratch\n")
	if err != nil {
		return err
	}
	err = t.Execute(file, struct{ YamlDir string }{yamlFolderName})
	if err != nil {
		return err
	}

	return nil
}

const dockerfileTmpl = `ADD {{.YamlDir}} /configs/{{.YamlDir}}
`
