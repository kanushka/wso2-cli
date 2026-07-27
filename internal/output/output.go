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

// Package output renders every user-facing byte the shell writes.
//
// Only the shell writes user output and diagnostics, so all formatting lives
// here: standard output carries the command's result, and standard error
// carries diagnostics. Keeping the streams separate means structured output
// stays parseable even when diagnostics are emitted. See
// docs/adr/0003-shell-owned-output.md.
package output

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/wso2/wso2-cli/sdk/problem"
)

// Streams are the shell's user-facing output destinations.
type Streams struct {
	// Out carries command results.
	Out io.Writer
	// Err carries diagnostics, warnings, and problems.
	Err io.Writer
}

// columnGap is the minimum number of spaces between table columns.
const columnGap = 3

// Table is a simple aligned text table.
type Table struct {
	headers []string
	rows    [][]string
}

// NewTable builds a table with the given column headers. Headers are rendered
// uppercase, matching the shell's inventory output convention.
func NewTable(headers ...string) *Table {
	upper := make([]string, len(headers))
	for index, header := range headers {
		upper[index] = strings.ToUpper(header)
	}
	return &Table{headers: upper}
}

// Append adds one row. Rows shorter than the header are padded with empty
// cells, so a partially known row still renders.
func (t *Table) Append(cells ...string) {
	row := make([]string, len(t.headers))
	copy(row, cells)
	t.rows = append(t.rows, row)
}

// Render writes the table, left-aligned, with no trailing spaces.
func (t *Table) Render(w io.Writer) error {
	widths := make([]int, len(t.headers))
	for index, header := range t.headers {
		widths[index] = utf8.RuneCountInString(header)
	}
	for _, row := range t.rows {
		for index, cell := range row {
			if width := utf8.RuneCountInString(cell); width > widths[index] {
				widths[index] = width
			}
		}
	}

	for _, row := range append([][]string{t.headers}, t.rows...) {
		if _, err := fmt.Fprintln(w, joinCells(row, widths)); err != nil {
			return err
		}
	}
	return nil
}

// Fields renders aligned "label value" lines, used for the shell's own
// version facts.
func Fields(w io.Writer, pairs [][2]string) error {
	label := 0
	for _, pair := range pairs {
		if width := utf8.RuneCountInString(pair[0]); width > label {
			label = width
		}
	}
	for _, pair := range pairs {
		padding := strings.Repeat(" ", label-utf8.RuneCountInString(pair[0])+columnGap)
		if _, err := fmt.Fprintf(w, "%s%s%s\n", pair[0], padding, pair[1]); err != nil {
			return err
		}
	}
	return nil
}

// Diagnostic writes one warning line, plus its recovery guidance when present.
// A problem's fields are rendered verbatim; they must never carry secret detail.
func Diagnostic(w io.Writer, p problem.Problem) {
	writeProblem(w, "warning", p)
}

// Problem writes one terminal failure with its recovery guidance.
func Problem(w io.Writer, p problem.Problem) {
	writeProblem(w, "error", p)
}

// writeProblem renders one problem under the given severity label.
func writeProblem(w io.Writer, severity string, p problem.Problem) {
	_, _ = fmt.Fprintf(w, "%s: %s (%s)\n", severity, p.Message, p.Code)
	if p.Recovery != "" {
		_, _ = fmt.Fprintf(w, "  %s\n", p.Recovery)
	}
}

func joinCells(row []string, widths []int) string {
	var line strings.Builder
	for index, cell := range row {
		if index == len(row)-1 {
			line.WriteString(cell)
			break
		}
		line.WriteString(cell)
		line.WriteString(strings.Repeat(" ", widths[index]-utf8.RuneCountInString(cell)+columnGap))
	}
	return strings.TrimRight(line.String(), " ")
}
