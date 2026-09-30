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

package yamldoc_test

import (
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/yamldoc"
)

func TestToJSONReadsYAMLAsJSON(t *testing.T) {
	got, err := yamldoc.ToJSON([]byte("schemaVersion: 4\nname: no\ncount: 0x10\nratio: 1.5\nlist:\n  - a\n  - null\nempty: {}\non: true\n"))
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	want := `{"schemaVersion":4,"name":"no","count":16,"ratio":1.5,"list":["a",null],"empty":{},"on":true}`
	if string(got) != want {
		t.Errorf("ToJSON = %s, want %s", got, want)
	}
}

func TestToJSONPassesJSONThroughUnchanged(t *testing.T) {
	in := "{\n\t\"schemaVersion\": 4\n}\n"
	got, err := yamldoc.ToJSON([]byte(in))
	if err != nil || string(got) != in {
		t.Errorf("ToJSON(%q) = %q, %v; want it unchanged", in, got, err)
	}
}

func TestToJSONRefusesWhatJSONCannotMean(t *testing.T) {
	for name, in := range map[string]string{
		"an anchor and alias": "a: &x 1\nb: *x\n",
		"a merge key":         "base: {a: 1}\nderived:\n  <<: {a: 1}\n",
		"a duplicate key":     "a: 1\na: 2\n",
		"a non-string key":    "1: a\n",
		"a custom tag":        "a: !secret x\n",
		"a binary tag":        "a: !!binary aGk=\n",
		"two documents":       "a: 1\n---\nb: 2\n",
		"an empty document":   "",
		"an infinity":         "a: .inf\n",
		"a tagged non-number": "a: !!int \"[1]\"\n",
		"a syntax error":      "a: [\n",
		"an oversized input":  "a: " + strings.Repeat("x", 1<<20) + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := yamldoc.ToJSON([]byte(in)); err == nil {
				t.Errorf("ToJSON accepted it as %s", got)
			}
		})
	}
}

func TestFromJSONRoundTrips(t *testing.T) {
	in := `{"schemaVersion":4,"name":"true","version":"1.0","none":"null","empty":"","url":"https://example.com/` +
		strings.Repeat("long", 40) + `","multi":"a\nb","list":[],"map":{},"n":7,"items":[{"k":"v"}]}`
	yamlData, err := yamldoc.FromJSON([]byte(in))
	if err != nil {
		t.Fatalf("FromJSON: %v", err)
	}
	if strings.Contains(string(yamlData), "{\"") {
		t.Errorf("FromJSON kept flow style:\n%s", yamlData)
	}
	back, err := yamldoc.ToJSON(yamlData)
	if err != nil {
		t.Fatalf("ToJSON of FromJSON output: %v\n%s", err, yamlData)
	}
	if string(back) != in {
		t.Errorf("round trip changed the document:\n got %s\nwant %s\nyaml:\n%s", back, in, yamlData)
	}
}

func TestToJSONReadsFlowStyleYAML(t *testing.T) {
	got, err := yamldoc.ToJSON([]byte("{schemaVersion: 4, name: a}\n"))
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	if want := `{"schemaVersion":4,"name":"a"}`; string(got) != want {
		t.Errorf("ToJSON = %s, want %s", got, want)
	}
}

func TestToJSONReadsADateAsWritten(t *testing.T) {
	got, err := yamldoc.ToJSON([]byte("created: 2026-01-01\nat: 2026-01-01T10:00:00Z\n"))
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	if want := `{"created":"2026-01-01","at":"2026-01-01T10:00:00Z"}`; string(got) != want {
		t.Errorf("ToJSON = %s, want %s", got, want)
	}
}

func TestIntegersBeyondInt64RoundTrip(t *testing.T) {
	in := `{"big":12345678901234567890,"negative":-12345678901234567890}`
	yamlData, err := yamldoc.FromJSON([]byte(in))
	if err != nil {
		t.Fatalf("FromJSON: %v", err)
	}
	back, err := yamldoc.ToJSON(yamlData)
	if err != nil {
		t.Fatalf("ToJSON of FromJSON output: %v\n%s", err, yamlData)
	}
	if string(back) != in {
		t.Errorf("round trip = %s, want %s", back, in)
	}
}
