/*
Copyright 2021 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

*/

package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"regexp"
	"slices"
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/controller-tools/pkg/crd"
	"sigs.k8s.io/controller-tools/pkg/loader"
	"sigs.k8s.io/controller-tools/pkg/markers"
	"sigs.k8s.io/yaml"
)

const (
	// FeatureSetAnnotation is the annotation key used in the Operator-Controller API CRDs to specify
	// the installed Operator-Controller API channel.
	GeneratorAnnotation = "olm.operatorframework.io/generator"
	VersionAnnotation   = "controller-gen.kubebuilder.io/version"
	StandardChannel     = "standard"
	ExperimentalChannel = "experimental"
)

var standardKinds = map[string]bool{
	"ClusterExtension": true,
	"ClusterCatalog":   true,
}

// This generation code is largely copied from below into operator-controller
// github.com/kubernetes-sigs/gateway-api/blob/b7d2c5788bf38fc2c18085de524e204034c69a14/pkg/generator/main.go
// This generation code is largely copied from below into gateway-api
// github.com/kubernetes-sigs/controller-tools/blob/ab52f76cc7d167925b2d5942f24bf22e30f49a02/pkg/crd/gen.go
func main() {
	runGenerator(os.Args[1:]...)
}

func runGenerator(args ...string) {
	outputDir := "config/crd"
	ctVer := ""
	crdRoot := "github.com/operator-framework/operator-controller/api/v1"
	if len(args) >= 1 {
		// Get the output directory
		outputDir = args[0]
	}
	if len(args) >= 2 {
		// get the controller-tools version
		ctVer = args[1]
	}
	if len(args) >= 3 {
		crdRoot = args[2]
	}

	roots, err := loader.LoadRoots(
		"k8s.io/apimachinery/pkg/runtime/schema", // Needed to parse generated register functions.
		crdRoot,
	)
	if err != nil {
		log.Fatalf("failed to load package roots: %s", err)
	}

	generator := &crd.Generator{}

	parser := &crd.Parser{
		Collector: &markers.Collector{Registry: &markers.Registry{}},
		Checker: &loader.TypeChecker{
			NodeFilters: []loader.NodeFilter{generator.CheckFilter()},
		},
	}

	err = generator.RegisterMarkers(parser.Collector.Registry)
	if err != nil {
		log.Fatalf("failed to register markers: %s", err)
	}

	crd.AddKnownTypes(parser)
	for _, r := range roots {
		parser.NeedPackage(r)
	}

	metav1Pkg := crd.FindMetav1(roots)
	if metav1Pkg == nil {
		log.Fatalf("no objects in the roots, since nothing imported metav1")
	}

	kubeKinds := crd.FindKubeKinds(parser, metav1Pkg)
	if len(kubeKinds) == 0 {
		log.Fatalf("no objects in the roots")
	}

	channels := []string{StandardChannel, ExperimentalChannel}
	for _, channel := range channels {
		for _, groupKind := range kubeKinds {
			if channel == StandardChannel && !standardKinds[groupKind.Kind] {
				continue
			}

			log.Printf("generating %s CRD for %v\n", channel, groupKind)

			parser.NeedCRDFor(groupKind, nil)
			crdRaw := parser.CustomResourceDefinitions[groupKind]

			// Inline version of "addAttribution(&crdRaw)" ...
			if crdRaw.Annotations == nil {
				crdRaw.Annotations = map[string]string{}
			}
			crdRaw.Annotations[GeneratorAnnotation] = channel
			if ctVer != "" {
				crdRaw.Annotations[VersionAnnotation] = ctVer
			}

			// Prevent the top level metadata for the CRD to be generated regardless of the intention in the arguments
			crd.FixTopLevelMetadata(crdRaw)

			channelCrd := crdRaw.DeepCopy()
			for i, version := range channelCrd.Spec.Versions {
				if channel == StandardChannel && strings.Contains(version.Name, "alpha") {
					channelCrd.Spec.Versions[i].Served = false
				}
				channelCrd.Spec.Versions[i].Schema.OpenAPIV3Schema.Properties = opconTweaksMap(channel, channelCrd.Spec.Versions[i].Schema.OpenAPIV3Schema)
			}

			conv, err := crd.AsVersion(*channelCrd, apiextensionsv1.SchemeGroupVersion)
			if err != nil {
				log.Fatalf("failed to convert CRD: %s", err)
			}

			out, err := yaml.Marshal(conv)
			if err != nil {
				log.Fatalf("failed to marshal CRD: %s", err)
			}

			// Do some filtering of the resulting YAML
			var yamlData map[string]any
			err = yaml.Unmarshal(out, &yamlData)
			if err != nil {
				log.Fatalf("failed to unmarshal data: %s", err)
			}

			scrapYaml(yamlData, "status")
			scrapYaml(yamlData, "metadata", "creationTimestamp")

			out, err = yaml.Marshal(yamlData)
			if err != nil {
				log.Fatalf("failed to re-marshal CRD: %s", err)
			}

			// If missing, add a break at the beginning of the file
			breakLine := []byte("---\n")
			if !bytes.HasPrefix(out, breakLine) {
				out = append(breakLine, out...)
			}

			fileName := fmt.Sprintf("%s/%s/%s_%s.yaml", outputDir, channel, crdRaw.Spec.Group, crdRaw.Spec.Names.Plural)
			err = os.WriteFile(fileName, out, 0o600)
			if err != nil {
				log.Fatalf("failed to write CRD: %s", err)
			}
		}
	}
}

// Apply Opcon specific tweaks to all properties in a map, and update the parent schema's required list according to opcon tags.
// For opcon validation optional/required tags, the parent schema's required list is mutated directly.
// TODO: if we need to support other conditions from opconTweaks, it will likely be preferable to convey the parent schema to facilitate direct alteration.
func opconTweaksMap(channel string, parentSchema *apiextensionsv1.JSONSchemaProps) map[string]apiextensionsv1.JSONSchemaProps {
	props := parentSchema.Properties

	for name := range props {
		jsonProps := props[name]
		p, reqStatus := opconTweaks(channel, name, jsonProps)
		if p == nil {
			delete(props, name)
		} else {
			props[name] = *p
			// Update required list based on tag
			switch reqStatus {
			case statusRequired:
				if !slices.Contains(parentSchema.Required, name) {
					parentSchema.Required = append(parentSchema.Required, name)
				}
			case statusOptional:
				parentSchema.Required = slices.DeleteFunc(parentSchema.Required, func(s string) bool { return s == name })
			default:
				// "" (unspecified) means keep existing status
			}
		}
	}
	return props
}

const (
	statusRequired  = "required"
	statusOptional  = "optional"
	statusNoOpinion = ""
)

// Custom Opcon API Tweaks for tags prefixed with `<opcon:` that get past
// the limitations of Kubebuilder annotations.
// Returns the modified schema and a string indicating required status where indicated by opcon tags:
// "required", "optional", or "" (no decision -- preserve any non-opcon required status).
func opconTweaks(channel string, name string, jsonProps apiextensionsv1.JSONSchemaProps) (*apiextensionsv1.JSONSchemaProps, string) {
	requiredStatus := statusNoOpinion

	if channel == StandardChannel {
		if strings.Contains(jsonProps.Description, "<opcon:experimental>") {
			return nil, statusNoOpinion
		}
	}

	// TODO(robscott): Figure out why crdgen switched this to "object"
	if jsonProps.Format == "date-time" {
		jsonProps.Type = "string"
	}

	validationPrefix := fmt.Sprintf("<opcon:%s:validation:", channel)
	numExpressions := strings.Count(jsonProps.Description, validationPrefix)
	numValid := 0
	if numExpressions > 0 {
		enumRe := regexp.MustCompile(validationPrefix + "Enum=([A-Za-z;]*)>")
		enumMatches := enumRe.FindAllStringSubmatch(jsonProps.Description, 64)
		for _, enumMatch := range enumMatches {
			if len(enumMatch) != 2 {
				log.Fatalf("Invalid %s Enum tag for %s", validationPrefix, name)
			}

			numValid++
			jsonProps.Enum = []apiextensionsv1.JSON{}
			for val := range strings.SplitSeq(enumMatch[1], ";") {
				jsonProps.Enum = append(jsonProps.Enum, apiextensionsv1.JSON{Raw: []byte("\"" + val + "\"")})
			}
		}

		celRe := regexp.MustCompile(validationPrefix + "XValidation:rule=\"([^\"]*)\",message=\"([^\"]*)\">")
		celMatches := celRe.FindAllStringSubmatch(jsonProps.Description, 64)
		for _, celMatch := range celMatches {
			if len(celMatch) != 3 {
				log.Fatalf("Invalid %s CEL tag for %s", validationPrefix, name)
			}

			numValid++
			jsonProps.XValidations = append(jsonProps.XValidations, apiextensionsv1.ValidationRule{
				Message: celMatch[1],
				Rule:    celMatch[2],
			})
		}
		optReqRe := regexp.MustCompile(validationPrefix + "(Optional|Required)>")
		optReqMatches := optReqRe.FindAllStringSubmatch(jsonProps.Description, 64)
		hasOptional := false
		hasRequired := false
		for _, optReqMatch := range optReqMatches {
			if len(optReqMatch) != 2 {
				log.Fatalf("Invalid %s Optional/Required tag for %s", validationPrefix, name)
			}

			numValid++
			switch optReqMatch[1] {
			case "Optional":
				hasOptional = true
				requiredStatus = statusOptional
			case "Required":
				hasRequired = true
				requiredStatus = statusRequired
			}
		}
		if hasOptional && hasRequired {
			log.Fatalf("Field %s has both Optional and Required validation tags for channel %s", name, channel)
		}
	}

	if numValid < numExpressions {
		log.Fatalf("Found %d Opcon validation expressions, but only %d were valid", numExpressions, numValid)
	}

	jsonProps.Description = formatDescription(jsonProps.Description, channel, name)

	if len(jsonProps.Properties) > 0 {
		jsonProps.Properties = opconTweaksMap(channel, &jsonProps)
	} else if jsonProps.Items != nil && jsonProps.Items.Schema != nil {
		jsonProps.Items.Schema, _ = opconTweaks(channel, name, *jsonProps.Items.Schema)
	}

	return &jsonProps, requiredStatus
}

func formatDescription(description string, channel string, name string) string {
	tagset := []struct {
		channel string
		tag     string
	}{
		{channel: ExperimentalChannel, tag: "opcon:standard:description"},
		{channel: StandardChannel, tag: "opcon:experimental:description"},
	}
	for _, ts := range tagset {
		startTag := fmt.Sprintf("<%s>", ts.tag)
		endTag := fmt.Sprintf("</%s>", ts.tag)
		if channel == ts.channel && strings.Contains(description, ts.tag) {
			regexPattern := `\n*` + regexp.QuoteMeta(startTag) + `(?s:(.*?))` + regexp.QuoteMeta(endTag) + `\n*`
			re := regexp.MustCompile(regexPattern)
			match := re.FindStringSubmatch(description)
			if len(match) != 2 {
				log.Fatalf("Invalid %s tag for %s", startTag, name)
			}
			description = re.ReplaceAllString(description, "\n\n")
		} else {
			description = strings.ReplaceAll(description, startTag, "")
			description = strings.ReplaceAll(description, endTag, "")
		}
	}

	// Comments within "opcon:util:excludeFromCRD" tag are not included in the generated CRD and all trailing \n operators before
	// and after the tags are removed and replaced with three \n operators.
	startTag := "<opcon:util:excludeFromCRD>"
	endTag := "</opcon:util:excludeFromCRD>"
	if strings.Contains(description, startTag) {
		regexPattern := `\n*` + regexp.QuoteMeta(startTag) + `(?s:(.*?))` + regexp.QuoteMeta(endTag) + `\n*`
		re := regexp.MustCompile(regexPattern)
		match := re.FindStringSubmatch(description)
		if len(match) != 2 {
			log.Fatalf("Invalid <opcon:util:excludeFromCRD> tag for %s", name)
		}
		description = re.ReplaceAllString(description, "\n\n\n")
	}

	opconRe := regexp.MustCompile(`<opcon:.*>`)
	description = opconRe.ReplaceAllLiteralString(description, "")

	// Remove anything following three hyphens
	regexPattern := `(?s)---.*`
	re := regexp.MustCompile(regexPattern)
	description = re.ReplaceAllString(description, "")

	// Remove any extra \n (more than 2 and all trailing at the end)
	regexPattern = `\n\n+`
	re = regexp.MustCompile(regexPattern)
	description = re.ReplaceAllString(description, "\n\n")
	description = strings.Trim(description, "\n")

	return description
}

// delete a field in unstructured YAML
func scrapYaml(data map[string]any, fields ...string) {
	if len(fields) == 0 {
		return
	}
	if len(fields) == 1 {
		delete(data, fields[0])
		return
	}
	if f, ok := data[fields[0]]; ok {
		if f2, ok := f.(map[string]any); ok {
			scrapYaml(f2, fields[1:]...)
		}
	}
}
