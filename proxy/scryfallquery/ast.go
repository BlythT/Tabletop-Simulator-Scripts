package scryfallquery

import (
	"fmt"
	"strings"
)

// Node represents a node in the Scryfall query AST.
type Node interface {
	nodeMarker()
	String() string
}

// AndNode represents an implicit/explicit AND operation between multiple conditions.
type AndNode struct {
	Children []Node
}

func (AndNode) nodeMarker() {}
func (n AndNode) String() string {
	var sb strings.Builder
	sb.WriteString("(AND")
	for _, child := range n.Children {
		sb.WriteString(" ")
		sb.WriteString(child.String())
	}
	sb.WriteString(")")
	return sb.String()
}

// OrNode represents an explicit OR operation between multiple conditions.
type OrNode struct {
	Children []Node
}

func (OrNode) nodeMarker() {}
func (n OrNode) String() string {
	var sb strings.Builder
	sb.WriteString("(OR")
	for _, child := range n.Children {
		sb.WriteString(" ")
		sb.WriteString(child.String())
	}
	sb.WriteString(")")
	return sb.String()
}

// NotNode represents the negation of a condition.
type NotNode struct {
	Child Node
}

func (NotNode) nodeMarker() {}
func (n NotNode) String() string {
	return fmt.Sprintf("(NOT %s)", n.Child.String())
}

// FieldNode represents a field-value comparison (e.g., t:creature or cmc>=3).
type FieldNode struct {
	Key   string
	Op    string // ":", "=", "!=", "<", ">", "<=", ">="
	Value string
}

func (FieldNode) nodeMarker() {}
func (n FieldNode) String() string {
	return fmt.Sprintf("%s%s%q", n.Key, n.Op, n.Value)
}

// NameNode represents a bare word or quoted string searching for card names.
type NameNode struct {
	Text string
}

func (NameNode) nodeMarker() {}
func (n NameNode) String() string {
	return fmt.Sprintf("%q", n.Text)
}
