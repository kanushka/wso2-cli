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

package scaffold

import "embed"

// templateFS carries the generated module's files.
//
// They are real files rather than string literals in Go source so that what a
// developer will read is what a maintainer edits: a Go template inside a Go
// string cannot be read as the file it becomes, and a README with a fenced code
// block cannot be a raw string literal at all. The .tmpl suffix keeps the
// generated Go out of this repository's own build and lint.
//
//go:embed templates
var templateFS embed.FS

// files are every file a generation writes, in the order they are written.
//
// The bodies are templates rather than copies of modules/reference, because a
// module copied from the reference module would inherit the parts that exist to
// test the shell. What a developer gets is the smallest module that works.
var files = []generatedFile{
	{pathTemplate: "go.mod", template: "go.mod.tmpl"},
	{pathTemplate: "module.json", template: "module.json.tmpl"},
	{pathTemplate: "README.md", template: "README.md.tmpl"},
	{pathTemplate: "cmd/{{.Executable}}/main.go", template: "main.go.tmpl"},
	{pathTemplate: "cmd/{{.Executable}}/main_test.go", template: "main_test.go.tmpl"},
}
