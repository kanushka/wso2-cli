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
	"errors"
	"fmt"
	"io"
	"runtime"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/internal/parsetree"
	"github.com/wso2/wso2-cli/internal/preferences"
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
	// Streams are the user-facing output destinations.
	Streams output.Streams
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

	// log is this invocation's diagnostic log. It is a pointer because the
	// flag that turns it on is parsed after the command tree that the call
	// sites hang off has been built, and because every Shell method takes its
	// receiver by value: the copies have to share one log, not one each. It is
	// unexported so that nothing outside the shell can decide what the user
	// sees, and it is safe to use while nil, which is what a Shell built
	// directly in a test is.
	log *output.Logger
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
	err := s.dispatch(args)
	if err == nil {
		return exit.OK
	}

	var typed problem.Problem
	if !errors.As(err, &typed) {
		typed = problem.New(problem.CategoryModuleProcess, "shell.unexpected_failure", err.Error())
	}
	output.Problem(s.Streams.Err, typed)
	return exit.ForProblem(typed)
}

func (s Shell) dispatch(args []string) error {
	// Created before the command tree so that every closure the tree captures
	// shares this log, and turned on later by --verbose. Until then it writes
	// nothing, which is what an invocation without the flag must produce.
	s.log = output.NewLogger()

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
	if root, err := s.stateRoot(); err == nil {
		if _, diagnostic := preferences.Load(root); diagnostic != nil {
			output.Diagnostic(s.Streams.Err, *diagnostic)
		}
	}

	root := s.rootCommand()

	if len(args) == 0 {
		return s.help(root)
	}

	name := args[0]
	if name == "--version" {
		// --version answers before Cobra parses anything, so no other shell
		// flag has been read and there is nothing to log about.
		return s.version(nil)
	}
	// A shell-owned command, or any leading flag, is Cobra's to route. Anything
	// else is a product namespace, and its arguments must reach the module
	// unparsed, so it never enters the command tree.
	if strings.HasPrefix(name, "-") || isShellCommand(root, name) {
		root.SetArgs(args)
		// Execute runs the command bodies too, so its error is returned as it
		// is. A flag-parsing failure has already been turned into a usage
		// problem by the flag-error hook; anything else is classified by Run.
		return root.Execute()
	}

	return s.dispatchNamespace(root, name, args[1:])
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
	args, verbose, err := takeVerbose(args)
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
		return unknownNamespace(root, namespace)
	}

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
	return s.invokeModule(namespace, resolved, args)
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
func unknownNamespace(root *cobra.Command, namespace string) error {
	recovery := "Run wso2 help to see the shell commands, or wso2 version to see the installed modules."
	if suggestion := suggestionFor(root, namespace); suggestion != "" {
		recovery = suggestion + " " + recovery
	}
	return problem.New(problem.CategoryUsage, "shell.unknown_command",
		fmt.Sprintf("%q is not a shell command and no installed module owns that namespace", namespace)).
		WithRecovery(recovery)
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
func (s Shell) helpTopic(root *cobra.Command, args []string) error {
	if len(args) == 0 {
		return s.help(root)
	}
	if isShellCommand(root, args[0]) {
		target, _, err := root.Find(args)
		if err != nil || target == nil {
			return s.help(root)
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
		return unknownNamespace(root, namespace)
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
	// the namespace does not serve is reported, not shrugged into the page for
	// the whole namespace.
	if routed.Unrouted != "" && len(routed.Command.Path) == 0 && declared.RootHasChildren() {
		return unknownProductCommand(namespace, routed.Unrouted, declared)
	}
	return s.renderProductHelp(namespace, declared, routed.Command)
}
