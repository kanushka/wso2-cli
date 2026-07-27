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
func resultTable(w io.Writer, produced result.Result) error {
	headers := make([]string, 0, len(produced.Fields))
	values := make([]string, 0, len(produced.Fields))
	for _, field := range produced.Fields {
		headers = append(headers, field.DisplayLabel())
		values = append(values, field.Value)
	}
	table := NewTable(headers...)
	table.Append(values...)
	return table.Render(w)
}

// resultJSON renders the result as a JSON object whose keys follow the module's
// field order.
//
// The document is assembled field by field rather than through a map, because a
// Go map would sort the keys and lose the order the module chose. Every key and
// value is still encoded by encoding/json, so escaping is not hand-rolled.
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
		if index < len(produced.Fields)-1 {
			document.WriteString(",")
		}
		document.WriteString("\n")
	}
	document.WriteString("}\n")

	_, err := w.Write(document.Bytes())
	return err
}
