package main

const (
	QueryGetByID = "SELECT raw_json FROM cards WHERE id = ? LIMIT 1"

	QueryGetByNamedExactSet  = "SELECT raw_json FROM cards WHERE name_clean = ? AND set_code = ? LIMIT 1"
	QueryGetByNamedExact     = "SELECT raw_json FROM cards WHERE name_clean = ? LIMIT 1"
	QueryGetByNamedPrefixSet = "SELECT raw_json FROM cards WHERE name_clean LIKE ? AND set_code = ? LIMIT 1"
	QueryGetByNamedPrefix    = "SELECT raw_json FROM cards WHERE name_clean LIKE ? LIMIT 1"

	QueryGetBySetColLang = "SELECT raw_json FROM cards WHERE set_code = ? AND collector_number = ? AND lang = ? LIMIT 1"
	QueryGetBySetCol     = "SELECT raw_json FROM cards WHERE set_code = ? AND collector_number = ? LIMIT 1"

	QueryGetRandomNoFilters = `SELECT raw_json 
				FROM cards, 
				     (SELECT (ABS(RANDOM()) % (SELECT COALESCE(MAX(rowid), 1) FROM cards)) + 1 AS rand_id) 
				WHERE rowid >= rand_id 
				LIMIT 1`

	QueryBaseSelect = "SELECT raw_json FROM cards WHERE 1=1"
)
