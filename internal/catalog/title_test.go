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

package catalog_test

import (
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/catalog"
)

func TestValidTitleAcceptsTheEmptyTitle(t *testing.T) {
	// Declaring a title is optional, so the empty one must always be valid.
	if !catalog.ValidTitle("") {
		t.Error("ValidTitle(\"\") = false, want true")
	}
}

func TestValidTitleRefusesWhatIsTooLongOrUnsafe(t *testing.T) {
	tooLong := strings.Repeat("a", catalog.MaxTitleLength+1)
	for name, title := range map[string]string{
		"longer than MaxTitleLength": tooLong,
		"invalid UTF-8":              "abc\xff",
		"a control character":        "abc\x1b[31m",
		"a bidi override":            "abc‮def",
	} {
		t.Run(name, func(t *testing.T) {
			if catalog.ValidTitle(title) {
				t.Errorf("ValidTitle(%q) = true, want false", title)
			}
		})
	}
}

func TestValidTitleAcceptsAnOrdinaryTitle(t *testing.T) {
	if !catalog.ValidTitle("WSO2 API Manager") {
		t.Error("ValidTitle on an ordinary title = false, want true")
	}
}

// TestSanitizedTitleDropsWhatMakesATitleUnsafe pins that SanitizedTitle makes
// a title safe to print even when it was never checked by ValidTitle: the
// generator refuses an unsafe title, but the shell prints what it was given,
// and a copy that did not come from the generator reaches this path.
func TestSanitizedTitleDropsWhatMakesATitleUnsafe(t *testing.T) {
	got := catalog.SanitizedTitle("abc\x1bdef‮ghi")
	if strings.ContainsAny(got, "\x1b‮") {
		t.Errorf("SanitizedTitle(...) = %q, still carries an unsafe character", got)
	}
	if got != "abcdefghi" {
		t.Errorf("SanitizedTitle(...) = %q, want abcdefghi", got)
	}
}

// TestSanitizedTitleCutsToTheMaxLength pins the truncation half of
// SanitizedTitle, distinct from Printable's own job of dropping unsafe
// characters.
func TestSanitizedTitleCutsToTheMaxLength(t *testing.T) {
	long := strings.Repeat("a", catalog.MaxTitleLength+10)
	got := catalog.SanitizedTitle(long)
	if len([]rune(got)) > catalog.MaxTitleLength {
		t.Errorf("SanitizedTitle kept %d runes, want at most %d", len([]rune(got)), catalog.MaxTitleLength)
	}
}

// TestPrintableTrimsAndDropsControlCharacters pins Printable directly: it
// is Printable, not just SanitizedTitle's helper, that drops what a
// terminal must not be handed and trims what remains.
func TestPrintableTrimsAndDropsControlCharacters(t *testing.T) {
	got := catalog.Printable("  \x1bred\x1b  ")
	if got != "red" {
		t.Errorf("Printable(...) = %q, want %q", got, "red")
	}
}

func TestPrintableDropsInvalidUTF8(t *testing.T) {
	got := catalog.Printable("abc\xffdef")
	if !strings.Contains(got, "abc") || !strings.Contains(got, "def") || strings.ContainsRune(got, 0xFFFD) {
		// strings.ToValidUTF8 with an empty replacement drops the invalid bytes
		// outright rather than substituting U+FFFD.
		t.Errorf("Printable(%q) = %q, want the invalid byte dropped and no replacement character", "abc\xffdef", got)
	}
}
