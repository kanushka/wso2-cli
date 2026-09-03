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

package module_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/wso2/wso2-cli/sdk/commandtree"
	"github.com/wso2/wso2-cli/sdk/module"
)

// TestServeDeclaresItselfInsteadOfServingWhenAsked proves the one way the shell
// learns a module's command tree without trusting anything remote: it runs the
// installed executable and reads what that executable says about itself.
//
// The request arrives in the environment rather than on the command line
// because a module parses its own arguments before the SDK sees them, and a flag
// the SDK reserved would be an unknown flag to a module that parses strictly.
func TestServeDeclaresItselfInsteadOfServingWhenAsked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "declaration.json")
	t.Setenv(module.CommandTreeEnv, path)
	declared := commandtree.New([]commandtree.Command{
		{Path: []string{"status"}, Runnable: true},
	})

	// Serving would block reading standard input. Returning at all is the
	// assertion that this never opened the protocol.
	err := module.Serve(context.Background(), module.Options{
		Namespace:   "reference",
		Version:     "1.2.3",
		CommandTree: declared,
	})
	if err != nil {
		t.Fatalf("declaring: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the declaration: %v", err)
	}
	var written module.Declaration
	if err := json.Unmarshal(content, &written); err != nil {
		t.Fatalf("decoding the declaration: %v", err)
	}
	if written.Module.Namespace != "reference" || written.Module.Version != "1.2.3" {
		t.Errorf("the declaration identifies %+v", written.Module)
	}
	if len(written.CommandTree.Commands) != 1 ||
		written.CommandTree.Commands[0].Path[0] != "status" {
		t.Errorf("the declaration carries the tree %+v", written.CommandTree)
	}
}

// TestDeclaringWritesNothingButTheDeclaration proves the file is the whole
// channel. The installer reads a file rather than a stream because a module is
// free to write diagnostics to standard error and protocol frames to standard
// output, and parsing a declaration out of either would make an unrelated write
// corrupt it.
func TestDeclaringWritesNothingButTheDeclaration(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "declaration.json")
	t.Setenv(module.CommandTreeEnv, path)

	if err := module.Serve(context.Background(), module.Options{Namespace: "reference"}); err != nil {
		t.Fatalf("declaring: %v", err)
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("reading the directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "declaration.json" {
		t.Errorf("declaring left %d entries in the directory", len(entries))
	}
}

// TestAModuleThatDeclaresNoTreeStillDeclaresItsIdentity proves the empty tree is
// a real answer rather than a failure. A module that does not use Cobra has no
// tree to report, and the shell reads the empty declaration as "parse for this
// module the way you always did".
func TestAModuleThatDeclaresNoTreeStillDeclaresItsIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "declaration.json")
	t.Setenv(module.CommandTreeEnv, path)

	if err := module.Serve(context.Background(), module.Options{Namespace: "plain"}); err != nil {
		t.Fatalf("declaring: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the declaration: %v", err)
	}
	var written module.Declaration
	if err := json.Unmarshal(content, &written); err != nil {
		t.Fatalf("decoding the declaration: %v", err)
	}
	if written.Module.Namespace != "plain" {
		t.Errorf("the declaration identifies %+v", written.Module)
	}
	if !written.CommandTree.Empty() {
		t.Errorf("a module that declared no tree reported %+v", written.CommandTree)
	}
}
