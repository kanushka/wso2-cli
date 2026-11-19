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

package runlog

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestThePathIsBesideTheModuleStore(t *testing.T) {
	root := t.TempDir()
	if got, want := Path(root), filepath.Join(root, "cli", "logs", "wso2.log"); got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
}

func TestOpenCreatesAPrivateDirectoryAndFile(t *testing.T) {
	root := t.TempDir()
	file, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer file.Close()
	if _, err := file.Write([]byte("{}\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	assertMode(t, filepath.Dir(Path(root)), 0o700)
	assertMode(t, Path(root), 0o600)
}

func TestOpenNarrowsAnExistingFileToItsOwner(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(Path(root)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(root), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(Path(root), 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer file.Close()
	assertMode(t, Path(root), 0o600)
}

func TestOpenNarrowsAnExistingDirectoryToItsOwner(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(Path(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(Path(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer file.Close()
	assertMode(t, filepath.Dir(Path(root)), 0o700)
}

func TestAFullFileRotatesAndOnlyTheNewestAreKept(t *testing.T) {
	root := t.TempDir()
	line := []byte(strings.Repeat("x", 1023) + "\n")
	// Enough for four files' worth, so the oldest has to have been dropped.
	for range 4 {
		file, err := Open(root)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		for range MaxSize / len(line) {
			if _, err := file.Write(line); err != nil {
				t.Fatalf("Write: %v", err)
			}
		}
		file.Close()
	}
	file, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := file.Write([]byte("newest\n")); err != nil {
		t.Fatal(err)
	}
	file.Close()

	entries, err := os.ReadDir(filepath.Dir(Path(root)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
		info, _ := entry.Info()
		if info.Size() > MaxSize {
			t.Errorf("%s grew to %d bytes, past the %d cap", entry.Name(), info.Size(), MaxSize)
		}
		assertMode(t, filepath.Join(filepath.Dir(Path(root)), entry.Name()), 0o600)
	}
	if got, want := strings.Join(names, " "), "wso2.log wso2.log.1 wso2.log.2"; got != want {
		t.Fatalf("the log directory holds %q, want %q", got, want)
	}
	current, _ := os.ReadFile(Path(root))
	if !strings.HasSuffix(string(current), "newest\n") {
		t.Fatalf("the newest record is not in the current file")
	}
}

func writeLog(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func ended(invocation, command string, code int, problem string) string {
	return fmt.Sprintf(`{"time":"2026-11-19T10:00:00Z","level":"INFO","msg":%q,"invocation":%q,"command":%q,"exit_code":%d,"problem":%q}`,
		EndedMessage, invocation, command, code, problem)
}

func TestLastFinishedReadsTheNewestEndedRunNotSkipped(t *testing.T) {
	root := t.TempDir()
	writeLog(t, Path(root),
		ended("a", "wso2 version", 0, ""),
		`not a record`,
		ended("b", "wso2 login", 77, "auth.denied"),
		`{"level":"DEBUG","msg":"something else","invocation":"c"}`,
		ended("current", "wso2 doctor", 0, ""))

	run, found := LastFinished(root, func(run Run) bool { return run.Invocation == "current" })
	if !found {
		t.Fatal("no finished run was found")
	}
	if run.Command != "wso2 login" || run.ExitCode != 77 || run.Problem != "auth.denied" || run.Invocation != "b" {
		t.Fatalf("LastFinished = %+v", run)
	}
	if run.Time.IsZero() {
		t.Fatal("the run carries no time")
	}
}

func TestLastFinishedLooksIntoTheRotatedFile(t *testing.T) {
	root := t.TempDir()
	writeLog(t, Path(root)+".1", ended("a", "wso2 login", 69, "shell.unreachable"))
	writeLog(t, Path(root), `{"level":"INFO","msg":"`+StartedMessage+`","invocation":"current"}`)
	run, found := LastFinished(root, func(run Run) bool { return run.Invocation == "current" })
	if !found || run.Command != "wso2 login" {
		t.Fatalf("LastFinished = %+v, %v", run, found)
	}
}

func TestLastFinishedFindsNothingInAnEmptyStateRoot(t *testing.T) {
	if _, found := LastFinished(t.TempDir(), nil); found {
		t.Fatal("a run was found where none was recorded")
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s has mode %v, want %v", path, got, want)
	}
}
