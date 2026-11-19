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
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
	"github.com/wso2/wso2-cli/internal/runlog"
)

func newRunLogShell(t *testing.T) (app.Shell, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	t.Setenv("WSO2_DEBUG", "")
	shell, out, errOut := newShell(t)
	shell.RunLog = true
	return shell, out, errOut
}

func runLogRecords(t *testing.T, shell app.Shell) []map[string]any {
	t.Helper()
	content, err := os.ReadFile(runlog.Path(shell.StateRoot))
	if err != nil {
		t.Fatalf("no run log was written: %v", err)
	}
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(content)), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("the run log line %q is not JSON: %v", line, err)
		}
		records = append(records, record)
	}
	return records
}

func TestEveryRunIsRecordedInTheStateRoot(t *testing.T) {
	shell, _, errOut := newRunLogShell(t)
	if code := shell.Run([]string{"version"}); code != exit.OK {
		t.Fatalf("version failed: exit %d, stderr %s", code, errOut)
	}
	if errOut.Len() != 0 {
		t.Fatalf("recording the run wrote to stderr:\n%s", errOut)
	}
	records := runLogRecords(t, shell)
	if len(records) != 2 {
		t.Fatalf("want a start and an end record, got %v", records)
	}
	started, ended := records[0], records[1]
	if started["msg"] != runlog.StartedMessage || started["shell_version"] == nil || started["platform"] == nil {
		t.Errorf("the start record is %v", started)
	}
	if ended["msg"] != runlog.EndedMessage || ended["command"] != "wso2 version" ||
		ended["exit_code"] != float64(0) || ended["duration_ms"] == nil {
		t.Errorf("the end record is %v", ended)
	}
	if _, present := ended["problem"]; present {
		t.Errorf("a successful run recorded a problem: %v", ended)
	}
	if started["invocation"] == "" || started["invocation"] != ended["invocation"] {
		t.Errorf("the two records do not share one invocation: %v / %v", started, ended)
	}
}

// The end record is kept on every run, so it carries the problem's stable
// code and nothing of its message, which can name a URL, an id or a context.
func TestAFailedRunRecordsItsCodeAndNotItsMessage(t *testing.T) {
	shell, _, errOut := newRunLogShell(t)
	code := shell.Run([]string{"context", "use", "staging-acme"})
	if code == exit.OK {
		t.Fatal("selecting an unknown context succeeded")
	}
	if !strings.Contains(errOut.String(), "staging-acme") {
		t.Fatalf("the refusal no longer names the context, so this test proves nothing:\n%s", errOut)
	}
	records := runLogRecords(t, shell)
	ended := records[len(records)-1]
	if ended["command"] != "wso2 context use" || ended["exit_code"] != float64(code) || ended["problem"] == "" {
		t.Fatalf("the end record is %v", ended)
	}
	content, _ := os.ReadFile(runlog.Path(shell.StateRoot))
	if strings.Contains(string(content), "staging-acme") {
		t.Fatalf("the run log carries the problem message:\n%s", content)
	}
}

// Without diagnostics the file holds only the run records, so no argument of
// any kind reaches it. With them, a product command's arguments still never
// do, because they belong to the module; a built-in's diagnostics may name an
// argument the user gave it, such as a context name, which is the detail they
// asked for.
func TestATokenShapedArgumentLeavesNoTraceInTheRunLog(t *testing.T) {
	const token = "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJydW5sb2ctdGVzdCJ9.c2lnbmF0dXJlLW11c3QtbmV2ZXItYXBwZWFy"
	for _, debug := range []string{"", "1"} {
		for name, args := range map[string][]string{
			"a built-in argument":    {"context", "use", token},
			"an unknown namespace":   {token, "status"},
			"a product argument":     {"reference", "status", token},
			"a product flag value":   {"reference", "status", "--token", token},
			"a product flag spelled": {"reference", "status", "--token=" + token},
		} {
			if debug != "" && args[0] == "context" {
				continue
			}
			t.Run(name+" debug="+debug, func(t *testing.T) {
				shell, _, _ := newRunLogShell(t)
				t.Setenv("WSO2_DEBUG", debug)
				installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0"})
				shell.Run(args)
				content, err := os.ReadFile(runlog.Path(shell.StateRoot))
				if err != nil {
					t.Fatalf("no run log was written: %v", err)
				}
				if strings.Contains(string(content), token) {
					t.Fatalf("the run log carries the token:\n%s", content)
				}
				for _, part := range strings.Split(token, ".") {
					if strings.Contains(string(content), part) {
						t.Fatalf("the run log carries part of the token %q:\n%s", part, content)
					}
				}
			})
		}
	}
}

func TestDiagnosticsReachTheRunLogOnlyWhenAskedFor(t *testing.T) {
	quiet, _, _ := newRunLogShell(t)
	quiet.Run([]string{"version"})
	content, _ := os.ReadFile(runlog.Path(quiet.StateRoot))
	if strings.Contains(string(content), debugStarted) {
		t.Fatalf("a run without diagnostics logged them to the file:\n%s", content)
	}

	shell, _, _ := newRunLogShell(t)
	shell.Run([]string{"version", "--verbose"})
	content, _ = os.ReadFile(runlog.Path(shell.StateRoot))
	if !strings.Contains(string(content), debugStarted) {
		t.Fatalf("--verbose did not reach the file:\n%s", content)
	}
}

func TestModuleDiagnosticsReachTheRunLogUnderDebug(t *testing.T) {
	const line = "the module said something worth keeping"
	for _, verbose := range []bool{false, true} {
		shell, _, errOut := newRunLogShell(t)
		installFixture(t, shell, fixture.Module{Namespace: "reference", Version: "0.1.0",
			Contents: []byte("#!/bin/sh\necho '" + line + "' >&2\nexit 0\n")})
		args := []string{"reference", "status"}
		if verbose {
			args = append(args, "--verbose")
		}
		shell.Run(args)
		if !strings.Contains(errOut.String(), line) {
			t.Fatalf("the module line no longer reaches stderr, so this test proves nothing:\n%s", errOut)
		}
		content, _ := os.ReadFile(runlog.Path(shell.StateRoot))
		if strings.Contains(string(content), line) != verbose {
			t.Fatalf("verbose=%v, but the file reads:\n%s", verbose, content)
		}
	}
}

func TestAShellWithoutTheRunLogWritesNoFile(t *testing.T) {
	shell, _, _ := newShell(t)
	shell.Run([]string{"version"})
	if _, err := os.Stat(runlog.Path(shell.StateRoot)); !os.IsNotExist(err) {
		t.Fatalf("a shell with the run log off wrote one: %v", err)
	}
}

// A completion request runs on every Tab and changes nothing on disk.
func TestACompletionRequestIsNotRecorded(t *testing.T) {
	shell, _, _ := newRunLogShell(t)
	shell.Run([]string{"__complete", "con"})
	if _, err := os.Stat(runlog.Path(shell.StateRoot)); !os.IsNotExist(err) {
		t.Fatalf("a completion request wrote the run log: %v", err)
	}
}

func TestAnUnwritableRunLogNeverFailsACommand(t *testing.T) {
	shell, _, errOut := newRunLogShell(t)
	// A file where the log directory should be.
	if err := os.MkdirAll(shell.StateRoot+"/cli", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shell.StateRoot+"/cli/logs", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := shell.Run([]string{"version"}); code != exit.OK {
		t.Fatalf("version failed: exit %d, stderr %s", code, errOut)
	}
	if errOut.Len() != 0 {
		t.Fatalf("an unwritable log was reported without diagnostics:\n%s", errOut)
	}
	if code := shell.Run([]string{"version", "--verbose"}); code != exit.OK {
		t.Fatalf("version --verbose failed: exit %d", code)
	}
	if !strings.Contains(errOut.String(), "the run log is not being written") {
		t.Fatalf("--verbose did not say why the run log is missing:\n%s", errOut)
	}
}

func TestDoctorPointsAtTheRunLogAfterAFailedRun(t *testing.T) {
	shell, out, errOut := newRunLogShell(t)
	baseline := shell.Run([]string{"doctor", "--output", "json"})
	if report := decodeDoctorReport(t, out.Bytes()); hasFinding(report, "last-run") {
		t.Fatalf("doctor reported a last run when none had failed:\n%s", out)
	}

	failed := shell.Run([]string{"context", "use", "staging-acme"})
	if failed == exit.OK {
		t.Fatal("selecting an unknown context succeeded")
	}
	out.Reset()
	errOut.Reset()
	if code := shell.Run([]string{"doctor", "--output", "json"}); code != baseline {
		t.Fatalf("a failed earlier run changed doctor's exit from %d to %d", baseline, code)
	}
	finding := decodeDoctorReport(t, out.Bytes()).findingFor(t, "last-run")
	if finding.Status != "notice" {
		t.Errorf("last-run status = %q, want notice", finding.Status)
	}
	if !strings.Contains(finding.Detail, "wso2 context use") || !strings.Contains(finding.Detail, "exited") {
		t.Errorf("last-run detail = %q", finding.Detail)
	}
	if !strings.Contains(finding.Recovery, "WSO2_DEBUG=1") || !strings.Contains(finding.Recovery, runlog.Path(shell.StateRoot)) {
		t.Errorf("last-run recovery = %q", finding.Recovery)
	}

	// Once a later run succeeds, the failure is history and doctor says nothing.
	shell.Run([]string{"version"})
	out.Reset()
	shell.Run([]string{"doctor", "--output", "json"})
	if report := decodeDoctorReport(t, out.Bytes()); hasFinding(report, "last-run") {
		t.Fatalf("doctor still reports a failure a later run superseded:\n%s", out)
	}
}

func TestDoctorNamesAnUnwritableRunLogFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are not enforced on Windows")
	}
	shell, out, errOut := newRunLogShell(t)
	logFile := runlog.Path(shell.StateRoot)
	if err := os.MkdirAll(filepath.Dir(logFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logFile, nil, 0o400); err != nil {
		t.Fatal(err)
	}
	shell.Run([]string{"doctor", "--output", "json"})
	finding := decodeDoctorReport(t, out.Bytes()).findingFor(t, "run-log")
	if !strings.Contains(finding.Detail, logFile) {
		t.Errorf("run-log detail = %q, want it to name %s; stderr %s", finding.Detail, logFile, errOut)
	}
	if !strings.Contains(finding.Recovery, "Make "+logFile+" writable") {
		t.Errorf("run-log recovery = %q, want the file's own recovery", finding.Recovery)
	}
}

func hasFinding(report doctorReport, check string) bool {
	for _, finding := range report.Checks {
		if finding.Check == check {
			return true
		}
	}
	return false
}

// The binary may be installed as ws; the log names commands one way whatever
// it was called, so doctor and anyone grepping the file read one spelling.
func TestARenamedShellRecordsCommandsUnderTheSourceName(t *testing.T) {
	shell, _, _ := newRunLogShell(t)
	shell.Name = "ws"
	shell.Run([]string{"context", "use", "staging-acme"})
	records := runLogRecords(t, shell)
	if got := records[len(records)-1]["command"]; got != "wso2 context use" {
		t.Fatalf("the end record names %q, want %q", got, "wso2 context use")
	}
}

func TestACompletionRequestWritesNothingUnderWSO2Debug(t *testing.T) {
	shell, _, errOut := newRunLogShell(t)
	t.Setenv("WSO2_DEBUG", "1")
	shell.Run([]string{"__complete", "con"})
	if strings.Contains(errOut.String(), debugStarted) || strings.Contains(errOut.String(), runlog.EndedMessage) {
		t.Fatalf("a completion request wrote diagnostics:\n%s", errOut)
	}
}

// A run stays silent when its log cannot be written; doctor is where the user
// is told, once, in plain words.
func TestDoctorSaysWhenTheRunLogCannotBeWritten(t *testing.T) {
	healthy, healthyOut, _ := newRunLogShell(t)
	want := healthy.Run([]string{"doctor", "--output", "json"})

	shell, out, errOut := newRunLogShell(t)
	if err := os.MkdirAll(shell.StateRoot+"/cli", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shell.StateRoot+"/cli/logs", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := shell.Run([]string{"doctor", "--output", "json"}); code != want {
		t.Fatalf("an unwritable run log changed doctor's exit from %d to %d; stderr %s", want, code, errOut)
	}
	finding := decodeDoctorReport(t, out.Bytes()).findingFor(t, "run-log")
	if finding.Status != "notice" {
		t.Errorf("run-log status = %q, want notice", finding.Status)
	}
	if !strings.Contains(finding.Detail, "not recorded") || strings.Contains(finding.Detail, "runlog:") {
		t.Errorf("run-log detail = %q, want a plain reason", finding.Detail)
	}
	if !strings.Contains(finding.Recovery, filepath.Dir(runlog.Path(shell.StateRoot))) {
		t.Errorf("run-log recovery = %q, want it to name the directory", finding.Recovery)
	}

	if hasFinding(decodeDoctorReport(t, healthyOut.Bytes()), "run-log") {
		t.Fatal("doctor reported a run log problem on a writable state root")
	}
}
