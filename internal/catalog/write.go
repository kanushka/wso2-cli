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
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Write publishes the catalog into a site directory, creating the modules
// subdirectory. Existing files are replaced and a namespace file no tag answers
// to any more is removed, because a stale file left behind would go on being
// served and would disagree with what was released.
func Write(directory string, catalog Catalog) error {
	files, err := catalog.Files()
	if err != nil {
		return err
	}
	published := map[string]bool{}
	for _, file := range files {
		path := filepath.Join(directory, filepath.FromSlash(file.Path))
		published[path] = true
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("catalog: creating %s failed: %w", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, file.Content, 0o644); err != nil {
			return fmt.Errorf("catalog: writing %s failed: %w", path, err)
		}
	}
	return pruneNamespaceFiles(directory, published)
}

// pruneNamespaceFiles removes the namespace files this generation did not write.
func pruneNamespaceFiles(directory string, published map[string]bool) error {
	modulesDir := filepath.Join(directory, filepath.FromSlash(NamespacePath("")))
	modulesDir = filepath.Dir(modulesDir)
	entries, err := os.ReadDir(modulesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("catalog: reading %s failed: %w", modulesDir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(modulesDir, entry.Name())
		if published[path] {
			continue
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("catalog: removing the stale %s failed: %w", path, err)
		}
	}
	return nil
}
