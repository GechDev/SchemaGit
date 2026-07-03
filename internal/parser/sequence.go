package parser

import (
	"fmt"
	"strconv"

	"github.com/schemagit/schemagit/internal/pgtype"
	"github.com/schemagit/schemagit/internal/schema"
	"github.com/schemagit/schemagit/internal/sqlexpr"
)

// sequenceTypeDefaults holds PostgreSQL's per type sequence bounds.
var sequenceTypeDefaults = map[string]struct {
	min int64
	max int64
}{
	"int8": {1, 9223372036854775807},
	"int4": {1, 2147483647},
	"int2": {1, 32767},
}

func (b *builder) createSequence(cursor *reader) error {
	if err := cursor.expectKeyword("sequence"); err != nil {
		return err
	}
	namespace, name, err := cursor.qualifiedName()
	if err != nil {
		return err
	}
	sequence := schema.Sequence{
		ID:        sequenceID(namespace, name),
		Name:      name,
		DataType:  "int8",
		Start:     1,
		Increment: 1,
		MinValue:  sequenceTypeDefaults["int8"].min,
		MaxValue:  sequenceTypeDefaults["int8"].max,
	}
	for !cursor.done() {
		switch {
		case cursor.acceptWords("as"):
			written, err := cursor.identifier()
			if err != nil {
				return err
			}
			canonical, err := pgtype.FromDDL(written)
			if err != nil {
				return err
			}
			bounds, supported := sequenceTypeDefaults[canonical]
			if !supported {
				return fmt.Errorf("sequence %s has unsupported type %s", sequence.ID, canonical)
			}
			sequence.DataType = canonical
			sequence.MinValue, sequence.MaxValue = bounds.min, bounds.max
		case cursor.acceptWords("increment", "by"), cursor.acceptWords("increment"):
			value, err := readSequenceNumber(cursor)
			if err != nil {
				return err
			}
			sequence.Increment = value
		case cursor.acceptWords("minvalue"):
			value, err := readSequenceNumber(cursor)
			if err != nil {
				return err
			}
			sequence.MinValue = value
		case cursor.acceptWords("maxvalue"):
			value, err := readSequenceNumber(cursor)
			if err != nil {
				return err
			}
			sequence.MaxValue = value
		case cursor.acceptWords("start", "with"), cursor.acceptWords("start"):
			value, err := readSequenceNumber(cursor)
			if err != nil {
				return err
			}
			sequence.Start = value
		case cursor.acceptWords("restart", "with"), cursor.acceptWords("restart"):
			if _, err := readSequenceNumber(cursor); err != nil {
				return err
			}
		case cursor.acceptWords("no"):
			switch {
			case cursor.acceptWords("minvalue"), cursor.acceptWords("maxvalue"),
				cursor.acceptWords("cycle"):
			default:
				return fmt.Errorf("unsupported sequence option near %q", truncate(cursor.statement, 60))
			}
		case cursor.acceptWords("cycle"):
			sequence.Cycle = true
		case cursor.acceptWords("cache"):
			if _, err := readSequenceNumber(cursor); err != nil {
				return err
			}
		case cursor.acceptWords("owned", "by"):
			if _, _, err := cursor.qualifiedName(); err != nil {
				if _, idErr := cursor.identifier(); idErr != nil {
					return idErr
				}
			}
		default:
			return fmt.Errorf("unsupported sequence option near %q", truncate(cursor.statement, 60))
		}
	}
	if sequence.Increment == 0 {
		return fmt.Errorf("sequence %s has INCREMENT 0", sequence.ID)
	}
	target := b.namespace(namespace)
	for _, existing := range target.Sequences {
		if existing.Name == name {
			return fmt.Errorf("sequence %s is defined twice", sequence.ID)
		}
	}
	target.Sequences = append(target.Sequences, sequence)
	return nil
}

func readSequenceNumber(cursor *reader) (int64, error) {
	token := cursor.peek()
	if token.Kind != sqlexpr.KindNumber {
		return 0, fmt.Errorf("expected an integer %s, found %s", cursor.offset(), cursor.describe(token))
	}
	cursor.position++
	value, err := strconv.ParseInt(token.Value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid integer %q %s", token.Value, cursor.offset())
	}
	return value, nil
}
