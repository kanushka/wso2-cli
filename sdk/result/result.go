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
	// Value is the rendered value. The architecture proof carries strings
	// only, so a module formats times and numbers itself.
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
	return nil
}
