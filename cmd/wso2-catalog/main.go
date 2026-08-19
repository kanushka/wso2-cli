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

// Command wso2-catalog generates the module catalog into a site directory.
//
// It is a build tool rather than a released artifact: the release
// configuration builds the shell alone, and nothing a user installs contains
// this command. It exists so the release job and a contributor run the same
// generator over the same inputs.
//
//	go run ./cmd/wso2-catalog -input releases.json -out site
//
// The input document names the module tags that exist and, for each one, what
// that tag published: the compatibility the build declared and the platform
// archives uploaded for it. The buildable modules are read from the checkout
// rather than from the input, so a tag naming a module this repository cannot
// build fails generation instead of publishing an entry pointing at nothing.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/wso2/wso2-cli/internal/catalog"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "wso2-catalog: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	inputPath := flag.String("input", "", "Path to the release input document.")
	outputDir := flag.String("out", "site", "Directory to write index.json and modules/ into.")
	repositoryRoot := flag.String("repo", ".", "Repository checkout to read module declarations from.")
	flag.Parse()

	if *inputPath == "" {
		return fmt.Errorf("no -input document was given")
	}

	content, err := os.ReadFile(*inputPath)
	if err != nil {
		return fmt.Errorf("reading %s failed: %w", *inputPath, err)
	}
	var input catalog.Input
	if err := json.Unmarshal(content, &input); err != nil {
		return fmt.Errorf("%s is not a readable release input: %w", *inputPath, err)
	}

	input.Modules, err = catalog.Discover(*repositoryRoot)
	if err != nil {
		return err
	}

	generated, err := catalog.Generate(input)
	if err != nil {
		return err
	}
	if err := catalog.Write(*outputDir, generated); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "wrote %s and %d namespace file(s) into %s\n",
		catalog.IndexPath, len(generated.Namespaces), *outputDir)
	return nil
}
