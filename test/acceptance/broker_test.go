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

// deployment is one isolated reference installation: the module, the context,
// and the local status service it targets.
type deployment struct {
	stateRoot string
	service   *httptest.Server
	// calls counts the requests that reached the status service.
	calls *atomic.Int64
}

// deploy installs the reference module, starts the local status service, and
// writes the context that points one at the other.
func deploy(t *testing.T, options statusservice.Options) deployment {
	t.Helper()
	stateRoot := isolatedStateRoot(t)
	installReferenceModule(t, stateRoot, buildReferenceModule(t))
	service := startStatusService(t, options)
	installReferenceContext(t, stateRoot, service.server.URL, credentialVariable)
	return deployment{stateRoot: stateRoot, service: service.server, calls: service.calls}
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
	if options.SourceCredential == "" {
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
	// and the shell renders what came back.
	shell := buildShell(t)
	deployed := deploy(t, statusservice.Options{})

	stdout, stderr := runShell(t, shell, deployed.stateRoot, "reference", "status")

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
}

func TestBrokeredReferenceStatusRendersTheServicesAnswerAsJSON(t *testing.T) {
	shell := buildShell(t)
	deployed := deploy(t, statusservice.Options{})

	stdout, stderr := runShell(t, shell, deployed.stateRoot, "reference", "status", "--output", "json")

	decoded := decodeStatusJSON(t, stdout)
	if decoded["organization"] != referenceOrganization {
		t.Errorf("the JSON result reports organization %q, want %q",
			decoded["organization"], referenceOrganization)
	}
	if decoded["status"] != "operational" {
		t.Errorf("the JSON result reports status %q, want %q", decoded["status"], "operational")
	}
	assertNoCredentialDisclosure(t, stdout, stderr)
}

func TestAMissingCredentialIsDeniedWithSafeRecoveryGuidance(t *testing.T) {
	// The context names a credential source that is not set. The shell refuses
	// the module's request rather than launching an unauthenticated call, and
	// tells the user what to set.
	shell := buildShell(t)
	deployed := deploy(t, statusservice.Options{})
	installReferenceContext(t, deployed.stateRoot, deployed.service.URL, "WSO2_REFERENCE_ABSENT_CREDENTIAL")

	stdout, stderr, err := tryShell(shell, deployed.stateRoot, "reference", "status")

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

	stdout, stderr, err := tryShell(shell, stateRoot, "reference", "status")

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
	// the receipt would allow.
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

	stdout, stderr, err := tryShell(shell, stateRoot, "reference", "status")

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
	// access the shell granted for this command is no longer accepted.
	shell := buildShell(t)
	expired := deploy(t, statusservice.Options{
		Now: func() time.Time { return time.Now().Add(time.Hour) },
	})

	stdout, stderr, err := tryShell(shell, expired.stateRoot, "reference", "status")

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

func TestAFailingServiceAndADeniedRequestEndInDifferentExitClasses(t *testing.T) {
	shell := buildShell(t)
	faulty := deploy(t, statusservice.Options{Fault: true})

	stdout, stderr, err := tryShell(shell, faulty.stateRoot, "reference", "status")

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
}

func TestAServiceThatRejectsTheAccessClaimsIsReported(t *testing.T) {
	// The service serves another organization, so the token the shell minted
	// for this context is refused. The command fails as a service answer, not
	// as shell policy: the shell's own decision was to grant.
	shell := buildShell(t)
	foreign := deploy(t, statusservice.Options{Organization: "another-organization"})

	stdout, stderr, err := tryShell(shell, foreign.stateRoot, "reference", "status")

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
	// it to find.
	shell := buildShell(t)
	stateRoot := isolatedStateRoot(t)
	installNoisyModule(t, stateRoot)
	writeControlFile(t, stateRoot, "report-environment", "")
	service := startStatusService(t, statusservice.Options{})
	installReferenceContext(t, stateRoot, service.server.URL, credentialVariable)

	stdout, stderr := runShell(t, shell, stateRoot, "reference", "status")

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
}

// assertNoCredentialDisclosure proves a run said nothing about the credential
// the shell holds.
func assertNoCredentialDisclosure(t *testing.T, streams ...string) {
	t.Helper()
	for _, stream := range streams {
		if strings.Contains(stream, canaryCredential) {
			t.Fatalf("the source credential was disclosed:\n%s", stream)
		}
	}
}
