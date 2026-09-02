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
	"fmt"
	"strings"

	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/internal/parsetree"
	"github.com/wso2/wso2-cli/sdk/commandtree"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// shellFlags are the flags the shell reads off a product command line for
// itself, wherever on the line they are written.
//
// They are the shell's whatever the module declares. A module that declared its
// own --output would find the shell reading it first, which is why the reserved
// spellings are documented for module authors rather than negotiated here: two
// readings of one flag on one line is the ambiguity this whole mechanism exists
// to remove.
type shellFlags struct {
	mode        output.Mode
	contextName string
}

// read consumes one shell-owned flag from the front of args.
//
// It reports how many arguments it took, and zero when the front of args is not
// the shell's to read — which is different from it being the shell's and
// malformed, and that case comes back as an error rather than as nothing.
func (f *shellFlags) read(namespace string, args []string) (int, error) {
	argument := args[0]
	switch {
	case argument == "--output" || argument == "-o":
		if len(args) < 2 {
			return 0, missingOutputValue(namespace, argument)
		}
		parsed, ok := output.ParseMode(args[1])
		if !ok {
			return 0, unknownOutputMode(namespace, args[1])
		}
		f.mode = parsed
		return 2, nil
	case attachedOutput(argument):
		// Every spelling pflag accepts for the shell's own flags is accepted
		// here too. A spelling that worked before a product namespace and not
		// after it would be the drift this path and the root command's parser
		// are pinned against.
		value, _ := outputFlagValue(argument)
		parsed, ok := output.ParseMode(value)
		if !ok {
			return 0, unknownOutputMode(namespace, value)
		}
		f.mode = parsed
		return 1, nil
	case argument == "--context" || strings.HasPrefix(argument, "--context="):
		name, consumed := contextFlagValue(args)
		if name == "" {
			return 0, missingContextValue(fmt.Sprintf("Run wso2 %s --context <name>.", namespace))
		}
		f.contextName = name
		return consumed, nil
	}
	return 0, nil
}

// parseProductArgs separates the shell's own flags from the module's arguments,
// reading the module's side against the command tree the module declared.
//
// The declaration is what makes a product command line parse the same wherever
// its flags are written. Without one the shell could only consume the flags it
// recognised and hand everything from the first flag it did not to the module,
// so "--output json" before an unknown product flag rendered JSON and the same
// flag after it silently rendered a table. Knowing which flags the command takes
// and which of them carry a value, the shell can read the whole line: the
// module's flags go to the module wherever they appear, the shell's are the
// shell's wherever they appear, and a flag belonging to neither is named rather
// than forwarded to fail somewhere else.
//
// A module that declares no tree — one built before declarations existed, or one
// that is not built on Cobra — is parsed the way every module was before: the
// command path is the leading run of plain words, and everything from the first
// unrecognised flag onward is the module's to interpret or refuse.
func parseProductArgs(namespace string, declared parsetree.Tree, args []string) (
	command, arguments []string, mode output.Mode, contextName string, err error) {
	flags := shellFlags{mode: output.ModeTable}
	remaining := args

	// The command path is the leading run of plain words. A flag the shell does
	// not own ends it, because from there a plain word could be that flag's
	// value rather than a command name.
	for len(remaining) > 0 {
		consumed, failure := flags.read(namespace, remaining)
		if failure != nil {
			return nil, nil, "", "", failure
		}
		if consumed > 0 {
			remaining = remaining[consumed:]
			continue
		}
		if strings.HasPrefix(remaining[0], "-") {
			break
		}
		command = append(command, remaining[0])
		remaining = remaining[1:]
	}

	if !declared.Declared() {
		return command, remaining, flags.mode, flags.contextName, nil
	}

	found, positional, ok := declared.Lookup(command)
	if !ok {
		return nil, nil, "", "", unknownProductCommand(namespace, command, declared)
	}
	arguments = append(arguments, positional...)

	for len(remaining) > 0 {
		consumed, failure := flags.read(namespace, remaining)
		if failure != nil {
			return nil, nil, "", "", failure
		}
		if consumed > 0 {
			remaining = remaining[consumed:]
			continue
		}
		argument := remaining[0]
		switch {
		case argument == "--":
			// Everything after the separator is the module's, unread. A
			// command that takes a file named "--output" needs a way to say so.
			arguments = append(arguments, remaining...)
			remaining = nil
		case strings.HasPrefix(argument, "--"):
			consumed, failure = readLongFlag(namespace, found, remaining, &arguments)
		case len(argument) > 1 && argument[0] == '-':
			consumed, failure = readShorthandFlags(namespace, found, remaining, &arguments)
		default:
			arguments = append(arguments, argument)
			consumed = 1
		}
		if failure != nil {
			return nil, nil, "", "", failure
		}
		remaining = remaining[consumed:]
	}
	// The resolved path replaces the words that were typed, so an alias reaches
	// the module under the name the module serves.
	return found.Path, arguments, flags.mode, flags.contextName, nil
}

// readLongFlag reads one of the module's long flags and the value it carries.
func readLongFlag(namespace string, found commandtree.Command, args []string,
	arguments *[]string) (int, error) {
	name, _, attached := strings.Cut(strings.TrimPrefix(args[0], "--"), "=")
	flag, declared := found.LookupFlag(name)
	if !declared {
		return 0, unknownProductFlag(namespace, found, "--"+name)
	}
	*arguments = append(*arguments, args[0])
	if !flag.TakesValue() || attached {
		return 1, nil
	}
	if len(args) < 2 {
		return 0, missingProductFlagValue(namespace, found, "--"+name)
	}
	*arguments = append(*arguments, args[1])
	return 2, nil
}

// readShorthandFlags reads a run of single-letter flags written as one argument.
//
// pflag lets "-ab" stand for "-a -b" while a letter that takes a value ends the
// run and claims the rest, so "-abvalue" is "-a -b value". Reading it the same
// way is what keeps a declaration honest: a shell that guessed differently from
// the module it forwards to would send a value the module never saw as one.
func readShorthandFlags(namespace string, found commandtree.Command, args []string,
	arguments *[]string) (int, error) {
	letters := []rune(args[0][1:])
	for index, letter := range letters {
		flag, declared := found.LookupShorthand(letter)
		if !declared {
			return 0, unknownProductFlag(namespace, found, "-"+string(letter))
		}
		if !flag.TakesValue() {
			continue
		}
		*arguments = append(*arguments, args[0])
		// The rest of the run is the value, whether or not an equals sign
		// joins it. Only an empty rest sends the parser to the next argument.
		if rest := strings.TrimPrefix(string(letters[index+1:]), "="); rest != "" {
			return 1, nil
		}
		if len(args) < 2 {
			return 0, missingProductFlagValue(namespace, found, "-"+string(letter))
		}
		*arguments = append(*arguments, args[1])
		return 2, nil
	}
	*arguments = append(*arguments, args[0])
	return 1, nil
}

// unknownProductCommand reports a command path the module does not serve.
//
// Naming it here is the point of the declaration. Before one existed the words
// went to the module, which answered for itself and could not be asked what it
// would have accepted, so the shell had nothing to suggest.
func unknownProductCommand(namespace string, words []string, declared parsetree.Tree) problem.Problem {
	typed := strings.Join(words, " ")
	reported := problem.New(problem.CategoryUsage, "shell.unknown_product_command",
		fmt.Sprintf("the %s module has no %q command", namespace, typed))
	if closest := closestCommand(declared, words); closest != "" {
		return reported.WithRecovery(fmt.Sprintf("Did you mean wso2 %s %s?", namespace, closest))
	}
	return reported.WithRecovery(fmt.Sprintf("Run wso2 %s --help to see what it does.", namespace))
}

// closestCommand reports the declared command path nearest to what was typed,
// or the empty string when nothing is near enough to be worth offering.
func closestCommand(declared parsetree.Tree, words []string) string {
	typed := strings.Join(words, " ")
	best, bestDistance := "", 0
	for _, candidate := range declared.Commands() {
		path := strings.Join(candidate.Path, " ")
		if path == "" {
			continue
		}
		distance := editDistance(typed, path)
		// Two edits is the same tolerance Cobra offers for the shell's own
		// commands, so a product command is suggested on the same terms as a
		// built-in one rather than on a rule of its own.
		if distance > 2 {
			continue
		}
		if best == "" || distance < bestDistance {
			best, bestDistance = path, distance
		}
	}
	return best
}

// editDistance reports the Levenshtein distance between two strings.
func editDistance(from, to string) int {
	previous := make([]int, len([]rune(to))+1)
	current := make([]int, len(previous))
	for index := range previous {
		previous[index] = index
	}
	for row, fromRune := range []rune(from) {
		current[0] = row + 1
		for column, toRune := range []rune(to) {
			substitution := previous[column]
			if fromRune != toRune {
				substitution++
			}
			current[column+1] = min(substitution, min(previous[column+1]+1, current[column]+1))
		}
		previous, current = current, previous
	}
	return previous[len([]rune(to))]
}

// unknownProductFlag reports a flag the command does not declare.
func unknownProductFlag(namespace string, found commandtree.Command, spelling string) problem.Problem {
	return problem.New(problem.CategoryUsage, "shell.unknown_product_flag",
		fmt.Sprintf("%s does not take %s", commandLabel(namespace, found), spelling)).
		WithRecovery(fmt.Sprintf("Run %s --help to see the flags it accepts.",
			commandLabel(namespace, found)))
}

// missingProductFlagValue reports a module flag written without the value it
// takes.
func missingProductFlagValue(namespace string, found commandtree.Command, spelling string) problem.Problem {
	return problem.New(problem.CategoryUsage, "shell.missing_flag_value",
		fmt.Sprintf("%s needs a value", spelling)).
		WithRecovery(fmt.Sprintf("Run %s --help to see what %s takes.",
			commandLabel(namespace, found), spelling))
}

// commandLabel renders a product command the way a user typed it.
func commandLabel(namespace string, found commandtree.Command) string {
	return strings.TrimSpace("wso2 " + namespace + " " + strings.Join(found.Path, " "))
}
