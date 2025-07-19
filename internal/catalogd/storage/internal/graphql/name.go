package graphql

import (
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

type namer struct {
	caser cases.Caser
}

func newNamer() *namer {
	return &namer{caser: cases.Title(language.English)}
}

func sanitizeName(name string) string {
	if name == "" {
		panic("name is empty")
	}
	// Replace any invalid characters with underscores
	// First, handle the first character (must be letter or underscore)
	firstCharRegex := regexp.MustCompile(`^[^_a-zA-Z]`)
	name = firstCharRegex.ReplaceAllString(name, "_")

	// Then handle subsequent characters (can be letters, numbers, or underscores)
	restRegex := regexp.MustCompile(`[^_a-zA-Z0-9]`)
	return restRegex.ReplaceAllString(name, "_")
}

func (n *namer) TypeNameForField(prefix, name string) string {
	return fmt.Sprintf("%s%s", prefix, sanitizeName(n.caser.String(name)))
}

func (n *namer) TypeNameForSchema(schema string) string {
	if schema == "" {
		panic("schema is empty")
	}
	var sb strings.Builder
	parts := strings.Split(schema, ".")
	for _, part := range parts {
		sb.WriteString(sanitizeName(n.caser.String(part)))
	}

	return sb.String()
}

func (n *namer) FieldNameForSchema(schema string) string {
	if schema == "" {
		panic("schema is empty")
	}
	var sb strings.Builder
	parts := strings.Split(schema, ".")
	sb.WriteString(sanitizeName(parts[0]))
	for _, part := range parts[1:] {
		sb.WriteString(sanitizeName(n.caser.String(part)))
	}

	return sb.String()
}
