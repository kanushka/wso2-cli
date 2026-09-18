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
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/internal/atomicfile"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// The shells wso2 completion install sets up, named the way Cobra names the
// commands that print their scripts.
const (
	shellBash       = "bash"
	shellZsh        = "zsh"
	shellFish       = "fish"
	shellPowerShell = "powershell"
)

var completionShells = []string{shellBash, shellZsh, shellFish, shellPowerShell}

// The markers of the block the installers write into a shell profile. The
// completion lines go inside it, so the uninstallers remove them with the rest
// of the block. They must match scripts/install.sh, scripts/uninstall.sh and
// scripts/uninstall.ps1 exactly.
const (
	profileBlockBegin = "# >>> wso2 cli >>>"
	profileBlockEnd   = "# <<< wso2 cli <<<"
)

const completionInstallUsage = "Run wso2 completion install [bash|zsh|fish|powershell] [--profile <file>]."

// completionStdoutIsTerminal decides whether wso2 completion <shell> is
// talking to a person. It is a variable so a test can answer it without a
// terminal to hand.
var completionStdoutIsTerminal = output.IsTerminal

// commandNameForProfile is what a command name has to look like before it is
// written into a profile, where anything else would be read as shell syntax.
var commandNameForProfile = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// extendCompletionCommand adds install to Cobra's completion command, and makes
// each shell's script command answer a terminal with how to set completion up
// rather than with the script. Output that is piped or redirected is the
// script, byte for byte as Cobra generates it, so source <(wso2 completion zsh)
// keeps working.
func (s Shell) extendCompletionCommand(command *cobra.Command) {
	command.Short = "Set up tab completion, or print a shell's completion script."
	command.Long = "Run wso2 completion install to set up tab completion for your shell. " +
		"wso2 completion <shell> prints the script itself when its output is piped or redirected; " +
		"at a terminal it says how to set completion up, and --print prints the script anyway."
	for _, child := range command.Commands() {
		if !slices.Contains(completionShells, child.Name()) {
			continue
		}
		generate := child.RunE
		var printScript bool
		child.Flags().BoolVar(&printScript, "print", false, "Print the script even when the output is a terminal.")
		child.RunE = func(command *cobra.Command, args []string) error {
			if !printScript && completionStdoutIsTerminal(s.Streams.Out) {
				return s.completionScriptHint(command.Name())
			}
			return generate(command, args)
		}
	}
	command.AddCommand(s.completionInstallCommand())
}

// completionScriptHint answers wso2 completion <shell> typed at a terminal,
// where two hundred lines of script read as an error and say nothing about what
// to do with them.
func (s Shell) completionScriptHint(shell string) error {
	out := s.Streams.Out
	_, err := fmt.Fprintf(out, "This prints the %s completion script, which is for your shell to load, not to read.\n\n"+
		"Set up tab completion:    %s\nPrint the script anyway:  %s\n", shell,
		output.Hint(out, "wso2 completion install"),
		output.Hint(out, "wso2 completion "+shell+" --print"))
	return err
}

func (s Shell) completionInstallCommand() *cobra.Command {
	var profile string
	command := &cobra.Command{
		Use:   "install [bash|zsh|fish|powershell]",
		Short: "Set up tab completion in your shell's profile.",
		Long: "Sets up tab completion for the named shell, or for the one $SHELL names. " +
			"For zsh and bash it adds a line to your profile that loads the script in every new terminal; " +
			"for fish it writes a completion file; for PowerShell it adds a line to $PROFILE. " +
			"Running it again changes nothing.",
		Args:      atMostOneArgument(completionInstallUsage),
		ValidArgs: completionShells,
		RunE: func(command *cobra.Command, args []string) error {
			return s.completionInstall(command, args, profile)
		},
	}
	command.Flags().StringVar(&profile, "profile", "",
		"Edit this file instead of the profile your shell reads. For fish, the completion file to write.")
	declareOutputFlag(command.Flags())
	return command
}

// completionSetUp is what wso2 completion install reports.
type completionSetUp struct {
	Shell   string `json:"shell"`
	File    string `json:"file"`
	Changed bool   `json:"changed"`
}

func (s Shell) completionInstall(command *cobra.Command, args []string, profile string) error {
	mode, err := s.shellOutputMode(command)
	if err != nil {
		return err
	}
	shell, err := completionShell(args)
	if err != nil {
		return err
	}
	// The command a new terminal runs to load the script is the one this was
	// invoked as, which is also the name the script completes.
	name := output.NameOf(s.Streams.Out)
	if !commandNameForProfile.MatchString(name) {
		return problem.New(problem.CategoryUsage, "shell.completion_unsupported_name",
			fmt.Sprintf("%q cannot be written into a shell profile as a command name", name)).
			WithRecovery("Run the WSO2 CLI under its installed name and try again.")
	}
	target, err := completionTarget(shell, name, profile)
	if err != nil {
		return err
	}
	s.log.Debug("setting up tab completion", "shell", shell, "file", target)

	var changed bool
	if shell == shellFish {
		changed, err = writeFishCompletion(target, name)
	} else {
		changed, err = addToProfile(target, completionLines(shell, name))
	}
	if err != nil {
		return err
	}

	report := completionSetUp{Shell: shell, File: target, Changed: changed}
	if mode == output.ModeJSON {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("app: cannot encode the completion result: %w", err)
		}
		_, err = fmt.Fprintf(s.Streams.Out, "%s\n", encoded)
		return err
	}
	sentence := "Tab completion added for %s.\n"
	if !changed {
		sentence = "Tab completion is already set up for %s.\n"
	}
	if _, err := fmt.Fprintf(s.Streams.Out, sentence, completionShellTitle(shell)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.Streams.Out, "Open a new terminal, then try: %s <TAB>\n\n", name); err != nil {
		return err
	}
	return output.Fields(s.Streams.Out, [][2]string{{"File", target}})
}

// completionShell reports the shell to set up: the one named, or the one $SHELL
// names. Windows sets no $SHELL, and PowerShell is its shell.
func completionShell(args []string) (string, error) {
	named := ""
	if len(args) > 0 {
		named = args[0]
	} else {
		named = strings.TrimSuffix(filepath.Base(os.Getenv("SHELL")), ".exe")
		if os.Getenv("SHELL") == "" {
			if runtime.GOOS != "windows" {
				return "", problem.New(problem.CategoryUsage, "shell.completion_shell_unknown",
					"cannot tell which shell to set up: SHELL is not set").
					WithRecovery(completionInstallUsage)
			}
			named = shellPowerShell
		}
	}
	if named == "pwsh" {
		named = shellPowerShell
	}
	if !slices.Contains(completionShells, named) {
		return "", problem.New(problem.CategoryUsage, "shell.completion_shell_unsupported",
			fmt.Sprintf("tab completion cannot be set up for %q", named)).
			WithRecovery("Name one of bash, zsh, fish or powershell. " + completionInstallUsage)
	}
	return named, nil
}

func completionShellTitle(shell string) string {
	if shell == shellPowerShell {
		return "PowerShell"
	}
	return shell
}

// completionLines are the profile lines that load the script in every new
// terminal. Loading it at start-up rather than saving it to a file means it
// never goes stale when the shell is updated.
//
// zsh has no compdef until compinit has run, and a plain zsh profile never runs
// it. It is run only when nothing has yet, because running it twice doubles
// the start-up cost for everyone whose framework already did. bash reads the
// script through eval, because the bash 3.2 macOS ships cannot source a
// process substitution.
//
// Both load the script only when the command is on PATH. The installer may
// have put it there in a file only a login shell reads, and a line that fails
// in every other terminal is worse than no completion there.
func completionLines(shell, name string) []string {
	switch shell {
	case shellZsh:
		return []string{
			"(( $+functions[compdef] )) || { autoload -Uz compinit && compinit; }",
			"(( $+commands[" + name + "] )) && source <(" + name + " completion zsh)",
		}
	case shellBash:
		return []string{"command -v " + name + ` >/dev/null 2>&1 && eval "$(` + name + ` completion bash)"`}
	default:
		return []string{name + " completion powershell | Out-String | Invoke-Expression"}
	}
}

// completionTarget reports the file to write for a shell.
//
// zsh loads completion only in an interactive shell, so it is always .zshrc,
// even when the installer wired PATH in .zprofile: there it would run before
// the compinit a framework in .zshrc runs, which discards it.
// bash takes the file the installer wired PATH in when that is one bash reads
// interactively, and otherwise the first of .bashrc and .bash_profile that
// exists. A profile the user keeps elsewhere and links to is written through the
// link, so the link survives.
func completionTarget(shell, name, profile string) (string, error) {
	if profile == "" && shell == shellPowerShell {
		return powerShellProfile()
	}
	if profile == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", problem.New(problem.CategoryUsage, "shell.completion_home_unknown",
				"cannot find your home directory: "+err.Error()).
				WithRecovery("Name the file to edit with --profile. " + completionInstallUsage)
		}
		switch shell {
		case shellZsh:
			profile = filepath.Join(home, ".zshrc")
		case shellBash:
			profile = bashProfile(home)
		case shellFish:
			config := os.Getenv("XDG_CONFIG_HOME")
			if config == "" {
				config = filepath.Join(home, ".config")
			}
			profile = filepath.Join(config, "fish", "completions", name+".fish")
		}
	}
	if resolved, err := filepath.EvalSymlinks(profile); err == nil {
		profile = resolved
	}
	return profile, nil
}

func bashProfile(home string) string {
	candidates := []string{filepath.Join(home, ".bashrc"), filepath.Join(home, ".bash_profile")}
	for _, candidate := range candidates {
		if contents, err := os.ReadFile(candidate); err == nil && strings.Contains(string(contents), profileBlockBegin) {
			return candidate
		}
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	// A login shell, which is what a macOS terminal opens, reads only
	// .bash_profile.
	if runtime.GOOS == "darwin" {
		return candidates[1]
	}
	return candidates[0]
}

// powerShellProfile asks PowerShell where its profile is, because the
// documents folder it lives under can be moved, and refuses when the execution
// policy would stop the profile from loading at all: a profile that fails at
// every start is worse than none.
func powerShellProfile() (string, error) {
	unknown := problem.New(problem.CategoryUsage, "shell.completion_profile_unknown",
		"cannot ask PowerShell where its profile is").
		WithRecovery("Name the file with --profile $PROFILE. " + completionInstallUsage)
	for _, candidate := range []string{"pwsh", "powershell"} {
		path, err := exec.LookPath(candidate)
		if err != nil {
			continue
		}
		answer, err := exec.Command(path, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command",
			"$PROFILE; Get-ExecutionPolicy").Output()
		if err != nil {
			return "", unknown
		}
		lines := strings.Fields(strings.ReplaceAll(string(answer), "\r", ""))
		if len(lines) < 2 {
			return "", unknown
		}
		profile, policy := strings.Join(lines[:len(lines)-1], " "), lines[len(lines)-1]
		if slices.Contains([]string{"Restricted", "AllSigned"}, policy) {
			return "", problem.New(problem.CategoryUsage, "shell.completion_profile_blocked",
				fmt.Sprintf("PowerShell's execution policy is %s, so it would not load a profile", policy)).
				WithRecovery("Run Set-ExecutionPolicy -Scope CurrentUser RemoteSigned in PowerShell, then run wso2 completion install again.")
		}
		return profile, nil
	}
	return "", unknown
}

// addToProfile puts the completion lines inside the profile's wso2 block,
// adding the block when there is none, and reports whether it changed the file.
func addToProfile(path string, lines []string) (bool, error) {
	current, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, profileFailure(path, err)
	}
	updated, changed, err := withCompletionLines(string(current), lines, path)
	if err != nil || !changed {
		return false, err
	}
	return true, writeProfile(path, []byte(updated))
}

// withCompletionLines returns contents with lines inside its wso2 block, in
// order, and reports whether it had to change anything. Every other line is
// kept byte for byte, including a Windows line ending.
func withCompletionLines(contents string, lines []string, path string) (string, bool, error) {
	newline := "\n"
	if strings.Contains(contents, "\r\n") {
		newline = "\r\n"
	}
	existing := strings.Split(contents, "\n")
	begin, end := -1, -1
	for index, line := range existing {
		switch strings.TrimSuffix(line, "\r") {
		case profileBlockBegin:
			if begin < 0 {
				begin = index
			}
		case profileBlockEnd:
			if begin >= 0 && end < 0 {
				end = index
			}
		}
	}
	if begin >= 0 && end < 0 {
		return "", false, problem.New(problem.CategoryUsage, "shell.completion_profile_unreadable",
			fmt.Sprintf("%s has the line %q but not %q, so it is not clear where the wso2 lines end", path,
				profileBlockBegin, profileBlockEnd)).
			WithRecovery(fmt.Sprintf("Add %q after the wso2 lines in %s, then run wso2 completion install again.",
				profileBlockEnd, path))
	}
	if begin < 0 {
		block := append(append([]string{profileBlockBegin}, lines...), profileBlockEnd)
		prefix := contents
		if prefix != "" && !strings.HasSuffix(prefix, "\n") {
			prefix += newline
		}
		return prefix + newline + strings.Join(block, newline) + newline, true, nil
	}
	// Any of the lines already there are taken out and all of them written
	// again, in order, so a block that lost one gets it back where it belongs.
	var kept []string
	present := 0
	for _, line := range existing[begin+1 : end] {
		if slices.Contains(lines, strings.TrimSuffix(line, "\r")) {
			present++
			continue
		}
		kept = append(kept, line)
	}
	if present == len(lines) {
		return contents, false, nil
	}
	rebuilt := make([]string, 0, len(existing)+len(lines))
	rebuilt = append(rebuilt, existing[:begin+1]...)
	rebuilt = append(rebuilt, kept...)
	for _, line := range lines {
		rebuilt = append(rebuilt, line+strings.TrimSuffix(newline, "\n"))
	}
	rebuilt = append(rebuilt, existing[end:]...)
	return strings.Join(rebuilt, "\n"), true, nil
}

// writeFishCompletion writes the file fish loads when it first completes the
// command. The file loads the script rather than holding it, for the same
// reason the profile line does, and carries the wso2 markers so an uninstall
// can tell it from a file the user wrote. A file without them is left alone.
func writeFishCompletion(path, name string) (bool, error) {
	want := strings.Join([]string{profileBlockBegin, name + " completion fish | source", profileBlockEnd}, "\n") + "\n"
	current, err := os.ReadFile(path)
	switch {
	case err == nil && string(current) == want:
		return false, nil
	case err == nil && !strings.Contains(string(current), profileBlockBegin):
		return false, problem.New(problem.CategoryUsage, "shell.completion_file_exists",
			fmt.Sprintf("%s already exists and was not written by wso2 completion install", path)).
			WithRecovery(fmt.Sprintf("Remove %s, or name another file with --profile, then try again.", path))
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return false, profileFailure(path, err)
	}
	return true, writeProfile(path, []byte(want))
}

// writeProfile replaces the file in one step, so an interrupted run cannot
// leave a profile half written, keeping the mode it had.
func writeProfile(path string, contents []byte) error {
	mode := fs.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
		// The replacement is a rename, which a read-only file does not stop,
		// so whether the file may be written is asked of the file itself.
		writable, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			return profileFailure(path, err)
		}
		_ = writable.Close()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return profileFailure(path, err)
	}
	if err := atomicfile.Write(path, contents, mode); err != nil {
		return profileFailure(path, err)
	}
	return nil
}

func profileFailure(path string, err error) problem.Problem {
	return problem.New(problem.CategoryUsage, "shell.completion_profile_unwritable",
		fmt.Sprintf("cannot set up tab completion in %s: %v", path, err)).
		WithRecovery("Check the file's permissions, or name another file with --profile. " + completionInstallUsage)
}
