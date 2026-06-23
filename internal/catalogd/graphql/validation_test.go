package graphql

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidateQueryComplexity_ValidSimpleQuery(t *testing.T) {
	err := ValidateQueryComplexity(`{ olmpackages { name } }`)
	if err != nil {
		t.Fatalf("expected no error for simple query, got: %v", err)
	}
}

func TestValidateQueryComplexity_ParseError(t *testing.T) {
	err := ValidateQueryComplexity(`{ invalid query {{{{`)
	if err == nil {
		t.Fatal("expected parse error for malformed query")
	}
	if !strings.Contains(err.Error(), "query parse error") {
		t.Errorf("expected 'query parse error' in message, got: %v", err)
	}
}

func TestValidateQueryComplexity_ExceedsDepth(t *testing.T) {
	// Build a query that exceeds maxQueryDepth (10)
	var b strings.Builder
	b.WriteString("{ ")
	for i := 0; i <= maxQueryDepth+1; i++ {
		b.WriteString(fmt.Sprintf("f%d { ", i))
	}
	b.WriteString("leaf")
	for i := 0; i <= maxQueryDepth+1; i++ {
		b.WriteString(" }")
	}
	b.WriteString(" }")

	err := ValidateQueryComplexity(b.String())
	if err == nil {
		t.Fatal("expected depth error")
	}
	if !strings.Contains(err.Error(), "maximum depth") {
		t.Errorf("expected 'maximum depth' in error, got: %v", err)
	}
}

func TestValidateQueryComplexity_WithinDepthLimit(t *testing.T) {
	// Build a query at exactly maxQueryDepth (should pass)
	var b strings.Builder
	b.WriteString("{ ")
	for i := 1; i < maxQueryDepth; i++ {
		b.WriteString(fmt.Sprintf("f%d { ", i))
	}
	b.WriteString("leaf")
	for i := 1; i < maxQueryDepth; i++ {
		b.WriteString(" }")
	}
	b.WriteString(" }")

	err := ValidateQueryComplexity(b.String())
	if err != nil {
		t.Fatalf("query at depth limit should pass, got: %v", err)
	}
}

func TestValidateQueryComplexity_ExceedsAliases(t *testing.T) {
	var b strings.Builder
	b.WriteString("{ ")
	for i := 0; i <= maxQueryAliases; i++ {
		b.WriteString(fmt.Sprintf("a%d: name ", i))
	}
	b.WriteString("}")

	err := ValidateQueryComplexity(b.String())
	if err == nil {
		t.Fatal("expected alias count error")
	}
	if !strings.Contains(err.Error(), "maximum alias count") {
		t.Errorf("expected 'maximum alias count' in error, got: %v", err)
	}
}

func TestValidateQueryComplexity_ExceedsFieldCount(t *testing.T) {
	var b strings.Builder
	b.WriteString("{ ")
	for i := 0; i <= maxQueryFields; i++ {
		b.WriteString(fmt.Sprintf("f%d ", i))
	}
	b.WriteString("}")

	err := ValidateQueryComplexity(b.String())
	if err == nil {
		t.Fatal("expected field count error")
	}
	if !strings.Contains(err.Error(), "maximum field count") {
		t.Errorf("expected 'maximum field count' in error, got: %v", err)
	}
}

func TestValidateQueryComplexity_WithFragments(t *testing.T) {
	query := `
		fragment PkgFields on OlmPackage {
			name
			defaultChannel
		}
		{
			olmpackages {
				...PkgFields
			}
		}
	`
	err := ValidateQueryComplexity(query)
	if err != nil {
		t.Fatalf("expected no error for query with fragments, got: %v", err)
	}
}

func TestValidateQueryComplexity_WithInlineFragment(t *testing.T) {
	query := `
		{
			olmpackages {
				... on OlmPackage {
					name
				}
			}
		}
	`
	err := ValidateQueryComplexity(query)
	if err != nil {
		t.Fatalf("expected no error for query with inline fragment, got: %v", err)
	}
}

func TestValidateQueryComplexity_EmptyQuery(t *testing.T) {
	err := ValidateQueryComplexity(`{ __typename }`)
	if err != nil {
		t.Fatalf("expected no error for minimal query, got: %v", err)
	}
}

func TestValidateQueryComplexity_DuplicateFragmentSpreadSkipped(t *testing.T) {
	query := `
		fragment F on OlmPackage {
			name
		}
		{
			olmpackages {
				...F
				...F
			}
		}
	`
	err := ValidateQueryComplexity(query)
	if err != nil {
		t.Fatalf("expected no error with duplicate fragment spreads, got: %v", err)
	}
}
