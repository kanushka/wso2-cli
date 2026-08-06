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

// Package smoke configures the live login runs and the Asgardeo experiments.
//
// The runs themselves are guarded by the `smoke` build tag and never execute in
// the default gate: they open a real browser, sign a real person in, and write
// to the operating system's secure store. What lives here, untagged, is the
// part that can be proven without a deployment — reading the environment that
// names one, and building the context document a run installs. That split is
// deliberate. A misread variable or a document the shell refuses to load are
// the two ways a live run wastes a human's attention, and both are decided
// before any browser opens.
//
// See test/smoke/RUNNING.md for what to export and docs/guides/login.md for how
// to register the application the variables describe.
package smoke

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/modules"
)

// The environment a live run is described by.
const (
	// IssuerVar names the OpenID provider, as its discovery document is
	// published: https://api.asgardeo.io/t/{org}/oauth2/token for Asgardeo,
	// https://localhost:9443/oauth2/token for a local Identity Server 7.x.
	IssuerVar = "WSO2_SMOKE_ISSUER"
	// ClientIDVar names the public client registered against that issuer with
	// the four loopback callbacks and PKCE made mandatory.
	ClientIDVar = "WSO2_SMOKE_CLIENT_ID"
	// AudienceVar names the API resource the brokered access must be bound to.
	AudienceVar = "WSO2_SMOKE_AUDIENCE"
	// ScopeVar lists the permissions the registered application may consent
	// to, separated by whitespace or commas. The narrowing experiment needs at
	// least two.
	ScopeVar = "WSO2_SMOKE_SCOPE"
	// TenantVar names the organization the identity calls home. It is optional:
	// left unset, the smoke context names no organization and the broker's
	// home-tenant check has nothing to disagree with.
	TenantVar = "WSO2_SMOKE_TENANT"
	// EndpointVar overrides the product endpoint recorded on the identity. It
	// is optional and defaults to the issuer's own origin; nothing in this
	// slice calls it, so it exists only to keep the document honest.
	EndpointVar = "WSO2_SMOKE_ENDPOINT"
	// IdentityTypeVar is "cloud" for Asgardeo or "onprem" for an Identity
	// Server deployment. It is optional and defaults to cloud.
	IdentityTypeVar = "WSO2_SMOKE_IDENTITY_TYPE"
	// UnregisteredPortVar is the loopback port the any-port experiment binds:
	// one deliberately outside the registered 10425-10428 range.
	UnregisteredPortVar = "WSO2_SMOKE_UNREGISTERED_PORT"
	// DeadlineVar bounds how long an experiment waits for a human at the
	// browser. It does not reach the smoke run: that one signs in through
	// `wso2 login`, which carries the shell's own five-minute deadline and
	// exposes no way to change it.
	DeadlineVar = "WSO2_SMOKE_DEADLINE"
	// EmpiricalVar opts into the experiments. They are separated from the smoke
	// run because one of them deliberately provokes a rejection, and a reader
	// who did not ask for that should not have to interpret it.
	EmpiricalVar = "WSO2_EMPIRICAL"
)

// The shape of the document a live run installs.
const (
	// Namespace is the module the run brokers access for. The reference module
	// is the only one this repository ships, and the broker's product checks
	// are keyed by namespace.
	Namespace = "reference"
	// ContextName is the smoke context's name.
	ContextName = "smoke"
	// IdentityName is the smoke identity's name.
	IdentityName = "smoke-identity"
	// CredentialRef is the secure-store entry a live login writes to. It is
	// deliberately distinct from anything a developer would choose by hand, so
	// a smoke run cannot overwrite a real session, and a cleanup that deletes
	// it cannot delete one.
	CredentialRef = "wso2-cli-smoke"
)

// Defaults for the knobs a run rarely sets.
const (
	defaultUnregisteredPort = 16000
	defaultDeadline         = 3 * time.Minute
	defaultIdentityType     = "cloud"
)

// ErrNotConfigured reports that no deployment was named, so there is nothing to
// run against. It is the one error a live test skips on: every other error
// means a deployment was named and described wrongly, which is a failure, not
// an absence.
var ErrNotConfigured = errors.New("no live deployment is configured")

// Config is one live deployment, as the environment describes it.
type Config struct {
	// Issuer is the OpenID provider the login runs against.
	Issuer string
	// ClientID is the registered public client.
	ClientID string
	// Audience is the API resource brokered access must be bound to.
	Audience string
	// Scopes are the permissions the identity registers for the product.
	Scopes []string
	// Tenant is the identity's home organization, or empty.
	Tenant string
	// Endpoint is the product endpoint recorded on the identity.
	Endpoint string
	// IdentityType is "cloud" or "onprem".
	IdentityType string
	// UnregisteredPort is the loopback port the any-port experiment binds.
	UnregisteredPort int
	// Deadline bounds a run that is waiting on a human.
	Deadline time.Duration
	// Kind is the authentication kind the installed document declares. It is
	// empty for the browser login every existing run performs, and set to the
	// device kind by the run that proves the device grant against the same
	// deployment. No other variable changes between the two, because no other
	// registration value differs.
	Kind string
}

// Load reads one deployment's description through lookup.
//
// It returns ErrNotConfigured when the run has nothing to run against, and a
// plain error when what it was given cannot describe a deployment. Keeping
// those apart is what stops a malformed value — an issuer that is not a URL, a
// port that is not a number — from being reported as a clean skip, because a
// run that silently passes over a deployment someone believed they had
// configured is worse than one that fails.
//
// The one case it cannot catch is a mistyped variable *name*, which is
// indistinguishable from an absent one and skips. That is why the skip names
// every variable it wanted rather than only saying it found none.
func Load(lookup func(string) (string, bool)) (Config, error) {
	read := func(name string) string {
		value, present := lookup(name)
		if !present {
			return ""
		}
		return strings.TrimSpace(value)
	}

	config := Config{
		Issuer:   read(IssuerVar),
		ClientID: read(ClientIDVar),
		Audience: read(AudienceVar),
		Scopes:   splitList(read(ScopeVar)),
		Tenant:   read(TenantVar),
		Endpoint: read(EndpointVar),
	}

	var missing []string
	for _, required := range []struct {
		name  string
		value string
	}{
		{IssuerVar, config.Issuer},
		{ClientIDVar, config.ClientID},
		{AudienceVar, config.Audience},
		// The parsed list, not the raw string: a value of "," or " , " is not
		// empty and would pass, leaving a run to reach a live login asking for
		// no permissions at all.
		{ScopeVar, strings.Join(config.Scopes, " ")},
	} {
		if required.value == "" {
			missing = append(missing, required.name)
		}
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("%w: set %s (see test/smoke/RUNNING.md)",
			ErrNotConfigured, strings.Join(missing, ", "))
	}

	issuerOrigin, err := origin(config.Issuer)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", IssuerVar, err)
	}
	if config.Endpoint == "" {
		config.Endpoint = issuerOrigin
	} else if _, err := origin(config.Endpoint); err != nil {
		return Config{}, fmt.Errorf("%s: %w", EndpointVar, err)
	}

	config.IdentityType = defaultIdentityType
	if declared := read(IdentityTypeVar); declared != "" {
		if declared != "cloud" && declared != "onprem" {
			return Config{}, fmt.Errorf(
				"%s: %q is not an identity type the context schema accepts; use cloud or onprem",
				IdentityTypeVar, declared)
		}
		config.IdentityType = declared
	}

	config.UnregisteredPort = defaultUnregisteredPort
	if declared := read(UnregisteredPortVar); declared != "" {
		port, err := strconv.Atoi(declared)
		if err != nil || port < 1 || port > 65535 {
			return Config{}, fmt.Errorf("%s: %q is not a port number", UnregisteredPortVar, declared)
		}
		if slices.Contains(RegisteredPorts(), port) {
			return Config{}, fmt.Errorf(
				"%s: %d is one of the registered callback ports, so binding it would prove nothing",
				UnregisteredPortVar, port)
		}
		config.UnregisteredPort = port
	}

	config.Deadline = defaultDeadline
	if declared := read(DeadlineVar); declared != "" {
		deadline, err := time.ParseDuration(declared)
		if err != nil || deadline <= 0 {
			return Config{}, fmt.Errorf("%s: %q is not a positive duration", DeadlineVar, declared)
		}
		config.Deadline = deadline
	}

	return config, nil
}

// Empirical reports whether the experiments were asked for.
func Empirical(lookup func(string) (string, bool)) bool {
	value, present := lookup(EmpiricalVar)
	return present && strings.TrimSpace(value) != ""
}

// RegisteredPorts are the callback ports the walkthrough registers, and the
// ones oauthflow binds by default.
func RegisteredPorts() []int { return []int{10425, 10426, 10427, 10428} }

// Document is the schema version 2 context document a live run installs.
//
// It is built rather than hand-written so that a run cannot drift from the
// shape the shell reads, and it is exercised by this package's own tests so
// that a run cannot fail on a document defect in front of a waiting human.
func (c Config) Document() contexts.Document {
	kind := c.Kind
	if kind == "" {
		kind = contexts.KindOAuthBrowser
	}
	return contexts.Document{
		SchemaVersion:  contexts.SchemaVersion,
		DefaultContext: ContextName,
		Identities: []contexts.Identity{{
			Name: IdentityName,
			Type: c.IdentityType,
			Auth: contexts.IdentityAuth{
				Kind:          kind,
				Issuer:        c.Issuer,
				ClientID:      c.ClientID,
				Tenant:        c.Tenant,
				CredentialRef: CredentialRef,
			},
			Products: map[string]contexts.Product{
				Namespace: {
					Endpoint: c.Endpoint,
					Audience: c.Audience,
					Scopes:   slices.Clone(c.Scopes),
				},
			},
		}},
		Contexts: []contexts.Context{{
			Name:     ContextName,
			Identity: IdentityName,
			// The context stays in the identity's home tenant. Naming any other
			// organization would provoke auth.organization_switch_unsupported,
			// which is a refusal about the document rather than the deployment.
			Organization: c.Tenant,
		}},
	}
}

// Capabilities are the receipt a module installed for this run would carry.
//
// The broker checks a request against the receipt before it checks anything
// else, so a live run declares exactly the audience and permissions it intends
// to ask for and nothing wider.
func (c Config) Capabilities() modules.Capabilities {
	return modules.Capabilities{
		AuthAudiences: []string{c.Audience},
		AuthScopes:    slices.Clone(c.Scopes),
	}
}

// NarrowTarget is the single permission the narrowing experiment asks for after
// logging in with all of them.
//
// It refuses when only one permission is configured, because narrowing a
// one-permission session to that same permission is indistinguishable from an
// issuer that ignored the request entirely — and a verdict that cannot tell
// those apart is not evidence.
func (c Config) NarrowTarget() (string, error) {
	if len(c.Scopes) < 2 {
		return "", fmt.Errorf(
			"the narrowing experiment needs at least two permissions in %s, and %d was configured",
			ScopeVar, len(c.Scopes))
	}
	return c.Scopes[0], nil
}

// splitList reads a permission list written with either separator.
func splitList(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	var list []string
	for _, field := range fields {
		if field != "" && !slices.Contains(list, field) {
			list = append(list, field)
		}
	}
	return list
}

// origin reduces an absolute HTTP URL to its scheme and host, and refuses
// anything that is not one.
func origin(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("%q is not a URL: %w", value, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("%q does not name an http or https URL", value)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("%q names no host", value)
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}
