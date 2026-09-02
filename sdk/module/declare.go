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

package module

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/wso2/wso2-cli/sdk/commandtree"
)

// CommandTreeEnv names the environment variable that asks a module to declare
// itself to a file instead of serving a command.
//
// Its value is the path to write. The request travels in the environment rather
// than as a command-line flag because a module parses its own arguments before
// the SDK is reached, and a flag the SDK reserved would reach a strict parser as
// an unknown flag and fail the run.
//
// The answer is a file rather than a stream for the same kind of reason.
// Standard output carries protocol frames and standard error carries the
// module's diagnostics, so a declaration written to either could be corrupted by
// an unrelated write the module was always entitled to make.
const CommandTreeEnv = "WSO2_MODULE_COMMAND_TREE"

// Declaration is what a module reports about itself when asked.
//
// It carries the module's identity beside its command tree so that whoever asked
// can prove the executable it ran is the module it meant to ask. The shell
// installs from a catalog it cannot authenticate, so the one thing it can
// establish is that the binary now on disk answers to the namespace the catalog
// claimed for it.
type Declaration struct {
	// Module is the runtime identity of the executable that produced this.
	Module Descriptor `json:"module"`
	// CommandTree is the command tree the executable serves. It is empty for a
	// module that declares none, which is a supported answer rather than a
	// failure.
	CommandTree commandtree.Tree `json:"commandTree"`
}

// declare writes this module's declaration to path.
func declare(path string, options Options) error {
	content, err := json.Marshal(Declaration{
		Module:      Describe(options),
		CommandTree: options.CommandTree,
	})
	if err != nil {
		return fmt.Errorf("module: encoding the command declaration: %w", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return fmt.Errorf("module: writing the command declaration: %w", err)
	}
	return nil
}
