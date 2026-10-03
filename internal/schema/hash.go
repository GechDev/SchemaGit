package schema

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// Hash returns the SHA-256 hash of the schema's canonical JSON representation.
func Hash(s *Schema) string {
	data, err := CanonicalJSON(s)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// CanonicalJSON marshals a copy of s with every AST-node slice sorted by ID.
func CanonicalJSON(s *Schema) ([]byte, error) {
	var normalized Schema
	data, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &normalized); err != nil {
		return nil, err
	}
	normalize(&normalized)
	return json.Marshal(&normalized)
}

func normalize(s *Schema) {
	sort.Slice(s.Namespaces, func(i, j int) bool { return s.Namespaces[i].ID < s.Namespaces[j].ID })
	for namespaceIndex := range s.Namespaces {
		namespace := &s.Namespaces[namespaceIndex]
		sort.Slice(namespace.Tables, func(i, j int) bool { return namespace.Tables[i].ID < namespace.Tables[j].ID })
		sort.Slice(namespace.Views, func(i, j int) bool { return namespace.Views[i].ID < namespace.Views[j].ID })
		sort.Slice(namespace.Enums, func(i, j int) bool { return namespace.Enums[i].ID < namespace.Enums[j].ID })
		sort.Slice(namespace.Sequences, func(i, j int) bool { return namespace.Sequences[i].ID < namespace.Sequences[j].ID })
		sort.Slice(namespace.Functions, func(i, j int) bool { return namespace.Functions[i].ID < namespace.Functions[j].ID })
		for tableIndex := range namespace.Tables {
			table := &namespace.Tables[tableIndex]
			sort.Slice(table.Columns, func(i, j int) bool { return table.Columns[i].ID < table.Columns[j].ID })
			sort.Slice(table.Constraints, func(i, j int) bool { return table.Constraints[i].ID < table.Constraints[j].ID })
			sort.Slice(table.Indexes, func(i, j int) bool { return table.Indexes[i].ID < table.Indexes[j].ID })
		}
	}
}
