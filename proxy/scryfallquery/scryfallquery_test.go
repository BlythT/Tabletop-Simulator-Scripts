package scryfallquery

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		query    string
		wantSql  string
		wantArgs []any
		wantErr  bool
	}{
		{
			query:    "set:kld r:common",
			wantSql:  "set_code = ? AND json_extract(raw_json, '$.rarity') = ?",
			wantArgs: []any{"kld", "common"},
		},
		{
			query:    "-t:basic s:kld",
			wantSql:  "json_extract(raw_json, '$.type_line') NOT LIKE ? AND set_code = ?",
			wantArgs: []any{"%basic%", "kld"},
		},
		{
			query:    "lightning bolt",
			wantSql:  "name_clean LIKE ? AND name_clean LIKE ?",
			wantArgs: []any{"%lightning%", "%bolt%"},
		},
		{
			query:    "t:legendary (t:goblin OR t:elf)",
			wantSql:  "json_extract(raw_json, '$.type_line') LIKE ? AND (json_extract(raw_json, '$.type_line') LIKE ? OR json_extract(raw_json, '$.type_line') LIKE ?)",
			wantArgs: []any{"%legendary%", "%goblin%", "%elf%"},
		},
		{
			query:    "-(s:kld r:rare)",
			wantSql:  "NOT (set_code = ? AND json_extract(raw_json, '$.rarity') = ?)",
			wantArgs: []any{"kld", "rare"},
		},
		{
			query:    "Sméagol",
			wantSql:  "name_clean LIKE ?",
			wantArgs: []any{"%smeagol%"},
		},
		{
			query:    "Tura Kennerüd",
			wantSql:  "name_clean LIKE ? AND name_clean LIKE ?",
			wantArgs: []any{"%tura%", "%kennerud%"},
		},
		{
			query:    "",
			wantSql:  "",
			wantArgs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			gotSql, gotArgs, err := Parse(tt.query)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parse() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if gotSql != tt.wantSql {
				t.Errorf("Parse() gotSql = %q, want %q", gotSql, tt.wantSql)
			}
			if !reflect.DeepEqual(gotArgs, tt.wantArgs) {
				t.Errorf("Parse() gotArgs = %v, want %v", gotArgs, tt.wantArgs)
			}
		})
	}
}
