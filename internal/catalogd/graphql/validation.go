package graphql

import (
	"fmt"

	"github.com/graphql-go/graphql/language/ast"
	"github.com/graphql-go/graphql/language/parser"
)

const (
	MaxQueryDepth   = 10
	MaxQueryAliases = 50
	MaxQueryFields  = 500
)

type queryComplexity struct {
	aliases   int
	fields    int
	fragments map[string]*ast.FragmentDefinition
	visited   map[string]bool
}

// ValidateQueryComplexity parses the query AST and rejects it if it exceeds
// depth, alias, or total field count thresholds.
func ValidateQueryComplexity(query string) error {
	doc, err := parser.Parse(parser.ParseParams{Source: query})
	if err != nil {
		return fmt.Errorf("query parse error: %w", err)
	}

	c := &queryComplexity{
		fragments: make(map[string]*ast.FragmentDefinition),
		visited:   make(map[string]bool),
	}
	for _, def := range doc.Definitions {
		if frag, ok := def.(*ast.FragmentDefinition); ok {
			c.fragments[frag.Name.Value] = frag
		}
	}
	for _, def := range doc.Definitions {
		if op, ok := def.(*ast.OperationDefinition); ok {
			if err := c.walkSelectionSet(op.SelectionSet, 1); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *queryComplexity) walkSelectionSet(ss *ast.SelectionSet, depth int) error {
	if ss == nil {
		return nil
	}
	if depth > MaxQueryDepth {
		return fmt.Errorf("query exceeds maximum depth of %d", MaxQueryDepth)
	}

	for _, sel := range ss.Selections {
		switch s := sel.(type) {
		case *ast.Field:
			c.fields++
			if c.fields > MaxQueryFields {
				return fmt.Errorf("query exceeds maximum field count of %d", MaxQueryFields)
			}
			if s.Alias != nil && s.Alias.Value != "" {
				c.aliases++
				if c.aliases > MaxQueryAliases {
					return fmt.Errorf("query exceeds maximum alias count of %d", MaxQueryAliases)
				}
			}
			if err := c.walkSelectionSet(s.SelectionSet, depth+1); err != nil {
				return err
			}
		case *ast.InlineFragment:
			if err := c.walkSelectionSet(s.SelectionSet, depth+1); err != nil {
				return err
			}
		case *ast.FragmentSpread:
			name := s.Name.Value
			if c.visited[name] {
				continue
			}
			c.visited[name] = true
			if frag, ok := c.fragments[name]; ok {
				if err := c.walkSelectionSet(frag.SelectionSet, depth+1); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
