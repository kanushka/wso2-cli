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

package smoke_test

import (
	"testing"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/test/smoke"
)

// thunderEnvironment is a Thunder deployment as the environment describes one.
// The audience is an absolute URI because Thunder refuses a resource identifier
// that is not.
func thunderEnvironment() map[string]string {
	return map[string]string{
		smoke.IssuerVar:       "https://localhost:8490",
		smoke.ClientIDVar:     "wso2-cli",
		smoke.AudienceVar:     "https://localhost:8490/reference-status",
		smoke.ScopeVar:        "read write",
		smoke.ProviderVar:     contexts.ProviderThunder,
		smoke.IdentityTypeVar: "onprem",
	}
}

// A run against a deployment that binds access to a named resource has to say
// so, or the document it installs derives the way every other deployment does
// and the login is refused before a browser opens.
func TestADeploymentMayDeclareItsIdentityProvider(t *testing.T) {
	config, err := smoke.Load(environment(thunderEnvironment()))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	identity := config.Document().Identities[0]
	if identity.Auth.Provider != contexts.ProviderThunder {
		t.Fatalf("the document names the provider %q, want %q",
			identity.Auth.Provider, contexts.ProviderThunder)
	}
	if got := identity.Auth.Derivation(); got != contexts.DerivationTokenResource {
		t.Fatalf("a Thunder deployment derives by %q, want %q",
			got, contexts.DerivationTokenResource)
	}
}

// The document a live run installs is read by the shell before any browser
// opens, so a Thunder run cannot fail on a document defect in front of a
// waiting human.
func TestAThunderDocumentIsReadableByTheShell(t *testing.T) {
	config, err := smoke.Load(environment(thunderEnvironment()))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	encoded, err := config.Document().Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, err := contexts.Decode(encoded); err != nil {
		t.Fatalf("the shell refused the document a Thunder run installs: %v", err)
	}
}

// A provider this shell does not read is a description of a deployment it
// cannot act on, and a run that reached a browser first would waste the sign-in.
func TestAnUnknownProviderIsRefusedBeforeAnyRun(t *testing.T) {
	values := thunderEnvironment()
	values[smoke.ProviderVar] = "acme-idp"
	if _, err := smoke.Load(environment(values)); err == nil {
		t.Fatal("an unreadable identity provider was accepted")
	}
}

// The non-interactive run installs its own document, because the identity it
// authenticates as is a different one: a confidential client with a secret,
// holding no secure-store reference and never logging in.
func TestTheNonInteractiveDocumentIsReadableByTheShell(t *testing.T) {
	values := thunderEnvironment()
	values[smoke.CIClientIDVar] = "wso2-cli-ci"
	config, err := smoke.Load(environment(values))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	document := config.CIDocument()
	encoded, err := document.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, err := contexts.Decode(encoded); err != nil {
		t.Fatalf("the shell refused the document a CI run installs: %v", err)
	}

	identity := document.Identities[0]
	if identity.Auth.Kind != contexts.KindClientCredentials {
		t.Fatalf("the CI identity is of kind %q, want %q",
			identity.Auth.Kind, contexts.KindClientCredentials)
	}
	if identity.Auth.ClientID != "wso2-cli-ci" {
		t.Fatalf("the CI identity presents %q, want the configured confidential client",
			identity.Auth.ClientID)
	}
	if identity.Auth.ClientSecretVariable != smoke.SecretVariable {
		t.Fatalf("the CI identity reads its secret from %q, want %q",
			identity.Auth.ClientSecretVariable, smoke.SecretVariable)
	}
	// A non-interactive identity never logs in, so it must hold no reference to
	// a stored session. The schema refuses one; so must the document this run
	// builds, or a live run fails on a defect this package could have caught.
	if identity.Auth.CredentialRef != "" {
		t.Fatalf("the CI identity names a secure-store reference %q; it never logs in",
			identity.Auth.CredentialRef)
	}
}

// The CI document derives the same way the interactive one does, because the
// deployment is the same deployment. A confidential client on Thunder needs the
// resource indicator too — it has no earlier authorization to inherit one from.
func TestTheNonInteractiveDocumentDerivesLikeTheDeployment(t *testing.T) {
	values := thunderEnvironment()
	values[smoke.CIClientIDVar] = "wso2-cli-ci"
	config, err := smoke.Load(environment(values))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := config.CIDocument().Identities[0].Auth.Derivation(); got != contexts.DerivationTokenResource {
		t.Fatalf("the CI identity derives by %q, want %q", got, contexts.DerivationTokenResource)
	}
}

// A deployment description names no secret and no secret's variable. The
// non-interactive run needs one, so the name it reads is fixed here instead,
// where nobody is invited to paste a value beside it.
func TestTheNonInteractiveSecretVariableIsNamedInCodeOnly(t *testing.T) {
	if smoke.SecretVariable == "" {
		t.Fatal("the non-interactive run names no environment variable for its client secret")
	}
	if smoke.SecretVariable == smoke.ClientIDVar || smoke.SecretVariable == smoke.IssuerVar {
		t.Fatalf("the client secret variable collides with a deployment variable: %q",
			smoke.SecretVariable)
	}
}
