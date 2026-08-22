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

// Command wso2-module-new creates a new product module in this repository.
//
//	go run ./cmd/wso2-module-new -namespace mycloud
//
// or, which is what the contributing guide points at:
//
//	make new-module NAMESPACE=mycloud
//
// What it writes builds and passes its own test immediately, so a developer has
// a known-good baseline before changing anything. The SDK version and the
// declared protocol versions are read from the checkout rather than written
// into a template, because a literal is correct until the next release and then
// produces a release-gate refusal the developer did not cause.
//
// This is contributor tooling rather than a released artifact: nothing a user
// installs contains it, and creating a module is not something a user's shell
// should be able to do.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wso2/wso2-cli/internal/scaffold"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "wso2-module-new: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	namespace := flag.String("namespace", "",
		"The product namespace the new module will own, such as mycloud.")
	flag.Parse()

	if *namespace == "" {
		flag.Usage()
		return fmt.Errorf("-namespace is required")
	}

	// The checkout is where the command is run from. A flag naming another one
	// would be a way to generate a module into a repository other than the one
	// whose SDK and protocol version it was generated against.
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	generated, err := scaffold.Generate(scaffold.Request{RepositoryRoot: root, Namespace: *namespace})
	if err != nil {
		return err
	}

	// The files are listed because the next thing the developer does is open
	// one, and the last line says which, so the guide does not have to.
	fmt.Printf("Created the %s module in %s:\n", *namespace, relative(root, generated.Directory))
	for _, file := range generated.Files {
		fmt.Printf("  %s\n", relative(root, file))
	}
	fmt.Printf("\nBuild and test it:\n  go test ./modules/%s/...\n", *namespace)
	fmt.Printf("Then open %s\n",
		relative(root, filepath.Join(generated.Directory, "cmd", "wso2-module-"+*namespace, "main.go")))
	return nil
}

// relative reports a path as a reader of the repository would name it.
func relative(root, path string) string {
	if shortened, err := filepath.Rel(root, path); err == nil {
		return shortened
	}
	return path
}
