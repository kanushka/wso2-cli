// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

// Package result declares the typed success a product module returns through
// the module contract.
//
// A result carries semantic values and their presentation order. It carries no
// formatting: the shell alone turns a result into a table or into JSON, so the
// two can never disagree and a module's SDK version cannot change how output
// looks. See docs/adr/0003-shell-owned-output.md.
package result

import "fmt"

// Result is one command's terminal success.
type Result struct {
	// Schema identifies the semantic shape, such as "reference.status/v1".
	// It lets the shell and downstream tools recognize a result without
	// interpreting its fields.
	Schema string
	// Fields are the result's values in presentation order.
	Fields []Field
	// Columns declare what each row carries, in presentation order. They are
	// empty for a result that reports one thing rather than a listing.
	Columns []Column
	// Rows are the listing's items, one per row, each carrying one value per
	// declared column in the same order.
	//
	// Columns and rows are separate because a listing is a table: declaring
	// the columns once is what makes "every row has the same columns" a fact
	// the shell can check, rather than something each row restates and two
	// rows could disagree about.
	Rows []Row
}

// Column declares one column of a listing.
type Column struct {
	// Name is the stable machine name, used as the JSON key of this column's
	// value in every row.
	Name string
	// Label is the human-readable header. It falls back to the name.
	Label string
}

// DisplayLabel is the column's label, or its name when it declares none.
func (c Column) DisplayLabel() string {
	if c.Label != "" {
		return c.Label
	}
	return c.Name
}

// Row is one item of a listing: one value per declared column, in the order
// the columns were declared.
type Row struct {
	// Values are the row's cells. Every value is a string for the same reason
	// a field's is: the shell renders a product's result without knowing
	// anything about it.
	Values []string
}

// Field is one named value of a result.
type Field struct {
	// Name is the stable machine name. The shell uses it as the JSON key, so
	// it must be unique within a result and must not change without a schema
	// change.
	Name string
	// Label is the human-readable label. It may be empty, in which case the
	// name is displayed.
	Label string
	// Value is the value to display.
	//
	// Every value is a string, so a module formats its own times and
	// numbers. That is a deliberate limit of the architecture proof rather
	// than a lasting design: it keeps the shell able to render any product's
	// result without knowing anything about it, and it keeps the wire
	// contract to one shape while the boundaries around it are still being
	// proven. The cost is that type information is lost at the boundary, and
	// two modules can format the same kind of value differently.
	//
	// Giving values their own types — a typed field, or a Protobuf oneof —
	// is the change to make when a product result needs more than strings.
	// It is a protocol change, so it belongs to a slice that can carry one.
	Value string
}

// DisplayLabel reports the label to show a user, falling back to the field's
// machine name so a field is never nameless in output.
func (f Field) DisplayLabel() string {
	if f.Label == "" {
		return f.Name
	}
	return f.Label
}

// New starts a result for the given schema.
func New(schema string) Result {
	return Result{Schema: schema}
}

// With returns a copy of the result with one field appended.
//
// It copies rather than appends in place, so results derived from a shared base
// cannot overwrite one another's fields.
func (r Result) With(name, label, value string) Result {
	fields := make([]Field, len(r.Fields), len(r.Fields)+1)
	copy(fields, r.Fields)
	r.Fields = append(fields, Field{Name: name, Label: label, Value: value})
	return r
}

// Validate reports whether the shell can render the result.
//
// Both sides call it: a module checks what it is about to send, and the shell
// checks what it received, because a result arriving over the contract is a
// peer's claim rather than a trusted value.
// WithColumn declares one column of a listing, after any already declared.
func (r Result) WithColumn(name, label string) Result {
	r.Columns = append(append([]Column(nil), r.Columns...), Column{Name: name, Label: label})
	return r
}

// WithRow adds one item to a listing, carrying one value per declared column.
func (r Result) WithRow(values ...string) Result {
	r.Rows = append(append([]Row(nil), r.Rows...), Row{Values: append([]string(nil), values...)})
	return r
}

func (r Result) Validate() error {
	if r.Schema == "" {
		return fmt.Errorf("result: no schema is declared")
	}
	if len(r.Fields) == 0 {
		return fmt.Errorf("result: schema %q carries no fields", r.Schema)
	}
	seen := make(map[string]struct{}, len(r.Fields))
	for index, field := range r.Fields {
		if field.Name == "" {
			return fmt.Errorf("result: field %d of schema %q has no name", index, r.Schema)
		}
		if _, duplicate := seen[field.Name]; duplicate {
			return fmt.Errorf("result: schema %q declares field %q more than once", r.Schema, field.Name)
		}
		seen[field.Name] = struct{}{}
	}
	return r.validateListing()
}

// validateListing proves a listing is a table: named columns, no duplicates,
// and every row carrying exactly one value per column.
//
// A row that carries the wrong number of values is refused rather than padded
// or trimmed, because either would put a value under a header it does not
// belong to, and the reader has no way to tell that happened.
func (r Result) validateListing() error {
	if len(r.Rows) > 0 && len(r.Columns) == 0 {
		return fmt.Errorf("result: schema %q carries %d rows and declares no columns",
			r.Schema, len(r.Rows))
	}
	seen := make(map[string]struct{}, len(r.Columns))
	for index, column := range r.Columns {
		if column.Name == "" {
			return fmt.Errorf("result: column %d of schema %q has no name", index, r.Schema)
		}
		if _, duplicate := seen[column.Name]; duplicate {
			return fmt.Errorf("result: schema %q declares column %q more than once",
				r.Schema, column.Name)
		}
		seen[column.Name] = struct{}{}
	}
	for index, row := range r.Rows {
		if len(row.Values) != len(r.Columns) {
			return fmt.Errorf("result: row %d of schema %q carries %d values for %d columns",
				index, r.Schema, len(row.Values), len(r.Columns))
		}
	}
	return nil
}
