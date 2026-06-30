package scryfallquery

import (
	"reflect"
	"testing"
)

func TestLexer(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []Token
	}{
		{
			name:  "simple word",
			input: "lightning",
			want: []Token{
				{Kind: TokWord, Text: "lightning"},
				{Kind: TokEOF},
			},
		},
		{
			name:  "multiple words",
			input: "lightning bolt",
			want: []Token{
				{Kind: TokWord, Text: "lightning"},
				{Kind: TokWord, Text: "bolt"},
				{Kind: TokEOF},
			},
		},
		{
			name:  "quoted string",
			input: `"lightning bolt"`,
			want: []Token{
				{Kind: TokQuoted, Text: "lightning bolt"},
				{Kind: TokEOF},
			},
		},
		{
			name:  "single-quoted string",
			input: `'lightning bolt'`,
			want: []Token{
				{Kind: TokQuoted, Text: "lightning bolt"},
				{Kind: TokEOF},
			},
		},
		{
			name:  "field filter",
			input: "t:creature",
			want: []Token{
				{Kind: TokField, Text: "t:creature", Key: "t", Op: ":", Value: "creature"},
				{Kind: TokEOF},
			},
		},
		{
			name:  "field with comparison operator",
			input: "cmc>=3",
			want: []Token{
				{Kind: TokField, Text: "cmc>=3", Key: "cmc", Op: ">=", Value: "3"},
				{Kind: TokEOF},
			},
		},
		{
			name:  "field with quoted value",
			input: `t:"legendary creature"`,
			want: []Token{
				{Kind: TokField, Text: `t:"legendary creature"`, Key: "t", Op: ":", Value: "legendary creature"},
				{Kind: TokEOF},
			},
		},
		{
			name:  "negation and grouping",
			input: "-t:creature (c:w or c:u)",
			want: []Token{
				{Kind: TokNot, Text: "-"},
				{Kind: TokField, Text: "t:creature", Key: "t", Op: ":", Value: "creature"},
				{Kind: TokLParen, Text: "("},
				{Kind: TokField, Text: "c:w", Key: "c", Op: ":", Value: "w"},
				{Kind: TokOr, Text: "or"},
				{Kind: TokField, Text: "c:u", Key: "c", Op: ":", Value: "u"},
				{Kind: TokRParen, Text: ")"},
				{Kind: TokEOF},
			},
		},
		{
			name:  "hyphenated word",
			input: "lim-dul",
			want: []Token{
				{Kind: TokWord, Text: "lim-dul"},
				{Kind: TokEOF},
			},
		},
		{
			name:  "negated hyphenated word",
			input: "-lim-dul",
			want: []Token{
				{Kind: TokNot, Text: "-"},
				{Kind: TokWord, Text: "lim-dul"},
				{Kind: TokEOF},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			var got []Token
			for {
				tok := lexer.NextToken()
				got = append(got, tok)
				if tok.Kind == TokEOF {
					break
				}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Lexer(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
