package scryfallquery

import (
	"fmt"
	"strings"
	"unicode"
)

type TokenKind int

const (
	TokEOF TokenKind = iota
	TokLParen
	TokRParen
	TokOr
	TokNot // '-' unary operator
	TokField
	TokWord
	TokQuoted
)

func (k TokenKind) String() string {
	switch k {
	case TokEOF:
		return "EOF"
	case TokLParen:
		return "("
	case TokRParen:
		return ")"
	case TokOr:
		return "OR"
	case TokNot:
		return "-"
	case TokField:
		return "FIELD"
	case TokWord:
		return "WORD"
	case TokQuoted:
		return "QUOTED"
	default:
		return "UNKNOWN"
	}
}

type Token struct {
	Kind  TokenKind
	Text  string // For Word, Quoted, or raw matched token
	Key   string // For Field: the field key (e.g. "t", "cmc")
	Op    string // For Field: operator (e.g. ":", "=", ">=")
	Value string // For Field: the field value (e.g. "creature", "3")
}

func (t Token) String() string {
	switch t.Kind {
	case TokField:
		return fmt.Sprintf("Field(%s%s%s)", t.Key, t.Op, t.Value)
	case TokWord:
		return fmt.Sprintf("Word(%s)", t.Text)
	case TokQuoted:
		return fmt.Sprintf("Quoted(%s)", t.Text)
	default:
		return t.Kind.String()
	}
}

type Lexer struct {
	input string
	runes []rune
	pos   int
}

func NewLexer(input string) *Lexer {
	return &Lexer{
		input: input,
		runes: []rune(input),
	}
}

func (l *Lexer) NextToken() Token {
	l.skipWhitespace()

	if l.pos >= len(l.runes) {
		return Token{Kind: TokEOF}
	}

	ch := l.runes[l.pos]

	// Handle parentheses
	if ch == '(' {
		l.pos++
		return Token{Kind: TokLParen, Text: "("}
	}
	if ch == ')' {
		// ')' is only a grouping parenthesis if the next character is whitespace,
		// another parenthesis, or EOF. Otherwise it's treated as part of the word
		// (e.g. in sql injection payload values like "modern')--").
		nextIsRParenOrSpaceOrEOF := true
		if l.pos+1 < len(l.runes) {
			nextCh := l.runes[l.pos+1]
			if !unicode.IsSpace(nextCh) && nextCh != ')' && nextCh != '(' {
				nextIsRParenOrSpaceOrEOF = false
			}
		}
		if nextIsRParenOrSpaceOrEOF {
			l.pos++
			return Token{Kind: TokRParen, Text: ")"}
		}
	}

	// Handle negation '-'
	if ch == '-' {
		l.pos++
		return Token{Kind: TokNot, Text: "-"}
	}

	// Handle double-quoted strings
	if ch == '"' {
		return l.readQuoted('"')
	}
	// Handle single-quoted strings
	if ch == '\'' {
		return l.readQuoted('\'')
	}

	// Let's check if the upcoming characters represent a Field (key + operator + value).
	if tok, ok := l.tryReadField(); ok {
		return tok
	}

	// Otherwise, it's a normal word (which might be "OR" or "or").
	start := l.pos
	for l.pos < len(l.runes) && !unicode.IsSpace(l.runes[l.pos]) && l.runes[l.pos] != '(' && l.runes[l.pos] != ')' {
		l.pos++
	}
	raw := string(l.runes[start:l.pos])

	if strings.ToUpper(raw) == "OR" {
		return Token{Kind: TokOr, Text: raw}
	}

	return Token{Kind: TokWord, Text: raw}
}

func (l *Lexer) skipWhitespace() {
	for l.pos < len(l.runes) && unicode.IsSpace(l.runes[l.pos]) {
		l.pos++
	}
}

func (l *Lexer) readQuoted(quoteRune rune) Token {
	l.pos++ // skip starting quote
	var sb strings.Builder
	for l.pos < len(l.runes) && l.runes[l.pos] != quoteRune {
		sb.WriteRune(l.runes[l.pos])
		l.pos++
	}
	if l.pos < len(l.runes) {
		l.pos++ // skip ending quote
	}
	return Token{Kind: TokQuoted, Text: sb.String()}
}

// tryReadField scans from the current position to see if it matches a field query.
func (l *Lexer) tryReadField() (Token, bool) {
	p := l.pos
	for p < len(l.runes) && unicode.IsLetter(l.runes[p]) {
		p++
	}
	keyLen := p - l.pos
	if keyLen == 0 {
		return Token{}, false
	}
	key := string(l.runes[l.pos:p])

	ops := []string{"!=", "<=", ">=", ":", "=", "<", ">"}
	var foundOp string
	for _, op := range ops {
		opRunes := []rune(op)
		if p+len(opRunes) <= len(l.runes) {
			match := true
			for i, r := range opRunes {
				if l.runes[p+i] != r {
					match = false
					break
				}
			}
			if match {
				foundOp = op
				p += len(opRunes)
				break
			}
		}
	}

	if foundOp == "" {
		return Token{}, false
	}

	if p >= len(l.runes) {
		return Token{}, false // empty value
	}

	var val string
	valStart := p
	if l.runes[p] == '"' || l.runes[p] == '\'' {
		quoteRune := l.runes[p]
		p++ // skip starting quote
		var sb strings.Builder
		for p < len(l.runes) && l.runes[p] != quoteRune {
			sb.WriteRune(l.runes[p])
			p++
		}
		if p < len(l.runes) {
			p++ // skip ending quote
		}
		val = sb.String()
	} else {
		// Read bare value until whitespace or paren (with the same boundary checks)
		for p < len(l.runes) && !unicode.IsSpace(l.runes[p]) && l.runes[p] != '(' {
			if l.runes[p] == ')' {
				// Stop at ')' only if it acts as a grouping parenthesis
				nextIsRParenOrSpaceOrEOF := true
				if p+1 < len(l.runes) {
					nextCh := l.runes[p+1]
					if !unicode.IsSpace(nextCh) && nextCh != ')' && nextCh != '(' {
						nextIsRParenOrSpaceOrEOF = false
					}
				}
				if nextIsRParenOrSpaceOrEOF {
					break
				}
			}
			p++
		}
		val = string(l.runes[valStart:p])
	}

	rawText := string(l.runes[l.pos:p])
	l.pos = p // consume runes

	return Token{
		Kind:  TokField,
		Text:  rawText,
		Key:   key,
		Op:    foundOp,
		Value: val,
	}, true
}
