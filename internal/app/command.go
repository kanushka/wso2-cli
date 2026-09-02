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
		SuggestionsMinimumDistance: 2,
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
			return s.dispatchNamespace(command.Root(), args[0],
				forwardToNamespace(command, args[1:]))
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
	root.SetFlagErrorFunc(func(command *cobra.Command, err error) error {
		return flagProblem(command, err)
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

	// --help and --verbose are persistent because every command honors them:
	// help is universal, and applyShellFlags turns the diagnostic log on off
	// the root's own flag set whoever the command is.
	//
	// --context and --output are not. They are declared by each command that
	// can act on one, and here only for the root's own job of dispatching a
	// product namespace, which honors both. Declaring them persistently is what
	// used to put them in every command's help while forwardShellFlags refused
	// them, so that help advertised what the command rejected and the refusal
	// pointed back at the help (#147).
	root.PersistentFlags().BoolP("help", "h", false, "Show help for a command.")
	root.PersistentFlags().Bool(verboseFlag, false, "Write diagnostics about what the shell attempted to stderr.")
	declareContextFlag(root.Flags())
	declareOutputFlag(root.Flags())

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
	if err := refuseShellFlagsWrittenEarly(command); err != nil {
		return err
	}
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

// shellFlag finds a shell-owned flag on the command that is running, or on a
// family it inherits one from, honoring the spelling written before the command
// name.
//
// A shell flag is accepted on either side of a command name, and the root
// parses the ones written before it into its own flag set. So "wso2 --output
// json whoami" and "wso2 whoami --output json" have to reach the same value
// from two different flag sets. The command's own wins when it was given, and
// the root's stands in when it was not.
//
// The command's declaration remains the whole truth about what it accepts: the
// root's copy is only consulted for a flag the command itself declares. A flag
// written before the name of a command that does not take it is refused by
// refuseShellFlagsWrittenEarly, not silently honored here.
func shellFlag(command *cobra.Command, name string) *pflag.Flag {
	own := command.Flags().Lookup(name)
	if own != nil && own.Changed {
		return own
	}
	if own == nil {
		return nil
	}
	if early := command.Root().Flags().Lookup(name); early != nil && early.Changed {
		return early
	}
	return own
}

// refuseShellFlagsWrittenEarly refuses a shell flag written before the name of a
// command that cannot act on it.
//
// Cobra parses a flag written before the command name into the root's flag set,
// so the command's own parser never sees it and cannot refuse it. Without this,
// "wso2 --output json version" would be accepted and silently ignored, while
// "wso2 version --output json" is refused — the same request answered two ways
// depending on where it was written.
func refuseShellFlagsWrittenEarly(command *cobra.Command) error {
	if command == command.Root() {
		return nil
	}
	for _, name := range []string{contextFlag, outputFlag} {
		early := command.Root().Flags().Lookup(name)
		if early == nil || !early.Changed {
			continue
		}
		if command.Flags().Lookup(name) != nil {
			continue
		}
		return unsupportedFlag(command, name)
	}
	return nil
}

// declareContextFlag declares --context on a command that acts on a named
// context.
//
// A command that selects no context does not call this, and a user who writes
// --context on one gets Cobra's own refusal, classified by flagProblem. There
// is no allowlist to keep in step with the declaration: the declaration is the
// allowlist.
func declareContextFlag(flags *pflag.FlagSet) {
	flags.String(contextFlag, "", "Use the named context instead of the selected one.")
}

// declareOutputFlag declares --output on a command that renders a
// machine-readable result.
func declareOutputFlag(flags *pflag.FlagSet) {
	flags.StringP(outputFlag, "o", string(output.ModeTable), "Render results as table or json.")
}

// forwardToNamespace re-attaches the shell flags Cobra parsed to the arguments
// bound for a product module, whose own parser reads them back off the list.
//
// The root honors both flags, so unlike the built-ins there is nothing to
// refuse here: what this does is carry a flag written before the namespace
// ("wso2 --output json api list") through to the parser that reads one written
// after it.
func forwardToNamespace(command *cobra.Command, args []string) []string {
	forwarded := make([]string, 0, len(args)+4)
	for _, name := range []string{contextFlag, outputFlag} {
		flag := command.Flags().Lookup(name)
		if flag == nil || !flag.Changed {
			continue
		}
		forwarded = append(forwarded, "--"+name, flag.Value.String())
	}
	return append(forwarded, args...)
}

// flagProblem classifies a flag-parsing failure, telling a flag that does not
// exist anywhere from one the shell owns that this command cannot act on.
//
// The distinction is worth keeping and used to be drawn by a hand-maintained
// allowlist. It is drawn here from the command tree instead: a flag the root
// declares is a real shell flag, so naming it on a command that did not declare
// it is a different mistake from a typo, and deserves a different message. The
// command that failed is named, rather than the family it belongs to, because
// naming the family sent the user to a help page for a command they had not
// typed (#147).
func flagProblem(command *cobra.Command, err error) error {
	name, ok := unknownFlagName(err)
	if !ok {
		return usageProblem(err)
	}
	if !ownsShellFlag(command.Root(), name) {
		return usageProblem(err)
	}
	return unsupportedFlag(command, name)
}

// flagProblemWithRecovery classifies as flagProblem does, but points a failure
// that is a plain typo at the command's own usage line. A shell flag the command
// cannot act on keeps flagProblem's recovery, which names the help that lists
// what it does accept.
func flagProblemWithRecovery(command *cobra.Command, err error, recovery string) error {
	if name, ok := unknownFlagName(err); ok && ownsShellFlag(command.Root(), name) {
		return unsupportedFlag(command, name)
	}
	return usageProblemWithRecovery(err, recovery)
}

// unsupportedFlag reports a shell-owned flag named on a command that does not
// declare it, naming the command typed rather than the family it belongs to.
func unsupportedFlag(command *cobra.Command, name string) error {
	path := command.CommandPath()
	return problem.New(problem.CategoryUsage, "shell.unsupported_flag",
		fmt.Sprintf("%s does not take the flag --%s", path, name)).
		WithRecovery(fmt.Sprintf("Run %s --help to see the flags it accepts.", path))
}

// ownsShellFlag reports whether a name is a shell-owned flag at all, which is
// what separates "this command does not take it" from "no such flag".
func ownsShellFlag(root *cobra.Command, name string) bool {
	if root.PersistentFlags().Lookup(name) != nil {
		return true
	}
	return root.Flags().Lookup(name) != nil
}

// unknownFlagName reads the flag name out of Cobra's unknown-flag error.
//
// Cobra reports the failure as text and offers no structured form of it, so the
// name is recovered from the message. A message this does not recognize falls
// back to usageProblem, which reports it verbatim: a worse message, never a
// wrong one.
func unknownFlagName(err error) (string, bool) {
	const prefix = "unknown flag: --"
	message := err.Error()
	index := strings.Index(message, prefix)
	if index < 0 {
		return "", false
	}
	name := message[index+len(prefix):]
	if cut := strings.IndexAny(name, "= "); cut >= 0 {
		name = name[:cut]
	}
	if name == "" {
		return "", false
	}
	return name, true
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
			// login logs in to a named context, so it declares --context. It
			// renders no machine-readable result and does not declare
			// --output: what a login writes is prose about a browser and a
			// session, and a caller that wants the outcome as data reads
			// wso2 whoami afterwards.
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
	declareContextFlag(command.Flags())
	return command
}

func (s Shell) logoutCommand() *cobra.Command {
	command := &cobra.Command{
		Use:                   "logout",
		Short:                 "End the selected context's session.",
		DisableFlagsInUseLine: true,
		Args:                  noArguments(logoutUsageRecovery),
		RunE: func(command *cobra.Command, args []string) error {
			// logout declares both shell flags: it acts on a named context,
			// and it is the only interactive-auth command that renders a
			// machine-readable result, because what the issuer was told about
			// the ended session is not observable any other way.
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
	declareContextFlag(command.Flags())
	declareOutputFlag(command.Flags())
	return command
}

func (s Shell) versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:                   "version",
		Short:                 "Show the shell, protocol, and installed module versions.",
		DisableFlagsInUseLine: true,
		// version declares neither shell flag. It renders fixed prose about
		// this build and the modules installed beside it, and selects no
		// context.
		RunE: func(command *cobra.Command, args []string) error {
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
