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

package catalog

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxTitleLength is the longest title, in characters, a product may declare.
// It is what fits beside a namespace on one help line.
const MaxTitleLength = 40

// ValidTitle reports whether a declared title may be published: no longer than
// MaxTitleLength characters, valid UTF-8, and free of control characters. The
// empty title is valid, because declaring one is optional.
//
// ADR 0015: a title is printed into a terminal, and nothing attests to the
// authenticity of a catalog entry, so the generator refuses a title that could
// do more than name a product.
func ValidTitle(title string) bool {
	if !utf8.ValidString(title) || utf8.RuneCountInString(title) > MaxTitleLength {
		return false
	}
	return !strings.ContainsFunc(title, unsafeInTitle)
}

// SanitizedTitle makes a title safe to print whatever it arrived as: it is made
// Printable and cut to MaxTitleLength characters. The generator already refuses
// a title that needs either, but the shell prints what it was given, and a copy
// that did not come from the generator is printed through this all the same.
func SanitizedTitle(title string) string {
	title = Printable(title)
	if utf8.RuneCountInString(title) > MaxTitleLength {
		title = strings.TrimSpace(string([]rune(title)[:MaxTitleLength]))
	}
	return title
}

// Printable drops everything from text that could do more than be read in a
// terminal — invalid UTF-8, control characters and formatting characters — and
// trims what remains. It is for one-line text a module declares, which the
// shell prints but did not write.
func Printable(text string) string {
	text = strings.ToValidUTF8(text, "")
	text = strings.Map(func(character rune) rune {
		if unsafeInTitle(character) {
			return -1
		}
		return character
	}, text)
	return strings.TrimSpace(text)
}

// unsafeInTitle reports a character a title may not carry: a control
// character, which includes the escape that starts a terminal sequence, and a
// formatting character, which includes the bidirectional overrides that make
// printed text read differently from what it is.
func unsafeInTitle(character rune) bool {
	return unicode.IsControl(character) || unicode.Is(unicode.Cf, character)
}
