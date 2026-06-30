package scryfallquery

import (
	"reflect"
	"testing"
)

func TestCompiler(t *testing.T) {
	tests := []struct {
		name     string
		node     Node
		wantSql  string
		wantArgs []any
		wantErr  bool
	}{
		{
			name:     "simple name search",
			node:     NameNode{Text: "Lightning"},
			wantSql:  "name_clean LIKE ?",
			wantArgs: []any{"%lightning%"},
		},
		{
			name:     "negated name search",
			node:     NotNode{Child: NameNode{Text: "Lightning"}},
			wantSql:  "name_clean NOT LIKE ?",
			wantArgs: []any{"%lightning%"},
		},
		{
			name:     "accented name search",
			node:     NameNode{Text: "Sméagol"},
			wantSql:  "name_clean LIKE ?",
			wantArgs: []any{"%smeagol%"},
		},
		{
			name:     "field search equal",
			node:     FieldNode{Key: "set", Op: ":", Value: "kld"},
			wantSql:  "set_code = ?",
			wantArgs: []any{"kld"},
		},
		{
			name:     "field search comparative",
			node:     FieldNode{Key: "cmc", Op: ">=", Value: "3"},
			wantSql:  "CAST(json_extract(raw_json, '$.cmc') AS REAL) >= ?",
			wantArgs: []any{"3"},
		},
		{
			name:     "negated field search comparative",
			node:     NotNode{Child: FieldNode{Key: "cmc", Op: ">=", Value: "3"}},
			wantSql:  "CAST(json_extract(raw_json, '$.cmc') AS REAL) < ?",
			wantArgs: []any{"3"},
		},
		{
			name:     "format legality check positive",
			node:     FieldNode{Key: "f", Op: ":", Value: "modern"},
			wantSql:  "json_extract(raw_json, '$.legalities.modern') = 'legal'",
			wantArgs: nil,
		},
		{
			name:     "format legality check negative",
			node:     NotNode{Child: FieldNode{Key: "f", Op: ":", Value: "modern"}},
			wantSql:  "json_extract(raw_json, '$.legalities.modern') IN ('not_legal', 'banned', 'restricted')",
			wantArgs: nil,
		},
		{
			name:     "format legality check with operator negation",
			node:     FieldNode{Key: "f", Op: "!=", Value: "modern"},
			wantSql:  "json_extract(raw_json, '$.legalities.modern') IN ('not_legal', 'banned', 'restricted')",
			wantArgs: nil,
		},
		{
			name: "AND logic",
			node: AndNode{Children: []Node{
				FieldNode{Key: "set", Op: ":", Value: "kld"},
				FieldNode{Key: "rarity", Op: ":", Value: "rare"},
			}},
			wantSql:  "set_code = ? AND json_extract(raw_json, '$.rarity') = ?",
			wantArgs: []any{"kld", "rare"},
		},
		{
			name: "OR logic",
			node: OrNode{Children: []Node{
				FieldNode{Key: "set", Op: ":", Value: "kld"},
				FieldNode{Key: "set", Op: ":", Value: "aer"},
			}},
			wantSql:  "(set_code = ? OR set_code = ?)",
			wantArgs: []any{"kld", "aer"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := Compile(tt.node, DefaultFields)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Compile() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if sql != tt.wantSql {
				t.Errorf("Compile() sql = %q, want %q", sql, tt.wantSql)
			}
			if !reflect.DeepEqual(args, tt.wantArgs) {
				t.Errorf("Compile() args = %v, want %v", args, tt.wantArgs)
			}
		})
	}
}
