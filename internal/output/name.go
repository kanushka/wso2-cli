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
	"io"
	"slices"
	"strings"

	"github.com/wso2/wso2-cli/sdk/result"
)

// DefaultName is the name the shell's own text uses for itself. Every message
// in the shell and in product modules is written with it; a shell invoked
// under another name renames it only as the text is rendered.
const DefaultName = "wso2"

// namedWriter is a stream that knows the name the shell was invoked as. It
// never changes the bytes written through it: renaming is the renderers' job,
// because only they know which text is prose and which is data.
type namedWriter struct {
	io.Writer
	name string
}

// namedFile is a namedWriter over a stream with a file descriptor, kept as a
// separate type so terminal and color detection still see the descriptor.
type namedFile struct {
	namedWriter
	fd func() uintptr
}

func (w namedFile) Fd() uintptr { return w.fd() }

// Named returns w carrying name, so the renderers that write to it name shell
// commands after it. With the default name, w itself is returned.
func Named(w io.Writer, name string) io.Writer {
	if name == "" || name == DefaultName {
		return w
	}
	named := namedWriter{Writer: w, name: name}
	if file, ok := w.(interface{ Fd() uintptr }); ok {
		return namedFile{namedWriter: named, fd: file.Fd}
	}
	return named
}

// Unnamed returns the stream Named wrapped, or w itself. A terminal form
// library needs the real file to find the terminal it draws on, and a
// question names no shell command to rename.
func Unnamed(w io.Writer) io.Writer {
	switch named := w.(type) {
	case namedWriter:
		return named.Writer
	case namedFile:
		return named.Writer
	}
	return w
}

// NameOf reports the name w carries, or DefaultName for a plain writer.
func NameOf(w io.Writer) string {
	switch named := w.(type) {
	case namedWriter:
		return named.name
	case namedFile:
		return named.name
	}
	return DefaultName
}

// Rename rewrites every shell command in text from DefaultName to name. A
// command is the same span Hint marks: a bare "wso2" followed by a command
// word, so wso2-cli, WSO2_HOME, paths, and "wso2" used as an ordinary word
// are left alone. A leading quote or bracket stays attached to the command.
func Rename(text, name string) string {
	if name == "" || name == DefaultName || !strings.Contains(text, DefaultName) {
		return text
	}
	tokens := strings.Split(text, " ")
	for index, token := range tokens {
		prefix, bare := splitLeading(token)
		if bare != DefaultName || index+1 >= len(tokens) {
			continue
		}
		// A command quoted in a sentence ends at its closing quote, which is
		// not part of the word that follows the name.
		next := strings.TrimRight(tokens[index+1], "\"'`.,;:?!)")
		if startsCommand([]string{bare, next}, 0, DefaultName) {
			tokens[index] = prefix + name
		}
	}
	return strings.Join(tokens, " ")
}

// splitLeading separates an opening quote or bracket from the start of a word.
func splitLeading(word string) (punctuation, body string) {
	body = strings.TrimLeft(word, "\"'`(")
	return word[:len(word)-len(body)], body
}

// RenameLines applies Rename to each line of a multi-line page, such as a
// help page, whose text is all prose.
func RenameLines(text, name string) string {
	if name == "" || name == DefaultName {
		return text
	}
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		lines[index] = Rename(line, name)
	}
	return strings.Join(lines, "\n")
}

// RecoveryField names a result field that says how to get past what the
// result reports, the way a problem's recovery does.
const RecoveryField = "recovery"

// proseFields are the result fields whose values are sentences naming shell
// commands, rather than data a command found. Only these are renamed.
var proseFields = []string{NextField, RecoveryField}

// fieldText is the value a result field is rendered with under the name w
// carries: a prose field is renamed, and any other field is data.
func fieldText(w io.Writer, field result.Field) string {
	if slices.Contains(proseFields, field.Name) {
		return Rename(field.Value, NameOf(w))
	}
	return field.Value
}

// RenameJSON applies Rename to the prose members of an indented JSON
// document, which the encoder always writes one member per line. Every other
// member is data and is left as it is.
func RenameJSON(document []byte, name string) []byte {
	if name == "" || name == DefaultName {
		return document
	}
	lines := strings.Split(string(document), "\n")
	for index, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		for _, field := range proseFields {
			if strings.HasPrefix(trimmed, `"`+field+`": `) {
				lines[index] = Rename(line, name)
			}
		}
	}
	return []byte(strings.Join(lines, "\n"))
}
