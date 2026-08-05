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

package oauthflow_test

import (
	"testing"

	"github.com/wso2/wso2-cli/internal/auth/oauthflow"
)

// TestOpenIsSuppressedByTheEnvironment proves WSO2_NO_BROWSER stops the shell
// from launching anything.
//
// "Did not launch a browser" is observed by emptying PATH: with no opener
// reachable, an attempt to launch one must fail, so the variable's effect is
// the difference between that failure and a clean no-op.
func TestOpenIsSuppressedByTheEnvironment(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	target := "http://127.0.0.1:10425/callback"

	t.Setenv(oauthflow.NoBrowserEnvVar, "")
	if err := oauthflow.Open(target); err == nil {
		t.Skip("this platform opens a browser without an opener on PATH; the suppression check cannot observe it")
	}

	t.Setenv(oauthflow.NoBrowserEnvVar, "1")
	if err := oauthflow.Open(target); err != nil {
		t.Fatalf("%s did not suppress the browser: %v", oauthflow.NoBrowserEnvVar, err)
	}
}
