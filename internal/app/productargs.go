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
	"strconv"
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
// flag after it silently rendered a table.
//
// It runs in the two phases Cobra runs in, and for the same reason: which
// command a line names cannot be settled by reading flags, because which flags
// exist depends on the command. So [parsetree.Tree.Route] locates the command
// first, and only then is the whole line read against what that command
// declares. Doing it in one pass is what makes a shell disagree with the module
// it forwards to about where a command ends and its arguments begin.
//
// A module that declares no tree — one built before declarations existed, or one
// that is not built on Cobra — is parsed the way every module was before: the
// command path is the leading run of plain words, and everything from the first
// unrecognised flag onward is the module's to interpret or refuse.
func parseProductArgs(namespace string, declared parsetree.Tree, args []string) (productLine, error) {
	if !declared.Declared() {
		return parseUndeclaredProductArgs(namespace, args)
	}

	routed := declared.Route(args)
	// An unmodified Cobra root refuses a plain word that names no subcommand,
	// and passes one along anywhere below the root. Matching that is what keeps
	// the shell from refusing a command the module would have run, or running
	// one it would have refused.
	if routed.Unrouted != "" && len(routed.Command.Path) == 0 && declared.RootHasChildren() {
		return productLine{}, unknownProductCommand(namespace, routed.Unrouted, declared)
	}

	var (
		arguments []string
		help      bool
	)
	flags := shellFlags{mode: output.ModeTable}
	for index := 0; index < len(args); index++ {
		if routed.Path[index] {
			continue
		}
		remaining := args[index:]
		consumed, failure := flags.read(namespace, remaining)
		if failure != nil {
			return productLine{}, failure
		}
		if consumed > 0 {
			index += consumed - 1
			continue
		}
		argument := remaining[0]
		switch {
		case argument == "--":
			// Everything after the separator is the module's, unread. A
			// command that takes a file named "--output" needs a way to say so.
			arguments = append(arguments, remaining...)
			index = len(args)
		case strings.HasPrefix(argument, "--"):
			var asked bool
			consumed, asked, failure = readLongFlag(namespace, routed.Command, remaining, &arguments)
			help = help || asked
		case len(argument) > 1 && argument[0] == '-':
			var asked bool
			consumed, asked, failure = readShorthandFlags(namespace, routed.Command, remaining, &arguments)
			help = help || asked
		default:
			arguments = append(arguments, argument)
			consumed = 1
		}
		if failure != nil {
			return productLine{}, failure
		}
		if consumed > 0 {
			index += consumed - 1
		}
	}
	// The resolved path replaces the words that were typed, so an alias reaches
	// the module under the name the module serves.
	return productLine{
		command:     routed.Command.Path,
		arguments:   arguments,
		mode:        flags.mode,
		contextName: flags.contextName,
		help:        help,
		declared:    routed.Command,
	}, nil
}

// productLine is one product command line as the shell read it.
//
// The pieces travel together because they are one reading of one line: which
// command it names, what the module is given, and what the shell kept for
// itself. Returning them separately was five values every caller and every test
// had to keep in the right order.
type productLine struct {
	command     []string
	arguments   []string
	mode        output.Mode
	contextName string
	// help reports that the line asked for help rather than for the command to
	// run. The shell answers it from the declaration and launches nothing.
	help bool
	// declared is the command the line resolved to, empty for a module that
	// declares no tree.
	declared commandtree.Command
}

// parseUndeclaredProductArgs is how every module was parsed before any of them
// declared a command tree, and how one that still declares none is parsed now.
//
// The shell reads the flags it owns, takes the leading run of plain words as the
// command, and stops at the first flag it does not recognise, because without a
// declaration it cannot tell whether the word after that flag is the flag's
// value or a command of its own. Everything from there is the module's to
// interpret or refuse.
func parseUndeclaredProductArgs(namespace string, args []string) (productLine, error) {
	var command []string
	flags := shellFlags{mode: output.ModeTable}
	remaining := args
	for len(remaining) > 0 {
		consumed, failure := flags.read(namespace, remaining)
		if failure != nil {
			return productLine{}, failure
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
	// Help is not answered for a module that declares nothing: there is no
	// declaration to answer from, so --help travels to the module as it always
	// did.
	return productLine{
		command: command, arguments: remaining,
		mode: flags.mode, contextName: flags.contextName,
	}, nil
}

// readLongFlag reads one of the module's long flags and the value it carries.
func readLongFlag(namespace string, found commandtree.Command, args []string,
	arguments *[]string) (consumed int, help bool, err error) {
	name, value, attached := strings.Cut(strings.TrimPrefix(args[0], "--"), "=")
	flag, declared := found.LookupFlag(name)
	if !declared {
		return 0, false, unknownProductFlag(namespace, found, "--"+name)
	}
	// Whether this is a request for help is answered here, where the word is
	// known to be a flag. Looking for it in the finished argument list instead
	// would find the one in "--since --help", which is the value --since
	// claimed, not a request for anything.
	//
	// An attached value settles it: pflag reads "--help=false" as false and
	// Cobra shows no help for it, so neither does the shell.
	help = name == commandtree.HelpFlagName && !flag.TakesValue() && asksForHelp(value, attached)
	*arguments = append(*arguments, args[0])
	if !flag.TakesValue() || attached {
		return 1, help, nil
	}
	if len(args) < 2 {
		return 0, false, missingProductFlagValue(namespace, found, "--"+name)
	}
	*arguments = append(*arguments, args[1])
	return 2, help, nil
}

// readShorthandFlags reads a run of single-letter flags written as one argument.
//
// pflag lets "-ab" stand for "-a -b" while a letter that takes a value ends the
// run and claims the rest, so "-abvalue" is "-a -b value". Reading it the same
// way is what keeps a declaration honest: a shell that guessed differently from
// the module it forwards to would send a value the module never saw as one.
func readShorthandFlags(namespace string, found commandtree.Command, args []string,
	arguments *[]string) (consumed int, help bool, err error) {
	letters := []rune(args[0][1:])
	for index, letter := range letters {
		flag, declared := found.LookupShorthand(letter)
		if !declared {
			return 0, false, unknownProductFlag(namespace, found, "-"+string(letter))
		}
		// An equals sign directly after a letter gives that letter the rest of
		// the run as its value, whatever its type — pflag reads "-a=false" as
		// false, and "-ab=false" as a set and b false. Checking this before the
		// type is what keeps an explicit value from being read as more letters.
		if index+1 < len(letters) && letters[index+1] == '=' {
			*arguments = append(*arguments, args[0])
			return 1, flag.Name == commandtree.HelpFlagName &&
				asksForHelp(string(letters[index+2:]), true), nil
		}
		if !flag.TakesValue() {
			help = help || flag.Name == commandtree.HelpFlagName
			continue
		}
		*arguments = append(*arguments, args[0])
		// The rest of the run is the value. Only an empty rest sends the
		// parser to the next argument.
		if rest := string(letters[index+1:]); rest != "" {
			return 1, help, nil
		}
		if len(args) < 2 {
			return 0, false, missingProductFlagValue(namespace, found, "-"+string(letter))
		}
		*arguments = append(*arguments, args[1])
		return 2, help, nil
	}
	*arguments = append(*arguments, args[0])
	return 1, help, nil
}

// asksForHelp reports whether a help flag written with an explicit value is
// asking for help. Written bare it always is; written with a value it is
// whatever pflag would read that value as, so "--help=false" runs the command.
func asksForHelp(value string, attached bool) bool {
	if !attached {
		return true
	}
	asked, err := strconv.ParseBool(value)
	return err == nil && asked
}

// unknownProductCommand reports a word that names no command the module serves.
//
// Naming it here is the point of the declaration. Before one existed the words
// went to the module, which answered for itself and could not be asked what it
// would have accepted, so the shell had nothing to suggest.
func unknownProductCommand(namespace, typed string, declared parsetree.Tree) problem.Problem {
	reported := problem.New(problem.CategoryUsage, "shell.unknown_product_command",
		fmt.Sprintf("the %s module has no %q command", namespace, typed))
	if closest := closestCommand(declared, typed); closest != "" {
		return reported.WithRecovery(fmt.Sprintf("Did you mean wso2 %s %s?", namespace, closest))
	}
	return reported.WithRecovery(fmt.Sprintf("Run wso2 %s --help to see what it does.", namespace))
}

// suggestionDistance is how far a mistyped word may be from a real command
// before the shell stops guessing. It is one number because a suggestion for a
// product command and one for a built-in are the same offer to the same user;
// two would be a difference nobody decided on.
const suggestionDistance = 2

// closestCommand reports the declared command nearest to what was typed, or the
// empty string when nothing is near enough to be worth offering.
func closestCommand(declared parsetree.Tree, typed string) string {
	best, bestDistance := "", 0
	for _, candidate := range declared.Commands() {
		if len(candidate.Path) != 1 {
			continue
		}
		name := candidate.Path[0]
		// A product command is offered on the same terms as a built-in one,
		// so the threshold is the one the root command hands Cobra rather
		// than a second number that could drift from it. Cobra keeps its own
		// distance function unexported, which is why the measure is written
		// out here and the threshold is not.
		distance := editDistance(typed, name)
		if distance > suggestionDistance {
			continue
		}
		if best == "" || distance < bestDistance {
			best, bestDistance = name, distance
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
//
// One spelling is singled out. The shell's own shorthand can be written beside a
// module's — "-o json" — but not inside a run of the module's letters, because a
// run belongs to one flag set and this one is the shell's. Saying the command
// does not take it would be untrue, and would send a reader looking through the
// module's documentation for a flag that is not the module's.
func unknownProductFlag(namespace string, found commandtree.Command, spelling string) problem.Problem {
	label := commandLabel(namespace, found)
	if shellShorthand(spelling) {
		return problem.New(problem.CategoryUsage, "shell.shell_flag_in_a_product_run",
			fmt.Sprintf("%s is the shell's own flag and cannot be joined to %s's flags", spelling, label)).
			WithRecovery(fmt.Sprintf("Write it on its own, as %s ... %s <mode>.", label, spelling))
	}
	return problem.New(problem.CategoryUsage, "shell.unknown_product_flag",
		fmt.Sprintf("%s does not take %s", label, spelling)).
		WithRecovery(fmt.Sprintf("Run %s --help to see the flags it accepts.", label))
}

// shellShorthand reports whether a single-letter spelling is one the shell owns.
func shellShorthand(spelling string) bool {
	return spelling == "-o"
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
