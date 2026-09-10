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
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/wso2/wso2-cli/internal/modules"
)

// releasedIndex is the published index.json as the shell's release job fetched
// it, base64-encoded so the linker flag carrying it holds no space or quote:
//
//	-X github.com/wso2/wso2-cli/internal/catalog.releasedIndex=<base64>
//
// It is empty in a development build, which knows of no released product.
//
// ADR 0015: help names every product the release knows about, offline, so the
// list is a copy of the catalog taken at release. The catalog is still a build
// output; the shell only carries a copy of one.
var releasedIndex string

// ReleasedIndex reports the index the running shell was released with. A copy that
// cannot be decoded reads as a release that knows of no product: the help page
// it feeds has to render whatever state the binary is in, and the release job
// proves the copy it injected reads back before it builds the release.
func ReleasedIndex() Index {
	index, err := DecodeReleased(releasedIndex)
	if err != nil {
		return Index{SchemaVersion: SchemaVersion}
	}
	return index
}

// DecodeReleased reads a base64-encoded index.json. The empty string is a
// release that carried no index, and decodes to one with no products.
func DecodeReleased(encoded string) (Index, error) {
	if encoded == "" {
		return Index{SchemaVersion: SchemaVersion}, nil
	}
	content, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return Index{}, fmt.Errorf("catalog: the released index is not base64: %w", err)
	}
	var index Index
	if err := json.Unmarshal(content, &index); err != nil {
		return Index{}, fmt.Errorf("catalog: the released index is not a readable index: %w", err)
	}
	if index.SchemaVersion != SchemaVersion {
		return Index{}, fmt.Errorf("catalog: the released index uses unsupported schema version %d",
			index.SchemaVersion)
	}
	seen := map[string]bool{}
	for _, entry := range index.Modules {
		if !modules.ValidNamespace(entry.Namespace) {
			return Index{}, fmt.Errorf("catalog: the released index names an invalid namespace %q",
				entry.Namespace)
		}
		if seen[entry.Namespace] {
			return Index{}, fmt.Errorf("catalog: the released index names %q twice", entry.Namespace)
		}
		seen[entry.Namespace] = true
	}
	return index, nil
}
