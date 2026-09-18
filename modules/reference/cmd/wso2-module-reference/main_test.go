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

package main

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/wso2/wso2-cli/sdk/module"
)

// TestReportIdentityWritesTheDescriptorAsJSONToStderr proves what "wso2-module-reference
// --module-info" actually promises acceptance tests: a single JSON document on
// standard error, since standard output is reserved for protocol frames.
func TestReportIdentityWritesTheDescriptorAsJSONToStderr(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("cannot open a pipe: %v", err)
	}
	original := os.Stderr
	os.Stderr = writer
	t.Cleanup(func() { os.Stderr = original })

	descriptor := module.Describe(moduleOptions())
	reportIdentity(descriptor)

	if err := writer.Close(); err != nil {
		t.Fatalf("cannot close the pipe writer: %v", err)
	}

	var decoded module.Descriptor
	if err := json.NewDecoder(bufio.NewReader(reader)).Decode(&decoded); err != nil {
		t.Fatalf("stderr did not carry a decodable descriptor: %v", err)
	}
	if decoded.Namespace != Namespace {
		t.Errorf("namespace = %q, want %q", decoded.Namespace, Namespace)
	}
	if decoded.Version != moduleVersion {
		t.Errorf("version = %q, want %q", decoded.Version, moduleVersion)
	}
	if len(decoded.AuthAudiences) != 1 || decoded.AuthAudiences[0] != StatusAudience {
		t.Errorf("authAudiences = %v, want [%s]", decoded.AuthAudiences, StatusAudience)
	}
	if len(decoded.AuthScopes) != 1 || decoded.AuthScopes[0] != StatusScope {
		t.Errorf("authScopes = %v, want [%s]", decoded.AuthScopes, StatusScope)
	}
}

// TestModuleOptionsDeclareTheReferenceNamespace pins what the shell reads off
// this module before it ever calls it: a wrong namespace or a dropped
// audience/scope here would silently break every access request the module
// makes, and every other test scripts access rather than checking this.
func TestModuleOptionsDeclareTheReferenceNamespace(t *testing.T) {
	options := moduleOptions()

	if options.Namespace != Namespace {
		t.Errorf("namespace = %q, want %q", options.Namespace, Namespace)
	}
	if len(options.AuthAudiences) != 1 || options.AuthAudiences[0] != StatusAudience {
		t.Errorf("authAudiences = %v, want [%s]", options.AuthAudiences, StatusAudience)
	}
	if len(options.AuthScopes) != 1 || options.AuthScopes[0] != StatusScope {
		t.Errorf("authScopes = %v, want [%s]", options.AuthScopes, StatusScope)
	}
}

// TestCommandTreeDeclaresEveryCommand is what lets the shell parse "wso2
// reference <command> --help" or name a mistyped command without launching
// the module: a tree that declared nothing would still serve, and this is the
// test that would catch it.
func TestCommandTreeDeclaresEveryCommand(t *testing.T) {
	declared := commandTree().Declare()

	if len(declared.Commands) == 0 {
		t.Fatal("the module declares no commands")
	}
	want := map[string]bool{"status": false, "call": false, "whoami": false}
	for _, command := range declared.Commands {
		if len(command.Path) != 1 {
			continue
		}
		if _, ok := want[command.Path[0]]; ok {
			want[command.Path[0]] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("the declared tree does not name %q", name)
		}
	}
}
