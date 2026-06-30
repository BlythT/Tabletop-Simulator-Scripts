package scryfallquery

import (
	"fmt"
	"strings"
)

// Compile walks the AST and produces a SQL WHERE clause and parameters.
func Compile(node Node, fields map[string]FieldDef) (string, []any, error) {
	return compileNode(node, fields, false, true)
}

func compileNode(node Node, fields map[string]FieldDef, negated bool, isRoot bool) (string, []any, error) {
	if node == nil {
		return "", nil, nil
	}

	switch v := node.(type) {
	case AndNode:
		var parts []string
		var args []any
		for _, child := range v.Children {
			sqlPart, childArgs, err := compileNode(child, fields, false, false)
			if err != nil {
				return "", nil, err
			}
			if sqlPart != "" {
				parts = append(parts, sqlPart)
				args = append(args, childArgs...)
			}
		}
		if len(parts) == 0 {
			return "", nil, nil
		}
		if len(parts) == 1 {
			if negated {
				return "NOT (" + parts[0] + ")", args, nil
			}
			return parts[0], args, nil
		}
		if isRoot && !negated {
			return strings.Join(parts, " AND "), args, nil
		}
		sql := "(" + strings.Join(parts, " AND ") + ")"
		if negated {
			sql = "NOT " + sql
		}
		return sql, args, nil

	case OrNode:
		var parts []string
		var args []any
		for _, child := range v.Children {
			sqlPart, childArgs, err := compileNode(child, fields, false, false)
			if err != nil {
				return "", nil, err
			}
			if sqlPart != "" {
				parts = append(parts, sqlPart)
				args = append(args, childArgs...)
			}
		}
		if len(parts) == 0 {
			return "", nil, nil
		}
		if len(parts) == 1 {
			if negated {
				return "NOT (" + parts[0] + ")", args, nil
			}
			return parts[0], args, nil
		}
		sql := "(" + strings.Join(parts, " OR ") + ")"
		if negated {
			sql = "NOT " + sql
		}
		return sql, args, nil

	case NotNode:
		return compileNode(v.Child, fields, !negated, isRoot)

	case FieldNode:
		key := strings.ToLower(v.Key)
		conf, ok := fields[key]
		if !ok {
			// Silently ignore unknown fields to match original parser behavior
			return "", nil, nil
		}

		if conf.Transform != nil {
			sql, args, err := conf.Transform(v.Op, v.Value, negated)
			return sql, args, err
		}

		// Standard field compilation.
		// If DefaultOp is set, it overrides the typed operator (e.g. ci>=uw uses LIKE).
		op := conf.DefaultOp
		if op == "" {
			op = v.Op
			if op == ":" {
				op = "="
			}
		}

		val := v.Value
		if conf.ValueXform != nil {
			val = conf.ValueXform(val)
		}

		finalOp := op
		if negated {
			finalOp = invertOp(op)
		}

		sql := conf.Column + " " + finalOp + " ?"
		return sql, []any{val}, nil

	case NameNode:
		clean := CleanName(v.Text)
		if clean == "" {
			return "", nil, nil
		}
		op := "LIKE"
		if negated {
			op = "NOT LIKE"
		}
		return "name_clean " + op + " ?", []any{"%" + clean + "%"}, nil

	default:
		return "", nil, fmt.Errorf("unsupported AST node type: %T", node)
	}
}

func invertOp(op string) string {
	switch op {
	case "=":
		return "!="
	case "!=":
		return "="
	case ">":
		return "<="
	case "<":
		return ">="
	case ">=":
		return "<"
	case "<=":
		return ">"
	case "LIKE":
		return "NOT LIKE"
	case "NOT LIKE":
		return "LIKE"
	default:
		if strings.HasPrefix(op, "NOT ") {
			return op[4:]
		}
		return "NOT " + op
	}
}
