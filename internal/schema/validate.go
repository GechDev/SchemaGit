package schema

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

const modelSchema = `{
	"$schema": "https://json-schema.org/draft/2020-12/schema",
	"$id": "https://schemagit.dev/schema/v1.json",
	"title": "SchemaGit Schema Model",
	"type": "object",
	"required": ["namespaces"],
	"additionalProperties": false,
	"properties": {
		"namespaces": {"type": "array", "items": {"$ref": "#/$defs/namespace"}}
	},
	"$defs": {
		"identified": {
			"type": "object",
			"required": ["id", "name"],
			"properties": {"id": {"type": "string", "minLength": 1}, "name": {"type": "string", "minLength": 1}}
		},
		"namespace": {
			"allOf": [
				{"$ref": "#/$defs/identified"},
				{"type": "object", "properties": {
					"tables": {"type": "array", "items": {"$ref": "#/$defs/table"}},
					"views": {"type": "array", "items": {"$ref": "#/$defs/view"}},
					"enums": {"type": "array", "items": {"$ref": "#/$defs/enum"}},
					"sequences": {"type": "array", "items": {"$ref": "#/$defs/sequence"}},
					"functions": {"type": "array", "items": {"$ref": "#/$defs/function"}}
				}}
			],
			"unevaluatedProperties": false
		},
		"table": {
			"allOf": [
				{"$ref": "#/$defs/identified"},
				{"type": "object", "required": ["columns"], "properties": {
					"columns": {"type": "array", "items": {"$ref": "#/$defs/column"}},
					"constraints": {"type": "array", "items": {"$ref": "#/$defs/constraint"}},
					"indexes": {"type": "array", "items": {"$ref": "#/$defs/index"}}
				}}
			],
			"unevaluatedProperties": false
		},
		"column": {"type": "object", "required": ["id", "name", "ordinal", "type", "nullable"], "additionalProperties": false,
			"properties": {
				"id": {"type": "string", "minLength": 1}, "name": {"type": "string", "minLength": 1},
				"ordinal": {"type": "integer", "minimum": 1}, "type": {"type": "string", "minLength": 1},
				"nullable": {"type": "boolean"}, "default": {"type": "string"},
				"is_identity": {"type": "boolean"}, "generated": {"type": "string"}
			}
		},
		"constraint": {"type": "object", "required": ["id", "name", "type"], "additionalProperties": false,
			"properties": {
				"id": {"type": "string", "minLength": 1}, "name": {"type": "string", "minLength": 1},
				"type": {"enum": ["pk", "unique", "check", "fk"]},
				"columns": {"type": "array", "items": {"type": "string"}}, "expression": {"type": "string"},
				"referenced_table": {"type": "string"}, "referenced_columns": {"type": "array", "items": {"type": "string"}}
			}
		},
		"index": {"type": "object", "required": ["id", "name"], "additionalProperties": false,
			"properties": {
				"id": {"type": "string", "minLength": 1}, "name": {"type": "string", "minLength": 1},
				"columns": {"type": "array", "items": {"type": "string"}}, "method": {"type": "string"},
				"unique": {"type": "boolean"}, "predicate": {"type": "string"}
			}
		},
		"view": {"allOf": [{"$ref": "#/$defs/identified"}, {"type": "object", "required": ["definition"], "properties": {
			"definition": {"type": "string"}, "depends_on": {"type": "array", "items": {"type": "string"}}
		}}], "unevaluatedProperties": false},
		"enum": {"allOf": [{"$ref": "#/$defs/identified"}, {"type": "object", "required": ["values"], "properties": {
			"values": {"type": "array", "items": {"type": "string"}}
		}}], "unevaluatedProperties": false},
		"sequence": {"allOf": [{"$ref": "#/$defs/identified"}, {"type": "object", "required": ["data_type", "start", "increment", "min_value", "max_value", "cycle"], "properties": {
			"data_type": {"type": "string"}, "start": {"type": "integer"}, "increment": {"type": "integer"},
			"min_value": {"type": "integer"}, "max_value": {"type": "integer"}, "cycle": {"type": "boolean"}
		}}], "unevaluatedProperties": false},
		"function": {"allOf": [{"$ref": "#/$defs/identified"}, {"type": "object", "required": ["return_type", "language", "definition"], "properties": {
			"arguments": {"type": "array", "items": {"type": "string"}}, "return_type": {"type": "string"},
			"language": {"type": "string"}, "definition": {"type": "string"}
		}}], "unevaluatedProperties": false}
	}
}`

// Validate verifies that s conforms to the published SchemaGit model.
func Validate(s *Schema) error {
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	if err := compiler.AddResource("schemagit.schema.json", bytes.NewReader([]byte(modelSchema))); err != nil {
		return fmt.Errorf("load schema model definition: %w", err)
	}
	compiled, err := compiler.Compile("schemagit.schema.json")
	if err != nil {
		return fmt.Errorf("compile schema model definition: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("decode schema model: %w", err)
	}
	if err := compiled.Validate(value); err != nil {
		return fmt.Errorf("schema model is invalid: %w", err)
	}
	return nil
}
