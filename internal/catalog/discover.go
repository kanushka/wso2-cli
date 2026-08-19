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

package catalog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// DeclarationFileName is the fixed name of a module's declaration, sitting
// beside its go.mod. A module directory without one declares no namespace and
// is therefore not something a tag may name.
const DeclarationFileName = "module.json"

// declarationRoots are the directories a module may live in, relative to the
// repository root. Product modules live one directory per namespace under
// modules/; the reference module lives under examples/ because it is not a
// product and owns a reserved namespace.
var declarationRoots = []string{"modules", "examples"}

// Discover reads the module declarations in a checkout, which is what decides
// whether a tag names a buildable module. A directory name is not the answer:
// the namespace a module owns is declared by the module, so the reference
// module's directory and its namespace are free to differ.
func Discover(repositoryRoot string) ([]Declaration, error) {
	declarations := []Declaration{}
	for _, root := range declarationRoots {
		entries, err := os.ReadDir(filepath.Join(repositoryRoot, root))
		if err != nil {
			if os.IsNotExist(err) {
				// A repository that has no product modules yet is not an error;
				// it is a catalog with no product namespaces in it.
				continue
			}
			return nil, fmt.Errorf("catalog: reading %s failed: %w", root, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			path := filepath.Join(repositoryRoot, root, entry.Name(), DeclarationFileName)
			declaration, found, err := readDeclaration(path)
			if err != nil {
				return nil, err
			}
			if found {
				declarations = append(declarations, declaration)
			}
		}
	}
	sort.Slice(declarations, func(left, right int) bool {
		return declarations[left].Namespace < declarations[right].Namespace
	})
	return declarations, nil
}

func readDeclaration(path string) (Declaration, bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Declaration{}, false, nil
		}
		return Declaration{}, false, fmt.Errorf("catalog: reading %s failed: %w", path, err)
	}
	var declaration Declaration
	if err := json.Unmarshal(content, &declaration); err != nil {
		return Declaration{}, false, fmt.Errorf("catalog: %s is not a readable module declaration: %w", path, err)
	}
	return declaration, true, nil
}
