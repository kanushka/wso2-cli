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

	root.AddCommand(s.contextCommand(), s.identityCommand(), s.loginCommand(),
		s.logoutCommand(), s.moduleCommand(), s.versionCommand())

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
	mode, err := shellOutputMode(command)
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
func shellOutputMode(command *cobra.Command) (output.Mode, error) {
	flag := shellFlag(command, outputFlag)
	if flag == nil || !flag.Changed {
		return output.ModeTable, nil
	}
	mode, ok := output.ParseMode(flag.Value.String())
	if !ok {
		return "", problem.New(problem.CategoryUsage, "shell.unknown_output_mode",
			fmt.Sprintf("%q is not an output mode", flag.Value.String())).
			WithRecovery("Use --output table or --output json.")
	}
	return mode, nil
}

// enableDiagnostics opens the diagnostic log, once.
//
// Diagnostics go to the stream that carries diagnostics, never to the one that
// carries the result, so --verbose cannot break a caller parsing standard
// output. See docs/adr/0003-shell-owned-output.md.
//
// It is idempotent because there are two doors into it: the root's flag parser
// for a flag written before the command name, and takeVerboseFlag for one
// written after it. A user who writes the flag in both places asked for
// diagnostics once.
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

// takeVerboseFlag removes --verbose from an argument list a built-in command
// parses for itself, and turns the diagnostic log on when it was there.
//
// The two commands that still disable Cobra's flag parsing — logout and module
// — never see the root's parse, so a flag written after the command name
// arrives in their own argument list and their own parsers refuse it as
// unknown. That refusal is worst for this flag in particular: it arrives while
// the user is already trying to diagnose something else. login no longer needs
// this: it declares its flags, so the root parses --verbose wherever it is
// written (#118).
//
// There is one of these rather than one scanner per command on purpose.
// --context is scanned separately by each of the two, and those scanners have
// already drifted apart from one another. This is the one place
// --verbose is read out of an argument list, and the one place to delete when
// each command declares its flags directly.
func (s Shell) takeVerboseFlag(command *cobra.Command, args []string) ([]string, error) {
	remaining := make([]string, 0, len(args))
	asked := false
	for _, argument := range args {
		switch {
		case argument == "--"+verboseFlag:
			asked = true
		case strings.HasPrefix(argument, "--"+verboseFlag+"="):
			value := strings.TrimPrefix(argument, "--"+verboseFlag+"=")
			enabled, err := strconv.ParseBool(value)
			if err != nil {
				return nil, usageProblem(fmt.Errorf("invalid argument %q for %q flag: %w",
					value, "--"+verboseFlag, err))
			}
			asked = asked || enabled
		default:
			remaining = append(remaining, argument)
		}
	}
	if !asked {
		return remaining, nil
	}
	// Recorded on the root's own flag, so that the flag has one value however
	// it was written and anything later asking whether diagnostics are on gets
	// the same answer as the parser would have given.
	if flag := shellFlag(command, verboseFlag); flag != nil {
		_ = flag.Value.Set("true")
		flag.Changed = true
	}
	mode, err := shellOutputMode(command)
	if err != nil {
		return nil, err
	}
	s.enableDiagnostics(command, mode)
	return remaining, nil
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
func shellFlagsFor(name string) []string {
	// --verbose is deliberately absent from every list. This set answers one
	// question — which flags forwardShellFlags may re-attach to a command's own
	// parser, and which it must refuse — and --verbose is outside that question
	// entirely: takeVerboseFlag enables the log directly off the root's flag
	// set, whoever the command is, and the flag is forwarded to nothing. Naming
	// it here would be a claim no reader of this set ever checks.
	switch name {
	case "wso2":
		// The root routes a product namespace, and a module command may act on
		// every forwarded shell flag.
		return []string{contextFlag, outputFlag}
	case "context":
		// The family renders a machine-readable result, and takes no --context:
		// naming a context is what its own arguments do, and a selection flag
		// alongside "wso2 context use beta" would be two answers to one
		// question.
		return []string{outputFlag}
	case "identity":
		// The family renders a machine-readable result, and takes no
		// --context: an identity is named by this family's own arguments, and
		// a selection flag alongside "wso2 identity list" would be a second
		// answer to a question nothing asked.
		return []string{outputFlag}
	case "login":
		return []string{contextFlag}
	case "logout":
		// The only interactive-auth command that renders a machine-readable
		// result, because it is the only one whose result a script has to read:
		// what the issuer was told about the ended session is not observable
		// any other way.
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

// wantsHelp reports whether a command whose own parser owns its arguments was
// asked for help. Flag parsing is disabled on those commands, so Cobra never
// sets its help flag and the request has to be recognized here.
func wantsHelp(args []string) bool {
	for _, argument := range args {
		if argument == "--help" || argument == "-h" {
			return true
		}
	}
	return false
}

// logoutCommand and moduleCommand keep their own argument parsing for now, so
// flag parsing is disabled on them rather than allowed to fail on a flag the
// root does not declare.
//
// Cobra's allowlist for unknown flags must not be used for this: it does not
// forward an unknown flag, it discards it together with its value, so a command
// would run without a flag the user gave and without any diagnostic.
//
// login no longer does: it declares its flags, so the root's parser refuses an
// unknown one, recognizes --help, and reads --verbose and --context wherever
// they are written. #89 moves the remaining two.
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
		DisableFlagParsing:    true,
		RunE: func(command *cobra.Command, args []string) error {
			if wantsHelp(args) {
				return command.Help()
			}
			args, err := s.takeVerboseFlag(command, args)
			if err != nil {
				return err
			}
			forwarded, err := forwardShellFlags(command, args)
			if err != nil {
				return err
			}
			return s.logout(forwarded)
		},
	}
}

func (s Shell) moduleCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "module <subcommand>",
		Short: "Install, list, and update product modules from the module catalog.",
		// module still routes its own subcommands, so they are named here
		// rather than walked from the tree. They move into the tree when the
		// command declares its flags directly.
		Long:                  "Subcommands: available, install, list, remove, update.",
		DisableFlagsInUseLine: true,
		DisableFlagParsing:    true,
		RunE: func(command *cobra.Command, args []string) error {
			if wantsHelp(args) {
				return command.Help()
			}
			args, err := s.takeVerboseFlag(command, args)
			if err != nil {
				return err
			}
			forwarded, err := forwardShellFlags(command, args)
			if err != nil {
				return err
			}
			return s.module(forwarded)
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
