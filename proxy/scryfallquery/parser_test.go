package scryfallquery

import (
	"testing"
)

func TestParser(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantStr string
		wantErr bool
	}{
		{
			name:    "simple word",
			input:   "lightning",
			wantStr: `"lightning"`,
		},
		{
			name:    "implicit AND",
			input:   "lightning bolt",
			wantStr: `(AND "lightning" "bolt")`,
		},
		{
			name:    "field filter",
			input:   "t:creature",
			wantStr: `t:"creature"`,
		},
		{
			name:    "explicit OR",
			input:   "t:creature OR t:artifact",
			wantStr: `(OR t:"creature" t:"artifact")`,
		},
		{
			name:    "AND and OR mix",
			input:   "t:legendary t:creature OR t:artifact",
			wantStr: `(OR (AND t:"legendary" t:"creature") t:"artifact")`, // OR has lower precedence than implicit AND
		},
		{
			name:    "parentheses grouping",
			input:   "t:legendary (t:creature OR t:artifact)",
			wantStr: `(AND t:"legendary" (OR t:"creature" t:"artifact"))`,
		},
		{
			name:    "negation of filter",
			input:   "-t:creature",
			wantStr: `(NOT t:"creature")`,
		},
		{
			name:    "negation of group",
			input:   "-(t:creature OR t:artifact)",
			wantStr: `(NOT (OR t:"creature" t:"artifact"))`,
		},
		{
			name:    "complex nested parens",
			input:   "t:creature (c:w OR c:u) (cmc>=3 OR pow>=4)",
			wantStr: `(AND t:"creature" (OR c:"w" c:"u") (OR cmc>="3" pow>="4"))`,
		},
		{
			name:    "unbalanced parens missing closing",
			input:   "(t:creature OR t:artifact",
			wantErr: true,
		},
		{
			name:    "unbalanced parens extra closing",
			input:   "t:creature OR t:artifact)",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			parser := newParser(lexer)
			node, err := parser.parse()
			if (err != nil) != tt.wantErr {
				t.Fatalf("parser.parse() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if node == nil {
				if tt.wantStr != "" {
					t.Fatalf("got nil node, want %s", tt.wantStr)
				}
				return
			}
			gotStr := node.String()
			if gotStr != tt.wantStr {
				t.Errorf("parser.parse() = %s, want %s", gotStr, tt.wantStr)
			}
		})
	}
}
