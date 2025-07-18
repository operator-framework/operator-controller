package graphql

import (
	"fmt"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
	"github.com/itchyny/gojq"
)

// applyJQQuery applies a jq query to JSON data and returns the result
func applyJQQuery(data interface{}, jqCode *gojq.Code) (interface{}, error) {
	iter := jqCode.Run(data)

	// Collect all results
	var results []interface{}
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, ok := v.(error); ok {
			return nil, fmt.Errorf("error executing jq query: %w", err)
		}
		results = append(results, v)
	}

	// Return single result if only one, otherwise return array
	if len(results) == 1 {
		return results[0], nil
	}
	return results, nil
}

// jsonScalar is a custom GraphQL scalar type that can handle arbitrary JSON objects
var jsonScalar = graphql.NewScalar(graphql.ScalarConfig{
	Name: "JSON",
	Description: "The `JSON` scalar type represents JSON values as specified by " +
		"[ECMA-404](http://www.ecma-international.org/publications/files/ECMA-ST/ECMA-404.pdf).",
	Serialize: func(value interface{}) interface{} {
		return value
	},
	ParseValue: func(value interface{}) interface{} {
		return value
	},
	ParseLiteral: func(valueAST ast.Value) interface{} {
		switch valueAST := valueAST.(type) {
		case *ast.StringValue:
			return valueAST.Value
		case *ast.IntValue:
			return valueAST.Value
		case *ast.FloatValue:
			return valueAST.Value
		case *ast.BooleanValue:
			return valueAST.Value
		default:
			return nil
		}
	},
})

func newJSONScalarField(fieldName string) *graphql.Field {
	return &graphql.Field{
		Type: jsonScalar,
		Args: graphql.FieldConfigArgument{
			"jq": &graphql.ArgumentConfig{
				Type:        graphql.String,
				Description: "jq query to apply to the JSON data",
			},
		},
		Resolve: func(p graphql.ResolveParams) (interface{}, error) {
			data := p.Source.(map[string]interface{})[fieldName]

			jqQueryString, ok := p.Args["jq"].(string)
			if !ok || jqQueryString == "" {
				return data, nil
			}

			jqCode, err := jqCodeFromContextOrCompileAndSet(p.Context, jqQueryString)
			if err != nil {
				return nil, err
			}

			return applyJQQuery(data, jqCode)
		},
	}
}
