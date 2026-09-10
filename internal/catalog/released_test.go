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

package catalog_test

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/catalog"
)

func encode(document string) string {
	return base64.StdEncoding.EncodeToString([]byte(document))
}

// A release build carries the published index as it was fetched, base64 so
// the linker flag that injects it holds no space or quote. Decoding it yields
// the products the release knew about, titles included.
func TestDecodeReleasedReadsTheIndexAReleaseCarried(t *testing.T) {
	index, err := catalog.DecodeReleased(encode(`{
  "schemaVersion": 1,
  "modules": [
    {"namespace": "api", "title": "API Platform", "path": "modules/api.json",
     "channels": [{"channel": "stable", "version": "1.0.0"}]},
    {"namespace": "reference", "path": "modules/reference.json",
     "channels": [{"channel": "stable", "version": "0.1.0"}]}
  ]
}`))
	if err != nil {
		t.Fatalf("decoding a published index returned %v", err)
	}
	if len(index.Modules) != 2 {
		t.Fatalf("decoded %d products, want 2: %+v", len(index.Modules), index.Modules)
	}
	if index.Modules[0].Namespace != "api" || index.Modules[0].Title != "API Platform" {
		t.Errorf("the first product decoded as %+v, want api titled API Platform", index.Modules[0])
	}
	if index.Modules[1].Namespace != "reference" || index.Modules[1].Title != "" {
		t.Errorf("the second product decoded as %+v, want reference with no title", index.Modules[1])
	}
}

// A development build carries nothing, which is a release that knows of no
// product rather than a failure: help still has installed products to name.
func TestDecodeReleasedReadsNothingAsNoProducts(t *testing.T) {
	index, err := catalog.DecodeReleased("")
	if err != nil {
		t.Fatalf("decoding a release that carried no copy returned %v", err)
	}
	if len(index.Modules) != 0 {
		t.Errorf("a release that carried no copy decoded to %+v, want no products", index.Modules)
	}
}

// Anything a release could not have meant is refused rather than half read, the
// way an unknown catalog schema is refused on the network.
func TestDecodeReleasedRefusesWhatNoReleaseWrites(t *testing.T) {
	for name, encoded := range map[string]string{
		"text that is not base64":  "not base64!",
		"base64 that is not JSON":  encode("not json"),
		"an unknown schema":        encode(`{"schemaVersion": 99, "modules": []}`),
		"an invalid namespace":     encode(`{"schemaVersion": 1, "modules": [{"namespace": "Not Valid"}]}`),
		"a namespace listed twice": encode(`{"schemaVersion": 1, "modules": [{"namespace": "api"}, {"namespace": "api"}]}`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := catalog.DecodeReleased(encoded); err == nil {
				t.Fatal("decoding succeeded")
			}
		})
	}
}

// The release job fetches the copy a shell carries from a URL written in the
// workflow, because the fetch runs before any shell exists to ask. Holding the
// two together here is what keeps a change of origin from leaving releases
// carrying a copy of a catalog nobody reads any more.
func TestTheReleaseJobFetchesTheIndexFromTheDefaultOrigin(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatalf("reading the release workflow returned %v", err)
	}
	want := catalog.DefaultOrigin + "/" + catalog.IndexPath
	if !strings.Contains(string(workflow), want) {
		t.Errorf("the release workflow does not fetch the catalog copy from %s", want)
	}
}
