# scryfallquery

`scryfallquery` is a standalone, importable Go package that lexes, parses, and compiles Scryfall search syntax into SQL WHERE clauses. It implements a hand-written recursive-descent parser, transforming the input query string into an Abstract Syntax Tree (AST), which is then processed by a compiler.

This architecture decouples query parsing from SQL schema design, making it easy to extend with new filters, operators, or target different database dialects.

## Features

- **Lexer**: Tokenizes bare words, quoted phrases (preserving internal spaces), grouping parentheses, field filters with operators, negations (`-`), and explicit `OR` operators.
- **Parser**: Hand-written recursive-descent parser matching precedence rules (`AND` binding tighter than `OR`, parentheses resetting precedence, negation binding highest).
- **AST**: Logic-oriented Abstract Syntax Tree (`AndNode`, `OrNode`, `NotNode`, `FieldNode`, `NameNode`).
- **Compiler**: Walks the AST to build parameterized SQL WHERE queries. Optimizes negated fields (e.g. `NOT (cmc >= 3)` -> `cmc < 3`) to ensure indexes are utilized by SQLite.
- **Decoupled Fields**: A table-driven field registry (`fields.go`) maps keys (and aliases) to DB columns, default operators, value transformers, and custom dynamic transforms (such as format legality).

## Usage

### Parsing to SQL

To parse and compile a query directly to SQLite WHERE clause syntax:

```go
import "tts-importer-proxy/scryfallquery"

sql, args, err := scryfallquery.Parse("t:legendary (t:goblin OR t:elf)")
// sql  == "json_extract(raw_json, '$.type_line') LIKE ? AND (json_extract(raw_json, '$.type_line') LIKE ? OR json_extract(raw_json, '$.type_line') LIKE ?)"
// args == []any{"%legendary%", "%goblin%", "%elf%"}
```

### Parsing to AST

To inspect the AST before compiling, or to implement a custom compiler:

```go
node, err := scryfallquery.ParseAST("t:legendary")
// node is a FieldNode{Key: "t", Op: ":", Value: "legendary"}
```

---

## Supported Syntax Checklist

### Boolean Operators
- [x] Implicit AND (space-separated terms)
- [x] Explicit OR (case-insensitive `OR`)
- [x] Parenthesized grouping with precedence
- [x] Negation (`-` prefix)

### Field Filters  
- [x] Name (bare word, quoted phrase, name:)
- [x] Type (t:, type:)
- [x] Oracle text (o:, oracle:) including quoted phrases
- [x] Set (s:, set:, e:, edition:)
- [x] Rarity (r:, rarity:)
- [x] Colors (c:, color:)
- [x] Color identity (id:, ci:, identity:)
- [x] Converted Mana Cost / Mana Value (cmc:, mv:)
- [x] Power (pow:, power:)
- [x] Toughness (tou:, toughness:)
- [x] Artist (a:, art:, artist:)
- [x] Language (lang:, l:)
- [x] Format legality (f:, format:, legal:)

### Comparison Operators
- [x] `:` (equals/contains depending on field default)
- [x] `=` (exact equality)
- [x] `!=` (not equal)
- [x] `>` `<` `>=` `<=` (numeric / stat comparison)

### Not Yet Implemented
- [ ] Loyalty (loy:, loyalty:)
- [ ] is: keyword (is:commander, is:reprint, etc.)
- [ ] Regex matching (/pattern/)
- [ ] Mana cost comparison (m:, mana:)
- [ ] Color name aliases (c:blue, id:esper, id:boros)
- [ ] Rarity ordering (r>uncommon)
- [ ] Exact name match (!name, !"exact name")
- [ ] Year filter (year:)
- [ ] Flavor text (ft:, flavor:)
- [ ] Watermark (wm:, watermark:)
- [ ] Frame/layout filters
- [ ] Price filters (usd>, eur>, tix>)
- [ ] order:/unique:/display: (display keywords, not filters)
