package scryfallquery

import (
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

type FieldDef struct {
	Column     string
	DefaultOp  string // "=", "LIKE", or "" (meaning "use query op")
	ValueXform func(string) string
	Transform  func(op, value string, negated bool) (string, []any, error)
}

var AllowedFormats = map[string]bool{
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

func IsAlphanumeric(s string) bool {
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return s != ""
}

func CleanName(name string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	normalized, _, err := transform.String(t, name)
	if err != nil {
		normalized = name
	}

	var sb strings.Builder
	for _, r := range normalized {
		if r >= 'A' && r <= 'Z' {
			r = r + ('a' - 'A')
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func identity(s string) string      { return s }
func wildcard(s string) string      { return "%" + s + "%" }
func wildcardLower(s string) string { return "%" + strings.ToLower(s) + "%" }
func wildcardUpper(s string) string { return "%" + strings.ToUpper(s) + "%" }

// DefaultFields contains the field mapping matching the proxy database schema.
var DefaultFields = map[string]FieldDef{
	"s":        {Column: "set_code", DefaultOp: "=", ValueXform: strings.ToLower},
	"set":      {Column: "set_code", DefaultOp: "=", ValueXform: strings.ToLower},
	"r":        {Column: "json_extract(raw_json, '$.rarity')", DefaultOp: "=", ValueXform: strings.ToLower},
	"rarity":   {Column: "json_extract(raw_json, '$.rarity')", DefaultOp: "=", ValueXform: strings.ToLower},
	"t":        {Column: "json_extract(raw_json, '$.type_line')", DefaultOp: "LIKE", ValueXform: wildcardLower},
	"type":     {Column: "json_extract(raw_json, '$.type_line')", DefaultOp: "LIKE", ValueXform: wildcardLower},
	"c":        {Column: "json_extract(raw_json, '$.colors')", DefaultOp: "LIKE", ValueXform: wildcardUpper},
	"color":    {Column: "json_extract(raw_json, '$.colors')", DefaultOp: "LIKE", ValueXform: wildcardUpper},
	"id":       {Column: "json_extract(raw_json, '$.color_identity')", DefaultOp: "LIKE", ValueXform: wildcardUpper},
	"ci":       {Column: "json_extract(raw_json, '$.color_identity')", DefaultOp: "LIKE", ValueXform: wildcardUpper},
	"identity": {Column: "json_extract(raw_json, '$.color_identity')", DefaultOp: "LIKE", ValueXform: wildcardUpper},
	"cmc":      {Column: "CAST(json_extract(raw_json, '$.cmc') AS REAL)", DefaultOp: "", ValueXform: identity},
	"mv":       {Column: "CAST(json_extract(raw_json, '$.cmc') AS REAL)", DefaultOp: "", ValueXform: identity},
	"pow":      {Column: "json_extract(raw_json, '$.power')", DefaultOp: "", ValueXform: identity},
	"power":    {Column: "json_extract(raw_json, '$.power')", DefaultOp: "", ValueXform: identity},
	"tou":      {Column: "json_extract(raw_json, '$.toughness')", DefaultOp: "", ValueXform: identity},
	"toughness": {Column: "json_extract(raw_json, '$.toughness')", DefaultOp: "", ValueXform: identity},
	"o":        {Column: "json_extract(raw_json, '$.oracle_text')", DefaultOp: "LIKE", ValueXform: wildcard},
	"oracle":   {Column: "json_extract(raw_json, '$.oracle_text')", DefaultOp: "LIKE", ValueXform: wildcard},
	"a":        {Column: "json_extract(raw_json, '$.artist')", DefaultOp: "LIKE", ValueXform: wildcard},
	"art":      {Column: "json_extract(raw_json, '$.artist')", DefaultOp: "LIKE", ValueXform: wildcard},
	"artist":   {Column: "json_extract(raw_json, '$.artist')", DefaultOp: "LIKE", ValueXform: wildcard},
	"lang":     {Column: "json_extract(raw_json, '$.lang')", DefaultOp: "=", ValueXform: strings.ToLower},
	"l":        {Column: "json_extract(raw_json, '$.lang')", DefaultOp: "=", ValueXform: strings.ToLower},

	// Special dynamic format checks
	"f":      {Transform: formatLegalityTransform},
	"format": {Transform: formatLegalityTransform},
	"legal":  {Transform: formatLegalityTransform},
}

func formatLegalityTransform(op, value string, negated bool) (string, []any, error) {
	formatName := strings.ToLower(value)
	if !AllowedFormats[formatName] || !IsAlphanumeric(formatName) {
		return "", nil, nil
	}
	jsonPath := fmt.Sprintf("'$.legalities.%s'", formatName)
	isNegated := negated != (op == "!=")
	if isNegated {
		return "json_extract(raw_json, " + jsonPath + ") IN ('not_legal', 'banned', 'restricted')", nil, nil
	}
	return "json_extract(raw_json, " + jsonPath + ") = 'legal'", nil, nil
}
