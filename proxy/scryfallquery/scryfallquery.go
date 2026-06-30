package scryfallquery

import (
	"strings"
)

// Parse tokenizes, parses, and compiles a Scryfall query string into SQL.
// Returns a WHERE clause fragment (without leading " AND ") and parameter slice.
func Parse(query string) (string, []any, error) {
	return ParseWithFields(query, DefaultFields)
}

// ParseWithFields is like Parse but uses a custom field registry.
func ParseWithFields(query string, fields map[string]FieldDef) (string, []any, error) {
	node, err := ParseAST(query)
	if err != nil {
		return "", nil, err
	}
	if node == nil {
		return "", nil, nil
	}

	return Compile(node, fields)
}

// ParseAST tokenizes and parses a query string into an AST without compiling to SQL.
func ParseAST(query string) (Node, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	lexer := NewLexer(query)
	p := newParser(lexer)
	return p.parse()
}
