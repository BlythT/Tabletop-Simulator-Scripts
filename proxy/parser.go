package main

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

type fieldConfig struct {
	col       string
	defaultOp string // "=", "LIKE", or "" (meaning "use comparison operator")
	xform     func(string) string
}

var (
	rxFilter = regexp.MustCompile(`(?i)^([a-zA-Z]+)(!=|<=|>=|[:=<>])(.+)$`)

	identity      = func(s string) string { return s }
	wildcard      = func(s string) string { return "%" + s + "%" }
	wildcardLower = func(s string) string { return "%" + strings.ToLower(s) + "%" }
	wildcardUpper = func(s string) string { return "%" + strings.ToUpper(s) + "%" }

	fields = map[string]fieldConfig{
		"s":        {col: "set_code", defaultOp: "=", xform: strings.ToLower},
		"set":      {col: "set_code", defaultOp: "=", xform: strings.ToLower},
		"r":        {col: "json_extract(raw_json, '$.rarity')", defaultOp: "=", xform: strings.ToLower},
		"rarity":   {col: "json_extract(raw_json, '$.rarity')", defaultOp: "=", xform: strings.ToLower},
		"t":        {col: "json_extract(raw_json, '$.type_line')", defaultOp: "LIKE", xform: wildcardLower},
		"type":     {col: "json_extract(raw_json, '$.type_line')", defaultOp: "LIKE", xform: wildcardLower},
		"c":        {col: "json_extract(raw_json, '$.colors')", defaultOp: "LIKE", xform: wildcardUpper},
		"color":    {col: "json_extract(raw_json, '$.colors')", defaultOp: "LIKE", xform: wildcardUpper},
		
		// Note: Scryfall uses 'id' / 'identity' / 'ci' for color identity (not card UUID).
		// Card UUID lookup in Tabletop Simulator is executed directly by Scryfall card ID paths.
		"id":       {col: "json_extract(raw_json, '$.color_identity')", defaultOp: "LIKE", xform: wildcardUpper},
		"ci":       {col: "json_extract(raw_json, '$.color_identity')", defaultOp: "LIKE", xform: wildcardUpper},
		"identity": {col: "json_extract(raw_json, '$.color_identity')", defaultOp: "LIKE", xform: wildcardUpper},
		
		"cmc":      {col: "CAST(json_extract(raw_json, '$.cmc') AS REAL)", defaultOp: "", xform: identity},
		"mv":       {col: "CAST(json_extract(raw_json, '$.cmc') AS REAL)", defaultOp: "", xform: identity},
		"pow":      {col: "json_extract(raw_json, '$.power')", defaultOp: "", xform: identity},
		"power":    {col: "json_extract(raw_json, '$.power')", defaultOp: "", xform: identity},
		"tou":      {col: "json_extract(raw_json, '$.toughness')", defaultOp: "", xform: identity},
		"toughness": {col: "json_extract(raw_json, '$.toughness')", defaultOp: "", xform: identity},
		"o":        {col: "json_extract(raw_json, '$.oracle_text')", defaultOp: "LIKE", xform: wildcard},
		"oracle":   {col: "json_extract(raw_json, '$.oracle_text')", defaultOp: "LIKE", xform: wildcard},
		"a":        {col: "json_extract(raw_json, '$.artist')", defaultOp: "LIKE", xform: wildcard},
		"art":      {col: "json_extract(raw_json, '$.artist')", defaultOp: "LIKE", xform: wildcard},
		"artist":   {col: "json_extract(raw_json, '$.artist')", defaultOp: "LIKE", xform: wildcard},
		"lang":     {col: "json_extract(raw_json, '$.lang')", defaultOp: "=", xform: strings.ToLower},
		"l":        {col: "json_extract(raw_json, '$.lang')", defaultOp: "=", xform: strings.ToLower},
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

func isAlphanumeric(s string) bool {
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return s != ""
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
	if op == ":" {
		return "="
	}
	return op
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
				sqlOp := conf.defaultOp
				if sqlOp == "" {
					sqlOp = op
				}
				clauses = append(clauses, conf.col+" "+negateOp(sqlOp, negate)+" ?")
				params = append(params, transformedVal)
			} else if key == "f" || key == "format" || key == "legal" {
				formatName := strings.ToLower(val)
				if allowedFormats[formatName] && isAlphanumeric(formatName) {
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
