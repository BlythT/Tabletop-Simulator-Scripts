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

// IndexConfig defines which fields and capabilities are indexed on the database schema.
type IndexConfig struct {
	IndexedFields  map[string]bool
	IndexNameField bool
}

// IsIndexable walks the AST to check if all leaf query nodes map to indexed fields or columns.
func IsIndexable(node Node, cfg IndexConfig) bool {
	if node == nil {
		return true
	}
	switch v := node.(type) {
	case AndNode:
		for _, child := range v.Children {
			if !IsIndexable(child, cfg) {
				return false
			}
		}
		return true
	case OrNode:
		for _, child := range v.Children {
			if !IsIndexable(child, cfg) {
				return false
			}
		}
		return true
	case NotNode:
		return IsIndexable(v.Child, cfg)
	case FieldNode:
		key := strings.ToLower(v.Key)
		return cfg.IndexedFields[key]
	case NameNode:
		return cfg.IndexNameField
	}
	return false
}
