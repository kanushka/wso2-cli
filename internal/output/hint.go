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
	"strings"
)

const (
	commandStyle = "\x1b[1;36m"
	resetStyle   = "\x1b[0m"
)

// Hint marks the wso2 commands inside a next step or a recovery, so a command
// stands apart from the sentence around it: bold and colored where w renders
// color, wrapped in backticks everywhere else.
//
// The text stays prose on the wire — modules and shell commands write it as a
// sentence, and JSON carries it verbatim — so finding the command is the
// renderer's job. A span starts at "wso2" and keeps every word that reads as a
// command word, flag, or placeholder; it ends at the first ordinary English
// word or at punctuation, which is why the words that end one are listed.
// Getting a boundary wrong only mis-highlights a word; the text is unchanged.
func Hint(w io.Writer, text string) string {
	open, close := "`", "`"
	if ColorEnabled(w) {
		open, close = commandStyle, resetStyle
	}
	return markCommands(text, open, close)
}

func markCommands(text, open, close string) string {
	tokens := strings.Split(text, " ")
	marked := make([]string, 0, len(tokens))
	for index := 0; index < len(tokens); index++ {
		if !startsCommand(tokens, index) {
			marked = append(marked, tokens[index])
			continue
		}
		end, punctuation := commandEnd(tokens, index)
		command := strings.Join(tokens[index:end+1], " ")
		command = strings.TrimSuffix(command, punctuation)
		marked = append(marked, open+command+close+punctuation)
		index = end
	}
	return strings.Join(marked, " ")
}

// startsCommand reports whether the token at index opens a command: a bare
// "wso2" followed by at least one more word of it.
func startsCommand(tokens []string, index int) bool {
	if tokens[index] != "wso2" || index+1 >= len(tokens) {
		return false
	}
	next, _ := splitTrailing(tokens[index+1])
	return commandWord(next) && !stopWords[next]
}

// commandEnd finds the last token of the command starting at index, and the
// sentence punctuation that token carries after the command itself.
func commandEnd(tokens []string, index int) (end int, punctuation string) {
	depth := 0
	for end = index; end < len(tokens); end++ {
		token := tokens[end]
		if end > index && depth == 0 {
			bare, _ := splitTrailing(token)
			if stopWords[bare] || !commandWord(bare) {
				return end - 1, ""
			}
		}
		depth += strings.Count(token, "[") + strings.Count(token, "<") -
			strings.Count(token, "]") - strings.Count(token, ">")
		if depth > 0 {
			continue
		}
		depth = 0
		if _, punctuation = splitTrailing(token); punctuation != "" {
			return end, punctuation
		}
	}
	return len(tokens) - 1, ""
}

// splitTrailing separates sentence punctuation from the end of a word. A
// closing bracket or angle bracket belongs to the command, so it stays.
func splitTrailing(word string) (body, punctuation string) {
	body = strings.TrimRight(word, ".,;:?!)")
	return body, word[len(body):]
}

// commandWord reports whether a word can belong to a command: a flag, a
// placeholder, an optional part, a format verb, a lowercase name, or a value
// such as MockAPI/1.0.0 that no sentence word looks like.
func commandWord(word string) bool {
	if word == "" {
		return false
	}
	switch word[0] {
	case '-', '<', '[', '%', '"':
		return true
	}
	lower, value := true, false
	for index, r := range word {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
			lower = false
			value = value || index > 0
		case r >= '0' && r <= '9', strings.ContainsRune("-/_.=:<>|]", r):
			value = true
		default:
			return false
		}
	}
	return lower || value
}

// stopWords end a command span: the lowercase words a sentence continues with
// after naming a command.
var stopWords = map[string]bool{
	"a": true, "after": true, "again": true, "and": true, "any": true, "are": true, "as": true, "be": true,
	"at": true, "before": true, "but": true, "by": true, "each": true, "every": true,
	"first": true, "for": true, "from": true, "has": true, "have": true, "if": true,
	"in": true, "instead": true, "is": true, "it": true, "lists": true, "must": true,
	"no": true, "not": true, "now": true, "of": true, "on": true, "once": true,
	"or": true, "reports": true, "see": true, "shows": true, "so": true, "that": true,
	"the": true, "then": true, "this": true, "to": true, "until": true, "when": true,
	"which": true, "will": true, "with": true, "would": true,
}
