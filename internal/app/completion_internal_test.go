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

package app

import (
	"slices"
	"strings"
	"testing"
)

// TestProductWordsStepOverTheRootFlags pins where completion finds the product
// namespace: after the root's own flags and their values, read the way pflag
// reads them.
func TestProductWordsStepOverTheRootFlags(t *testing.T) {
	root := Shell{}.rootCommand()
	for _, test := range []struct {
		words     string
		namespace string
		rest      string
	}{
		{"api apis", "api", "apis"},
		{"--context prod api", "api", ""},
		{"--context=prod api", "api", ""},
		{"--verbose api", "api", ""},
		{"-o json api", "api", ""},
		{"-ojson api", "api", ""},
		{"-o=json api", "api", ""},
		{"-ho json api list", "api", "list"},
		{"-oh api", "api", ""},
		{"-h api", "api", ""},
		{"--output", "", ""},
		{"-x api", "api", ""},
		{"-- api", "", ""},
	} {
		t.Run(test.words, func(t *testing.T) {
			namespace, rest, found := productWords(root, strings.Fields(test.words))
			if namespace != test.namespace || found != (test.namespace != "") {
				t.Fatalf("namespace = %q (found %v), want %q", namespace, found, test.namespace)
			}
			if want := strings.Fields(test.rest); !slices.Equal(rest, want) && len(rest)+len(want) > 0 {
				t.Fatalf("rest = %q, want %q", rest, want)
			}
		})
	}
}
