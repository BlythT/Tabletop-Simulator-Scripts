package scryfallquery

import (
	"fmt"
)

type parser struct {
	lexer *Lexer
	cur   Token
	peek  Token
}

func newParser(l *Lexer) *parser {
	p := &parser{lexer: l}
	p.nextToken()
	p.nextToken()
	return p
}

func (p *parser) nextToken() {
	p.cur = p.peek
	p.peek = p.lexer.NextToken()
}

func (p *parser) parse() (Node, error) {
	node, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.cur.Kind != TokEOF {
		return nil, fmt.Errorf("unexpected token %s at end of query", p.cur)
	}
	return node, nil
}

// parseExpr parses: and ( OR and )*
func (p *parser) parseExpr() (Node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}

	var children []Node
	if left != nil {
		children = append(children, left)
	}

	for p.cur.Kind == TokOr {
		p.nextToken() // consume OR
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		if right != nil {
			children = append(children, right)
		}
	}

	if len(children) == 0 {
		return nil, nil
	}
	if len(children) == 1 {
		return children[0], nil
	}

	// Flatten nested OrNodes
	var flatChildren []Node
	for _, child := range children {
		if orNode, ok := child.(OrNode); ok {
			flatChildren = append(flatChildren, orNode.Children...)
		} else {
			flatChildren = append(flatChildren, child)
		}
	}
	return OrNode{Children: flatChildren}, nil
}

// parseAnd parses: term+
func (p *parser) parseAnd() (Node, error) {
	var children []Node
	for {
		if p.cur.Kind == TokEOF || p.cur.Kind == TokRParen || p.cur.Kind == TokOr {
			break
		}
		child, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		if child != nil {
			children = append(children, child)
		}
	}

	if len(children) == 0 {
		return nil, nil
	}
	if len(children) == 1 {
		return children[0], nil
	}

	// Flatten nested AndNodes
	var flatChildren []Node
	for _, child := range children {
		if andNode, ok := child.(AndNode); ok {
			flatChildren = append(flatChildren, andNode.Children...)
		} else {
			flatChildren = append(flatChildren, child)
		}
	}
	return AndNode{Children: flatChildren}, nil
}

// parseTerm parses: '-'? ( field | name | '(' expr ')' )
func (p *parser) parseTerm() (Node, error) {
	negated := false
	if p.cur.Kind == TokNot {
		negated = true
		p.nextToken() // consume '-'
	}

	var node Node
	var err error

	switch p.cur.Kind {
	case TokLParen:
		p.nextToken() // consume '('
		node, err = p.parseExpr()
		if err != nil {
			return nil, err
		}
		if p.cur.Kind != TokRParen {
			return nil, fmt.Errorf("unbalanced parentheses: missing closing ')'")
		}
		p.nextToken() // consume ')'
	case TokField:
		node = FieldNode{
			Key:   p.cur.Key,
			Op:    p.cur.Op,
			Value: p.cur.Value,
		}
		p.nextToken()
	case TokWord:
		node = NameNode{Text: p.cur.Text}
		p.nextToken()
	case TokQuoted:
		node = NameNode{Text: p.cur.Text}
		p.nextToken()
	case TokEOF:
		if negated {
			return nil, fmt.Errorf("trailing '-' without expression")
		}
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected token: %s", p.cur)
	}

	if negated && node != nil {
		node = NotNode{Child: node}
	}

	return node, nil
}
