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
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wso2/wso2-cli/internal/catalog"
)

func TestModuleTagsExcludeLocalExampleAndRetiredReferenceNamespaces(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	git("init", "-q")
	git("-c", "user.name=Test", "-c", "user.email=test@example.test", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-qm", "fixture")
	for _, tag := range []string{"reference/v0.1.0", "reference/v0.1.0-rc.1", "sdk/v0.3.0", "example/v0.1.0", "apim/v1.0.0", "v1.0.0"} {
		git("tag", tag)
	}
	tags, err := (publishedReleases{repositoryRoot: root}).ModuleTags()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"apim/v1.0.0"}; !reflect.DeepEqual(tags, want) {
		t.Fatalf("module tags = %v, want %v", tags, want)
	}
}

func TestPublicCatalogDeclarationsExcludeDemonstrationModules(t *testing.T) {
	root := t.TempDir()
	for _, namespace := range []string{"apim", "iam", "example", "reference"} {
		directory := filepath.Join(root, "modules", namespace)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "module.json"), []byte(`{"namespace":"`+namespace+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	declarations, err := catalog.DiscoverProducts(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, declaration := range declarations {
		names = append(names, declaration.Namespace)
	}
	if want := []string{"apim", "iam"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("public catalog namespaces = %v, want %v", names, want)
	}
}
