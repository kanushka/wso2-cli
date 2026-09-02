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

package install

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/wso2/wso2-cli/sdk/commandtree"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// declarationTimeout bounds how long an executable gets to declare itself.
//
// A module that ignores the request opens the protocol instead and waits on
// standard input, which never arrives here. Without a bound that is an install
// that stops with nothing on screen, so the wait is capped and a module that
// spends it is treated as one that declares nothing.
const declarationTimeout = 10 * time.Second

// declaredTree reports the command tree the freshly installed executable
// declares about itself.
//
// This is the step that makes the shell's parsing trustworthy. The tree decides
// how a user's command line is interpreted, and the catalog it could otherwise
// be copied from is fetched over the network and carries no signature. Asking
// the binary means the tree the shell parses with is the tree of the code that
// runs, which the receipt's digest then pins for every later launch.
//
// Only a namespace the executable disputes is an error. Everything else — an
// executable too old to understand the request, one that fails, one that never
// answers, one whose answer will not decode — leaves the module with no declared
// tree, and the shell parses for it the way it did before declarations existed.
// That fallback is the previous behaviour rather than a weaker one: it forwards
// the arguments to the module, which accepts or refuses them itself, so a module
// that cannot declare is not a module whose flags stop being checked.
func declaredTree(ctx context.Context, namespace, versionDir, executableName string) (
	commandtree.Tree, error) {
	answers, err := os.MkdirTemp("", "wso2-declaration-")
	if err != nil {
		return commandtree.Tree{}, storeFailure("creating a directory for the module declaration", err)
	}
	defer func() { _ = os.RemoveAll(answers) }()
	answer := filepath.Join(answers, "declaration.json")

	ctx, cancel := context.WithTimeout(ctx, declarationTimeout)
	defer cancel()
	// Every stream is left nil, which attaches the null device directly. A
	// module remains free to write diagnostics while it declares itself, and
	// none of it is this step's business.
	//
	// Leaving them nil rather than discarding them is what makes the timeout
	// work. A discarding writer is not a file, so os/exec would build a pipe
	// and copy it on a goroutine that Wait blocks on; a module that spawned a
	// child holding that pipe would keep the install waiting long after the
	// module itself was killed. WaitDelay bounds what remains.
	command := exec.CommandContext(ctx, filepath.Join(versionDir, executableName))
	command.Dir = versionDir
	command.Env = append(os.Environ(), module.CommandTreeEnv+"="+answer)
	command.WaitDelay = time.Second
	if err := command.Run(); err != nil {
		return commandtree.Tree{}, nil
	}

	content, err := os.ReadFile(answer)
	if err != nil {
		return commandtree.Tree{}, nil
	}
	var declared module.Declaration
	if err := json.Unmarshal(content, &declared); err != nil {
		return commandtree.Tree{}, nil
	}
	// The catalog is unsigned, so an entry filed under one namespace can point
	// at an archive holding another module. The executable's own answer is the
	// only thing that settles which module was actually installed, and a
	// disagreement is refused rather than recorded.
	if declared.Module.Namespace != namespace {
		return commandtree.Tree{}, problem.New(problem.CategoryModuleTrust,
			"install.namespace_disputed",
			fmt.Sprintf("the executable installed for the %q module declares the namespace %q",
				namespace, declared.Module.Namespace)).
			WithRecovery("Report this to whoever publishes the module; the published entry does not match what it contains.")
	}
	return declared.CommandTree, nil
}
