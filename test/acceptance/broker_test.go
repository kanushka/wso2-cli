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
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/contexts"
	contextfixture "github.com/wso2/wso2-cli/internal/contexts/fixture"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
	"github.com/wso2/wso2-cli/internal/state"
	"github.com/wso2/wso2-cli/internal/statusservice"
)

// The isolated reference deployment every brokered run is exercised against.
const (
	// canaryCredential is the development credential the harness supplies. No
	// run may disclose it anywhere a user or a log could see it.
	canaryCredential = "canary-source-credential-2f8c-do-not-disclose"
	// credentialVariable is the environment variable the context names. Only
	// the shell ever reads it.
	credentialVariable    = "WSO2_REFERENCE_DEV_CREDENTIAL"
	referenceContextName  = "reference-local"
	referenceOrganization = "reference-org"
	referenceAudience     = "reference-status"
	referenceReadScope    = "reference:status:read"
)

// The exit classes the shell owns. A broker denial and a product-service
// failure are deliberately different numbers: automation must be able to tell
// "you may not" from "it is broken" without reading text.
const (
	exitAuthPolicy     = 77
	exitProductService = 75
)

// The client-credentials identity the issuer-minted arm authenticates as. It
// mirrors login_test.go's inline deployment, which proves the same identity
// kind against an in-process shell; here it runs the built binary instead.
const (
	oauthIdentityName   = "reference-machine"
	oauthClientID       = "wso2cli-reference"
	oauthSecretVariable = "WSO2_REFERENCE_CLIENT_SECRET"
	// oauthClientSecret is a second canary. The issuer holds the client to it,
	// so a run that succeeds could only have read it from the variable, and no
	// surface the shell writes may contain it.
	oauthClientSecret = "canary-reference-client-secret-4b71"
)

// credentialKind is how a deployment's shell obtains the access it hands the
// module.
type credentialKind int

const (
	// developmentCredential is the architecture proof's fixture: a shared
	// secret the shell signs a token with and the service verifies with.
	developmentCredential credentialKind = iota
	// issuerMinted is a client-credentials identity whose access tokens a real
	// OpenID issuer signs and the service verifies against published keys.
	issuerMinted
)

func (k credentialKind) String() string {
	if k == issuerMinted {
		return "the token is minted by an issuer"
	}
	return "the token is minted from the development credential"
}

// bothCredentialKinds are the two ways a deployment obtains access. Tests whose
// subject is the audience boundary run under both, because a boundary that
// holds only for the fixture credential is not a boundary.
var bothCredentialKinds = []credentialKind{developmentCredential, issuerMinted}

// installation is what varies between one deployed reference installation and
// another: how the shell obtains access, what the service enforces, and how the
// issuer behaves when there is one.
type installation struct {
	kind    credentialKind
	service statusservice.Options
	issuer  fakeissuer.Options
	// serviceIssuer overrides the issuer the service trusts, so a test can
	// point the shell at one deployment and its audience at another. Only the
	// organization-binding test sets it.
	serviceIssuer string
}

// deployment is one isolated reference installation: the module, the context,
// and the local status service it targets.
type deployment struct {
	stateRoot string
	service   *httptest.Server
	// calls counts the requests that reached the status service.
	calls *atomic.Int64
	// environment is what the shell is run with. It carries whichever
	// credential source this deployment's identity names.
	environment []string
}

// deploy installs the development-credential arm, which is what most tests
// want.
func deploy(t *testing.T, options statusservice.Options) deployment {
	t.Helper()
	return deployAs(t, installation{service: options})
}

// deployAs installs the reference module, starts the local status service, and
// writes the context that points one at the other, for either credential kind.
func deployAs(t *testing.T, install installation) deployment {
	t.Helper()
	stateRoot := isolatedStateRoot(t)
	installReferenceModule(t, stateRoot, buildReferenceModule(t))

	if install.kind == developmentCredential {
		return deployInstalled(t, stateRoot, install.service)
	}

	if install.issuer.Audience == "" {
		install.issuer.Audience = referenceAudience
	}
	// The issuer holds the client to a secret, so a granted run proves the
	// shell read the variable rather than that the fixture is permissive.
	install.issuer.ClientSecret = oauthClientSecret
	issuer := fakeissuer.New(t, install.issuer)

	// The service trusts the deployment's own issuer unless a test points it
	// somewhere else, and it verifies signatures instead of holding a shared
	// secret — so it must be told to stop expecting one.
	install.service.Issuer = issuer.URL
	if install.serviceIssuer != "" {
		install.service.Issuer = install.serviceIssuer
	}
	// HTTPClient bounds the discovery New performs at construction, so a
	// deployment that cannot answer fails the test rather than hanging it.
	install.service.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	service := startStatusService(t, install.service)
	installOAuthContext(t, stateRoot, issuer.URL, service.server.URL)

	return deployment{
		stateRoot:   stateRoot,
		service:     service.server,
		calls:       service.calls,
		environment: shellEnvironment(stateRoot, oauthSecretVariable+"="+oauthClientSecret),
	}
}

// deployInstalled starts the status service and writes the development-credential
// context for a state root whose reference module is already installed, so a
// test that installs its own build of the module still deploys it the one way.
func deployInstalled(t *testing.T, stateRoot string, options statusservice.Options) deployment {
	t.Helper()
	service := startStatusService(t, options)
	installReferenceContext(t, stateRoot, service.server.URL, credentialVariable)
	return deployment{
		stateRoot:   stateRoot,
		service:     service.server,
		calls:       service.calls,
		environment: shellEnvironment(stateRoot),
	}
}

// installOAuthContext writes a schema version 2 document whose identity
// authenticates non-interactively against the given issuer.
//
// Client credentials rather than a browser login, because this package runs the
// shell as a built subprocess: a browser identity keeps its session in the OS
// secure store, and go-keyring's mock reaches only the process that installs
// it. What this arm gives up is the login step, which login_test.go proves; what
// it keeps is the shell's own process boundary, which login_test.go cannot.
func installOAuthContext(t *testing.T, stateRoot, issuerURL, endpoint string) {
	t.Helper()
	if err := contextfixture.WriteV2(stateRoot, contexts.Document{
		SchemaVersion:  contexts.SchemaVersion,
		DefaultContext: referenceContextName,
		Identities: []contexts.Identity{{
			Name: oauthIdentityName,
			Type: "cloud",
			Auth: contexts.IdentityAuth{
				Kind:                 contexts.KindClientCredentials,
				Issuer:               issuerURL,
				ClientID:             oauthClientID,
				Tenant:               referenceOrganization,
				ClientSecretVariable: oauthSecretVariable,
			},
			Products: map[string]contexts.Product{
				"reference": {
					Endpoint: endpoint,
					Audience: referenceAudience,
					Scopes:   []string{referenceReadScope},
				},
			},
		}},
		Contexts: []contexts.Context{{
			Name:         referenceContextName,
			Identity:     oauthIdentityName,
			Organization: referenceOrganization,
		}},
	}); err != nil {
		t.Fatalf("installing the v2 context document: %v", err)
	}
}

// run executes one shell command in this deployment's environment and requires
// it to succeed.
func (d deployment) run(t *testing.T, shell string, args ...string) (string, string) {
	t.Helper()
	stdout, stderr, err := runShellWith(shell, d.environment, args...)
	if err != nil {
		t.Fatalf("wso2 %s failed: %v\nstdout:\n%s\nstderr:\n%s",
			strings.Join(args, " "), err, stdout, stderr)
	}
	return stdout, stderr
}

// try executes one shell command in this deployment's environment and returns
// the exit error for the caller to classify.
func (d deployment) try(shell string, args ...string) (string, string, error) {
	return runShellWith(shell, d.environment, args...)
}

// recordedService is a running status service and its call counter.
type recordedService struct {
	server *httptest.Server
	calls  *atomic.Int64
}

// startStatusService runs the local read-only status service for this test.
func startStatusService(t *testing.T, options statusservice.Options) recordedService {
	t.Helper()
	if options.Audience == "" {
		options.Audience = referenceAudience
	}
	if options.RequiredScope == "" {
		options.RequiredScope = referenceReadScope
	}
	if options.Organization == "" {
		options.Organization = referenceOrganization
	}
	if options.SourceCredential == "" && options.Issuer == "" {
		options.SourceCredential = canaryCredential
	}
	service, err := statusservice.New(options)
	if err != nil {
		t.Fatalf("statusservice.New returned %v", err)
	}

	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		service.ServeHTTP(writer, request)
	}))
	t.Cleanup(server.Close)
	return recordedService{server: server, calls: &calls}
}

// installReferenceContext writes the isolated context the shell runs against.
func installReferenceContext(t *testing.T, stateRoot, endpoint, credentialSource string) {
	t.Helper()
	if err := contextfixture.Install(stateRoot, contextfixture.LegacyDocument{
		SchemaVersion:  contexts.SchemaVersionLegacy,
		DefaultContext: referenceContextName,
		Contexts: []contextfixture.LegacyContext{{
			Name:           referenceContextName,
			OrganizationID: referenceOrganization,
			Endpoint:       endpoint,
			Auth: contextfixture.LegacyAuth{
				Method:             contexts.MethodDevelopmentCredential,
				CredentialVariable: credentialSource,
			},
		}},
	}); err != nil {
		t.Fatalf("installing the reference context: %v", err)
	}
}

func TestBrokeredReferenceStatusReportsTheServicesOwnAnswer(t *testing.T) {
	// The whole boundary in one run: the shell resolves and launches the
	// module, the module asks for access, the shell brokers a token from a
	// credential the module never sees, the service accepts only that token,
	// and the shell renders what came back. Both kinds prove it, because
	// nothing about this boundary is specific to how the token was minted.
	for _, kind := range bothCredentialKinds {
		t.Run(kind.String(), func(t *testing.T) {
			shell := buildShell(t)
			deployed := deployAs(t, installation{kind: kind})

			stdout, stderr := deployed.run(t, shell, "reference", "call")

			if deployed.calls.Load() != 1 {
				t.Fatalf("the status service was called %d times, want once", deployed.calls.Load())
			}
			for _, want := range []string{referenceOrganization, "operational"} {
				if !strings.Contains(stdout, want) {
					t.Errorf("the table does not report %q:\n%s", want, stdout)
				}
			}
			if stderr != "" {
				t.Errorf("a successful command wrote diagnostics:\n%s", stderr)
			}
			assertNoCredentialDisclosure(t, stdout, stderr)
		})
	}
}

func TestBrokeredReferenceStatusRendersTheServicesAnswerAsJSON(t *testing.T) {
	// The same brokered answer, read back as the machine-readable rendering
	// instead of the table. Both kinds, for the same reason as above: the
	// rendering is downstream of the same brokered token.
	for _, kind := range bothCredentialKinds {
		t.Run(kind.String(), func(t *testing.T) {
			shell := buildShell(t)
			deployed := deployAs(t, installation{kind: kind})

			stdout, stderr := deployed.run(t, shell, "reference", "call", "--output", "json")

			decoded := decodeStatusJSON(t, stdout)
			if decoded["organization"] != referenceOrganization {
				t.Errorf("the JSON result reports organization %q, want %q",
					decoded["organization"], referenceOrganization)
			}
			if decoded["status"] != "operational" {
				t.Errorf("the JSON result reports status %q, want %q", decoded["status"], "operational")
			}
			assertNoCredentialDisclosure(t, stdout, stderr)
		})
	}
}

func TestAMissingCredentialIsDeniedWithSafeRecoveryGuidance(t *testing.T) {
	// The context names a credential source that is not set. The shell refuses
	// the module's request rather than launching an unauthenticated call, and
	// tells the user what to set.
	//
	// The development kind only. The issuer-minted equivalents are
	// TestAnInlineIdentityWithNoSecretTellsTheUserWhichVariableToSet in
	// login_test.go and the organization-binding tests below, which refuse for
	// reasons this kind has no counterpart for.
	shell := buildShell(t)
	deployed := deploy(t, statusservice.Options{})
	installReferenceContext(t, deployed.stateRoot, deployed.service.URL, "WSO2_REFERENCE_ABSENT_CREDENTIAL")

	stdout, stderr, err := tryShell(shell, deployed.stateRoot, "reference", "call")

	if exitCode(t, err) != exitAuthPolicy {
		t.Fatalf("exit status = %v, want the authentication class %d\nstderr:\n%s", err, exitAuthPolicy, stderr)
	}
	if !strings.Contains(stderr, "auth.credential_unavailable") {
		t.Errorf("stderr does not name the broker denial:\n%s", stderr)
	}
	if !strings.Contains(stderr, "WSO2_REFERENCE_ABSENT_CREDENTIAL") {
		t.Errorf("stderr does not tell the user what to set:\n%s", stderr)
	}
	if deployed.calls.Load() != 0 {
		t.Errorf("a denied command still called the status service %d times", deployed.calls.Load())
	}
	if stdout != "" {
		t.Errorf("a denied command still wrote to standard output:\n%s", stdout)
	}
	assertNoCredentialDisclosure(t, stdout, stderr)
}

func TestAnUndeclaredAudienceIsDenied(t *testing.T) {
	// The module receipt is the ceiling on what a module may ask for. An
	// installation that declares no audience cannot acquire one at runtime.
	//
	// One kind only: the receipt is checked before any source is consulted, so
	// a second pass would prove the same refusal twice and say nothing about
	// where the token would have come from.
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	if _, err := fixture.Install(state.ModuleStore(stateRoot), fixture.Module{
		Namespace:        "reference",
		Version:          testModuleVersion,
		ShellRange:       ">=0.1.0 <1.0.0",
		ProtocolVersions: []int{testProtocolVersionNumber},
		SourcePath:       buildReferenceModule(t),
		// No declared audience or scope.
	}); err != nil {
		t.Fatalf("fixture.Install returned %v", err)
	}
	service := startStatusService(t, statusservice.Options{})
	installReferenceContext(t, stateRoot, service.server.URL, credentialVariable)

	stdout, stderr, err := tryShell(shell, stateRoot, "reference", "call")

	if exitCode(t, err) != exitAuthPolicy {
		t.Fatalf("exit status = %v, want the authentication class %d\nstderr:\n%s", err, exitAuthPolicy, stderr)
	}
	if !strings.Contains(stderr, "auth.audience_not_declared") {
		t.Errorf("stderr does not report the undeclared audience:\n%s", stderr)
	}
	if service.calls.Load() != 0 {
		t.Errorf("a denied command still called the status service %d times", service.calls.Load())
	}
	assertNoCredentialDisclosure(t, stdout, stderr)
}

func TestAnExcessiveScopeIsDenied(t *testing.T) {
	// The installation declares the audience but not the permission the module
	// asks for, so the broker refuses rather than granting the narrower access
	// the receipt would allow. One kind only, for the same reason as the
	// undeclared audience above: the receipt is checked before any source is
	// consulted.
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	if _, err := fixture.Install(state.ModuleStore(stateRoot), fixture.Module{
		Namespace:        "reference",
		Version:          testModuleVersion,
		ShellRange:       ">=0.1.0 <1.0.0",
		ProtocolVersions: []int{testProtocolVersionNumber},
		SourcePath:       buildReferenceModule(t),
		AuthAudiences:    []string{referenceAudience},
		AuthScopes:       []string{"reference:status:write"},
	}); err != nil {
		t.Fatalf("fixture.Install returned %v", err)
	}
	service := startStatusService(t, statusservice.Options{})
	installReferenceContext(t, stateRoot, service.server.URL, credentialVariable)

	stdout, stderr, err := tryShell(shell, stateRoot, "reference", "call")

	if exitCode(t, err) != exitAuthPolicy {
		t.Fatalf("exit status = %v, want the authentication class %d\nstderr:\n%s", err, exitAuthPolicy, stderr)
	}
	if !strings.Contains(stderr, "auth.scope_not_declared") {
		t.Errorf("stderr does not report the undeclared permission:\n%s", stderr)
	}
	if service.calls.Load() != 0 {
		t.Errorf("a denied command still called the status service %d times", service.calls.Load())
	}
	assertNoCredentialDisclosure(t, stdout, stderr)
}

func TestExpiredAccessIsRefusedByTheService(t *testing.T) {
	// The service reads a clock well past the token's near-term expiry, so the
	// access the shell granted for this command is no longer accepted. Under
	// the issuer-minted kind this is a real JWT expiry check against exp, not a
	// fixture's own lifetime rule.
	for _, kind := range bothCredentialKinds {
		t.Run(kind.String(), func(t *testing.T) {
			shell := buildShell(t)
			expired := deployAs(t, installation{
				kind: kind,
				service: statusservice.Options{
					Now: func() time.Time { return time.Now().Add(time.Hour) },
				},
			})

			stdout, stderr, err := expired.try(shell, "reference", "call")

			if exitCode(t, err) != exitProductService {
				t.Fatalf("exit status = %v, want the product-service class %d\nstderr:\n%s",
					err, exitProductService, stderr)
			}
			if !strings.Contains(stderr, "reference.status_access_rejected") {
				t.Errorf("stderr does not report the refused access:\n%s", stderr)
			}
			if stdout != "" {
				t.Errorf("a refused command still wrote to standard output:\n%s", stdout)
			}
			assertNoCredentialDisclosure(t, stdout, stderr)
		})
	}
}

func TestAFailingServiceAndADeniedRequestEndInDifferentExitClasses(t *testing.T) {
	// A faulty service and a denied request must not be confused with each
	// other under either credential kind: the shell already granted the
	// access, so what fails here is the product service, not the broker.
	for _, kind := range bothCredentialKinds {
		t.Run(kind.String(), func(t *testing.T) {
			shell := buildShell(t)
			faulty := deployAs(t, installation{kind: kind, service: statusservice.Options{Fault: true}})

			stdout, stderr, err := faulty.try(shell, "reference", "call")

			if exitCode(t, err) != exitProductService {
				t.Fatalf("exit status = %v, want the product-service class %d\nstderr:\n%s",
					err, exitProductService, stderr)
			}
			if !strings.Contains(stderr, "reference.status_unavailable") {
				t.Errorf("stderr does not report the service failure:\n%s", stderr)
			}
			if strings.Contains(stderr, "auth.") {
				t.Errorf("a service failure was reported as an access failure:\n%s", stderr)
			}
			if stdout != "" {
				t.Errorf("a failed command still wrote to standard output:\n%s", stdout)
			}
			assertNoCredentialDisclosure(t, stdout, stderr)
		})
	}
}

func TestAServiceThatRejectsTheAccessClaimsIsReported(t *testing.T) {
	// The service serves another organization, so the token the shell minted
	// for this context is refused. The command fails as a service answer, not
	// as shell policy: the shell's own decision was to grant.
	//
	// The development kind only, because this refusal has no issuer-minted
	// counterpart to parameterize over. A service configured for another
	// organization would accept an issuer-minted token, which carries no
	// org_id: on that arm the issuer is what binds the organization, and the
	// organization-binding tests below are where that binding is proved.
	shell := buildShell(t)
	foreign := deploy(t, statusservice.Options{Organization: "another-organization"})

	stdout, stderr, err := tryShell(shell, foreign.stateRoot, "reference", "call")

	if exitCode(t, err) != exitProductService {
		t.Fatalf("exit status = %v, want the product-service class %d\nstderr:\n%s",
			err, exitProductService, stderr)
	}
	if !strings.Contains(stderr, "reference.status_access_rejected") {
		t.Errorf("stderr does not report the refused claims:\n%s", stderr)
	}
	assertNoCredentialDisclosure(t, stdout, stderr)
}

func TestTheModuleEnvironmentCarriesNoAmbientCredential(t *testing.T) {
	// The shell reads the credential source from its own environment. The
	// module is launched with nothing at all, so there is no ambient value for
	// it to find. Both kinds, because the ambient leak this test rules out
	// could as easily be the OAuth client secret as the development
	// credential — the sweep below already rejects any WSO2_-prefixed name, so
	// it catches WSO2_REFERENCE_CLIENT_SECRET without change.
	for _, kind := range bothCredentialKinds {
		t.Run(kind.String(), func(t *testing.T) {
			shell := buildShell(t)
			deployed := deployAs(t, installation{kind: kind})
			installNoisyModule(t, deployed.stateRoot)
			writeControlFile(t, deployed.stateRoot, "report-environment", "")

			stdout, stderr := deployed.run(t, shell, "reference", "status")

			const prefix = "module-environment: "
			if !strings.Contains(stderr, prefix) {
				t.Fatalf("the module did not report the environment it was launched with:\n%s", stderr)
			}
			for _, line := range strings.Split(stderr, "\n") {
				reported, found := strings.CutPrefix(strings.TrimSpace(line), prefix)
				if !found || strings.HasPrefix(reported, "count=") {
					continue
				}
				name, _, _ := strings.Cut(reported, "=")
				if strings.HasPrefix(name, "WSO2_") || name == state.RootEnvVar {
					t.Errorf("the module was launched with the shell's %q", name)
				}
			}
			assertNoCredentialDisclosure(t, stdout, stderr)
		})
	}
}

func TestTheModulesIssuerMintedAccessIsAcceptedByAVerifyingService(t *testing.T) {
	// The claim this whole slice exists to make. The shell obtains an access
	// token from a real issuer, the module presents it, and the service accepts
	// it only after verifying the issuer's signature against the keys that
	// issuer publishes — so nothing here turns on a secret the test planted on
	// both sides.
	shell := buildShell(t)
	deployed := deployAs(t, installation{kind: issuerMinted})

	stdout, stderr := deployed.run(t, shell, "reference", "call")

	if deployed.calls.Load() != 1 {
		t.Fatalf("the status service was called %d times, want once", deployed.calls.Load())
	}
	for _, want := range []string{referenceOrganization, "operational"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the table does not report %q:\n%s", want, stdout)
		}
	}
	if stderr != "" {
		t.Errorf("a successful command wrote diagnostics:\n%s", stderr)
	}
	assertNoCredentialDisclosure(t, stdout, stderr)
	if strings.Contains(stdout+stderr, oauthClientSecret) {
		t.Error("the client secret was disclosed")
	}
}

func TestAccessFromAnotherOrganizationsIssuerIsRefused(t *testing.T) {
	// A deployment that mints no organization claim — Asgardeo's default —
	// binds a token to one organization through its issuer and nothing else.
	// So the service is pointed at an issuer that is not the one the shell
	// authenticates against, and the perfectly valid, correctly scoped,
	// correctly audienced token the shell obtains is refused anyway.
	//
	// This is the case a rule that only checked a claim when the token carried
	// one would accept, which is why it is here.
	//
	// The two fakeissuer.New calls below each generate their own signing key,
	// so what actually refuses this token is signature verification against
	// the wrong issuer's published keys, not the iss claim comparison — this
	// proves the end-to-end refusal but not which guard inside it did the
	// refusing. That guard, in isolation, is "the token names another issuer"
	// in TestAnIssuerMintedTokenIsRefusedWhenItsClaimsAreNotServed in
	// internal/statusservice/jwks_test.go, which signs with the service's own
	// trusted key and varies only iss.
	shell := buildShell(t)
	foreign := fakeissuer.New(t, fakeissuer.Options{Audience: referenceAudience})
	deployed := deployAs(t, installation{kind: issuerMinted, serviceIssuer: foreign.URL})

	stdout, stderr, err := deployed.try(shell, "reference", "call")

	if exitCode(t, err) != exitProductService {
		t.Fatalf("exit status = %v, want the product-service class %d\nstderr:\n%s",
			err, exitProductService, stderr)
	}
	if !strings.Contains(stderr, "reference.status_access_rejected") {
		t.Errorf("stderr does not report the refused access:\n%s", stderr)
	}
	if stdout != "" {
		t.Errorf("a refused command still wrote to standard output:\n%s", stdout)
	}
	assertNoCredentialDisclosure(t, stdout, stderr)
}

func TestAccessNamingAnotherOrganizationIsRefused(t *testing.T) {
	// The sub-organization case: one issuer serving many organizations states
	// which one a token is for, and this service serves a different one.
	shell := buildShell(t)
	deployed := deployAs(t, installation{
		kind:   issuerMinted,
		issuer: fakeissuer.Options{OrganizationClaim: "another-organization"},
	})

	stdout, stderr, err := deployed.try(shell, "reference", "call")

	if exitCode(t, err) != exitProductService {
		t.Fatalf("exit status = %v, want the product-service class %d\nstderr:\n%s",
			err, exitProductService, stderr)
	}
	if !strings.Contains(stderr, "reference.status_access_rejected") {
		t.Errorf("stderr does not report the refused access:\n%s", stderr)
	}
	if stdout != "" {
		t.Errorf("a refused command still wrote to standard output:\n%s", stdout)
	}
	assertNoCredentialDisclosure(t, stdout, stderr)
}

func TestAccessNamingThisOrganizationIsAccepted(t *testing.T) {
	// The third shape a deployment can take: an issuer that does mint the
	// claim, for the organization this service serves. It is the only one of
	// the three where the claim itself admits the token, and it has to keep
	// working alongside the two refusals above.
	//
	// Read alone, this test cannot tell "the claim was checked and matched"
	// from "the claim was ignored": authorize's guard is false both for an
	// absent org_id and for one equal to this service's organization, so its
	// observables are the same as the no-claim happy path in
	// TestTheModulesIssuerMintedAccessIsAcceptedByAVerifyingService. What
	// makes this test evidence is standing next to
	// TestAccessNamingAnotherOrganizationIsRefused: same deployment shape, one
	// claim value apart, opposite outcomes. It is that pairing, not this test
	// by itself, that shows the claim is load-bearing.
	shell := buildShell(t)
	deployed := deployAs(t, installation{
		kind:   issuerMinted,
		issuer: fakeissuer.Options{OrganizationClaim: referenceOrganization},
	})

	stdout, stderr := deployed.run(t, shell, "reference", "call")

	if !strings.Contains(stdout, "operational") {
		t.Errorf("the table does not report the service's answer:\n%s", stdout)
	}
	if deployed.calls.Load() != 1 {
		t.Errorf("the status service was called %d times, want once", deployed.calls.Load())
	}
	assertNoCredentialDisclosure(t, stdout, stderr)
}

// assertNoCredentialDisclosure proves a run said nothing about either canary
// credential the shell might hold: the development credential and the OAuth
// client secret.
func assertNoCredentialDisclosure(t *testing.T, streams ...string) {
	t.Helper()
	for _, stream := range streams {
		if strings.Contains(stream, canaryCredential) {
			t.Fatalf("the source credential was disclosed:\n%s", stream)
		}
		if strings.Contains(stream, oauthClientSecret) {
			t.Fatalf("the OAuth client secret was disclosed:\n%s", stream)
		}
	}
}
