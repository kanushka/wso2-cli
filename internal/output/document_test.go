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
	"github.com/wso2/wso2-cli/sdk/problem"
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

func TestDocumentWritesOnlyValidJSON(t *testing.T) {
	var w bytes.Buffer
	if err := output.Document(&w, []byte("\u00a0{\"a\": 1}")); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(w.Bytes()) {
		t.Errorf("wrote what is not JSON: %q", w.String())
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

func TestFailureDocumentCarriesTheProblem(t *testing.T) {
	var w bytes.Buffer
	failure := problem.New(problem.CategoryNotFound, "contexts.unknown_context", "no context named \"x\" is configured").
		WithRecovery("Run wso2 context list to see the configured contexts.")
	if err := output.FailureDocument(&w, nil, failure, 66); err != nil {
		t.Fatal(err)
	}
	var document struct {
		Error struct {
			Code     string   `json:"code"`
			Category string   `json:"category"`
			Message  string   `json:"message"`
			Recovery string   `json:"recovery"`
			Next     []string `json:"next"`
			Exit     int      `json:"exit"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Bytes(), &document); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, w.String())
	}
	got := document.Error
	if got.Code != "contexts.unknown_context" || got.Category != "not_found" || got.Exit != 66 ||
		got.Message != `no context named "x" is configured` || got.Recovery == "" {
		t.Errorf("error = %+v", got)
	}
	if len(got.Next) != 1 || got.Next[0] != "wso2 context list" {
		t.Errorf("next = %+v", got.Next)
	}
}

func TestFailureDocumentAlwaysHasANextList(t *testing.T) {
	var w bytes.Buffer
	if err := output.FailureDocument(&w, nil, problem.New(problem.CategoryUsage, "x.y", "it failed"), 64); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(w.Bytes(), []byte(`"next": []`)) {
		t.Errorf("next is not an empty list:\n%s", w.String())
	}
	if bytes.Contains(w.Bytes(), []byte(`"recovery"`)) {
		t.Errorf("an empty recovery is present:\n%s", w.String())
	}
}

func TestFailureDocumentJoinsAReportTheCommandAlreadyRendered(t *testing.T) {
	var w bytes.Buffer
	failure := problem.New(problem.CategoryAuthPolicy, "auth.login_required", "log in")
	if err := output.FailureDocument(&w, []byte("{\n  \"checks\": []\n}\n"), failure, 77); err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(w.Bytes(), &document); err != nil {
		t.Fatalf("not one JSON document: %v\n%s", err, w.String())
	}
	if _, ok := document["checks"]; !ok {
		t.Errorf("the report was dropped:\n%s", w.String())
	}
	if _, ok := document["error"]; !ok {
		t.Errorf("the error was not added:\n%s", w.String())
	}
}

func TestFailureDocumentKeepsProseAsTheMessage(t *testing.T) {
	var w bytes.Buffer
	failure := problem.New(problem.CategoryModuleProcess, "modules.install_failed", "it failed")
	if err := output.FailureDocument(&w, []byte("Installing apim\n"), failure, 70); err != nil {
		t.Fatal(err)
	}
	var document struct {
		Message string          `json:"message"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(w.Bytes(), &document); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, w.String())
	}
	if document.Message != "Installing apim" || document.Error == nil {
		t.Errorf("got %s", w.String())
	}
}

func TestDocumentCarriesAProseNextLineAsCommands(t *testing.T) {
	var w bytes.Buffer
	printed := "Logged in.\n\nNext  `wso2 apim api list --project p-1`\n"
	if err := output.Document(&w, []byte(printed)); err != nil {
		t.Fatal(err)
	}
	var document struct {
		Message string   `json:"message"`
		Next    []string `json:"next"`
	}
	if err := json.Unmarshal(w.Bytes(), &document); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, w.String())
	}
	if len(document.Next) != 1 || document.Next[0] != "wso2 apim api list --project p-1" {
		t.Errorf("next = %q", document.Next)
	}
}
