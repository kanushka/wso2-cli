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
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A line an earlier version wrote, which ran the command by name, is replaced
// by the one that runs it by path rather than kept beside it.
func TestAnEarlierCompletionLineIsReplaced(t *testing.T) {
	lines := completionLines(shellBash, "/opt/wso2/bin/wso2")
	earlier := profileBlockBegin + "\nexport PATH=\"/opt/wso2/bin:$PATH\"\n" +
		`command -v wso2 >/dev/null 2>&1 && eval "$(wso2 completion bash)"` + "\n" + profileBlockEnd + "\n"
	want := profileBlockBegin + "\nexport PATH=\"/opt/wso2/bin:$PATH\"\n" + lines[0] + "\n" + profileBlockEnd + "\n"
	got, changed, err := withCompletionLines(earlier, lines, shellBash, "profile")
	if err != nil {
		t.Fatal(err)
	}
	if got != want || !changed {
		t.Errorf("got changed=%v\n%s\nwant\n%s", changed, got, want)
	}

	powerShell := completionLines(shellPowerShell, `C:\Users\me\.wso2\bin\ws.exe`)
	earlier = profileBlockBegin + "\r\nws completion powershell | Out-String | Invoke-Expression\r\n" + profileBlockEnd + "\r\n"
	want = profileBlockBegin + "\r\n" + powerShell[0] + "\r\n" + profileBlockEnd + "\r\n"
	if got, _, err := withCompletionLines(earlier, powerShell, shellPowerShell, "profile"); err != nil || got != want {
		t.Errorf("got %q, %v\nwant %q", got, err, want)
	}
}

func TestCompletionLinesQuoteThePath(t *testing.T) {
	for _, test := range []struct {
		shell, path, want string
	}{
		{shellBash, "/home/o'brien/my bin/wso2",
			`[ -x '/home/o'\''brien/my bin/wso2' ] && eval "$('/home/o'\''brien/my bin/wso2' completion bash)"`},
		{shellZsh, "/a b/wso2", `[[ -x '/a b/wso2' ]] && source <('/a b/wso2' completion zsh)`},
		{shellPowerShell, `C:\Users\O'Brien\My Tools\ws.exe`,
			`if (Test-Path -LiteralPath 'C:\Users\O''Brien\My Tools\ws.exe' -PathType Leaf) { ` +
				`& 'C:\Users\O''Brien\My Tools\ws.exe' completion powershell | Out-String | Invoke-Expression }`},
		{shellPowerShell, "C:\\\u2018x\u2019\\ws.exe",
			"if (Test-Path -LiteralPath 'C:\\\u2018\u2018x\u2019\u2019\\ws.exe' -PathType Leaf) { " +
				"& 'C:\\\u2018\u2018x\u2019\u2019\\ws.exe' completion powershell | Out-String | Invoke-Expression }"},
	} {
		lines := completionLines(test.shell, test.path)
		if got := lines[len(lines)-1]; got != test.want {
			t.Errorf("%s %q:\n got %s\nwant %s", test.shell, test.path, got, test.want)
		}
	}
	if got, want := fishQuote(`/a b/it's\x`), `'/a b/it\'s\\x'`; got != want {
		t.Errorf("fishQuote = %s, want %s", got, want)
	}
}

// completionShellsHere reports each shell on this machine that can run a
// profile line, with the command that runs one.
func completionShellsHere() map[string][]string {
	found := map[string][]string{}
	for shell, command := range map[string][]string{
		shellBash:       {"bash", "--noprofile", "--norc", "-c"},
		shellZsh:        {"zsh", "-f", "-c"},
		shellFish:       {"fish", "--no-config", "-c"},
		shellPowerShell: {"pwsh", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command"},
	} {
		if path, err := exec.LookPath(command[0]); err == nil {
			found[shell] = append([]string{path}, command[1:]...)
		}
	}
	return found
}

// profileLines is what a new terminal runs for shell to load completion
// from executable.
func profileLines(shell, executable string) string {
	if shell == shellFish {
		return fishCompletionLine(executable)
	}
	return strings.Join(completionLines(shell, executable), "\n")
}

// The profile evaluates what the command prints, so it must run the installed
// executable, not whichever program of the same name comes first on PATH.
func TestTheProfileRunsTheInstalledExecutableNotTheFirstOnPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in executables are shell scripts; test/acceptance covers Windows")
	}
	shells := completionShellsHere()
	for _, shell := range completionShells {
		command, ok := shells[shell]
		if !ok {
			t.Logf("%s is not installed; not run", shell)
			continue
		}
		t.Run(shell, func(t *testing.T) {
			markers := t.TempDir()
			loaded, hostileRan := filepath.Join(markers, "loaded"), filepath.Join(markers, "hostile")
			// Every character here is one a quoting mistake would expose. PowerShell
			// reads a backslash as a directory separator even on Unix, so a name
			// holding one is not a path it can run.
			dir := "o'brien \u2018x\u2019 $HOME `id`"
			if shell != shellPowerShell {
				dir += ` \q`
			}
			installed := filepath.Join(t.TempDir(), "my tools", dir, "wso2")
			writeScript(t, installed, "echo \"touch '"+loaded+"'\"")
			hostile := filepath.Join(t.TempDir(), "wso2")
			writeScript(t, hostile, "touch '"+hostileRan+"'")

			run := exec.Command(command[0], append(command[1:], profileLines(shell, installed))...)
			run.Env = []string{"HOME=" + t.TempDir(), "PATH=" + filepath.Dir(hostile) + ":/usr/bin:/bin"}
			out, err := run.CombinedOutput()
			if err != nil {
				t.Fatalf("the profile line failed: %v\n%s", err, out)
			}
			if _, err := os.Stat(hostileRan); err == nil {
				t.Errorf("the same-named command earlier on PATH ran\n%s", out)
			}
			if _, err := os.Stat(loaded); err != nil {
				t.Errorf("the installed executable's output was not loaded\n%s", out)
			}
		})
	}
}

// A binary that was moved or removed leaves a line that does nothing, rather
// than one that complains in every new terminal.
func TestTheProfileIsQuietWhenTheExecutableIsGone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test/acceptance covers Windows")
	}
	for shell, command := range completionShellsHere() {
		t.Run(shell, func(t *testing.T) {
			gone := filepath.Join(t.TempDir(), "moved away", "wso2")
			run := exec.Command(command[0], append(command[1:], profileLines(shell, gone))...)
			run.Env = []string{"HOME=" + t.TempDir(), "PATH=/usr/bin:/bin"}
			var stderr bytes.Buffer
			run.Stderr = &stderr
			_ = run.Run()
			if stderr.Len() != 0 {
				t.Errorf("the profile line complained: %s", stderr.String())
			}
		})
	}
}

func writeScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}
