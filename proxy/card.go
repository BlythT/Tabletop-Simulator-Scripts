package main

import "encoding/json"

// IngestionCard represents the minimal card structure parsed during database population.
type IngestionCard struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Set             string          `json:"set"`
	CollectorNumber string          `json:"collector_number"`
	Lang            string          `json:"lang"`
	RawJSON         json.RawMessage `json:"-"` // Stored directly as raw bytes
}

// UnmarshalJSON captures both the structured fields and the raw card JSON bytes.
func (c *IngestionCard) UnmarshalJSON(data []byte) error {
	c.RawJSON = make([]byte, len(data))
	copy(c.RawJSON, data)

	type Alias IngestionCard
	var aux Alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	c.ID = aux.ID
	c.Name = aux.Name
	c.Set = aux.Set
	c.CollectorNumber = aux.CollectorNumber
	c.Lang = aux.Lang
	return nil
}
