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

package cobratree_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/sdk/cobratree"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/result"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

// value reports a named field's value, so a test asserts on the field it cares
// about rather than on the whole result.
func value(produced *result.Result, name string) string {
	for _, field := range produced.Fields {
		if field.Name == name {
			return field.Value
		}
	}
	return ""
}

func options() module.Options {
	return module.Options{Namespace: "reference", Version: "1.0.0"}
}

// TestACommandInTheTreeIsInvokedByItsPath proves the adapter routes the shell's
// command path onto the matching command in the tree.
func TestACommandInTheTreeIsInvokedByItsPath(t *testing.T) {
	root := &cobra.Command{Use: "reference"}
	status := &cobra.Command{Use: "status"}
	root.AddCommand(status)

	tree := cobratree.New(root)
	tree.Handle(status, func(_ context.Context, _ module.Request) (result.Result, error) {
		return result.New("reference.test/v1").With("ran", "", "status"), nil
	})

	outcome := testkit.Run(context.Background(), options(), tree.Commands(),
		testkit.Invocation{Command: []string{"status"}})

	if outcome.Err != nil {
		t.Fatalf("the exchange failed: %v", outcome.Err)
	}
	if outcome.Result == nil || value(outcome.Result, "ran") != "status" {
		t.Fatalf("the status handler did not run: result %+v problem %+v", outcome.Result, outcome.Problem)
	}
}

// TestAHandlerReadsItsOwnFlags proves the adapter parses the module's arguments
// with the matched command's flag set before the handler runs.
//
// The shell hands arguments over unparsed, so a module's flags are the module's
// to parse. This is the whole point of the adapter: an existing Cobra CLI keeps
// its flag declarations.
func TestAHandlerReadsItsOwnFlags(t *testing.T) {
	root := &cobra.Command{Use: "reference"}
	status := &cobra.Command{Use: "status"}
	status.Flags().String("env", "", "Target environment.")
	root.AddCommand(status)

	tree := cobratree.New(root)
	tree.Handle(status, func(_ context.Context, _ module.Request) (result.Result, error) {
		env, err := status.Flags().GetString("env")
		if err != nil {
			return result.Result{}, err
		}
		return result.New("reference.test/v1").With("env", "", env), nil
	})

	for _, arguments := range [][]string{
		{"--env", "prod"},
		{"--env=prod"},
	} {
		t.Run(strings.Join(arguments, " "), func(t *testing.T) {
			outcome := testkit.Run(context.Background(), options(), tree.Commands(),
				testkit.Invocation{Command: []string{"status"}, Arguments: arguments})

			if outcome.Result == nil || value(outcome.Result, "env") != "prod" {
				t.Fatalf("the flag did not reach the handler: result %+v problem %+v",
					outcome.Result, outcome.Problem)
			}
		})
	}
}

// TestAnUnparseableArgumentIsATypedProblem proves a flag failure inside a module
// arrives as a typed problem rather than as Cobra's own error text, so the shell
// has something to render and classify.
func TestAnUnparseableArgumentIsATypedProblem(t *testing.T) {
	root := &cobra.Command{Use: "reference"}
	status := &cobra.Command{Use: "status"}
	root.AddCommand(status)

	tree := cobratree.New(root)
	tree.Handle(status, func(_ context.Context, _ module.Request) (result.Result, error) {
		return result.Result{}, nil
	})

	outcome := testkit.Run(context.Background(), options(), tree.Commands(),
		testkit.Invocation{Command: []string{"status"}, Arguments: []string{"--nonexistent"}})

	if outcome.Problem == nil {
		t.Fatalf("an unknown flag produced no typed problem: result %+v", outcome.Result)
	}
	if outcome.Problem.Code == "" {
		t.Fatalf("the problem carries no stable code: %+v", outcome.Problem)
	}
}

// TestTheTreeCannotWriteUserOutput proves the command tree's own output never
// reaches standard output.
//
// In production the protocol frames travel over this process's standard output,
// so anything else written there corrupts the stream the shell is reading. What
// the adapter can guarantee is that every writer in the tree points at standard
// error and that Cobra prints neither errors nor usage itself, without the
// module author arranging it. It cannot stop a handler calling fmt.Println
// directly; nothing outside the handler can.
func TestTheTreeCannotWriteUserOutput(t *testing.T) {
	root := &cobra.Command{Use: "reference"}
	status := &cobra.Command{Use: "status"}
	root.AddCommand(status)

	tree := cobratree.New(root)
	tree.Handle(status, func(_ context.Context, _ module.Request) (result.Result, error) {
		status.Println("this must not reach standard output")
		status.PrintErrln("a diagnostic may")
		return result.New("reference.test/v1").With("ran", "", "yes"), nil
	})
	commands := tree.Commands()

	captured := filepath.Join(t.TempDir(), "stdout")
	file, err := os.Create(captured)
	if err != nil {
		t.Fatalf("os.Create returned %v", err)
	}
	restore := os.Stdout
	os.Stdout = file
	outcome := testkit.Run(context.Background(), options(), commands,
		testkit.Invocation{Command: []string{"status"}})
	os.Stdout = restore
	if err := file.Close(); err != nil {
		t.Fatalf("closing the capture returned %v", err)
	}

	written, err := os.ReadFile(captured)
	if err != nil {
		t.Fatalf("os.ReadFile returned %v", err)
	}
	if len(written) != 0 {
		t.Fatalf("the command tree wrote to standard output: %q", written)
	}

	// Capturing standard output alone would pass even with no writer set,
	// because Cobra's own print methods default to standard error. The writers
	// are asserted directly, so a later SetOut(os.Stdout) anywhere in the tree
	// is caught rather than tolerated.
	for _, command := range []*cobra.Command{root, status} {
		if command.OutOrStdout() != os.Stderr {
			t.Errorf("%q writes results somewhere other than standard error", command.Name())
		}
		if command.ErrOrStderr() != os.Stderr {
			t.Errorf("%q writes diagnostics somewhere other than standard error", command.Name())
		}
		if !command.SilenceErrors || !command.SilenceUsage {
			t.Errorf("%q lets Cobra print errors or usage itself", command.Name())
		}
	}

	if outcome.Result == nil || value(outcome.Result, "ran") != "yes" {
		t.Fatalf("the handler did not run: result %+v problem %+v", outcome.Result, outcome.Problem)
	}
}

// TestEveryCommandInTheTreeIsServed proves the adapter walks the whole tree, so
// a command an author adds to it is served without a second registration.
func TestEveryCommandInTheTreeIsServed(t *testing.T) {
	root := &cobra.Command{Use: "reference"}
	status := &cobra.Command{Use: "status"}
	whoami := &cobra.Command{Use: "whoami"}
	root.AddCommand(status, whoami)

	tree := cobratree.New(root)
	for _, command := range []*cobra.Command{status, whoami} {
		name := command.Name()
		tree.Handle(command, func(_ context.Context, _ module.Request) (result.Result, error) {
			return result.New("reference.test/v1").With("ran", "", name), nil
		})
	}

	for _, name := range []string{"status", "whoami"} {
		outcome := testkit.Run(context.Background(), options(), tree.Commands(),
			testkit.Invocation{Command: []string{name}})
		if outcome.Result == nil || value(outcome.Result, "ran") != name {
			t.Fatalf("%q was not served: result %+v problem %+v", name, outcome.Result, outcome.Problem)
		}
	}
}

// TestACommandWithNoHandlerIsNotServed proves the adapter serves what an author
// bound a handler to and nothing else, so an unhandled command is the shell's
// unknown-command refusal rather than a silent success.
func TestACommandWithNoHandlerIsNotServed(t *testing.T) {
	root := &cobra.Command{Use: "reference"}
	status := &cobra.Command{Use: "status"}
	root.AddCommand(status)

	commands := cobratree.New(root).Commands()

	if len(commands) != 0 {
		t.Fatalf("an unhandled command was served: %+v", commands)
	}
}
