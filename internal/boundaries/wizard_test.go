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

package boundaries_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestOnlyTheWizardImportsTheTerminalUILibrary keeps the terminal form library
// behind internal/wizard (ADR 0017). The shell asks every question through
// that package's Prompter, so the library can be replaced, and the rest of the
// shell can never draw to the terminal around mayPrompt's decision.
func TestOnlyTheWizardImportsTheTerminalUILibrary(t *testing.T) {
	root := repoRoot(t)
	allowed := filepath.Join("internal", "wizard")
	prefixes := []string{"charm.land/", "github.com/charmbracelet/"}

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || (path != root && isCheckoutRoot(path)) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if filepath.Dir(relative) == allowed {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("cannot parse %s: %v", relative, err)
		}
		for _, imported := range file.Imports {
			importPath, _ := strconv.Unquote(imported.Path.Value)
			for _, prefix := range prefixes {
				if strings.HasPrefix(importPath, prefix) {
					t.Errorf("%s imports %s; only %s may, so every question goes through its Prompter",
						relative, importPath, allowed)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
