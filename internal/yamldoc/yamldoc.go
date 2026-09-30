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

// Package yamldoc is the shell's only YAML parser, and it reads and writes
// YAML as a spelling of JSON.
//
// A YAML document is converted to JSON before anything decodes it, so every
// reader keeps its strict encoding/json decode, its tags and its validation,
// and a JSON document is read exactly as before. Only the subset of YAML that
// has a JSON meaning is accepted: anchors, aliases, merge keys, custom tags,
// non-string keys and duplicate keys are refused rather than interpreted. See
// docs/adr/0019-yaml-context-documents.md.
package yamldoc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"

	"go.yaml.in/yaml/v3"
)

// maxSize bounds the input. A context file is a few kilobytes; a document far
// larger than any real one is refused before the parser sees it.
const maxSize = 1 << 20

// ToJSON reads data as one YAML document and returns the same value as JSON.
//
// Data that is already valid JSON is returned unchanged, so a JSON input file
// keeps its exact decode, including the tab-indented JSON that YAML does not
// accept. Anything else, flow-style YAML included, is parsed as YAML.
func ToJSON(data []byte) ([]byte, error) {
	if len(data) > maxSize {
		return nil, fmt.Errorf("is larger than %d bytes", maxSize)
	}
	if json.Valid(data) {
		return data, nil
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("is empty")
		}
		return nil, fmt.Errorf("is not valid YAML: %w", err)
	}
	var second yaml.Node
	if err := decoder.Decode(&second); !errors.Is(err, io.EOF) {
		return nil, errors.New("contains more than one YAML document")
	}
	var out bytes.Buffer
	if err := writeJSON(&out, &document); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// writeJSON renders one node as JSON, keeping mapping order.
func writeJSON(out *bytes.Buffer, node *yaml.Node) error {
	if node.Anchor != "" || node.Kind == yaml.AliasNode {
		return fmt.Errorf("uses an anchor or alias at line %d, which a context file does not allow", node.Line)
	}
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) != 1 {
			return errors.New("is empty")
		}
		return writeJSON(out, node.Content[0])
	case yaml.MappingNode:
		out.WriteByte('{')
		seen := map[string]bool{}
		for index := 0; index+1 < len(node.Content); index += 2 {
			key, value := node.Content[index], node.Content[index+1]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Anchor != "" {
				return fmt.Errorf("has a key at line %d that is not a plain string", key.Line)
			}
			if seen[key.Value] {
				return fmt.Errorf("repeats the key %q at line %d", key.Value, key.Line)
			}
			seen[key.Value] = true
			if index > 0 {
				out.WriteByte(',')
			}
			writeString(out, key.Value)
			out.WriteByte(':')
			if err := writeJSON(out, value); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	case yaml.SequenceNode:
		out.WriteByte('[')
		for index, item := range node.Content {
			if index > 0 {
				out.WriteByte(',')
			}
			if err := writeJSON(out, item); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case yaml.ScalarNode:
		return writeScalar(out, node)
	default:
		return fmt.Errorf("has a value at line %d that has no JSON meaning", node.Line)
	}
	return nil
}

// writeScalar renders a scalar by its resolved tag. Only the tags JSON can
// represent are accepted; a custom or binary tag is refused.
func writeScalar(out *bytes.Buffer, node *yaml.Node) error {
	switch node.Tag {
	case "!!str", "!!timestamp":
		// A date is kept as written, which is what the same text means in JSON.
		writeString(out, node.Value)
	case "!!null":
		out.WriteString("null")
	case "!!bool":
		var value bool
		if err := node.Decode(&value); err != nil {
			return fmt.Errorf("has an unreadable boolean at line %d", node.Line)
		}
		out.WriteString(strconv.FormatBool(value))
	case "!!int":
		// A decimal literal JSON can read is kept as written, so an integer
		// beyond int64 survives a round trip through FromJSON.
		if isJSONNumber(node.Value) {
			out.WriteString(node.Value)
			return nil
		}
		var value int64
		if err := node.Decode(&value); err != nil {
			return fmt.Errorf("has an unreadable integer at line %d", node.Line)
		}
		out.WriteString(strconv.FormatInt(value, 10))
	case "!!float":
		// As with an integer: YAML resolves a decimal beyond int64, such as
		// -12345678901234567890, as a float, and the literal is kept.
		if isJSONNumber(node.Value) {
			out.WriteString(node.Value)
			return nil
		}
		var value float64
		if err := node.Decode(&value); err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
			return fmt.Errorf("has a number at line %d that JSON cannot represent", node.Line)
		}
		out.WriteString(strconv.FormatFloat(value, 'g', -1, 64))
	default:
		return fmt.Errorf("uses the tag %s at line %d, which a context file does not allow", node.Tag, node.Line)
	}
	return nil
}

// isJSONNumber reports whether value is a JSON number literal. An explicit tag
// such as !!int "[1]" must not pass other JSON through as a number.
func isJSONNumber(value string) bool {
	if value == "" || (value[0] != '-' && (value[0] < '0' || value[0] > '9')) {
		return false
	}
	return json.Valid([]byte(value))
}

func writeString(out *bytes.Buffer, value string) {
	encoded, _ := json.Marshal(value)
	out.Write(encoded)
}

// FromJSON renders a JSON document as block-style YAML, keeping member order.
//
// A string that YAML would read as another type, such as "true", "1.0" or
// "null", is quoted, so ToJSON reads the result back as the same JSON value.
func FromJSON(data []byte) ([]byte, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("yamldoc: cannot read the JSON to convert: %w", err)
	}
	clearStyle(&document)
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(&document); err != nil {
		return nil, fmt.Errorf("yamldoc: cannot encode YAML: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("yamldoc: cannot encode YAML: %w", err)
	}
	return out.Bytes(), nil
}

// clearStyle clears the JSON presentation (flow collections, double-quoted
// strings) so the encoder chooses block style, and quotes only where the
// value needs it.
func clearStyle(node *yaml.Node) {
	node.Style = 0
	for _, child := range node.Content {
		clearStyle(child)
	}
}
