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
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/internal/preferences"
	"github.com/wso2/wso2-cli/internal/version"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// The flags every shell command shares, declared once on the root command.
const (
	contextFlag = "context"
	outputFlag  = "output"
	verboseFlag = "verbose"
)

// helpTemplate preserves the help shape the shell published before Cobra routed
// it. The wording is a user-facing contract, so it is templated rather than
// left to Cobra's default layout. What Cobra supplies is that the command list
// is walked from the real tree, so it cannot omit a command that exists.
const helpTemplate = `Usage: {{.UseLine}}
{{if .Long}}
{{.Long}}
{{end}}{{if .HasAvailableSubCommands}}
Shell commands
{{range .Commands}}{{if or .IsAvailableCommand (eq .Name "help")}}   {{rpad .Name .NamePadding}}   {{.Short}}
{{end}}{{end}}{{end}}{{if .HasAvailableFlags}}
Flags
{{.Flags.FlagUsages}}{{end}}{{if not .HasParent}}
Product commands are provided by installed modules.
{{end}}`

// rootCommand builds the shell's command tree.
//
// Only shell-owned commands are registered. A product namespace is resolved
// from the managed module store instead, so built-in precedence stays a
// property of dispatch order rather than an interaction between Cobra's command
// lookup and a command set discovered at runtime, and so asking for help reads
// no module store.
func (s Shell) rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:  "wso2 <command> [arguments]",
		Args: cobra.ArbitraryArgs,
		// The shell reports every failure as a typed problem through one exit
		// path, so Cobra must not write errors or usage itself.
		SilenceErrors: true,
		SilenceUsage:  true,
		// A shell flag is accepted on either side of a command name.
		TraverseChildren:      true,
		DisableFlagsInUseLine: true,
		// The shell offers its own suggestions, so that they can later cover
		// resolved namespaces as well as built-in commands.
		DisableSuggestions: true,
		// SuggestionsFor is used directly, so the distance Cobra would default
		// during Execute has to be set here.
		SuggestionsMinimumDistance: suggestionDistance,
		PersistentPreRunE:          s.applyShellFlags,
		// Arguments left after the root's own flags are a product namespace and
		// its arguments. Reaching them here is how a shell flag written before
		// the namespace is honored.
		RunE: func(command *cobra.Command, args []string) error {
			if len(args) == 0 {
				return s.help(command.Root())
			}
			// A product namespace honors every shell flag, and its own parser
			// reads them back off the argument list.
			forwarded, err := forwardShellFlags(command, args[1:])
			if err != nil {
				return err
			}
			return s.dispatchNamespace(command.Root(), args[0], forwarded)
		},
	}
	// Completion is deliberately absent. Until a module declares its command
	// tree, a generated completion would know every built-in and no product
	// command, which reads as "that command does not exist" rather than as
	// missing information.
	root.CompletionOptions.DisableDefaultCmd = true
	// Only a flag-parsing failure becomes a usage problem. Cobra reports one
	// through this hook, so wrapping here keeps every other error a command
	// returns — an unwritable stream, a failed lookup — classified as what it
	// is instead of as the user's mistake.
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageProblem(err)
	})
	root.SetHelpTemplate(helpTemplate)
	root.SetUsageTemplate(helpTemplate)
	root.SetOut(s.Streams.Out)
	root.SetErr(s.Streams.Err)

	// Flag parsing stops at the first argument that is not a flag. Everything
	// after it may be a product namespace and the module's own flags, and those
	// must reach the module verbatim rather than being parsed here. Without
	// this, "wso2 --context prod api list --env stage" fails on --env, which is
	// the module's flag and none of the shell's business.
	root.Flags().SetInterspersed(false)

	// The help flag is declared rather than left to Cobra so its description
	// reads in the shell's voice rather than as the framework's default.
	root.PersistentFlags().BoolP("help", "h", false, "Show help for a command.")
	root.PersistentFlags().String(contextFlag, "", "Use the named context instead of the selected one.")
	root.PersistentFlags().StringP(outputFlag, "o", string(output.ModeTable), "Render results as table or json.")
	// Declared here rather than scanned out of the argument list by hand, so
	// that it appears in help, is refused with a value, and is parsed by the
	// same code that parses every other shell flag.
	root.PersistentFlags().Bool(verboseFlag, false, "Write diagnostics about what the shell attempted to stderr.")

	root.AddCommand(s.configCommand(), s.contextCommand(), s.doctorCommand(), s.identityCommand(),
		s.loginCommand(), s.logoutCommand(), s.moduleCommand(), s.orgCommand(), s.versionCommand(),
		s.whoamiCommand())

	// Cobra's generated help command describes itself generically. The shell
	// published its own summary for it, and that wording is kept.
	root.InitDefaultHelpCmd()
	for _, command := range root.Commands() {
		if command.Name() == "help" {
			command.Short = "Show the shell command tree."
		}
	}
	return root
}

// applyShellFlags refuses an unusable value for a shell-owned flag before any
// command runs, so the refusal is one typed problem rather than one per command,
// and turns on the diagnostic log when the user asked for it.
//
// The log is opened here because this is the first point at which the flags are
// parsed and no command body has run yet: a failure inside the very first thing
// a command does is still explained.
func (s Shell) applyShellFlags(command *cobra.Command, _ []string) error {
	// The preferences diagnostic used to be surfaced here, once per
	// invocation. It moved to dispatch, before this and the product-namespace
	// path fork apart (fix round 1, F1): this hook is Cobra's own
	// PersistentPreRunE, which a product namespace never triggers at all, so
	// the diagnostic silently never fired for the ordinary case of a wso2
	// <namespace> command. See dispatch's comment in app.go.
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	// The error is discarded rather than reported: it can only mean the flag is
	// absent or is not a boolean, and both are this file's own mistake to make,
	// not a user's to be told about.
	if verbose, _ := command.Flags().GetBool(verboseFlag); verbose {
		s.enableDiagnostics(command, mode)
	}
	return nil
}

// shellOutputMode reports the rendering this invocation asked for, refusing an
// unusable value before any command runs.
//
// The --output flag wins when given. Otherwise the "output" preference
// (wso2 config set output table|json) wins over the built-in default,
// output.ModeTable — configuration is always the new, lowest layer above a
// built-in default, never something that overrides a more specific source.
func (s Shell) shellOutputMode(command *cobra.Command) (output.Mode, error) {
	flag := shellFlag(command, outputFlag)
	if flag != nil && flag.Changed {
		mode, ok := output.ParseMode(flag.Value.String())
		if !ok {
			return "", problem.New(problem.CategoryUsage, "shell.unknown_output_mode",
				fmt.Sprintf("%q is not an output mode", flag.Value.String())).
				WithRecovery("Use --output table or --output json.")
		}
		return mode, nil
	}
	root, err := s.stateRoot()
	if err != nil {
		// Left to the caller: nearly every command resolves its own state root
		// immediately afterward and reports this properly. Defaulting to
		// output.ModeTable here costs nothing, because the command never
		// reaches a point where that default is rendered.
		return output.ModeTable, nil
	}
	document, _ := preferences.Load(root)
	if configured, set := document.Get(preferences.KeyOutputMode); set {
		if mode, ok := output.ParseMode(configured); ok {
			return mode, nil
		}
		// A stored value Set already validates cannot fail to parse here in
		// production; this is only reached by a document edited by hand.
		// Falling back rather than refusing keeps R9's asymmetry: a bad
		// preference must not break every command that reads it.
	}
	return output.ModeTable, nil
}

// enableDiagnostics opens the diagnostic log, once.
//
// Diagnostics go to the stream that carries diagnostics, never to the one that
// carries the result, so --verbose cannot break a caller parsing standard
// output. See docs/adr/0003-shell-owned-output.md.
//
// It is idempotent because there are two doors into it: the root's flag
// parser, which every built-in command now inherits --verbose through
// wherever it is written, and the product-namespace path's own takeVerbose
// scan (app.go), which still parses its own arguments by hand because a
// module's flags must reach it unparsed. A user who writes the flag through
// both doors — before a shell flag and after a product namespace's own — asked
// for diagnostics once.
func (s Shell) enableDiagnostics(command *cobra.Command, mode output.Mode) {
	if s.log.Enabled() {
		return
	}
	s.log.Enable(s.Streams.Err, mode)
	// The first line every verbose run writes, so that a bug report pasted
	// from a terminal names the shell that produced it. The argument list
	// is deliberately absent: a product command's arguments belong to the
	// module, and a module is free to take a credential as one.
	s.log.Debug("the shell started",
		"command", command.Name(),
		"shell_version", version.Shell(),
		"platform", version.Platform(),
		"output_mode", string(mode))
}

// takeVerbose removes every spelling of --verbose from an argument list and
// reports whether the list asked for diagnostics.
//
// The last occurrence wins, because that is what pflag does with the same
// argument list before a command name. A spelling that means one thing written
// before the command and another written after it would be a worse answer than
// refusing the flag was: the user would be reading a log they had switched off.
func takeVerbose(args []string) (remaining []string, asked bool, err error) {
	remaining = make([]string, 0, len(args))
	for _, argument := range args {
		switch {
		case argument == "--"+verboseFlag:
			asked = true
		case strings.HasPrefix(argument, "--"+verboseFlag+"="):
			value := strings.TrimPrefix(argument, "--"+verboseFlag+"=")
			enabled, parseErr := strconv.ParseBool(value)
			if parseErr != nil {
				return nil, false, usageProblem(fmt.Errorf("invalid argument %q for %q flag: %w",
					value, "--"+verboseFlag, parseErr))
			}
			asked = enabled
		default:
			remaining = append(remaining, argument)
		}
	}
	return remaining, asked, nil
}

// diagnosticMode reports the rendering the diagnostics must follow.
//
// The arguments are read before the parsed flag because the commands that reach
// here have disabled Cobra's flag parsing: for "wso2 logout --output json" the
// root's flag is still at its default while the command's own parser renders
// JSON, and diagnostics interleaved with a machine-readable result have to be
// machine-readable too. An unusable value is left in place rather than refused
// here, so that the parser that owns the flag is the one that explains it.
func (s Shell) diagnosticMode(command *cobra.Command, args []string) (output.Mode, error) {
	if mode, found := argumentOutputMode(args); found {
		return mode, nil
	}
	return s.shellOutputMode(command)
}

// argumentOutputMode reports the rendering an argument list names, in every
// spelling parseProductArgs accepts, and whether it named one at all. The last
// occurrence wins, as it does for every other flag pflag parses.
func argumentOutputMode(args []string) (output.Mode, bool) {
	var (
		mode  output.Mode
		found bool
	)
	for index, argument := range args {
		var value string
		switch {
		case argument == "--"+outputFlag || argument == "-o":
			if index+1 >= len(args) {
				continue
			}
			value = args[index+1]
		case attachedOutput(argument):
			value, _ = outputFlagValue(argument)
		default:
			continue
		}
		if parsed, ok := output.ParseMode(value); ok {
			mode, found = parsed, true
		}
	}
	return mode, found
}

// shellFlag finds a shell-owned flag whether or not the command it is asked
// about parses flags. A command with parsing disabled still inherits the root's
// persistent flag set, but does not always have it merged into its own.
func shellFlag(command *cobra.Command, name string) *pflag.Flag {
	if flag := command.Flags().Lookup(name); flag != nil {
		return flag
	}
	return command.Root().PersistentFlags().Lookup(name)
}

// shellFlagsFor reports the shell-owned flags a built-in command honors.
//
// A shell flag is declared once on the root, but not every command can act on
// every one of them: version and the module lifecycle commands render their own
// fixed output and select no context. Naming what each command honors keeps a
// flag it cannot act on a refusal rather than a value silently ignored.
//
// A known consequence, left as it is: because the flags are persistent on the
// root, --help lists --context under a family that refuses it, and the
// refusal's recovery points back at that help. It has been true of context
// and identity since they shipped, and config and org inherit it. The fix is
// not local to this function — it means declaring the shell flags per command
// instead of once on the root, which is the same change that would retire
// forwardShellFlags — so making it here for two families and not the others
// would trade one inconsistency for a worse one.
func shellFlagsFor(name string) []string {
	// --verbose is deliberately absent from every list. This set answers one
	// question — which flags forwardShellFlags may re-attach to a command's own
	// parser, and which it must refuse — and --verbose is outside that question
	// entirely: applyShellFlags enables the log directly off the root's flag
	// set, whoever the command is, and the flag is forwarded to nothing. Naming
	// it here would be a claim no reader of this set ever checks.
	switch name {
	case "wso2":
		// The root routes a product namespace, and a module command may act on
		// every forwarded shell flag.
		return []string{contextFlag, outputFlag}
	case "config":
		// Preferences are machine-local, not context-scoped: a saved output
		// mode or catalog origin applies to every context on this machine,
		// so --context has nothing to select here.
		return []string{outputFlag}
	case "context":
		// The family renders a machine-readable result, and takes no --context:
		// naming a context is what its own arguments do, and a selection flag
		// alongside "wso2 context use beta" would be two answers to one
		// question.
		return []string{outputFlag}
	case "doctor":
		// doctor reports ON a selected context, so naming one with --context is
		// meaningful, and its findings are read by scripts as much as by a
		// person.
		return []string{contextFlag, outputFlag}
	case "identity":
		// The family renders a machine-readable result, and takes no
		// --context: an identity is named by this family's own arguments, and
		// a selection flag alongside "wso2 identity list" would be a second
		// answer to a question nothing asked.
		return []string{outputFlag}
	case "login":
		return []string{contextFlag}
	case "module":
		// Every module lifecycle command renders fixed, non-JSON text and
		// selects no context: an install or an update names its target as an
		// argument, not by --context, and its report is prose meant to be
		// read, not a schema a script parses. moduleinstall_test.go's
		// TestVerboseInstallKeepsProgressOffStdout confirms by hand that
		// wso2 module install <module> --output json is refused outright, and
		// this is the entry that refusal comes from.
		return nil
	case "logout":
		// The only interactive-auth command that renders a machine-readable
		// result, because it is the only one whose result a script has to read:
		// what the issuer was told about the ended session is not observable
		// any other way.
		return []string{contextFlag, outputFlag}
	case "org":
		// The family always acts on the selected context, exactly as wso2
		// context does: naming one with --context would be a second answer to
		// a question the family does not ask, since org use writes the
		// selected context's Organization field, not some other one's.
		return []string{outputFlag}
	case "whoami":
		// whoami reports ON a selected context exactly as doctor does (R5,
		// #112), so naming one with --context is meaningful for the same
		// reason.
		return []string{contextFlag, outputFlag}
	default:
		return nil
	}
}

// forwardShellFlags re-attaches the shell flags Cobra parsed to the argument
// list a built-in command's own parser expects, and refuses a flag the command
// cannot act on.
//
// The re-attachment is deliberate scaffolding. The built-in command bodies still
// parse their own arguments, so this change alters routing and not flag
// semantics, and the existing tests stay a regression suite for it. It goes away
// when each command declares its flags directly.
func forwardShellFlags(command *cobra.Command, args []string) ([]string, error) {
	honored := shellFlagsFor(command.Name())
	forwarded := make([]string, 0, len(args)+4)
	// --verbose is honored by every command but forwarded to none of them. A
	// built-in reads it off the root's own flag set, and the product namespace
	// boundary cannot forward it yet: until a module declares its command tree,
	// the shell cannot tell a flag it should pass on from one the module owns,
	// and a module that does not know the flag would refuse the whole command.
	for _, name := range []string{contextFlag, outputFlag} {
		flag := command.Root().PersistentFlags().Lookup(name)
		if flag == nil || !flag.Changed {
			continue
		}
		if !slices.Contains(honored, name) {
			return nil, problem.New(problem.CategoryUsage, "shell.unsupported_flag",
				fmt.Sprintf("wso2 %s does not take the flag --%s", command.Name(), name)).
				WithRecovery(fmt.Sprintf("Run wso2 %s --help to see the flags it accepts.", command.Name()))
		}
		forwarded = append(forwarded, "--"+name, flag.Value.String())
	}
	return append(forwarded, args...), nil
}

// loginCommand declares its own flags directly, and reads the two the shell
// declares once on the root (--context, --verbose) off the parsed flag set
// rather than a second time here — declaring them again would shadow the
// root's and leave two flags of one name disagreeing about what was asked.
//
// Cobra's allowlist for unknown flags could not have been used to reach this
// for any of the commands that used to disable flag parsing (login, logout,
// module): it does not forward an unknown flag, it discards it together with
// its value, so a command would have run without a flag the user gave and
// without any diagnostic. Declaring the flags directly, as all three now do,
// is what let each one drop DisableFlagParsing instead.
func (s Shell) loginCommand() *cobra.Command {
	var flags loginFlags
	command := &cobra.Command{
		Use:                   "login",
		Short:                 "Log in, creating the identity and context when an issuer is named.",
		DisableFlagsInUseLine: true,
		Args:                  noArguments(loginUsageRecovery),
		RunE: func(command *cobra.Command, args []string) error {
			// The returned list is discarded: login declares its own flags, so
			// nothing is forwarded anywhere and what is wanted is the refusal
			// of a shell flag this command cannot act on.
			if _, err := forwardShellFlags(command, nil); err != nil {
				return err
			}
			// --context is the shell's own flag, declared once on the root, so
			// it is read off the parsed flag set rather than declared a second
			// time here. Declaring it again would shadow the root's and leave
			// two flags of one name disagreeing about what was asked.
			if flag := shellFlag(command, contextFlag); flag != nil {
				flags.contextName = flag.Value.String()
			}
			return s.login(flags)
		},
	}
	command.Flags().StringVar(&flags.issuer, "url", "",
		"Log in against this issuer, creating the identity and context it authenticates.")
	command.Flags().StringVar(&flags.clientID, "client-id", "",
		"Present this registered OAuth application. Required with --url.")
	command.Flags().BoolVar(&flags.noInput, "no-input", false,
		"Refuse rather than prompt, open a browser, or wait for a human.")
	return command
}

func (s Shell) logoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:                   "logout",
		Short:                 "End the selected context's session.",
		DisableFlagsInUseLine: true,
		Args:                  noArguments(logoutUsageRecovery),
		RunE: func(command *cobra.Command, args []string) error {
			// Both flags logout honors are the root's own, declared once on
			// PersistentFlags. shellFlagsFor("logout") lists both, so nothing
			// is refused here the way refuseUnusableShellFlags refuses a flag
			// a family cannot act on elsewhere: this family can act on both.
			mode, err := s.shellOutputMode(command)
			if err != nil {
				return err
			}
			var contextName string
			if flag := shellFlag(command, contextFlag); flag != nil {
				contextName = flag.Value.String()
			}
			return s.logout(logoutFlags{contextName: contextName, mode: mode})
		},
	}
}

func (s Shell) versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:                   "version",
		Short:                 "Show the shell, protocol, and installed module versions.",
		DisableFlagsInUseLine: true,
		RunE: func(command *cobra.Command, args []string) error {
			if _, err := forwardShellFlags(command, args); err != nil {
				return err
			}
			return s.version(args)
		},
	}
}

// usageProblem wraps a flag-parsing failure as a typed problem.
//
// Cobra and pflag report a parse failure as a plain error with no category and
// no recovery guidance, and the process would exit 1. The shell's exit classes
// are a documented contract, so a parse failure has to arrive as a usage
// problem like any other.
//
// It is reached only from the flag-error hook, so every error it sees is a parse
// failure. An error a command body returns is left alone for the shell's own
// classification.
func usageProblem(err error) error {
	var typed problem.Problem
	if errors.As(err, &typed) {
		return typed
	}
	message := err.Error()
	code := "shell.flag_invalid"
	switch {
	case strings.Contains(message, "needs an argument"):
		code = "shell.flag_needs_value"
	case strings.Contains(message, "unknown flag"), strings.Contains(message, "unknown shorthand flag"):
		code = "shell.unknown_flag"
	}
	return problem.New(problem.CategoryUsage, code, message).
		WithRecovery("Run wso2 help to see the shell commands and the flags they accept.")
}

// usageProblemWithRecovery classifies a flag-parsing failure exactly as
// usageProblem does, but points the recovery at the command that failed
// rather than at the generic "wso2 help".
//
// A command whose own flag set is worth naming in the recovery — wso2 module
// update's --yes, --dry-run, and --no-input, for instance, which a mistyped
// flag most needs reminding of — sets this as its own FlagErrorFunc instead
// of inheriting the root's. It is still reached only from a flag-error hook,
// so, like usageProblem, every error it sees is a parse failure.
func usageProblemWithRecovery(err error, recovery string) error {
	wrapped := usageProblem(err)
	var typed problem.Problem
	if errors.As(wrapped, &typed) {
		return typed.WithRecovery(recovery)
	}
	return wrapped
}

// suggestionFor reports the shell command closest to an unrecognized name, so a
// typo costs a keystroke rather than a search through the documentation.
func suggestionFor(root *cobra.Command, name string) string {
	candidates := root.SuggestionsFor(name)
	if len(candidates) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		quoted = append(quoted, "wso2 "+candidate)
	}
	return fmt.Sprintf("Did you mean %s?", strings.Join(quoted, " or "))
}
