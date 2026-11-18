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
	encoded, err := marshalIndentPlain(document)
	if err != nil {
		return err
	}
	_, err = w.Write(append(encoded, '\n'))
	return err
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
