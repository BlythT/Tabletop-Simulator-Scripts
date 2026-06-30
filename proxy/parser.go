package main

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

type fieldOp int

const (
	opEqual fieldOp = iota
	opComparable
	opLike
)

type fieldConfig struct {
	col   string
	op    fieldOp
	xform func(string) string
}

var (
	rxFilter = regexp.MustCompile(`(?i)^([a-zA-Z]+)(!=|<=|>=|[:=<>])(.+)$`)

	identity      = func(s string) string { return s }
	wildcard      = func(s string) string { return "%" + s + "%" }
	wildcardLower = func(s string) string { return "%" + strings.ToLower(s) + "%" }
	wildcardUpper = func(s string) string { return "%" + strings.ToUpper(s) + "%" }

	fields = map[string]fieldConfig{
		"s":        {col: "set_code", op: opEqual, xform: strings.ToLower},
		"set":      {col: "set_code", op: opEqual, xform: strings.ToLower},
		"r":        {col: "json_extract(raw_json, '$.rarity')", op: opEqual, xform: strings.ToLower},
		"rarity":   {col: "json_extract(raw_json, '$.rarity')", op: opEqual, xform: strings.ToLower},
		"t":        {col: "json_extract(raw_json, '$.type_line')", op: opLike, xform: wildcardLower},
		"type":     {col: "json_extract(raw_json, '$.type_line')", op: opLike, xform: wildcardLower},
		"c":        {col: "json_extract(raw_json, '$.colors')", op: opLike, xform: wildcardUpper},
		"color":    {col: "json_extract(raw_json, '$.colors')", op: opLike, xform: wildcardUpper},
		"id":       {col: "json_extract(raw_json, '$.color_identity')", op: opLike, xform: wildcardUpper},
		"ci":       {col: "json_extract(raw_json, '$.color_identity')", op: opLike, xform: wildcardUpper},
		"identity": {col: "json_extract(raw_json, '$.color_identity')", op: opLike, xform: wildcardUpper},
		"cmc":      {col: "CAST(json_extract(raw_json, '$.cmc') AS REAL)", op: opComparable, xform: identity},
		"mv":       {col: "CAST(json_extract(raw_json, '$.cmc') AS REAL)", op: opComparable, xform: identity},
		"pow":      {col: "json_extract(raw_json, '$.power')", op: opComparable, xform: identity},
		"power":    {col: "json_extract(raw_json, '$.power')", op: opComparable, xform: identity},
		"tou":      {col: "json_extract(raw_json, '$.toughness')", op: opComparable, xform: identity},
		"toughness": {col: "json_extract(raw_json, '$.toughness')", op: opComparable, xform: identity},
		"o":        {col: "json_extract(raw_json, '$.oracle_text')", op: opLike, xform: wildcard},
		"oracle":   {col: "json_extract(raw_json, '$.oracle_text')", op: opLike, xform: wildcard},
		"a":        {col: "json_extract(raw_json, '$.artist')", op: opLike, xform: wildcard},
		"art":      {col: "json_extract(raw_json, '$.artist')", op: opLike, xform: wildcard},
		"artist":   {col: "json_extract(raw_json, '$.artist')", op: opLike, xform: wildcard},
		"lang":     {col: "json_extract(raw_json, '$.lang')", op: opEqual, xform: strings.ToLower},
		"l":        {col: "json_extract(raw_json, '$.lang')", op: opEqual, xform: strings.ToLower},
	}

	allowedFormats = map[string]bool{
		"standard":         true,
		"future":           true,
		"historic":         true,
		"timeless":         true,
		"gladiator":        true,
		"pioneer":          true,
		"modern":           true,
		"legacy":           true,
		"pauper":           true,
		"vintage":          true,
		"penny":            true,
		"commander":        true,
		"oathbreaker":      true,
		"standardbrawl":    true,
		"brawl":            true,
		"competitivebrawl": true,
		"alchemy":          true,
		"paupercommander":  true,
		"duel":             true,
		"oldschool":        true,
		"premodern":        true,
		"predh":            true,
		"tlr":              true,
	}
)

func cleanName(name string) string {
	var sb strings.Builder
	for _, r := range name {
		if r >= 'A' && r <= 'Z' {
			r = r + ('a' - 'A')
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func negateOp(op string, negate bool) string {
	if negate {
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
		default:
			return "NOT " + op
		}
	}
	return op
}

func comparisonOp(op string) string {
	switch op {
	case ":", "=":
		return "="
	case "!=":
		return "!="
	case "<":
		return "<"
	case ">":
		return ">"
	case "<=":
		return "<="
	case ">=":
		return ">="
	default:
		return "="
	}
}

func parseQuery(q string) (whereSql string, params []any) {
	uDec, err := url.QueryUnescape(q)
	if err == nil {
		q = uDec
	}

	q = strings.ReplaceAll(q, "+", " ")
	tokens := strings.Fields(q)

	var clauses []string
	for _, token := range tokens {
		negate := false
		if strings.HasPrefix(token, "-") {
			negate = true
			token = token[1:]
		}

		var key, opStr, val string
		if m := rxFilter.FindStringSubmatch(token); len(m) > 3 {
			key = m[1]
			opStr = m[2]
			val = m[3]
		}

		if key != "" {
			key = strings.ToLower(key)
			op := comparisonOp(opStr)

			if conf, ok := fields[key]; ok {
				transformedVal := conf.xform(val)
				switch conf.op {
				case opEqual:
					clauses = append(clauses, conf.col+" "+negateOp("=", negate)+" ?")
					params = append(params, transformedVal)
				case opComparable:
					clauses = append(clauses, conf.col+" "+negateOp(op, negate)+" ?")
					params = append(params, transformedVal)
				case opLike:
					clauses = append(clauses, conf.col+" "+negateOp("LIKE", negate)+" ?")
					params = append(params, transformedVal)
				}
			} else if key == "f" || key == "format" || key == "legal" {
				formatName := strings.ToLower(val)
				if allowedFormats[formatName] {
					jsonPath := fmt.Sprintf("'$.legalities.%s'", formatName)
					if negate {
						clauses = append(clauses, "json_extract(raw_json, "+jsonPath+") IN ('not_legal', 'banned', 'restricted')")
					} else {
						clauses = append(clauses, "json_extract(raw_json, "+jsonPath+") = 'legal'")
					}
				}
			}
		} else {
			clean := cleanName(token)
			if clean != "" {
				clauses = append(clauses, "name_clean "+negateOp("LIKE", negate)+" ?")
				params = append(params, "%"+clean+"%")
			}
		}
	}

	if len(clauses) > 0 {
		whereSql = " AND " + strings.Join(clauses, " AND ")
	}
	return whereSql, params
}
