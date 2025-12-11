package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"text/template"

	"gopkg.in/yaml.v3"
)

// Reads templated documents and does templating based on the inValues, dumps to stdout
func executeTemplate(inValues io.Reader, templates ...string) error {
	tpl, err := template.ParseFiles(templates...)
	if err != nil {
		return fmt.Errorf("error parsing template(s): %v", err)
	}

	buf := bytes.NewBuffer(nil)
	_, err = io.Copy(buf, inValues)
	if err != nil {
		return fmt.Errorf("failed to read values: %v", err)
	}

	var values map[string]interface{}
	err = yaml.Unmarshal(buf.Bytes(), &values)
	if err != nil {
		return fmt.Errorf("failed to parse values: %v", err)
	}

	// Add the .Values to the values that are read, to make it more helm-like
	topvalues := map[string]interface{}{
		"Values": values,
	}
	err = tpl.Execute(os.Stdout, topvalues)
	if err != nil {
		return fmt.Errorf("failed to execute template: %v", err)
	}
	return nil
}

func main() {
	valuesFile := flag.String("values", "", "Path to values YAML file (required)")
	flag.Parse()

	if *valuesFile == "" {
		log.Println("Error: --values flag is required")
		flag.Usage()
		os.Exit(1)
	}

	valuesReader, err := os.Open(*valuesFile)
	if err != nil {
		log.Printf("Failed to open values file: %v\n", err)
		os.Exit(1)
	}
	defer valuesReader.Close()

	err = executeTemplate(valuesReader, flag.Args()...)
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}
}
