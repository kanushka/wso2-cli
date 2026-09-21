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

package app_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
	"github.com/wso2/wso2-cli/sdk/commandtree"
)

// completionTree is a product with a group, a hidden command, and flags that
// do and do not take a value.
func completionTree() commandtree.Tree {
	return commandtree.New([]commandtree.Command{
		{Path: nil, Short: "Manage APIs."},
		{Path: []string{"apis"}, Short: "Work with APIs.", Flags: []commandtree.Flag{
			{Name: "all", Type: "bool", NoOptDefault: "true", Usage: "Include every API."},
		}},
		{Path: []string{"apis", "list"}, Runnable: true, Short: "List APIs.", Flags: []commandtree.Flag{
			{Name: "output", Shorthand: "o", Type: "string", Usage: "Render results as table or json."},
			{Name: "context", Type: "string", Usage: "Use the named context."},
			{Name: "env", Type: "string", Usage: "The environment."},
			{Name: "all", Type: "bool", NoOptDefault: "true", Usage: "Include every API."},
		}},
		{Path: []string{"apis", "delete"}, Runnable: true, Short: "Delete an API."},
		{Path: []string{"secret"}, Runnable: true, Hidden: true},
		{Path: []string{"status"}, Runnable: true, Short: "Report the status."},
	})
}

// completionShell is a shell with the api product installed and two contexts
// configured.
func completionShell(t *testing.T) app.Shell {
	t.Helper()
	shell, _, _ := newShell(t)
	installFixture(t, shell, fixture.Module{Namespace: "api", Version: "0.1.0", CommandTree: completionTree()})
	installFixture(t, shell, fixture.Module{Namespace: "bare", Version: "0.1.0"})
	document := contexts.Document{SchemaVersion: contexts.SchemaVersion,
		Contexts: []contexts.Context{acmeCloud("prod"), acmeCloud("stage")}}
	if err := contexts.Save(shell.StateRoot, document); err != nil {
		t.Fatal(err)
	}
	return shell
}

// complete runs one completion request the way a completion script does and
// returns the offered words and the directive line.
func complete(t *testing.T, shell app.Shell, words ...string) ([]string, string) {
	t.Helper()
	out, errOut := &strings.Builder{}, &strings.Builder{}
	shell.Streams.Out, shell.Streams.Err = out, errOut
	if code := shell.Run(append([]string{"__completeNoDesc"}, words...)); code != exit.OK {
		t.Fatalf("__complete %q exited %d; stderr: %s", words, code, errOut)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	return lines[:len(lines)-1], lines[len(lines)-1]
}

func TestCompletionOffersWhatTheShellKnows(t *testing.T) {
	const noFiles, files = ":4", ":0"
	for _, test := range []struct {
		name      string
		words     []string
		want      []string
		directive string
	}{
		{"built-ins and namespaces", []string{""}, []string{"api", "bare", "completion", "context", "login", "version"}, noFiles},
		{"namespace prefix", []string{"a"}, []string{"api"}, noFiles},
		{"after a root flag", []string{"--context", "prod", "a"}, []string{"api"}, noFiles},
		{"built-in subcommand", []string{"context", "u"}, []string{"use"}, noFiles},
		{"context name argument", []string{"context", "use", ""}, []string{"prod", "stage"}, noFiles},
		{"root context flag", []string{"--context", "s"}, []string{"stage"}, noFiles},
		{"built-in output flag", []string{"context", "list", "--output", ""}, []string{"table", "json"}, noFiles},
		{"product commands", []string{"api", ""}, []string{"apis", "status"}, noFiles},
		{"product subcommands", []string{"api", "apis", ""}, []string{"list", "delete"}, noFiles},
		{"product subcommand prefix", []string{"api", "apis", "l"}, []string{"list"}, noFiles},
		{"product flags", []string{"api", "apis", "list", "--"}, []string{"--output", "--context", "--env", "--all", "--verbose", "--no-input"}, noFiles},
		{"product flag prefix", []string{"api", "apis", "list", "--e"}, []string{"--env"}, noFiles},
		{"product output value", []string{"api", "apis", "list", "--output", ""}, []string{"table", "json"}, noFiles},
		{"product output shorthand", []string{"api", "apis", "list", "-o", "j"}, []string{"json"}, noFiles},
		{"product attached value", []string{"api", "apis", "list", "--context=p"}, []string{"prod"}, noFiles},
		{"shell flag before namespace", []string{"--output", "json", "api", "apis", "list", "--context", ""}, []string{"prod", "stage"}, noFiles},
		{"other product flag value", []string{"api", "apis", "list", "--env", ""}, nil, files},
		{"after a boolean flag", []string{"api", "apis", "--all", ""}, []string{"list", "delete"}, noFiles},
		{"leaf command argument", []string{"api", "status", ""}, nil, files},
		{"unknown product word", []string{"api", "nope", ""}, nil, files},
		{"undeclared tree", []string{"bare", ""}, nil, files},
		{"unknown namespace", []string{"nope", ""}, nil, noFiles},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, directive := complete(t, completionShell(t), test.words...)
			if directive != test.directive {
				t.Errorf("directive = %s, want %s", directive, test.directive)
			}
			for _, want := range test.want {
				if !slices.Contains(got, want) {
					t.Errorf("completions %q do not offer %q", got, want)
				}
			}
			if test.want == nil && len(got) > 0 {
				t.Errorf("completions = %q, want none", got)
			}
			for _, hidden := range []string{"secret", "__complete"} {
				if slices.Contains(got, hidden) {
					t.Errorf("completions %q offer %q", got, hidden)
				}
			}
		})
	}
}

func TestProductSubcommandsAreOfferedAlone(t *testing.T) {
	got, _ := complete(t, completionShell(t), "api", "")
	slices.Sort(got)
	if want := []string{"apis", "status"}; !slices.Equal(got, want) {
		t.Fatalf("completions = %q, want %q", got, want)
	}
}

func TestTheCompletionScriptIsNamedAfterTheShell(t *testing.T) {
	shell, out, errOut := newShell(t)
	if code := shell.Run([]string{"completion", "zsh"}); code != exit.OK {
		t.Fatalf("exit code = %d; stderr: %s", code, errOut)
	}
	if !strings.Contains(out.String(), "#compdef wso2") {
		t.Fatalf("the zsh script does not complete wso2:\n%.200s", out)
	}
}

// completionHome is a home directory of the test's own, with $SHELL naming
// shell, so nothing reaches the real profile.
func completionHome(t *testing.T, shell string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SHELL", shell)
	return home
}

func runInstall(t *testing.T, args ...string) (string, string, exit.Code) {
	t.Helper()
	shell, out, errOut := newShell(t)
	code := shell.Run(append([]string{"completion", "install"}, args...))
	return out.String(), errOut.String(), code
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func TestCompletionInstallAddsOneLineHoweverOftenItRuns(t *testing.T) {
	home := completionHome(t, "/bin/zsh")
	profile := filepath.Join(home, ".zshrc")

	out, errOut, code := runInstall(t)
	if code != exit.OK {
		t.Fatalf("exit %d; stderr: %s", code, errOut)
	}
	for _, want := range []string{"Tab completion added for zsh.", "Open a new terminal, then try: wso2 <TAB>", profile} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not say %q:\n%s", want, out)
		}
	}

	out, errOut, code = runInstall(t)
	if code != exit.OK {
		t.Fatalf("second run exit %d; stderr: %s", code, errOut)
	}
	if !strings.Contains(out, "already set up for zsh") {
		t.Errorf("the second run does not say nothing changed:\n%s", out)
	}
	contents := readFile(t, profile)
	if got := strings.Count(contents, "source <(wso2 completion zsh)"); got != 1 {
		t.Errorf("profile loads the script %d times, want 1:\n%s", got, contents)
	}
	if !strings.Contains(contents, "compinit") {
		t.Errorf("a plain zsh profile gets no compinit:\n%s", contents)
	}
}

func TestCompletionInstallJoinsTheInstallersBlock(t *testing.T) {
	home := completionHome(t, "/bin/bash")
	profile := filepath.Join(home, ".bashrc")
	installed := "# mine\n\n# >>> wso2 cli >>>\nexport PATH=\"/x/bin:$PATH\"\n# <<< wso2 cli <<<\n"
	if err := os.WriteFile(profile, []byte(installed), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, errOut, code := runInstall(t); code != exit.OK {
		t.Fatalf("exit %d; stderr: %s", code, errOut)
	}
	want := "# mine\n\n# >>> wso2 cli >>>\nexport PATH=\"/x/bin:$PATH\"\n" +
		"command -v wso2 >/dev/null 2>&1 && eval \"$(wso2 completion bash)\"\n# <<< wso2 cli <<<\n"
	if got := readFile(t, profile); got != want {
		t.Errorf("profile =\n%s\nwant\n%s", got, want)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(profile); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("the profile's mode was not kept: %v %v", info.Mode(), err)
		}
	}
}

func TestCompletionInstallWritesAFishCompletionFile(t *testing.T) {
	home := completionHome(t, "/usr/bin/fish")
	file := filepath.Join(home, ".config", "fish", "completions", "wso2.fish")

	if _, errOut, code := runInstall(t); code != exit.OK {
		t.Fatalf("exit %d; stderr: %s", code, errOut)
	}
	if got := readFile(t, file); !strings.Contains(got, "wso2 completion fish | source") {
		t.Errorf("the fish file does not load the script:\n%s", got)
	}
	if out, errOut, code := runInstall(t); code != exit.OK || !strings.Contains(out, "already set up") {
		t.Errorf("second run: exit %d, stdout %s, stderr %s", code, out, errOut)
	}
}

func TestCompletionInstallLeavesAFishFileItDidNotWrite(t *testing.T) {
	home := completionHome(t, "/usr/bin/fish")
	file := filepath.Join(home, ".config", "fish", "completions", "wso2.fish")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, errOut, code := runInstall(t); code != exit.Usage || !strings.Contains(errOut, "shell.completion_file_exists") {
		t.Fatalf("exit %d, want a usage refusal; stderr: %s", code, errOut)
	}
	if got := readFile(t, file); got != "# mine\n" {
		t.Errorf("the user's file was replaced:\n%s", got)
	}
}

func TestCompletionInstallEditsANamedPowerShellProfile(t *testing.T) {
	completionHome(t, "")
	profile := filepath.Join(t.TempDir(), "PowerShell", "Microsoft.PowerShell_profile.ps1")

	if _, errOut, code := runInstall(t, "powershell", "--profile", profile); code != exit.OK {
		t.Fatalf("exit %d; stderr: %s", code, errOut)
	}
	if got := readFile(t, profile); !strings.Contains(got, "wso2 completion powershell | Out-String | Invoke-Expression") ||
		!strings.Contains(got, "# >>> wso2 cli >>>") {
		t.Errorf("the profile does not load the script inside the wso2 block:\n%s", got)
	}
}

func TestCompletionInstallRefusesAShellItCannotSetUp(t *testing.T) {
	completionHome(t, "/bin/tcsh")
	if _, errOut, code := runInstall(t); code != exit.Usage || !strings.Contains(errOut, "shell.completion_shell_unsupported") {
		t.Fatalf("exit %d, want a usage refusal; stderr: %s", code, errOut)
	}
	if _, errOut, code := runInstall(t, "cmd"); code != exit.Usage {
		t.Fatalf("exit %d, want a usage refusal; stderr: %s", code, errOut)
	}
}

func TestCompletionInstallRefusesAProfileItCannotWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a read-only mode is not what stops a write on Windows")
	}
	home := completionHome(t, "/bin/zsh")
	profile := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(profile, []byte("# mine\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if _, errOut, code := runInstall(t); code != exit.Usage || !strings.Contains(errOut, "shell.completion_profile_unwritable") {
		t.Fatalf("exit %d, want a usage refusal; stderr: %s", code, errOut)
	}
	if got := readFile(t, profile); got != "# mine\n" {
		t.Errorf("a read-only profile was rewritten:\n%s", got)
	}
}

func TestCompletionInstallWritesThroughALinkedProfile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating a symbolic link needs privileges on Windows")
	}
	home := completionHome(t, "/bin/zsh")
	real := filepath.Join(t.TempDir(), "zshrc")
	if err := os.WriteFile(real, []byte("# dotfiles\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, ".zshrc")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, errOut, code := runInstall(t); code != exit.OK {
		t.Fatalf("exit %d; stderr: %s", code, errOut)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the linked profile is no longer a link")
	}
	if got := readFile(t, real); !strings.Contains(got, "wso2 completion zsh") {
		t.Errorf("the linked file was not written:\n%s", got)
	}
}

func TestCompletionInstallReportsJSON(t *testing.T) {
	home := completionHome(t, "/bin/zsh")
	out, errOut, code := runInstall(t, "--output", "json")
	if code != exit.OK {
		t.Fatalf("exit %d; stderr: %s", code, errOut)
	}
	var report struct {
		Shell   string `json:"shell"`
		File    string `json:"file"`
		Changed bool   `json:"changed"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if report.Shell != "zsh" || report.File != filepath.Join(home, ".zshrc") || !report.Changed {
		t.Errorf("report = %+v", report)
	}
}
