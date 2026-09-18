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

import "testing"

func TestProductLabelNamesTheSummaryWhenThereIsOne(t *testing.T) {
	if got := productLabel("api", "WSO2 API Platform"); got != "api — WSO2 API Platform" {
		t.Fatalf("label = %q", got)
	}
	if got := productLabel("api", ""); got != "api" {
		t.Fatalf("label without a summary = %q", got)
	}
}
