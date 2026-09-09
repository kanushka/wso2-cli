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

package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/wso2/wso2-cli/sdk/result"
)

// Mode is a user-facing rendering of a command result.
type Mode string

const (
	// ModeTable is the readable default.
	ModeTable Mode = "table"
	// ModeJSON is deterministic machine output.
	ModeJSON Mode = "json"
)

// Modes are the renderings this shell supports, in the order they are offered
// to a user.
func Modes() []Mode {
	return []Mode{ModeTable, ModeJSON}
}

// ParseMode reads an --output value.
func ParseMode(value string) (Mode, bool) {
	for _, mode := range Modes() {
		if string(mode) == value {
			return mode, true
		}
	}
	return "", false
}

// Result renders a module result in the given mode.
//
// Both renderings are driven by the same ordered fields, so the table and the
// JSON object can never disagree about what the command found. See
// docs/adr/0003-shell-owned-output.md.
func Result(w io.Writer, mode Mode, produced result.Result) error {
	switch mode {
	case ModeJSON:
		return resultJSON(w, produced)
	default:
		return resultTable(w, produced)
	}
}

// resultTable renders the result as a header row and one value row.
// NextField names the one field a table does not hold as a column. A module
// says what a user most likely runs next in a field named next; a full command
// there would stretch every column, so table mode prints it as a line after
// the table, and JSON keeps it as an ordinary member a script can ignore.
const NextField = "next"

func resultTable(w io.Writer, produced result.Result) error {
	if len(produced.Rows) > 0 {
		return listingTable(w, produced)
	}
	headers := make([]string, 0, len(produced.Fields))
	values := make([]string, 0, len(produced.Fields))
	next := ""
	for _, field := range produced.Fields {
		if field.Name == NextField {
			next = field.Value
			continue
		}
		headers = append(headers, field.DisplayLabel())
		values = append(values, field.Value)
	}
	if len(headers) > 0 {
		table := NewTable(headers...)
		table.Append(values...)
		if err := table.Render(w); err != nil {
			return err
		}
	}
	return nextLine(w, next)
}

// listingTable renders a result that carries rows: the fields first as
// "label value" lines, then the rows as the table, then the next line.
//
// The fields cannot be the table's header row here, the way they are for a
// result that reports one thing: a listing's fields describe the listing —
// how many there are, what was filtered — while its rows are the answer, and
// a renderer that put both in one table would be claiming they are the same
// kind of thing. They are rendered exactly as the shell renders its own
// commands' summary lines, so a product's listing reads like wso2 account
// list rather than like a second dialect.
func listingTable(w io.Writer, produced result.Result) error {
	next := ""
	summary := make([][2]string, 0, len(produced.Fields))
	for _, field := range produced.Fields {
		if field.Name == NextField {
			next = field.Value
			continue
		}
		summary = append(summary, [2]string{field.DisplayLabel(), field.Value})
	}
	if len(summary) > 0 {
		if err := Fields(w, summary); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	headers := make([]string, 0, len(produced.Columns))
	for _, column := range produced.Columns {
		headers = append(headers, column.DisplayLabel())
	}
	table := NewTable(headers...)
	for _, row := range produced.Rows {
		table.Append(row.Values...)
	}
	if err := table.Render(w); err != nil {
		return err
	}
	return nextLine(w, next)
}

// nextLine writes the trailing next-step line, or nothing when there is none.
func nextLine(w io.Writer, next string) error {
	if next == "" {
		return nil
	}
	_, err := fmt.Fprintf(w, "\nNext  %s\n", next)
	return err
}

// encodeRows renders a listing's rows as an array of objects keyed by column
// name.
//
// The key is the column's machine name and never its label: the label exists
// for a person reading a table and may be reworded without a schema change,
// while the name is what a script depends on. Encoding through the standard
// marshaller is what keeps a value carrying a quote or a newline from breaking
// the document, the same property the field loop relies on.
func encodeRows(produced result.Result) ([]byte, error) {
	rows := make([]map[string]string, 0, len(produced.Rows))
	for _, row := range produced.Rows {
		object := make(map[string]string, len(produced.Columns))
		for index, column := range produced.Columns {
			// Validate has already proved every row carries one value per
			// column, so the index is in range for any result the shell agreed
			// to render.
			object[column.Name] = row.Values[index]
		}
		rows = append(rows, object)
	}
	encoded, err := json.MarshalIndent(rows, "  ", "  ")
	if err != nil {
		return nil, fmt.Errorf("output: cannot encode the rows of schema %q: %w", produced.Schema, err)
	}
	return encoded, nil
}

func resultJSON(w io.Writer, produced result.Result) error {
	var document bytes.Buffer
	document.WriteString("{\n")
	for index, field := range produced.Fields {
		name, err := json.Marshal(field.Name)
		if err != nil {
			return fmt.Errorf("output: cannot encode the field name %q: %w", field.Name, err)
		}
		value, err := json.Marshal(field.Value)
		if err != nil {
			return fmt.Errorf("output: cannot encode the value of %q: %w", field.Name, err)
		}
		document.WriteString("  ")
		document.Write(name)
		document.WriteString(": ")
		document.Write(value)
		if index < len(produced.Fields)-1 || len(produced.Rows) > 0 {
			document.WriteString(",")
		}
		document.WriteString("\n")
	}
	if len(produced.Rows) > 0 {
		encoded, err := encodeRows(produced)
		if err != nil {
			return err
		}
		document.WriteString("  \"rows\": ")
		document.Write(encoded)
		document.WriteString("\n")
	}
	document.WriteString("}\n")

	_, err := w.Write(document.Bytes())
	return err
}
