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
	"bytes"
	"strings"
	"testing"
	"time"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/contexts/fixture"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/output"
)

// TestLoginGivesUpWhenTheBrowserNeverComesBack proves the login arms a
// deadline. The flow honours a cancelled context on its own, but nothing proved
// the command ever gave it one, and an unarmed login waits forever holding a
// callback port. The deadline is shortened here rather than waited out.
func TestLoginGivesUpWhenTheBrowserNeverComesBack(t *testing.T) {
	keyring.MockInit()
	t.Setenv("WSO2_CONTEXT", "")
	t.Setenv("WSO2_NON_INTERACTIVE", "")
	previous := loginDeadline
	loginDeadline = 50 * time.Millisecond
	t.Cleanup(func() { loginDeadline = previous })

	issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status"})
	root := t.TempDir()
	if err := fixture.WriteV2(root, contexts.Document{
		SchemaVersion:  contexts.SchemaVersion,
		DefaultContext: "acme-dev",
		Identities: []contexts.Identity{{
			Name: "acme-cloud",
			Type: "cloud",
			Auth: contexts.IdentityAuth{
				Kind:          contexts.KindOAuthBrowser,
				Issuer:        issuer.URL,
				ClientID:      "client-123",
				CredentialRef: "acme-cloud-login",
			},
		}},
		Contexts: []contexts.Context{{Name: "acme-dev", Identity: "acme-cloud", Organization: "acme"}},
	}); err != nil {
		t.Fatalf("install context document: %v", err)
	}

	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	shell := Shell{
		StateRoot: root,
		Streams:   output.Streams{Out: out, Err: errOut},
		// The browser opens and the user never finishes signing in.
		OpenBrowser: func(string) error { return nil },
	}

	if code := shell.Run([]string{"login"}); code != exit.AuthPolicy {
		t.Fatalf("exit code = %d, want %d (auth policy); stderr: %s", code, exit.AuthPolicy, errOut)
	}
	if !strings.Contains(errOut.String(), "(auth.credential_unavailable)") {
		t.Fatalf("an abandoned login did not report a typed refusal:\n%s", errOut)
	}
}
