package schema

// Schema is the canonical, serializable representation of a database schema.
type Schema struct {
	Namespaces []Namespace `json:"namespaces" yaml:"namespaces"`
}

type Namespace struct {
	ID        string     `json:"id" yaml:"id"`
	Name      string     `json:"name" yaml:"name"`
	Tables    []Table    `json:"tables,omitempty" yaml:"tables,omitempty"`
	Views     []View     `json:"views,omitempty" yaml:"views,omitempty"`
	Enums     []Enum     `json:"enums,omitempty" yaml:"enums,omitempty"`
	Sequences []Sequence `json:"sequences,omitempty" yaml:"sequences,omitempty"`
	Functions []Function `json:"functions,omitempty" yaml:"functions,omitempty"`
}

type Table struct {
	ID          string       `json:"id" yaml:"id"`
	Name        string       `json:"name" yaml:"name"`
	Columns     []Column     `json:"columns" yaml:"columns"`
	Constraints []Constraint `json:"constraints,omitempty" yaml:"constraints,omitempty"`
	Indexes     []Index      `json:"indexes,omitempty" yaml:"indexes,omitempty"`
}

type Column struct {
	ID         string `json:"id" yaml:"id"`
	Name       string `json:"name" yaml:"name"`
	Ordinal    int    `json:"ordinal" yaml:"ordinal"`
	Type       string `json:"type" yaml:"type"`
	Nullable   bool   `json:"nullable" yaml:"nullable"`
	Default    string `json:"default,omitempty" yaml:"default,omitempty"`
	IsIdentity bool   `json:"is_identity,omitempty" yaml:"is_identity,omitempty"`
	Generated  string `json:"generated,omitempty" yaml:"generated,omitempty"`
}

type Constraint struct {
	ID              string   `json:"id" yaml:"id"`
	Name            string   `json:"name" yaml:"name"`
	Type            string   `json:"type" yaml:"type"`
	Columns         []string `json:"columns,omitempty" yaml:"columns,omitempty"`
	Expression      string   `json:"expression,omitempty" yaml:"expression,omitempty"`
	ReferencedTable string   `json:"referenced_table,omitempty" yaml:"referenced_table,omitempty"`
	ReferencedCols  []string `json:"referenced_columns,omitempty" yaml:"referenced_columns,omitempty"`
}

type Index struct {
	ID        string   `json:"id" yaml:"id"`
	Name      string   `json:"name" yaml:"name"`
	Columns   []string `json:"columns,omitempty" yaml:"columns,omitempty"`
	Method    string   `json:"method,omitempty" yaml:"method,omitempty"`
	Unique    bool     `json:"unique,omitempty" yaml:"unique,omitempty"`
	Predicate string   `json:"predicate,omitempty" yaml:"predicate,omitempty"`
}

type View struct {
	ID         string   `json:"id" yaml:"id"`
	Name       string   `json:"name" yaml:"name"`
	Definition string   `json:"definition" yaml:"definition"`
	DependsOn  []string `json:"depends_on,omitempty" yaml:"depends_on,omitempty"`
}

type Enum struct {
	ID     string   `json:"id" yaml:"id"`
	Name   string   `json:"name" yaml:"name"`
	Values []string `json:"values" yaml:"values"`
}

type Sequence struct {
	ID        string `json:"id" yaml:"id"`
	Name      string `json:"name" yaml:"name"`
	DataType  string `json:"data_type" yaml:"data_type"`
	Start     int64  `json:"start" yaml:"start"`
	Increment int64  `json:"increment" yaml:"increment"`
	MinValue  int64  `json:"min_value" yaml:"min_value"`
	MaxValue  int64  `json:"max_value" yaml:"max_value"`
	Cycle     bool   `json:"cycle" yaml:"cycle"`
}

type Function struct {
	ID         string   `json:"id" yaml:"id"`
	Name       string   `json:"name" yaml:"name"`
	Arguments  []string `json:"arguments,omitempty" yaml:"arguments,omitempty"`
	ReturnType string   `json:"return_type" yaml:"return_type"`
	Language   string   `json:"language" yaml:"language"`
	Definition string   `json:"definition" yaml:"definition"`
}
