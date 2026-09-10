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

package output

import (
	"bytes"
	"testing"
)

func TestHintMarksCommandsInsideSentences(t *testing.T) {
	t.Setenv("FORCE_COLOR", "")
	cases := map[string]string{
		"Run wso2 login.": "Run `wso2 login`.",
		"Run wso2 login to establish a session for this context.":                            "Run `wso2 login` to establish a session for this context.",
		"Run wso2 login to authorize every product, or wso2 login --only <product> for one.": "Run `wso2 login` to authorize every product, or `wso2 login --only <product>` for one.",
		"Create the account first with wso2 identity connect <login-provider-url> [--account <name>], then run this command again; or pass --login-provider <issuer-url> naming an account that exists. wso2 account list shows them.": "Create the account first with `wso2 identity connect <login-provider-url> [--account <name>]`, then run this command again; or pass --login-provider <issuer-url> naming an account that exists. `wso2 account list` shows them.",
		"Run wso2 identity --help to see what this module can do at http://localhost:8501.":              "Run `wso2 identity --help` to see what this module can do at http://localhost:8501.",
		"Run wso2 context create <name> --account <account> [--organization <name>] [--project <name>].": "Run `wso2 context create <name> --account <account> [--organization <name>] [--project <name>]`.",
		"Run wso2 logout [--context <name>] [--output table|json].":                                      "Run `wso2 logout [--context <name>] [--output table|json]`.",
		"Nothing holds a session until wso2 login has established one.":                                  "Nothing holds a session until `wso2 login` has established one.",
		"Run wso2 login --context demo":                                                                  "Run `wso2 login --context demo`",
		"Run wso2 apim apis deploy MockAPI/1.0.0.":                                                       "Run `wso2 apim apis deploy MockAPI/1.0.0`.",
		"The WSO2 CLI names no command here.":                                                            "The WSO2 CLI names no command here.",
		"Valid keys: output, catalog-origin.":                                                            "Valid keys: output, catalog-origin.",
	}
	for text, want := range cases {
		if got := Hint(&bytes.Buffer{}, text); got != want {
			t.Errorf("Hint(%q)\n got  %q\n want %q", text, got, want)
		}
	}
}

func TestHintColorsCommandsWhereColorIsForced(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")
	want := "Run " + commandStyle + "wso2 login" + resetStyle + "."
	if got := Hint(&bytes.Buffer{}, "Run wso2 login."); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// NO_COLOR outranks FORCE_COLOR.
	t.Setenv("NO_COLOR", "1")
	if got := Hint(&bytes.Buffer{}, "Run wso2 login."); got != "Run `wso2 login`." {
		t.Errorf("NO_COLOR ignored: %q", got)
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "0")
	if got := Hint(&bytes.Buffer{}, "Run wso2 login."); got != "Run `wso2 login`." {
		t.Errorf("FORCE_COLOR=0 forced color: %q", got)
	}
}
