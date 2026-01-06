package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

const controllerToolsVersion = "v0.20.0"

func TestRunGenerator(t *testing.T) {
	here, err := os.Getwd()
	require.NoError(t, err)
	// Get to repo root
	err = os.Chdir("../../..")
	require.NoError(t, err)
	defer func() {
		_ = os.Chdir(here)
	}()
	dir, err := os.MkdirTemp("", "crd-generate-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "standard"), 0o700))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "experimental"), 0o700))
	runGenerator(dir, controllerToolsVersion)

	f1 := filepath.Join(dir, "standard/olm.operatorframework.io_clusterextensions.yaml")
	f2 := "helm/olmv1/base/operator-controller/crd/standard/olm.operatorframework.io_clusterextensions.yaml"
	fmt.Printf("comparing: %s to %s\n", f1, f2)
	compareFiles(t, f1, f2)

	f1 = filepath.Join(dir, "standard/olm.operatorframework.io_clustercatalogs.yaml")
	f2 = "helm/olmv1/base/catalogd/crd/standard/olm.operatorframework.io_clustercatalogs.yaml"
	fmt.Printf("comparing: %s to %s\n", f1, f2)
	compareFiles(t, f1, f2)

	f1 = filepath.Join(dir, "experimental/olm.operatorframework.io_clusterextensions.yaml")
	f2 = "helm/olmv1/base/operator-controller/crd/experimental/olm.operatorframework.io_clusterextensions.yaml"
	fmt.Printf("comparing: %s to %s\n", f1, f2)
	compareFiles(t, f1, f2)

	f1 = filepath.Join(dir, "experimental/olm.operatorframework.io_clustercatalogs.yaml")
	f2 = "helm/olmv1/base/catalogd/crd/experimental/olm.operatorframework.io_clustercatalogs.yaml"
	fmt.Printf("comparing: %s to %s\n", f1, f2)
	compareFiles(t, f1, f2)
}

func TestTags(t *testing.T) {
	here, err := os.Getwd()
	require.NoError(t, err)
	err = os.Chdir("testdata")
	defer func() {
		_ = os.Chdir(here)
	}()
	require.NoError(t, err)
	dir, err := os.MkdirTemp("", "crd-generate-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "standard"), 0o700))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "experimental"), 0o700))
	runGenerator(dir, controllerToolsVersion, "github.com/operator-framework/operator-controller/hack/tools/crd-generator/testdata/api/v1")

	f1 := filepath.Join(dir, "standard/olm.operatorframework.io_clusterextensions.yaml")
	f2 := "output/standard/olm.operatorframework.io_clusterextensions.yaml"
	fmt.Printf("comparing: %s to %s\n", f1, f2)
	compareFiles(t, f1, f2)

	f1 = filepath.Join(dir, "experimental/olm.operatorframework.io_clusterextensions.yaml")
	f2 = "output/experimental/olm.operatorframework.io_clusterextensions.yaml"
	fmt.Printf("comparing: %s to %s\n", f1, f2)
	compareFiles(t, f1, f2)
}

func TestFormatDescription(t *testing.T) {
	tests := []struct {
		name      string
		channel   string
		fieldName string
		input     string
		expected  string
	}{
		{
			name:      "standard channel removes experimental description",
			channel:   StandardChannel,
			fieldName: "testField",
			input:     "Base description.\n<opcon:experimental:description>\nExperimental content.\n</opcon:experimental:description>\nMore content.",
			expected:  "Base description.\n\nMore content.",
		},
		{
			name:      "experimental channel removes standard description",
			channel:   ExperimentalChannel,
			fieldName: "testField",
			input:     "Base description.\n<opcon:standard:description>\nStandard content.\n</opcon:standard:description>\nMore content.",
			expected:  "Base description.\n\nMore content.",
		},
		{
			name:      "excludeFromCRD tag removes content",
			channel:   StandardChannel,
			fieldName: "testField",
			input:     "Before.\n\n<opcon:util:excludeFromCRD>\nExcluded content.\n</opcon:util:excludeFromCRD>\n\nAfter.",
			expected:  "Before.\n\nAfter.",
		},
		{
			name:      "three hyphens removes trailing content",
			channel:   StandardChannel,
			fieldName: "testField",
			input:     "Visible content.\n---\nHidden content after separator.",
			expected:  "Visible content.",
		},
		{
			name:      "multiple newlines collapsed to double",
			channel:   StandardChannel,
			fieldName: "testField",
			input:     "Line one.\n\n\n\n\nLine two.",
			expected:  "Line one.\n\nLine two.",
		},
		{
			name:      "trailing newlines removed",
			channel:   StandardChannel,
			fieldName: "testField",
			input:     "Content with trailing newlines.\n\n\n",
			expected:  "Content with trailing newlines.",
		},
		{
			name:      "combined tags and formatting",
			channel:   ExperimentalChannel,
			fieldName: "testField",
			input:     "Main text.\n<opcon:standard:description>\nStandard only.\n</opcon:standard:description>\n\n\n<opcon:util:excludeFromCRD>\nInternal notes.\n</opcon:util:excludeFromCRD>\n\nFinal text.\n\n\n",
			expected:  "Main text.\n\nFinal text.",
		},
		{
			name:      "empty input",
			channel:   StandardChannel,
			fieldName: "testField",
			input:     "",
			expected:  "",
		},
		{
			name:      "no tags plain text",
			channel:   StandardChannel,
			fieldName: "testField",
			input:     "Simple description without any tags.",
			expected:  "Simple description without any tags.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDescription(tt.input, tt.channel, tt.fieldName)
			require.Equal(t, tt.expected, result)
		})
	}
}

// TestOpconTweaksOptionalRequired tests the opconTweaks function for handling
// optional and required tags in field descriptions.
func TestOpconTweaksOptionalRequired(t *testing.T) {
	tests := []struct {
		name           string
		channel        string
		fieldName      string
		description    string
		expectedStatus string
	}{
		{
			name:           "optional tag in standard channel",
			channel:        StandardChannel,
			fieldName:      "testField",
			description:    "Field description.\n<opcon:standard:validation:Optional>",
			expectedStatus: statusOptional,
		},
		{
			name:           "required tag in standard channel",
			channel:        StandardChannel,
			fieldName:      "testField",
			description:    "Field description.\n<opcon:standard:validation:Required>",
			expectedStatus: statusRequired,
		},
		{
			name:           "optional tag in experimental channel",
			channel:        ExperimentalChannel,
			fieldName:      "testField",
			description:    "Field description.\n<opcon:experimental:validation:Optional>",
			expectedStatus: statusOptional,
		},
		{
			name:           "required tag in experimental channel",
			channel:        ExperimentalChannel,
			fieldName:      "testField",
			description:    "Field description.\n<opcon:experimental:validation:Required>",
			expectedStatus: statusRequired,
		},
		{
			name:           "no validation tag",
			channel:        StandardChannel,
			fieldName:      "testField",
			description:    "Field description without tags.",
			expectedStatus: statusNoOpinion,
		},
		{
			name:           "experimental tag in standard channel ignored",
			channel:        StandardChannel,
			fieldName:      "testField",
			description:    "Field description.\n<opcon:experimental:validation:Optional>",
			expectedStatus: statusNoOpinion,
		},
		{
			name:           "standard tag in experimental channel ignored",
			channel:        ExperimentalChannel,
			fieldName:      "testField",
			description:    "Field description.\n<opcon:standard:validation:Required>",
			expectedStatus: statusNoOpinion,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonProps := apiextensionsv1.JSONSchemaProps{
				Description: tt.description,
				Type:        "string",
			}
			_, status := opconTweaks(tt.channel, tt.fieldName, jsonProps)
			require.Equal(t, tt.expectedStatus, status)
		})
	}
}

// TestOpconTweaksMapRequiredList tests the opconTweaksMap function for correctly
// updating the required list based on field descriptions.
func TestOpconTweaksMapRequiredList(t *testing.T) {
	tests := []struct {
		name             string
		channel          string
		props            map[string]apiextensionsv1.JSONSchemaProps
		existingRequired []string
		expectedRequired []string
	}{
		{
			name:    "add field to required list if not required but opcon required",
			channel: StandardChannel,
			props: map[string]apiextensionsv1.JSONSchemaProps{
				"field1": {
					Description: "Field 1.\n<opcon:standard:validation:Required>",
					Type:        "string",
				},
			},
			existingRequired: []string{},
			expectedRequired: []string{"field1"},
		},
		{
			name:    "remove field from required list if required but opcon optional",
			channel: StandardChannel,
			props: map[string]apiextensionsv1.JSONSchemaProps{
				"field1": {
					Description: "Field 1.\n<opcon:standard:validation:Optional>",
					Type:        "string",
				},
			},
			existingRequired: []string{"field1"},
			expectedRequired: []string{},
		},
		{
			name:    "preserve existing required without overriding opcon tag",
			channel: StandardChannel,
			props: map[string]apiextensionsv1.JSONSchemaProps{
				"field1": {
					Description: "Field 1 without tag.",
					Type:        "string",
				},
			},
			existingRequired: []string{"field1"},
			expectedRequired: []string{"field1"},
		},
		{
			name:    "multiple fields with mixed optional/required tags",
			channel: StandardChannel,
			props: map[string]apiextensionsv1.JSONSchemaProps{
				"field1": {
					Description: "Field 1.\n<opcon:standard:validation:Required>",
					Type:        "string",
				},
				"field2": {
					Description: "Field 2.\n<opcon:standard:validation:Optional>",
					Type:        "string",
				},
				"field3": {
					Description: "Field 3 without tag.",
					Type:        "string",
				},
			},
			existingRequired: []string{"field2", "field3"},
			expectedRequired: []string{"field3", "field1"},
		},
		{
			name:    "no duplicate in required list when tag/opcon-tag both required",
			channel: StandardChannel,
			props: map[string]apiextensionsv1.JSONSchemaProps{
				"field1": {
					Description: "Field 1.\n<opcon:standard:validation:Required>",
					Type:        "string",
				},
			},
			existingRequired: []string{"field1"},
			expectedRequired: []string{"field1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parentSchema := &apiextensionsv1.JSONSchemaProps{
				Properties: tt.props,
				Required:   tt.existingRequired,
			}
			opconTweaksMap(tt.channel, parentSchema)
			require.ElementsMatch(t, tt.expectedRequired, parentSchema.Required)
		})
	}
}

func compareFiles(t *testing.T, file1, file2 string) {
	f1, err := os.Open(file1)
	require.NoError(t, err)
	defer func() {
		_ = f1.Close()
	}()

	f2, err := os.Open(file2)
	require.NoError(t, err)
	defer func() {
		_ = f2.Close()
	}()

	for {
		b1 := make([]byte, 64000)
		b2 := make([]byte, 64000)
		n1, err1 := f1.Read(b1)
		n2, err2 := f2.Read(b2)

		// Success if both have EOF at the same time
		if err1 == io.EOF && err2 == io.EOF {
			return
		}
		require.NoError(t, err1)
		require.NoError(t, err2)
		require.Equal(t, n1, n2)
		require.Equal(t, b1, b2)
	}
}
