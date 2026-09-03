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

// Command declarehelper stands in for an installed module while the installer's
// declaration step is tested.
//
// It is a real executable rather than a shell script because the installer runs
// what it installed, and on Windows os/exec resolves an executable by extension
// and never reads a shebang — a script would simply not run, and the tests would
// pass an empty tree instead of testing anything.
//
// It takes its instructions from a control file beside itself, because the
// installer supplies a module with neither arguments nor an environment: the
// environment is built from nothing, and nothing is passed on the command line.
// The acceptance modules steer the same way, for the same reason.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ControlFileName is the file this helper reads, beside its own executable.
const ControlFileName = "control.json"

// Control is what a test asks this helper to do.
type Control struct {
	// Mode selects the behaviour: "declare", "fail", "hang", or "garbage".
	Mode string `json:"mode"`
	// Namespace is what the declaration claims to be, so a test can install a
	// module that answers to another name.
	Namespace string `json:"namespace"`
	// Command is the single command the declared tree carries.
	Command string `json:"command"`
	// Flag is a value flag declared on that command, when not empty.
	Flag string `json:"flag"`
	// EchoEnvironment names an environment variable whose value becomes the
	// declared command, so a test can see what this process inherited.
	EchoEnvironment string `json:"echoEnvironment"`
}

func main() {
	control, err := readControl()
	if err != nil {
		fmt.Fprintf(os.Stderr, "declarehelper: %v\n", err)
		os.Exit(1)
	}

	switch control.Mode {
	case "fail":
		fmt.Fprintln(os.Stderr, "declarehelper: this module does not understand the request")
		os.Exit(2)
	case "hang":
		time.Sleep(10 * time.Minute)
	case "garbage":
		write([]byte("not json"))
	default:
		declare(control)
	}
}

// readControl reads the control file beside this executable.
func readControl() (Control, error) {
	executable, err := os.Executable()
	if err != nil {
		return Control{}, fmt.Errorf("locating this executable: %w", err)
	}
	content, err := os.ReadFile(filepath.Join(filepath.Dir(executable), ControlFileName))
	if err != nil {
		return Control{}, fmt.Errorf("reading the control file: %w", err)
	}
	var control Control
	if err := json.Unmarshal(content, &control); err != nil {
		return Control{}, fmt.Errorf("decoding the control file: %w", err)
	}
	return control, nil
}

// declare writes the declaration the control file asks for.
//
// The shapes are spelled out here rather than imported from the SDK, so that
// this helper proves the wire format the installer actually reads rather than
// agreeing with it by construction.
func declare(control Control) {
	command := control.Command
	if control.EchoEnvironment != "" {
		command = os.Getenv(control.EchoEnvironment)
		if command == "" {
			command = "unset"
		}
	}
	declared := map[string]any{"path": []string{command}, "runnable": true}
	if control.Flag != "" {
		declared["flags"] = []map[string]any{{"name": control.Flag, "type": "string"}}
	}
	content, err := json.Marshal(map[string]any{
		"module":      map[string]any{"namespace": control.Namespace},
		"commandTree": map[string]any{"commands": []any{declared}},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "declarehelper: encoding: %v\n", err)
		os.Exit(1)
	}
	write(content)
}

// write puts the answer where the installer asked for it.
func write(content []byte) {
	path := os.Getenv("WSO2_MODULE_COMMAND_TREE")
	if path == "" {
		fmt.Fprintln(os.Stderr, "declarehelper: no declaration path was requested")
		os.Exit(1)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "declarehelper: writing: %v\n", err)
		os.Exit(1)
	}
}
