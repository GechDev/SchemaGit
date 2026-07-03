package parser

import (
	"sort"
	"strings"

	"github.com/schemagit/schemagit/internal/schema"
)

// sortNamespace orders every AST slice by ID so hashing is deterministic.
func sortNamespace(namespace *schema.Namespace) {
	byID := func(left, right string) bool { return left < right }
	sort.Slice(namespace.Tables, func(i, j int) bool { return byID(namespace.Tables[i].ID, namespace.Tables[j].ID) })
	sort.Slice(namespace.Views, func(i, j int) bool { return byID(namespace.Views[i].ID, namespace.Views[j].ID) })
	sort.Slice(namespace.Enums, func(i, j int) bool { return byID(namespace.Enums[i].ID, namespace.Enums[j].ID) })
	sort.Slice(namespace.Sequences, func(i, j int) bool {
		return byID(namespace.Sequences[i].ID, namespace.Sequences[j].ID)
	})
	sort.Slice(namespace.Functions, func(i, j int) bool {
		return byID(namespace.Functions[i].ID, namespace.Functions[j].ID)
	})
	for index := range namespace.Tables {
		table := &namespace.Tables[index]
		sortColumns(table)
		sort.Slice(table.Constraints, func(i, j int) bool { return byID(table.Constraints[i].ID, table.Constraints[j].ID) })
		sort.Slice(table.Indexes, func(i, j int) bool { return byID(table.Indexes[i].ID, table.Indexes[j].ID) })
	}
}

// sortColumns orders columns by ID while keeping ordinals dense and ascending,
// which keeps an added-then-dropped column from changing the hash of the table.
func sortColumns(table *schema.Table) {
	sort.Slice(table.Columns, func(i, j int) bool { return table.Columns[i].ID < table.Columns[j].ID })
	for index := range table.Columns {
		table.Columns[index].Ordinal = index + 1
	}
}

func findTable(namespace *schema.Namespace, name string) *schema.Table {
	for index := range namespace.Tables {
		if namespace.Tables[index].Name == name {
			return &namespace.Tables[index]
		}
	}
	return nil
}

func findView(namespace *schema.Namespace, name string) *schema.View {
	for index := range namespace.Views {
		if namespace.Views[index].Name == name {
			return &namespace.Views[index]
		}
	}
	return nil
}

func columnExists(table *schema.Table, name string) bool {
	for _, column := range table.Columns {
		if column.Name == name {
			return true
		}
	}
	return false
}

func qualified(namespace, name string) string {
	if namespace == "" {
		namespace = "public"
	}
	return namespace + "." + name
}

func tableID(namespace, name string) string { return "tbl:" + qualified(namespace, name) }
func columnID(namespace, table, name string) string {
	return "col:" + qualified(namespace, table) + "." + name
}
func constraintID(namespace, table, name string) string {
	return "con:" + qualified(namespace, table) + "." + name
}
func indexID(namespace, table, name string) string {
	return "idx:" + qualified(namespace, table) + "." + name
}
func viewID(namespace, name string) string { return "view:" + qualified(namespace, name) }
func enumID(namespace, name string) string { return "enum:" + qualified(namespace, name) }
func sequenceID(namespace, name string) string {
	return "seq:" + qualified(namespace, name)
}

func lowerName(value string) string { return strings.ToLower(value) }
