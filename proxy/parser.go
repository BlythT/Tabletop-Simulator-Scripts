package main

import (
	"net/url"
	"strings"

	"tts-importer-proxy/scryfallquery"
)

func parseQuery(q string) (whereSql string, params []any) {
	uDec, err := url.QueryUnescape(q)
	if err == nil {
		q = uDec
	}

	q = strings.ReplaceAll(q, "+", " ")
	sql, args, err := scryfallquery.Parse(q)
	if err != nil || sql == "" {
		return "", nil
	}

	return " AND " + sql, args
}
