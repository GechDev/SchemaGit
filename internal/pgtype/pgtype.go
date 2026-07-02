// Package pgtype translates PostgreSQL type names between the server's
// display form (pg_catalog.format_type) and SchemaGit's canonical internal
// form. Both the DDL parser and the PostgreSQL introspection adapter depend on
// this package so that a schema parsed from DDL and the same schema read back
// from the catalog produce identical hashes.
package pgtype

import (
	"fmt"
	"strings"
)

// DefaultNamespace is assumed when a type name is not schema qualified.
const DefaultNamespace = "public"

var canonicalNames = map[string]string{
	"bigint":                      "int8",
	"integer":                     "int4",
	"smallint":                    "int2",
	"boolean":                     "bool",
	"text":                        "text",
	"character varying":           "varchar",
	"character":                   "bpchar",
	"numeric":                     "numeric",
	"decimal":                     "numeric",
	"real":                        "float4",
	"double precision":            "float8",
	"date":                        "date",
	"timestamp without time zone": "timestamp",
	"timestamp with time zone":    "timestamptz",
	"time without time zone":      "time",
	"time with time zone":         "timetz",
	"interval":                    "interval",
	"uuid":                        "uuid",
	"json":                        "json",
	"jsonb":                       "jsonb",
	"bytea":                       "bytea",
	"money":                       "money",
	"inet":                        "inet",
	"cidr":                        "cidr",
	"macaddr":                     "macaddr",
	"macaddr8":                    "macaddr8",
	"oid":                         "oid",
	"name":                        "name",
	"xml":                         "xml",
	"tsvector":                    "tsvector",
	"tsquery":                     "tsquery",
	"bit":                         "bit",
	"bit varying":                 "varbit",
	"pg_lsn":                      "pg_lsn",
	"txid_snapshot":               "txid_snapshot",
	"pg_snapshot":                 "pg_snapshot",
	"point":                       "point",
	"line":                        "line",
	"lseg":                        "lseg",
	"box":                         "box",
	"path":                        "path",
	"polygon":                     "polygon",
	"circle":                      "circle",
	"int4range":                   "int4range",
	"int8range":                   "int8range",
	"numrange":                    "numrange",
	"tsrange":                     "tsrange",
	"tstzrange":                   "tstzrange",
	"daterange":                   "daterange",
	"int4multirange":              "int4multirange",
	"int8multirange":              "int8multirange",
	"nummultirange":               "nummultirange",
	"tsmultirange":                "tsmultirange",
	"tstzmultirange":              "tstzmultirange",
	"datemultirange":              "datemultirange",
}

var displayNames = map[string]string{
	"int8":        "bigint",
	"int4":        "integer",
	"int2":        "smallint",
	"bool":        "boolean",
	"text":        "text",
	"varchar":     "character varying",
	"bpchar":      "character",
	"numeric":     "numeric",
	"float4":      "real",
	"float8":      "double precision",
	"date":        "date",
	"timestamp":   "timestamp without time zone",
	"timestamptz": "timestamp with time zone",
	"time":        "time without time zone",
	"timetz":      "time with time zone",
}

// alternate spellings accepted by the DDL parser
var parserAliases = map[string]string{
	"int":         "int4",
	"integer":     "int4",
	"int4":        "int4",
	"int2":        "int2",
	"int8":        "int8",
	"bigint":      "int8",
	"smallint":    "int2",
	"bool":        "bool",
	"boolean":     "bool",
	"text":        "text",
	"varchar":     "varchar",
	"char":        "bpchar",
	"bpchar":      "bpchar",
	"decimal":     "numeric",
	"numeric":     "numeric",
	"float":       "float8",
	"float4":      "float4",
	"float8":      "float8",
	"double":      "float8",
	"real":        "float4",
	"timestamptz": "timestamptz",
	"timetz":      "timetz",
}

// IsBuiltin reports whether canonical names a PostgreSQL built-in type.
func IsBuiltin(canonical string) bool {
	base := Base(canonical)
	_, ok := displayNames[base]
	return ok
}

// Base strips array markers and type modifiers, returning the bare type name.
func Base(canonical string) string {
	name := canonical
	if index := strings.Index(name, "["); index >= 0 {
		name = name[:index]
	}
	if index := strings.Index(name, "("); index >= 0 {
		name = name[:index]
	}
	return strings.TrimSpace(name)
}

// Modifiers returns the parenthesised modifier list of canonical, if present.
func Modifiers(canonical string) string {
	if index := strings.Index(canonical, "("); index >= 0 && strings.HasSuffix(canonical, ")") {
		return canonical[index:]
	}
	return ""
}

// IsArray reports whether canonical denotes an array type.
func IsArray(canonical string) bool {
	return strings.Contains(canonical, "[]")
}

// Element returns the array element type of canonical.
func Element(canonical string) (string, error) {
	if !IsArray(canonical) {
		return "", fmt.Errorf("type %q is not an array", canonical)
	}
	element := strings.ReplaceAll(canonical, "[]", "")
	element = strings.ReplaceAll(element, " ", "")
	if element == "" {
		return "", fmt.Errorf("array type %q has no element type", canonical)
	}
	return element, nil
}

// Array returns the array type of canonical.
func Array(canonical string) string {
	return strings.ReplaceAll(canonical, "[]", "") + "[]"
}

// FromDDL normalises a type name as written in a CREATE statement. Schema
// qualification is dropped, aliases are resolved and modifiers are preserved.
func FromDDL(written string) (string, error) {
	name := strings.TrimSpace(written)
	if name == "" {
		return "", fmt.Errorf("empty PostgreSQL type name")
	}
	arrayCount := 0
	for {
		trimmed := strings.TrimSpace(name)
		if !strings.HasSuffix(trimmed, "[]") {
			break
		}
		name = trimmed[:len(trimmed)-2]
		arrayCount++
	}
	name = strings.TrimSpace(name)
	modifiers := ""
	if index := strings.Index(name, "("); index >= 0 {
		if !strings.HasSuffix(name, ")") {
			return "", fmt.Errorf("unterminated type modifier in %q", written)
		}
		modifiers = "(" + strings.ReplaceAll(name[index+1:len(name)-1], " ", "") + ")"
		name = name[:index]
	}
	name = Unqualify(name)
	if name == "" {
		return "", fmt.Errorf("empty PostgreSQL type name in %q", written)
	}
	base := name
	if alias, ok := parserAliases[name]; ok {
		base = alias
	} else if canonical, ok := canonicalNames[name]; ok {
		base = canonical
	}
	canonical := base + modifiers
	for index := 0; index < arrayCount; index++ {
		canonical += "[]"
	}
	return canonical, nil
}

// Unqualify reduces a possibly schema qualified name to its last component.
func Unqualify(name string) string {
	trimmed := strings.TrimSpace(name)
	trimmed = strings.Trim(trimmed, `"`)
	if index := strings.LastIndex(trimmed, "."); index >= 0 {
		return trimmed[index+1:]
	}
	return trimmed
}

// FromCatalog normalises a type name as reported by pg_catalog.format_type.
func FromCatalog(display string) (string, error) {
	name := strings.Join(strings.Fields(display), " ")
	name = strings.TrimSuffix(name, "[]")
	modifiers := ""
	if index := strings.Index(name, "("); index >= 0 {
		if !strings.HasSuffix(name, ")") {
			return "", fmt.Errorf("unterminated type modifier in %q", display)
		}
		modifiers = "(" + strings.ReplaceAll(name[index+1:len(name)-1], " ", "") + ")"
		name = name[:index]
	}
	isArray := strings.HasSuffix(display, "[]")
	name = Unqualify(name)
	if name == "" {
		return "", fmt.Errorf("empty PostgreSQL type name in %q", display)
	}
	base := name
	if canonical, ok := canonicalNames[name]; ok {
		base = canonical
	} else if alias, ok := parserAliases[name]; ok {
		base = alias
	}
	canonical := base + modifiers
	if isArray {
		canonical += "[]"
	}
	return canonical, nil
}

// ToCatalog renders canonical in the form pg_catalog.format_type returns, which
// is what the PostgreSQL server stores in expression casts.
func ToCatalog(canonical string) string {
	base := Base(canonical)
	modifiers := Modifiers(canonical)
	display, ok := displayNames[base]
	if !ok {
		display = base
	}
	rendered := display + modifiers
	if IsArray(canonical) {
		rendered += "[]"
	}
	return rendered
}
