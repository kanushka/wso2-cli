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

package acceptance_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	contextfixture "github.com/wso2/wso2-cli/internal/contexts/fixture"
	"github.com/wso2/wso2-cli/internal/exit"
	modulefixture "github.com/wso2/wso2-cli/internal/modules/fixture"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/internal/state"
	"github.com/wso2/wso2-cli/internal/version"
	"github.com/wso2/wso2-cli/sdk/protocol"
)

// These tests run the shell in-process rather than as a built binary, which is
// the one place this package departs from its own rule.
//
// The reason is the OS secure store. A session lives in the real keychain, and
// a test must not write to a developer's own; go-keyring's mock replaces the
// backend inside the process that calls it, and a subprocess would not inherit
// it. Everything else about the chain stays external: the shell resolves and
// launches the reference module as a real subprocess, the module speaks the
// real protocol over its pipes, and it presents the token it was granted to a
// real HTTP service. What is proved here is what a user gets, not what the
// shell believes.
const (
	// loginIdentityName is the browser identity these tests log in as.
	loginIdentityName = "reference-cloud"
	// loginCredentialRef is the secure-store entry its session lives under.
	loginCredentialRef = "reference-login"
	// referenceWriteScope is the second permission the identity's product
	// declares. The login session holds it and no module command asks for it,
	// which is what makes narrowing observable rather than incidental.
	referenceWriteScope = "reference:status:write"
	// loginClientID is the public OAuth client the shell presents itself as.
	loginClientID = "wso2cli"
	// inlineSecretVariable is the environment variable a client-credentials
	// identity names as its client secret source.
	inlineSecretVariable = "WSO2_REFERENCE_CLIENT_SECRET"
	// inlineClientSecret is the canary a CI job would really export. The
	// deployment checks it, so a command that succeeds could only have read it
	// from the variable — and no surface the shell writes may contain it.
	inlineClientSecret = "canary-ci-client-secret-6a20"
)

// recordingService stands in for the product service, recording the bearer
// token it was presented.
//
// The token is never printed by the shell or the module — it must not be — so
// the only honest place to observe what the module actually received is the
// service that received it. Everything asserted about a grant in this file is
// asserted from here or from the issuer's introspection endpoint.
type recordingService struct {
	server *httptest.Server

	mutex   sync.Mutex
	bearers []string
}

// startRecordingService serves the reference status shape and records every
// presented bearer token. It deliberately verifies nothing: the point of these
// tests is what the shell issued, and a service that refused would hide it
// behind a product failure.
func startRecordingService(t *testing.T) *recordingService {
	t.Helper()
	service := &recordingService{}
	service.server = httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			service.mutex.Lock()
			service.bearers = append(service.bearers,
				strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer "))
			service.mutex.Unlock()
			writer.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(writer).Encode(map[string]string{
				"organization": referenceOrganization,
				"service":      "reference",
				"status":       "operational",
				"checkedAt":    time.Now().UTC().Format(time.RFC3339),
			}); err != nil {
				t.Errorf("the recording service could not answer: %v", err)
			}
		}))
	t.Cleanup(service.server.Close)
	return service
}

// presented returns the bearer tokens the service has been shown, in order.
func (s *recordingService) presented() []string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return append([]string(nil), s.bearers...)
}

// loginDeployment is one isolated browser-identity installation: an issuer, the
// product service, the installed reference module, and the shell that runs
// against them.
type loginDeployment struct {
	shell     app.Shell
	out       *bytes.Buffer
	errOut    *bytes.Buffer
	issuer    *fakeissuer.Issuer
	service   *recordingService
	stateRoot string
}

// deployLogin installs a schema version 2 browser identity against a running
// fake issuer. The document is handed to alter before it is written, so a test
// can break exactly one thing about the deployment.
func deployLogin(
	t *testing.T, options fakeissuer.Options, alter func(*contexts.Document),
) *loginDeployment {
	t.Helper()
	return deployLoginInstallation(t, options, alter, true)
}

// deployLoginWithoutModule is the same installation with no product module in
// the store.
//
// Installing one builds the reference module, which costs more than everything
// else these tests do put together. A test whose subject is the session itself —
// establishing one, ending one — never dispatches a namespace, so it never
// resolves a module, and paying for one would buy nothing. A test that does
// dispatch must use deployLogin.
func deployLoginWithoutModule(
	t *testing.T, options fakeissuer.Options, alter func(*contexts.Document),
) *loginDeployment {
	t.Helper()
	return deployLoginInstallation(t, options, alter, false)
}

func deployLoginInstallation(
	t *testing.T, options fakeissuer.Options, alter func(*contexts.Document), withModule bool,
) *loginDeployment {
	t.Helper()
	keyring.MockInit()
	// A developer's own environment must not decide what these tests prove.
	t.Setenv("WSO2_CONTEXT", "")
	t.Setenv("WSO2_NON_INTERACTIVE", "")
	if options.Audience == "" {
		options.Audience = referenceAudience
	}
	issuer := fakeissuer.New(t, options)
	service := startRecordingService(t)
	stateRoot := isolatedStateRoot(t)
	if withModule {
		installInProcessReferenceModule(t, stateRoot)
	}

	document := browserDocument(issuer.URL, service.server.URL)
	if alter != nil {
		alter(&document)
	}
	if err := contextfixture.WriteV2(stateRoot, document); err != nil {
		t.Fatalf("installing the v2 context document: %v", err)
	}

	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	return &loginDeployment{
		shell: app.Shell{
			StateRoot: stateRoot,
			Streams:   output.Streams{Out: out, Err: errOut},
		},
		out: out, errOut: errOut,
		issuer: issuer, service: service, stateRoot: stateRoot,
	}
}

// installInProcessReferenceModule installs the reference module for a shell
// running in this process rather than as a built binary.
//
// The rest of this package builds both sides with matching version ldflags. An
// in-process shell reports the package defaults instead, so the module is built
// and its receipt written against those: the compatibility check is real, and
// pinning it to what the shell under test actually is keeps it that way.
func installInProcessReferenceModule(t *testing.T, stateRoot string) {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "wso2-module-reference"+executableSuffix())
	build(t, filepath.Join(repoRoot(t), "modules", "reference"), binary, "",
		"./cmd/wso2-module-reference")
	if _, err := modulefixture.Install(state.ModuleStore(stateRoot), modulefixture.Module{
		Namespace:  "reference",
		Version:    version.Shell(),
		SourcePath: binary,
		// The module was built with no protocol ldflag, so its receipt records
		// the protocol version the SDK declares rather than a fixed number.
		ProtocolVersions: protocol.Supported(),
		AuthAudiences:    []string{referenceAudience},
		AuthScopes:       []string{referenceReadScope},
	}); err != nil {
		t.Fatalf("installing the reference module: %v", err)
	}
}

// browserDocument is the context document a browser login runs against: one
// identity, one product, and a context that stays in the identity's home
// tenant.
func browserDocument(issuerURL, endpoint string) contexts.Document {
	return contexts.Document{
		SchemaVersion:  contexts.SchemaVersion,
		DefaultContext: referenceContextName,
		Identities: []contexts.Identity{{
			Name: loginIdentityName,
			Type: "cloud",
			Auth: contexts.IdentityAuth{
				Kind:          contexts.KindOAuthBrowser,
				Issuer:        issuerURL,
				ClientID:      loginClientID,
				Tenant:        referenceOrganization,
				CredentialRef: loginCredentialRef,
			},
			Products: map[string]contexts.Product{
				"reference": {
					Endpoint: endpoint,
					Audience: referenceAudience,
					Scopes:   []string{referenceReadScope, referenceWriteScope},
				},
			},
		}},
		Contexts: []contexts.Context{{
			Name:         referenceContextName,
			Identity:     loginIdentityName,
			Organization: referenceOrganization,
		}},
	}
}

// login runs wso2 login, driving the browser step by following the printed
// authorization URL. The fake issuer auto-approves, so one redirect-following
// request is the whole sign-in.
func (d *loginDeployment) login(t *testing.T) {
	t.Helper()
	d.shell.OpenBrowser = func(authURL string) error {
		go func() {
			response, err := http.Get(authURL)
			if err == nil {
				_ = response.Body.Close()
			}
		}()
		return nil
	}
	if code := d.shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("wso2 login exited %d\nstderr:\n%s", code, d.errOut)
	}
}

// seedSession stores the session a completed login would have left, for tests
// whose subject is the derivation rather than the login.
func (d *loginDeployment) seedSession(t *testing.T) string {
	t.Helper()
	refresh := d.issuer.SeedSession([]string{
		"openid", "offline_access", referenceReadScope, referenceWriteScope,
	})
	if err := (session.Store{StateRoot: d.stateRoot}).Save(loginCredentialRef, session.Session{
		Issuer: d.issuer.URL, RefreshToken: refresh,
	}); err != nil {
		t.Fatalf("seeding the stored session: %v", err)
	}
	return refresh
}

// storedSession is the session the secure store holds now.
func (d *loginDeployment) storedSession(t *testing.T) session.Session {
	t.Helper()
	stored, err := (session.Store{StateRoot: d.stateRoot}).Load(loginCredentialRef)
	if err != nil {
		t.Fatalf("loading the stored session: %v", err)
	}
	return stored
}

// status runs one reference status command and returns its exit code.
func (d *loginDeployment) status(t *testing.T) exit.Code {
	t.Helper()
	return d.shell.Run([]string{"reference", "status", "--output", "json"})
}

func TestLoginThenTheModuleReceivesIssuerMintedNarrowedAccess(t *testing.T) {
	// The whole chain in one run: a browser login establishes a session, a
	// module command derives access from it, and what the module presents to
	// its product service is a token the issuer will vouch for — carrying the
	// one permission the module asked for out of the several the session
	// holds, bound to the product's audience.
	//
	// The issuer rotates refresh tokens, so the second run also proves the
	// replacement was persisted: the token the first run presented is dead by
	// the time the second one starts.
	deployment := deployLogin(t, fakeissuer.Options{
		RefreshScopeMode:     "honor",
		RotateRefreshTokens:  true,
		AllowAnyLoopbackPort: true,
	}, nil)
	deployment.login(t)
	afterLogin := deployment.storedSession(t)

	if code := deployment.status(t); code != exit.OK {
		t.Fatalf("the first reference status exited %d\nstderr:\n%s", code, deployment.errOut)
	}
	afterFirst := deployment.storedSession(t)
	if code := deployment.status(t); code != exit.OK {
		t.Fatalf("the second reference status exited %d\nstderr:\n%s", code, deployment.errOut)
	}
	afterSecond := deployment.storedSession(t)

	presented := deployment.service.presented()
	if len(presented) != 2 {
		t.Fatalf("the product service was shown %d bearer tokens, want 2", len(presented))
	}
	for index, token := range presented {
		active, scopes, audiences := deployment.issuer.Introspect(t, token)
		if !active {
			t.Fatalf("run %d presented a token the issuer did not mint", index+1)
		}
		if len(scopes) != 1 || scopes[0] != referenceReadScope {
			t.Errorf("run %d presented scopes %v, want exactly [%s]",
				index+1, scopes, referenceReadScope)
		}
		if len(audiences) != 1 || audiences[0] != referenceAudience {
			t.Errorf("run %d presented audience %v, want [%s]",
				index+1, audiences, referenceAudience)
		}
	}

	// Rotation: each run left a replacement behind, and the second run could
	// only have succeeded by using what the first one stored.
	if afterFirst.RefreshToken == afterLogin.RefreshToken {
		t.Fatal("the first run did not persist the rotated refresh token")
	}
	if afterSecond.RefreshToken == afterFirst.RefreshToken {
		t.Fatal("the second run did not persist the rotated refresh token")
	}
	// The token the login left is dead: the issuer replaced it two runs ago.
	if err := (session.Store{StateRoot: deployment.stateRoot}).Save(loginCredentialRef, afterLogin); err != nil {
		t.Fatalf("restoring the superseded session: %v", err)
	}
	if code := deployment.status(t); code != exitAuthPolicy {
		t.Fatalf("a superseded refresh token exited %d, want the authentication class %d",
			code, exitAuthPolicy)
	}
	if !strings.Contains(deployment.errOut.String(), "auth.login_required") {
		t.Errorf("a superseded session did not refuse auth.login_required:\n%s", deployment.errOut)
	}
}

func TestAModuleRunWithoutALoginIsRefusedWithTheCommandThatFixesIt(t *testing.T) {
	deployment := deployLogin(t, fakeissuer.Options{RefreshScopeMode: "honor"}, nil)

	code := deployment.status(t)

	if code != exitAuthPolicy {
		t.Fatalf("exit = %d, want the authentication class %d\nstderr:\n%s",
			code, exitAuthPolicy, deployment.errOut)
	}
	stderr := deployment.errOut.String()
	if !strings.Contains(stderr, "auth.login_required") {
		t.Errorf("stderr does not name the refusal:\n%s", stderr)
	}
	if !strings.Contains(stderr, "wso2 login") {
		t.Errorf("stderr does not name the command that fixes it:\n%s", stderr)
	}
	if len(deployment.service.presented()) != 0 {
		t.Error("a refused command still reached the product service")
	}
	if deployment.out.String() != "" {
		t.Errorf("a refused command wrote to standard output:\n%s", deployment.out)
	}
}

func TestAContextOutsideTheIdentitysHomeTenantIsRefused(t *testing.T) {
	// This release logs in once, into one tenant. A context pointing that
	// session at another organization is refused rather than run somewhere the
	// user did not ask for.
	deployment := deployLogin(t, fakeissuer.Options{RefreshScopeMode: "honor"},
		func(document *contexts.Document) {
			document.Contexts[0].Organization = "another-org"
		})
	deployment.seedSession(t)

	code := deployment.status(t)

	if code != exitAuthPolicy {
		t.Fatalf("exit = %d, want the authentication class %d\nstderr:\n%s",
			code, exitAuthPolicy, deployment.errOut)
	}
	if !strings.Contains(deployment.errOut.String(), "auth.organization_switch_unsupported") {
		t.Errorf("stderr does not name the refusal:\n%s", deployment.errOut)
	}
	if len(deployment.service.presented()) != 0 {
		t.Error("a refused command still reached the product service")
	}
}

func TestADeploymentThatCannotNarrowIsRefusedRatherThanGrantedMore(t *testing.T) {
	// A deployment that will not scope a session down, or that binds tokens to
	// an audience the module does not serve, gets no grant at all. The module
	// is never handed the broader session it would otherwise receive.
	for name, options := range map[string]fakeissuer.Options{
		"the deployment ignores the narrower request": {RefreshScopeMode: "ignore"},
		"the deployment refuses to narrow":            {RefreshScopeMode: "reject"},
		"the deployment registers another audience": {
			RefreshScopeMode: "honor", Audience: "other-api",
		},
	} {
		t.Run(name, func(t *testing.T) {
			deployment := deployLogin(t, options, nil)
			deployment.seedSession(t)

			code := deployment.status(t)

			if code != exitAuthPolicy {
				t.Fatalf("exit = %d, want the authentication class %d\nstderr:\n%s",
					code, exitAuthPolicy, deployment.errOut)
			}
			if !strings.Contains(deployment.errOut.String(), "auth.narrowing_unavailable") {
				t.Errorf("stderr does not name the refusal:\n%s", deployment.errOut)
			}
			if len(deployment.service.presented()) != 0 {
				t.Error("a refused command still reached the product service")
			}
		})
	}
}

func TestNonInteractiveRunsRefuseInsteadOfWaitingForABrowser(t *testing.T) {
	// A job that declares itself non-interactive must fail fast on both
	// commands: the login it cannot complete, and the module run that has no
	// session to derive from. Neither may wait for a human who is not there.
	deployment := deployLogin(t, fakeissuer.Options{RefreshScopeMode: "honor"}, nil)
	t.Setenv("WSO2_NON_INTERACTIVE", "1")
	deployment.shell.OpenBrowser = func(string) error {
		t.Error("a non-interactive run opened a browser")
		return nil
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if code := deployment.shell.Run([]string{"login"}); code != exitAuthPolicy {
			t.Errorf("wso2 login exited %d, want the authentication class %d", code, exitAuthPolicy)
		}
		if code := deployment.status(t); code != exitAuthPolicy {
			t.Errorf("reference status exited %d, want the authentication class %d",
				code, exitAuthPolicy)
		}
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("a non-interactive run waited instead of refusing")
	}

	stderr := deployment.errOut.String()
	for _, expected := range []string{"auth.non_interactive", "auth.login_required"} {
		if !strings.Contains(stderr, expected) {
			t.Errorf("stderr does not name %s:\n%s", expected, stderr)
		}
	}
}

func TestNoTokenMaterialReachesAnyOutputSurface(t *testing.T) {
	// The sweep only means something if the run held something worth
	// disclosing, so it first proves a real session and a real grant existed,
	// then scans every stream both commands wrote.
	deployment := deployLogin(t, fakeissuer.Options{
		RefreshScopeMode:     "honor",
		RotateRefreshTokens:  true,
		AllowAnyLoopbackPort: true,
	}, nil)
	deployment.login(t)
	loginOut, loginErr := deployment.out.String(), deployment.errOut.String()
	afterLogin := deployment.storedSession(t)

	if code := deployment.status(t); code != exit.OK {
		t.Fatalf("reference status exited %d\nstderr:\n%s", code, deployment.errOut)
	}
	presented := deployment.service.presented()
	if len(presented) != 1 {
		t.Fatalf("the product service was shown %d bearer tokens, want 1", len(presented))
	}

	material := map[string]string{
		"the login's refresh token": afterLogin.RefreshToken,
		"the login's access token":  afterLogin.AccessToken,
		"the rotated refresh token": deployment.storedSession(t).RefreshToken,
		"the module's access token": presented[0],
	}
	surfaces := map[string]string{
		"login standard output":  loginOut,
		"login diagnostics":      loginErr,
		"status standard output": deployment.out.String(),
		"status diagnostics":     deployment.errOut.String(),
	}
	for label, secret := range material {
		if secret == "" {
			t.Fatalf("%s is empty, so this sweep scans for nothing", label)
		}
		for surface, stream := range surfaces {
			if strings.Contains(stream, secret) {
				t.Errorf("%s appeared on %s:\n%s", label, surface, stream)
			}
		}
	}
}

// deployInline installs the same deployment as a non-interactive identity: the
// credential is an environment variable a CI job exported, there is no secure
// store reference, and no login ever happens.
//
// secret is what the job exported. The empty string models the variable being
// absent, which is what an unset one and a blank one both amount to.
func deployInline(t *testing.T, options fakeissuer.Options, secret string) *loginDeployment {
	t.Helper()
	// The deployment holds the client to a secret, so a grant proves the shell
	// read the variable rather than that the fixture is permissive.
	options.ClientSecret = inlineClientSecret
	deployment := deployLogin(t, options, func(document *contexts.Document) {
		identity := &document.Identities[0].Auth
		identity.Kind = contexts.KindClientCredentials
		// A non-interactive identity holds no secure-store reference: there is
		// no session to keep.
		identity.CredentialRef = ""
		identity.ClientSecretVariable = inlineSecretVariable
	})
	t.Setenv(inlineSecretVariable, secret)
	return deployment
}

func TestAnInlineIdentityAuthenticatesACommandWithNoLoginStep(t *testing.T) {
	// The CI shape end to end: one module command, no login, and what the
	// module presents to its product service is a token the deployment minted
	// for exactly the permission the module asked for.
	deployment := deployInline(t, fakeissuer.Options{}, inlineClientSecret)

	if code := deployment.status(t); code != exit.OK {
		t.Fatalf("reference status exited %d\nstderr:\n%s", code, deployment.errOut)
	}

	presented := deployment.service.presented()
	if len(presented) != 1 {
		t.Fatalf("the product service was shown %d bearer tokens, want 1", len(presented))
	}
	active, scopes, audiences := deployment.issuer.Introspect(t, presented[0])
	if !active {
		t.Fatal("the module presented a token the issuer did not mint")
	}
	if len(scopes) != 1 || scopes[0] != referenceReadScope {
		t.Errorf("presented scopes %v, want exactly [%s]", scopes, referenceReadScope)
	}
	if len(audiences) != 1 || audiences[0] != referenceAudience {
		t.Errorf("presented audience %v, want [%s]", audiences, referenceAudience)
	}

	// Nothing was kept. The credential was already on the machine, so a stored
	// session would be a second copy of an authority the job already has.
	for _, ref := range []string{loginCredentialRef, loginIdentityName} {
		if _, err := keyring.Get(session.Service, ref); !errors.Is(err, keyring.ErrNotFound) {
			t.Errorf("the secure store holds an entry under %q after an inline run", ref)
		}
	}
	if _, err := os.Stat(filepath.Join(deployment.stateRoot, "cli", "locks")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("an inline run left session state under the state root: %v", err)
	}
}

func TestAnInlineIdentityWithNoSecretTellsTheUserWhichVariableToSet(t *testing.T) {
	// The refusal a job hits on its first run. It has to name the variable, or
	// the operator has nothing to act on; the module receives the same refusal
	// without it.
	for name, exported := range map[string]string{
		"the variable is empty": "",
		"the variable is blank": "   ",
	} {
		t.Run(name, func(t *testing.T) {
			deployment := deployInline(t, fakeissuer.Options{}, exported)

			code := deployment.status(t)

			if code != exitAuthPolicy {
				t.Fatalf("exit = %d, want the authentication class %d\nstderr:\n%s",
					code, exitAuthPolicy, deployment.errOut)
			}
			stderr := deployment.errOut.String()
			if !strings.Contains(stderr, "auth.credential_unavailable") {
				t.Errorf("stderr does not name the refusal:\n%s", stderr)
			}
			if !strings.Contains(stderr, inlineSecretVariable) {
				t.Errorf("stderr does not name the variable to set:\n%s", stderr)
			}
			if len(deployment.service.presented()) != 0 {
				t.Error("a refused command still reached the product service")
			}
		})
	}
}

func TestAnInlineDeploymentThatCannotNarrowIsRefusedRatherThanGrantedMore(t *testing.T) {
	// The verification the browser derivation applies is the same one applied
	// here. A deployment that answers with the registered client's whole
	// authority, or refuses to narrow at all, hands the module nothing.
	for name, options := range map[string]fakeissuer.Options{
		"the deployment ignores the narrower request": {
			ClientScopeMode: "ignore",
			ClientScopes:    []string{referenceReadScope, referenceWriteScope},
		},
		"the deployment refuses to narrow": {ClientScopeMode: "reject"},
	} {
		t.Run(name, func(t *testing.T) {
			deployment := deployInline(t, options, inlineClientSecret)

			code := deployment.status(t)

			if code != exitAuthPolicy {
				t.Fatalf("exit = %d, want the authentication class %d\nstderr:\n%s",
					code, exitAuthPolicy, deployment.errOut)
			}
			if !strings.Contains(deployment.errOut.String(), "auth.narrowing_unavailable") {
				t.Errorf("stderr does not name the refusal:\n%s", deployment.errOut)
			}
			if len(deployment.service.presented()) != 0 {
				t.Error("a refused command still reached the product service")
			}
		})
	}
}

func TestNonInteractiveCIRefusesLoginWhileTheInlinePathStillWorks(t *testing.T) {
	// The whole CI contract, in one environment. A job that declares itself
	// non-interactive gets no browser: an identity that would need one is
	// refused as non-interactive, and an identity that never needed one is
	// refused as not requiring a login at all. The command the job actually
	// runs succeeds in the same environment that refused both logins.
	//
	// Both deployments are built before the variable is set, because deploying
	// clears it — the point is that one environment produces all three answers.
	interactive := deployLogin(t, fakeissuer.Options{}, nil)
	inline := deployInline(t, fakeissuer.Options{}, inlineClientSecret)
	t.Setenv("WSO2_NON_INTERACTIVE", "1")
	for _, deployment := range []*loginDeployment{interactive, inline} {
		deployment.shell.OpenBrowser = func(string) error {
			t.Error("a non-interactive run opened a browser")
			return nil
		}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if code := interactive.shell.Run([]string{"login"}); code != exitAuthPolicy {
			t.Errorf("the interactive wso2 login exited %d, want the authentication class %d",
				code, exitAuthPolicy)
		}
		if code := inline.shell.Run([]string{"login"}); code != exitAuthPolicy {
			t.Errorf("the inline wso2 login exited %d, want the authentication class %d",
				code, exitAuthPolicy)
		}
		if code := inline.status(t); code != exit.OK {
			t.Errorf("reference status exited %d, want %d", code, exit.OK)
		}
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("a non-interactive run waited instead of answering")
	}

	if !strings.Contains(interactive.errOut.String(), "auth.non_interactive") {
		t.Errorf("an interactive identity was not refused as non-interactive:\n%s", interactive.errOut)
	}
	if !strings.Contains(inline.errOut.String(), "auth.login_not_required") {
		t.Errorf("an inline identity was not refused as needing no login:\n%s", inline.errOut)
	}
	presented := inline.service.presented()
	if len(presented) != 1 {
		t.Fatalf("the product service was shown %d bearer tokens, want 1", len(presented))
	}
	if active, _, _ := inline.issuer.Introspect(t, presented[0]); !active {
		t.Error("the module presented a token the issuer did not mint")
	}
}

func TestNoClientSecretReachesAnyOutputSurface(t *testing.T) {
	// The secret is the one thing on the machine worth stealing, and a CI log
	// is the easiest place to lose it. The sweep covers a run that used it and
	// a run that was refused for want of it — the refusal names the variable,
	// which is exactly the message most likely to quote the value by mistake.
	//
	// The module's own environment is not swept here because it is empty by
	// construction: the shell builds the child environment from nothing, which
	// internal/rpc's TestAModuleInheritsNoneOfTheShellsEnvironment pins.
	deployment := deployInline(t, fakeissuer.Options{}, inlineClientSecret)
	if code := deployment.status(t); code != exit.OK {
		t.Fatalf("reference status exited %d\nstderr:\n%s", code, deployment.errOut)
	}
	presented := deployment.service.presented()
	if len(presented) != 1 {
		t.Fatalf("the product service was shown %d bearer tokens, want 1", len(presented))
	}

	refused := deployInline(t, fakeissuer.Options{}, "")
	if code := refused.status(t); code != exitAuthPolicy {
		t.Fatalf("a run with no secret exited %d, want the authentication class %d",
			code, exitAuthPolicy)
	}

	material := map[string]string{
		"the client secret":         inlineClientSecret,
		"the module's access token": presented[0],
	}
	surfaces := map[string]string{
		"the granted run's standard output": deployment.out.String(),
		"the granted run's diagnostics":     deployment.errOut.String(),
		"the refused run's standard output": refused.out.String(),
		"the refused run's diagnostics":     refused.errOut.String(),
	}
	for label, secret := range material {
		if secret == "" {
			t.Fatalf("%s is empty, so this sweep scans for nothing", label)
		}
		for surface, stream := range surfaces {
			if strings.Contains(stream, secret) {
				t.Errorf("%s appeared on %s:\n%s", label, surface, stream)
			}
		}
	}
}
