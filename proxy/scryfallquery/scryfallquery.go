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

// IsBatchSafe walks the AST to ensure the query only uses fast, indexed filters
// suitable for batch processing (set/rarity constraints only, no unindexed fields or wildcards).
func IsBatchSafe(node Node) bool {
	if node == nil {
		return true
	}
	switch v := node.(type) {
	case AndNode:
		for _, child := range v.Children {
			if !IsBatchSafe(child) {
				return false
			}
		}
		return true
	case OrNode:
		for _, child := range v.Children {
			if !IsBatchSafe(child) {
				return false
			}
		}
		return true
	case NotNode:
		return IsBatchSafe(v.Child)
	case FieldNode:
		key := strings.ToLower(v.Key)
		// Block oracle text queries as they trigger expensive unindexed text searches.
		// Allow all other fields (format, set, rarity, color, type, cmc, etc.) which are either
		// indexed or run on index-reduced datasets (e.g. format-filtered sets).
		return key != "oracle" && key != "o"
	case NameNode:
		// Bare word names trigger LIKE '%name%' wildcard prefix scans
		return false
	}
	return false
}
