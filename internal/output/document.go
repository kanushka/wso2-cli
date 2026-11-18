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
	"io"
	"strings"

	"github.com/wso2/wso2-cli/sdk/problem"
)

// Document writes what a command printed under --output json as exactly one
// JSON document. A command that rendered JSON is passed through unchanged. One
// that printed prose is carried as its message, and one that printed nothing
// is an empty object. See docs/adr/0023-exit-codes-by-cause-and-one-json-document-per-command.md.
func Document(w io.Writer, printed []byte) error {
	trimmed := bytes.TrimSpace(printed)
	if json.Valid(trimmed) && len(trimmed) > 0 {
		_, err := w.Write(printed)
		return err
	}
	document := map[string]any{}
	if len(trimmed) > 0 {
		document["message"] = string(trimmed)
	}
	if next := proseNext(w, string(trimmed)); len(next) > 0 {
		document["next"] = next
	}
	encoded, err := marshalIndentPlain(document)
	if err != nil {
		return err
	}
	_, err = w.Write(append(encoded, '\n'))
	return err
}

// proseNext reads the commands a prose report's trailing Next line names, the
// line output.NextStep writes.
func proseNext(w io.Writer, prose string) []string {
	for _, line := range strings.Split(prose, "\n") {
		if rest, found := strings.CutPrefix(line, "Next  "); found {
			return NextCommands(w, strings.ReplaceAll(rest, "`", ""))
		}
	}
	return nil
}

// WithNext adds a next member, the commands next names, to a JSON object
// rendered by an indenting encoder. A next step that names no command adds
// nothing, so a result keeps its shape when there is nothing to run.
func WithNext(w io.Writer, document []byte, next string) ([]byte, error) {
	commands := NextCommands(w, next)
	if len(commands) == 0 {
		return document, nil
	}
	trimmed := bytes.TrimSpace(document)
	if len(trimmed) < 2 || trimmed[0] != '{' {
		return document, nil
	}
	joined, err := withMember(trimmed, "next", commands)
	if err != nil {
		return nil, err
	}
	return append(joined, '\n'), nil
}

// marshalIndentPlain encodes a value indented, without escaping HTML
// characters, so a placeholder such as <name> reads as written.
func marshalIndentPlain(value any) ([]byte, error) {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimRight(encoded.Bytes(), "\n"), nil
}

// failure is the error member of a failure document.
type failure struct {
	Code     string   `json:"code"`
	Category string   `json:"category"`
	Message  string   `json:"message"`
	Recovery string   `json:"recovery,omitempty"`
	Next     []string `json:"next"`
	Exit     int      `json:"exit"`
}

// FailureDocument writes a failed command's one JSON document: what the
// command printed before failing, with an error member carrying the problem.
// A JSON object the command rendered keeps its members and gains error, a
// report such as wso2 doctor's being the answer even when it fails. Prose is
// carried as the message. next lists the commands the recovery names, in the
// shape a result's next has, and recovery says why each one is suggested. See
// docs/adr/0023-exit-codes-by-cause-and-one-json-document-per-command.md.
func FailureDocument(w io.Writer, printed []byte, p problem.Problem, status int) error {
	name := NameOf(w)
	reported := failure{
		Code:     p.Code,
		Category: string(p.Category),
		Message:  Rename(p.Message, name),
		Recovery: Rename(p.Recovery, name),
		Next:     NextCommands(w, p.Recovery),
		Exit:     status,
	}
	trimmed := bytes.TrimSpace(printed)
	document := []byte("{}")
	switch {
	case len(trimmed) > 1 && trimmed[0] == '{' && json.Valid(trimmed):
		document = trimmed
	case len(trimmed) > 0:
		var err error
		if document, err = withMember(document, "message", string(trimmed)); err != nil {
			return err
		}
	}
	document, err := withMember(document, "error", reported)
	if err != nil {
		return err
	}
	_, err = w.Write(append(document, '\n'))
	return err
}

// withMember adds one member to the end of a JSON object, indented the way
// the shell indents every document, so a member is added without reordering
// the ones the command wrote.
func withMember(object []byte, name string, value any) ([]byte, error) {
	key, err := marshalIndentPlain(name)
	if err != nil {
		return nil, err
	}
	encoded, err := marshalIndentPlain(value)
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(object)
	body := bytes.TrimSpace(trimmed[:len(trimmed)-1])
	var joined bytes.Buffer
	joined.Write(body)
	if len(body) > 1 {
		joined.WriteString(",")
	}
	joined.WriteString("\n  ")
	joined.Write(key)
	joined.WriteString(": ")
	joined.Write(bytes.ReplaceAll(encoded, []byte("\n"), []byte("\n  ")))
	joined.WriteString("\n}")
	return joined.Bytes(), nil
}
