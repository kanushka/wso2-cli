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

// Package app is the shell's command dispatcher.
//
// The shell owns policy: it parses its own arguments, gives built-in commands
// precedence over every installed namespace, and resolves product namespaces
// only from its managed module store.
package app

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/internal/parsetree"
	"github.com/wso2/wso2-cli/internal/preferences"
	"github.com/wso2/wso2-cli/internal/rpc"
	"github.com/wso2/wso2-cli/internal/runlog"
	"github.com/wso2/wso2-cli/internal/state"
	"github.com/wso2/wso2-cli/internal/version"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// Shell holds the dependencies one invocation needs. Both the state root and
// the output streams are injected so tests run against isolated state and
// captured output.
type Shell struct {
	// StateRoot is the shell-owned state root. When empty, it is resolved
	// from the environment or the user home directory.
	StateRoot string
	// RunLog records every run in the state root's log file (internal/runlog).
	// cmd/wso2/main.go turns it on; a Shell built in a test leaves it off
	// unless the test is about the log.
	RunLog bool
	// Streams are the user-facing output destinations.
	Streams output.Streams
	// Name is the name the shell was invoked as, which every command the
	// shell suggests is phrased with. cmd/wso2/main.go sets it from the
	// process arguments; empty means output.CommandName.
	Name string
	// OpenBrowser overrides how an interactive login opens the authorization
	// URL. It is nil in production, which is the OS browser opener; a test uses
	// it to drive a login without a display. It can only change how the URL is
	// opened, never what is authorized.
	OpenBrowser func(url string) error

	// Reader is where an interactive prompt reads its answer from. It is the
	// reader #86 named and login_create.go's resolveClientID pre-committed to:
	// the shell has streams for what it writes (Streams) and, until this
	// field, none for what it reads. cmd/wso2/main.go sets it to os.Stdin; a
	// Shell built directly in a test leaves it nil, which reader() treats the
	// same as os.Stdin, and a test that wants to answer a prompt without a
	// real terminal to hand it sets this to something else entirely — see
	// mayPrompt in prompt.go for what that distinction is for.
	Reader io.Reader

	// RunEditor opens a file in the user's editor and waits for it to close.
	// It is nil in production, which runs $VISUAL or $EDITOR (vi without
	// either) on the terminal; a test uses it to edit the file itself. It can
	// only change how the file is edited: what is written is validated the
	// same either way.
	RunEditor func(path string) error

	// ReleasedIndex is the catalog index the help page names products from. It is
	// nil in production, which is the copy this binary was released with
	// (catalog.ReleasedIndex); a test sets it to stand in for what a release knew.
	ReleasedIndex *catalog.Index

	// PlatformURL overrides where WSO2 Cloud's platform service answers. It is
	// empty in production, where the context's environment gives it; a test
	// points it at a local server.
	PlatformURL string

	// log is this invocation's diagnostic log. It is a pointer because the
	// flag that turns it on is parsed after the command tree that the call
	// sites hang off has been built, and because every Shell method takes its
	// receiver by value: the copies have to share one log, not one each. It is
	// unexported so that nothing outside the shell can decide what the user
	// sees, and it is safe to use while nil, which is what a Shell built
	// directly in a test is.
	log *output.Logger

	// invocation holds what applyShellFlags read from flags every command
	// shares, for the closures the command tree captured before parsing. It
	// is a pointer for the reason log is, and nil in a Shell built directly.
	invocation *invocationState
}

// invocationState is what one invocation's shared flags asked for.
type invocationState struct {
	// noInput is --no-input written anywhere a shell command parses it,
	// including before the command name.
	noInput bool
	// runID ties this run's records in the run log together.
	runID string
	// runLogErr is why this run's log could not be opened. The run itself
	// stays silent about it; ws doctor reports it.
	runLogErr error
	// runLog is this run's open log, which remembers a write that failed
	// after it opened.
	runLog *runlog.File
	// commandPath is the command this run resolved to, in declared words
	// only: "wso2 context use", "wso2 apim api list". An argument never
	// becomes part of it, because the run log keeps it on every run.
	commandPath string
	// verbose is --verbose written where a shell command parsed it, nil when
	// it was not written, so a product namespace can tell an explicit
	// --verbose=false from silence and let only silence defer to WSO2_DEBUG.
	verbose *bool
}

// logUnderlyingCause writes what a refusal wraps beyond its rendered problem,
// such as the secure-store backend's own error, which the refusal leaves out.
func (s Shell) logUnderlyingCause(err error) {
	var typed problem.Problem
	if err == nil || !errors.As(err, &typed) || err.Error() == typed.Error() {
		return
	}
	s.log.Debug("the refusal wraps an underlying error", "error", err.Error())
}

// CommandNames reports the shell's own command names, sorted.
//
// A namespace with one of these names is unreachable: the shell resolves its
// own commands before it consults an installed module, so a module owning such
// a namespace would build, release, install, and never run. Whatever decides
// whether a namespace may be created has to ask this rather than keep a list
// of its own, because a list of its own is one that stops being true the next
// time a command is added.
func CommandNames() []string {
	root := (Shell{}).rootCommand()
	names := make([]string, 0, len(root.Commands()))
	for _, command := range root.Commands() {
		names = append(names, command.Name())
	}
	slices.Sort(names)
	return names
}

// Run executes one invocation and returns the process exit code.
func (s Shell) Run(args []string) exit.Code {
	started := time.Now()
	// Created before the command tree so that every closure the tree captures
	// shares this log, and turned on later by --verbose or WSO2_DEBUG. Until
	// then it writes nothing to stderr, which is what an invocation without
	// either must produce.
	s.log = output.NewLogger()
	s.invocation = &invocationState{commandPath: recordedCommand()}
	defer s.openRunLog(args)()
	s.log.Record(runlog.StartedMessage,
		"shell_version", version.Shell(),
		"platform", version.Platform())
	status, code := s.run(args)
	ended := []any{
		runlog.KeyCommand, s.invocation.commandPath,
		runlog.KeyExitCode, int(status),
	}
	if code != "" {
		ended = append(ended, runlog.KeyProblem, code)
	}
	s.log.Record(runlog.EndedMessage, append(ended, "duration_ms", time.Since(started).Milliseconds())...)
	return status
}

// openRunLog attaches the state root's run log, and returns what closes it.
//
// A log that cannot be opened never fails the command it would have
// described; the reason is written once diagnostics are on. A completion
// request is never recorded, because it runs on every Tab.
func (s Shell) openRunLog(args []string) func() {
	if !s.RunLog || len(args) > 0 && isCompletionRequest(args[0]) {
		return func() {}
	}
	root, err := s.stateRoot()
	if err != nil {
		return func() {}
	}
	runID, err := rpc.NewInvocationID()
	if err != nil {
		s.log.FileUnavailable(err)
		return func() {}
	}
	file, err := runlog.Open(root)
	if err != nil {
		s.invocation.runLogErr = err
		s.log.FileUnavailable(err)
		return func() {}
	}
	s.invocation.runID = runID
	s.invocation.runLog = file
	s.log.AttachFile(file, runlog.KeyInvocation, runID, "pid", os.Getpid())
	return func() { _ = file.Close() }
}

// run executes one invocation and reports its exit code and, when it failed,
// the problem's code.
func (s Shell) run(args []string) (exit.Code, string) {
	s.Streams.Out = output.Named(s.Streams.Out, s.Name)
	s.Streams.Err = output.Named(s.Streams.Err, s.Name)
	// Under --output json, standard output is one JSON document whatever the
	// command printed, so a command that has only prose is still parseable
	// (ADR 0023). What it prints is held until it returns.
	stdout := s.Streams.Out
	var printed bytes.Buffer
	asJSON := s.requestedMode(args) == output.ModeJSON
	if asJSON {
		s.Streams.Out = output.Named(&printed, output.NameOf(stdout))
	}
	err := s.dispatch(args)
	written := false
	if asJSON && err == nil {
		// A completion script is sourced as printed, and wso2 config unset
		// output answers in the table mode it leaves in force.
		if printsCompletionScript(args) || s.requestedMode(args) != output.ModeJSON {
			_, err = stdout.Write(printed.Bytes())
		} else {
			err = output.Document(stdout, printed.Bytes())
		}
		written = true
	}
	if err == nil {
		return exit.OK, ""
	}

	var typed problem.Problem
	if !errors.As(err, &typed) {
		typed = problem.New(problem.CategoryModuleProcess, "shell.unexpected_failure", err.Error())
	}
	// The prose stays on standard error under --output json too: a person
	// reads a CI log there, and a script that already did keeps working.
	output.Problem(s.Streams.Err, typed)
	status := exit.ForProblem(typed)
	if asJSON && !written {
		_ = output.FailureDocument(stdout, printed.Bytes(), typed, int(status))
	}
	return status, typed.Code
}

// requestedMode reads the rendering a command line asks for before anything
// parses it: --output wherever the shell would read it, before a bare "--",
// or else the "output" preference. A completion request is never JSON.
func (s Shell) requestedMode(args []string) output.Mode {
	if len(args) > 0 && isCompletionRequest(args[0]) {
		return output.ModeTable
	}
	if separator := slices.Index(args, "--"); separator >= 0 {
		args = args[:separator]
	}
	if mode, found := argumentOutputMode(args); found {
		return mode
	}
	return s.preferredMode()
}

// printsCompletionScript reports whether a command line is wso2 completion
// <shell>, read past any leading flags.
func printsCompletionScript(args []string) bool {
	var words []string
	for index := 0; index < len(args); index++ {
		switch argument := args[index]; {
		case argument == "-o" || argument == "--"+outputFlag || argument == "--"+contextFlag:
			index++
		case !strings.HasPrefix(argument, "-"):
			words = append(words, argument)
		}
	}
	return len(words) >= 2 && words[0] == "completion" &&
		slices.Contains([]string{"bash", "fish", "powershell", "zsh"}, words[1])
}

func (s Shell) dispatch(args []string) (err error) {
	defer func() { s.logUnderlyingCause(err) }()

	// Trust is widened before anything reaches the network, because the first
	// request a login makes is the discovery fetch and a self-signed deployment
	// fails there. See CAFileEnvVar.
	if err := installTrust(); err != nil {
		return err
	}

	// The preferences diagnostic is surfaced here, once, before any fork this
	// function makes — rather than in applyShellFlags, Cobra's
	// PersistentPreRunE, which a product namespace never reaches at all.
	//
	// Fix round 1, F1: dispatchNamespace never enters the Cobra command tree,
	// so applyShellFlags's diagnostic never ran for it — and that path is the
	// ordinary case for a product command, not an edge, so R9's "never
	// silent" promise was broken for every wso2 <namespace> invocation whose
	// preferences document could not be read. Every one of Cobra's own
	// commands still runs PersistentPreRunE and would have been covered
	// either way, whether or not it happened to disable flag parsing — #89
	// removed the last two that did (module, logout); this is structural
	// rather than depending on which path a command happens to take, and it
	// also now covers the bare `wso2` and `--version` cases below, which
	// applyShellFlags never reached either.
	// A completion request runs on every Tab, so it writes nothing to stderr and
	// changes nothing on disk. It is answered from the command tree and the
	// installed receipts alone.
	completing := len(args) > 0 && isCompletionRequest(args[0])
	if root, err := s.stateRoot(); err == nil && !completing {
		if _, diagnostic := preferences.Load(root); diagnostic != nil {
			output.Diagnostic(s.Streams.Err, *diagnostic)
		}
		s.upgradeContexts(root)
	}

	root := s.rootCommand()

	if len(args) == 0 {
		return s.help(root)
	}

	name := args[0]
	if name == "--version" {
		// --version answers before Cobra parses anything, so no other shell
		// flag has been read and there is nothing to log about.
		if err := refuseUnknownOutputMode(args); err != nil {
			return err
		}
		return s.version(nil, s.requestedMode(args))
	}
	// A shell-owned command, or any leading flag, is Cobra's to route. Anything
	// else is a product namespace, and its arguments must reach the module
	// unparsed, so it never enters the command tree.
	if completing {
		s.prepareCompletion(root, args[1:])
	}
	if completing || strings.HasPrefix(name, "-") || isShellCommand(root, name) {
		root.SetArgs(args)
		// Execute runs the command bodies too, so its error is returned as it
		// is. A flag-parsing failure has already been turned into a usage
		// problem by the flag-error hook, except one in a parent's flags:
		// Cobra parses those while it finds the command, before any hook.
		// Anything else is classified by Run.
		err := root.Execute()
		if parentFlagFailure(err) {
			return flagProblem(root, err)
		}
		return err
	}

	return s.dispatchNamespace(root, name, args[1:])
}

func parentFlagFailure(err error) bool {
	var (
		notExist  *pflag.NotExistError
		required  *pflag.ValueRequiredError
		invalid   *pflag.InvalidValueError
		malformed *pflag.InvalidSyntaxError
	)
	return errors.As(err, &notExist) || errors.As(err, &required) ||
		errors.As(err, &invalid) || errors.As(err, &malformed)
}

// isShellCommand reports whether a name is a command the shell owns. A built-in
// command takes precedence over every installed namespace, so this is asked
// before the module store is opened.
func isShellCommand(root *cobra.Command, name string) bool {
	for _, command := range root.Commands() {
		if command.Name() == name {
			return true
		}
	}
	return false
}

// dispatchNamespace resolves a product namespace from the managed module store
// and runs the command in it.
//
// Resolution proves the module is integrity-checked and compatible before any
// product code runs, and the executable digest is recomputed as part of it, so
// nothing is launched that the receipt does not still describe.
func (s Shell) dispatchNamespace(root *cobra.Command, namespace string, args []string) error {
	// A product namespace never enters the command tree, so nothing has parsed
	// its arguments: --verbose written after the namespace is taken here or not
	// at all. It is taken and not forwarded, because until a module declares
	// its command tree the shell cannot tell a flag it should pass on from one
	// the module owns, and a module that does not know the flag would refuse
	// the whole command. See forwardToNamespace.
	args, verbose, err := s.productVerbose(args)
	if err != nil {
		return err
	}
	// --no-input is taken the same way and for the same reason.
	args, noInput, err := s.productNoInput(args)
	if err != nil {
		return err
	}
	if verbose {
		// The module's own --output governs, because these diagnostics
		// interleave with the result the module renders under it.
		mode, err := s.diagnosticMode(root, args)
		if err != nil {
			return err
		}
		s.enableDiagnostics(root, mode)
	}

	store, err := s.store()
	if err != nil {
		return err
	}
	namespaces, err := store.Namespaces()
	if err != nil {
		return err
	}
	if !slices.Contains(namespaces, namespace) {
		// A command the shell removed is answered with its replacement
		// before the namespace is reported unknown (redirects.go).
		if moved := movedCommand(namespace, args, nil); moved != nil {
			return moved
		}
		return s.unknownNamespace(root, namespace, namespaces)
	}
	s.invocation.commandPath = recordedCommand(namespace)

	identity, err := s.identity()
	if err != nil {
		return err
	}
	resolved, err := store.Resolve(namespace, identity)
	if err != nil {
		return err
	}
	// What the shell decided to launch, recorded before it is launched: a
	// module that crashes or hangs leaves this line behind, and the path names
	// which installed version was actually chosen.
	s.log.Debug("resolved a product namespace",
		"namespace", namespace,
		"executable", resolved.ExecutablePath,
		"module_version", resolved.Receipt.ModuleVersion,
		"protocol_version", resolved.ProtocolVersion)
	// A word the module does not declare, which the shell used to answer
	// itself under every namespace, is answered with its replacement. The
	// module never sees it.
	if len(args) > 0 && !declaresCommand(resolved.Receipt, args[0]) {
		if moved := movedCommand(namespace, args, resolved.Receipt.Capabilities.Product); moved != nil {
			return moved
		}
	}
	return s.invokeModule(namespace, resolved, args, noInput)
}

// identity reports the shell-side facts an installed module must be compatible
// with.
func (s Shell) identity() (modules.ShellIdentity, error) {
	shellVersion, err := version.ShellSemver()
	if err != nil {
		return modules.ShellIdentity{}, problem.New(problem.CategoryUsage, "shell.version_malformed", err.Error()).
			WithRecovery("Reinstall the WSO2 CLI from an official release.")
	}
	return modules.ShellIdentity{
		Version:          shellVersion,
		ProtocolVersions: version.ProtocolVersions(),
		Platform:         modules.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH},
	}, nil
}

// store opens the managed module store for this invocation.
func (s Shell) store() (modules.Store, error) {
	root, err := s.stateRoot()
	if err != nil {
		return modules.Store{}, err
	}
	return modules.NewStore(state.ModuleStore(root)), nil
}

// stateRoot reports the shell-owned state root every local fact is read from.
func (s Shell) stateRoot() (string, error) {
	if s.StateRoot != "" {
		return s.StateRoot, nil
	}
	resolved, err := state.Root()
	if err != nil {
		return "", problem.New(problem.CategoryUsage, "shell.state_root_unresolved", err.Error()).
			WithRecovery(fmt.Sprintf("Unset %s or set it to an absolute directory.", state.RootEnvVar))
	}
	return resolved, nil
}

// help shows the shell command tree, rendered from the command tree itself so
// that a command cannot exist without being listed.
func (s Shell) help(root *cobra.Command) error {
	return root.Help()
}

// unknownNamespace reports a name that neither the shell nor any installed
// module answers to. Dispatch and the help command refuse with it alike, so a
// typo costs the same message whichever way it was asked about.
func (s Shell) unknownNamespace(root *cobra.Command, namespace string, namespaces []string) error {
	recovery := "Run wso2 help to see the shell commands, or wso2 version to see the installed products."
	if suggestion := suggestionFor(root, namespace, s.installedProducts(namespaces)); suggestion != "" {
		recovery = suggestion + " " + recovery
	}
	return problem.New(problem.CategoryUsage, "shell.unknown_command",
		fmt.Sprintf("%q is not a shell command and no installed product owns that namespace", namespace)).
		WithRecovery(recovery)
}

// installedProducts reports every installed namespace with the commands its
// verified receipt declares beneath it. A namespace whose receipt cannot be
// read is still offered by name: a suggestion is not where that is diagnosed.
func (s Shell) installedProducts(namespaces []string) []installedProduct {
	declared := map[string][]string{}
	if store, err := s.store(); err == nil {
		if installed, _, err := store.Inventory(); err == nil {
			for _, module := range installed {
				for _, command := range parsetree.FromReceipt(module.Receipt).Commands() {
					if len(command.Path) == 1 {
						declared[module.Namespace] = append(declared[module.Namespace], command.Path[0])
					}
				}
			}
		}
	}
	products := make([]installedProduct, 0, len(namespaces))
	for _, namespace := range namespaces {
		products = append(products, installedProduct{namespace: namespace, commands: declared[namespace]})
	}
	return products
}

// helpTopic answers wso2 help and wso2 help <topic>, where a topic is a shell
// command or an installed product namespace.
//
// Cobra's own help command answered any topic it did not recognise with the
// root page and success, which told a user asking about an installed module —
// the one place product commands could have been discovered — that their
// question meant nothing, and told a script that a typo had worked. A shell
// command still renders its own page. An installed namespace renders the
// module's declared help, the same page invoking it with --help shows, and the
// words after it walk the declaration so wso2 help <namespace> <subcommand>
// answers too. Anything else is refused exactly as dispatch refuses it.
//
// With --output json the same topic is rendered as one JSON document from the
// same trees, with nothing launched: the whole tree for a bare wso2 help, a
// shell command's subtree, or a product's declared tree beneath the command
// named.
func (s Shell) helpTopic(help *cobra.Command, args []string) error {
	root := help.Root()
	mode, err := s.shellOutputMode(help)
	if err != nil {
		return err
	}
	asJSON := mode == output.ModeJSON
	if len(args) == 0 {
		if asJSON {
			return s.renderHelpJSON(s.shellHelpDocument(root))
		}
		return s.help(root)
	}
	if isShellCommand(root, args[0]) {
		target, rest, err := root.Find(args)
		if err != nil || target == nil {
			if asJSON {
				return s.renderHelpJSON(s.shellHelpDocument(root))
			}
			return s.help(root)
		}
		// Find stops at the deepest command and hands back what it could not
		// place. A word left over names no command, and rendering the parent's
		// page for it would tell a script the typo worked — the same silent
		// success wso2 help <typo> itself used to report (review on #161).
		if len(rest) > 0 {
			resolved := strings.Join(args[:len(args)-len(rest)], " ")
			return problem.New(problem.CategoryUsage, "shell.unknown_command",
				fmt.Sprintf("%q is not a wso2 %s subcommand", rest[0], resolved)).
				WithRecovery(fmt.Sprintf("Run wso2 help %s to see what it accepts.", resolved))
		}
		if asJSON {
			return s.renderHelpJSON(s.shellCommandHelpDocument(target))
		}
		return target.Help()
	}
	namespace := args[0]
	store, err := s.store()
	if err != nil {
		return err
	}
	namespaces, err := store.Namespaces()
	if err != nil {
		return err
	}
	if !slices.Contains(namespaces, namespace) {
		return s.unknownNamespace(root, namespace, namespaces)
	}
	identity, err := s.identity()
	if err != nil {
		return err
	}
	resolved, err := store.Resolve(namespace, identity)
	if err != nil {
		return err
	}
	declared := parsetree.FromReceipt(resolved.Receipt)
	if !declared.Declared() {
		return undeclaredModuleHelp(namespace)
	}
	routed := declared.Route(args[1:])
	// The same refusal parseProductArgs makes for the same words: a plain word
	// the namespace or a group does not serve is reported, not shrugged into
	// the page for the command above it.
	if namesNoCommand(declared, routed) {
		return unknownProductCommand(namespace, routed, declared)
	}
	if asJSON {
		return s.renderHelpJSON(s.productHelpDocument(namespace, declared, routed.Command,
			resolved.Receipt.Capabilities.Product))
	}
	return s.renderProductHelp(namespace, declared, routed.Command, resolved.Receipt.Capabilities.Product)
}

// upgradeContexts rewrites a context document an earlier shell wrote in the
// current schema, once, and says what the upgrade changed when it changed
// anything a user has to act on: a context whose shared session stayed with
// another context needs a login of its own (ADR 0016).
//
// It is best effort. A document that cannot be upgraded now — a read-only
// state root, a lock held by another invocation — is still read through the
// same migration by every command, and the next write stores it; the command
// the user ran is not refused over a housekeeping step it did not ask for.
func (s Shell) upgradeContexts(root string) {
	migration, migrated, err := contexts.Upgrade(root)
	if err != nil {
		s.log.Debug("the context document could not be upgraded yet", "error", err.Error())
		return
	}
	if !migrated {
		return
	}
	notes := migration.Notes()
	if len(notes) == 0 {
		return
	}
	_, _ = fmt.Fprintf(s.Streams.Err, "Upgraded the context document to schema version %d: each context "+
		"now holds its own login and products.\n", contexts.SchemaVersion)
	for _, note := range notes {
		_, _ = fmt.Fprintln(s.Streams.Err, note)
	}
}

// recordedCommand is a command as the run log names it: in declared words,
// under the shell's source name whatever the binary was invoked as.
func recordedCommand(words ...string) string {
	return strings.Join(append([]string{output.SourceName}, words...), " ")
}
