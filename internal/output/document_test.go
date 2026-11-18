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

package output_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/wso2/wso2-cli/internal/output"
)

func TestDocumentPassesJSONThrough(t *testing.T) {
	var w bytes.Buffer
	if err := output.Document(&w, []byte("{\n  \"a\": 1\n}\n")); err != nil {
		t.Fatal(err)
	}
	if w.String() != "{\n  \"a\": 1\n}\n" {
		t.Errorf("rewrote a JSON document: %q", w.String())
	}
}

func TestDocumentCarriesProseAsTheMessage(t *testing.T) {
	var w bytes.Buffer
	if err := output.Document(&w, []byte("Switched to <ctx>.\n")); err != nil {
		t.Fatal(err)
	}
	var document map[string]string
	if err := json.Unmarshal(w.Bytes(), &document); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, w.String())
	}
	if document["message"] != "Switched to <ctx>." {
		t.Errorf("message = %q", document["message"])
	}
	if bytes.Contains(w.Bytes(), []byte(`\u003c`)) {
		t.Errorf("escaped a placeholder: %s", w.String())
	}
}

func TestDocumentOfNothingIsAnEmptyObject(t *testing.T) {
	var w bytes.Buffer
	if err := output.Document(&w, nil); err != nil {
		t.Fatal(err)
	}
	if w.String() != "{}\n" {
		t.Errorf("got %q, want an empty object", w.String())
	}
}
