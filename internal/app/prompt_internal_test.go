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

package app

import (
	"bytes"
	"io"
	"testing"

	"github.com/wso2/wso2-cli/internal/output"
)

// TestIsAffirmative table-tests the one line standing in front of an
// irreversible os.RemoveAll and an unbounded update: confirm's consent
// predicate. Nothing else in this package's test suite pins it — F1 of the
// first fix round found that mutating it to accept an empty line as yes, or
// to never accept yes at all, survived the entire internal/app suite. This
// test is written to fail on both of those mutants:
//
//   - "" (an empty line): if isAffirmative ever treats a blank answer as
//     consent, the "empty line" case below turns from false to true and this
//     fails.
//   - never accepting yes: if the "y"/"yes" branch is deleted or its cases
//     are emptied, the "y" and "yes" cases below turn from true to false and
//     this fails.
func TestIsAffirmative(t *testing.T) {
	for _, testCase := range []struct {
		name string
		line string
		want bool
	}{
		{"a bare yes", "yes", true},
		{"a bare y", "y", true},
		{"uppercase Y", "Y", true},
		{"uppercase YES", "YES", true},
		{"mixed case", "Yes", true},
		{"padded with whitespace", "  yes  ", true},
		{"padded y", " y ", true},
		{"empty line", "", false},
		{"whitespace only", "   ", false},
		{"a tab", "\t", false},
		{"no", "no", false},
		{"n", "n", false},
		{"garbage", "sure why not", false},
		{"yes with trailing punctuation", "yes!", false},
		{"a digit", "1", false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isAffirmative(testCase.line); got != testCase.want {
				t.Errorf("isAffirmative(%q) = %v, want %v", testCase.line, got, testCase.want)
			}
		})
	}
}

// TestConfirmTreatsNoAnswerAtAllAsNo covers the case isAffirmative never
// sees: a reader that produces nothing before EOF (Scan returns false), which
// is what a piped-but-otherwise-permitted reader carrying no line at all
// looks like. confirm must not block waiting for a line that never comes, and
// must resolve it as "no", the same fail-closed answer isAffirmative gives an
// empty line.
func TestConfirmTreatsNoAnswerAtAllAsNo(t *testing.T) {
	shell := Shell{Streams: output.Streams{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}, Reader: emptyReader{}}
	confirmed, err := shell.confirm("Proceed? [y/N]: ")
	if err != nil {
		t.Fatalf("confirm returned %v", err)
	}
	if confirmed {
		t.Error("confirm treated an empty stream as consent")
	}
}

// emptyReader always reports EOF without ever producing a byte, standing in
// for a reader whose scanner.Scan() returns false immediately.
type emptyReader struct{}

func (emptyReader) Read([]byte) (int, error) { return 0, io.EOF }
