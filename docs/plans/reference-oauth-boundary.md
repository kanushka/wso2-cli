# Reference Module OAuth Boundary Implementation Plan

**Status:** Implemented. Tasks 1-7 are delivered on
`test/reference-oauth-boundary` and opened as PR #48, with the definition of
done met. Two corrections outran the plan and are recorded against the tasks
that carried them: the signing-algorithm guard does not do what Task 2's
commentary claimed, and Task 1's zero-value organization rule weakened the
fixture path until the final review caught it.

**Date:** 2026-08-08

**Outcome:** The reference audience verifies issuer-minted access

**Related:** [Issue #47](https://github.com/wso2/wso2-cli/issues/47) is the
spec — read it before starting any task — with
[architecture](../architecture.md),
[ADR 0004](../adr/0004-shell-brokered-authentication.md) and
[ADR 0005](../adr/0005-audience-side-verification.md), which this work
established.

Each task below ends in a green test and a commit. Steps use checkbox
(`- [ ]`) syntax so progress is visible in the file itself.

**Goal:** Give the reference status service a second way to establish that a
token is genuine — verifying an OpenID issuer's RS256 signature against the keys
it publishes — so the acceptance suite's boundary assertions hold for a real
OAuth access token and not only for the development fixture credential.

**Architecture:** `internal/statusservice` keeps one `authorize` and gains a
`verifier` seam beneath it. `devtokenVerifier` is today's shared-secret path;
`jwksVerifier` verifies an issuer-signed JWT through `github.com/coreos/go-oidc/v3`.
Both answer in one normalized `access` shape, so `authorize` never learns which
format it was handed. The acceptance harness then grows a second arm: a schema
version 2 client-credentials identity against `internal/auth/fakeissuer`, which
needs no keyring and therefore runs the shell as a real subprocess.

**Tech Stack:** Go 1.25, `github.com/coreos/go-oidc/v3` v3.20.0 (already a direct
dependency, used by `internal/auth/discovery.go`), `github.com/go-jose/go-jose/v4`
v4.1.4 (test JWT signing only).

**Branch:** `test/reference-oauth-boundary`, cut from `main` at ea083c2.

## Global Constraints

- Every new `.go` file opens with the repository's Apache 2.0 header. Copy it
  verbatim from any existing file, with the year `2026`.
- Comments explain *why*, in complete sentences, in the register the
  surrounding files use. A comment that restates the code is worse than none.
  Read `internal/statusservice/statusservice.go` before writing any.
- Every failure a user can reach is a typed problem with a stable code and an
  exit class. This plan adds none: the reference service's refusal codes are
  internal to the fixture, and the module already maps 401 and 403 onto
  `reference.status_access_rejected` (exit 75).
- Conventional commit subjects. Commits are GPG-signed; the repository is
  configured for it, so an ordinary `git commit` signs.
- `make test vet lint acceptance` must pass before pushing. `make test` runs
  with `-race -count=1`.
- No test may read or write the developer's real WSO2 state or keychain. Every
  state root comes from `isolatedStateRoot(t)`.
- Nothing in `internal/auth/source.go` and nothing in the context schema
  changes. Two sibling branches are working there.

---

### Task 1: Normalize the verifier seam

Pure refactor. The service behaves identically afterwards; every existing test
must still pass without being edited. Doing this first means Task 2 adds a
verifier rather than restructuring the service and adding one at the same time.

**Files:**
- Create: `internal/statusservice/verifier.go`
- Modify: `internal/statusservice/statusservice.go:86-198`
- Test: `internal/statusservice/statusservice_test.go` (unchanged — it passing
  unedited is the deliverable)

**Interfaces:**
- Consumes: `devtoken.Verify`, `devtoken.ErrExpired`, `devtoken.Claims` from
  `internal/auth/devtoken`.
- Produces: the unexported `access` struct, its `serves`/`allows` methods, the
  `verifier` interface, and the `errAccessExpired` / `errAccessRejected`
  sentinels. Task 2 adds a second implementation of `verifier`.

- [ ] **Step 1: Run the existing tests so you know they pass before you start**

Run: `go test ./internal/statusservice/ ./test/acceptance/ -count=1`
Expected: PASS. If anything fails now, stop and report it rather than
proceeding — this task's whole claim is that it changes nothing.

- [ ] **Step 2: Create the verifier seam**

Create `internal/statusservice/verifier.go` (after the licence header):

```go
package statusservice

import (
	"errors"
	"slices"
	"time"

	"github.com/wso2/wso2-cli/internal/auth/devtoken"
)

// access is what a verified token asserts, normalized across token formats.
//
// A verifier answers in this shape so that authorize never learns which format
// it was handed. A member the format does not carry is left zero, and every
// check that reads one states what it does with the zero value — because "the
// token does not say" and "the token says something this service rejects" are
// different answers, and conflating them is how an absent claim becomes an
// unenforced one.
type access struct {
	// Audiences are the services the token names. RFC 7519 section 4.1.3
	// allows one or many, so this is always a list.
	Audiences []string
	// Scopes are the permissions the token conveys.
	Scopes []string
	// Organization is the organization the token itself names. It is empty
	// when the issuer mints no organization claim.
	Organization string
	// Invocation is the shell invocation the token was bound to. It is empty
	// for issuer-minted tokens, because no OAuth issuer mints such a claim.
	Invocation string
}

// serves reports whether the token is bound to the given audience.
func (a access) serves(audience string) bool {
	return slices.Contains(a.Audiences, audience)
}

// allows reports whether the token conveys the given permission.
func (a access) allows(scope string) bool {
	return slices.Contains(a.Scopes, scope)
}

// The refusals a verifier may report.
//
// They are the whole vocabulary on purpose. A caller is told that its access
// expired or that it was not accepted, and never which token format failed,
// which key did not match, or how a signature was malformed — none of which it
// is owed, and all of which describe the service's own configuration.
var (
	errAccessExpired  = errors.New("statusservice: the presented access has expired")
	errAccessRejected = errors.New("statusservice: the presented access was not issued for this service")
)

// verifier establishes that a presented token is genuine and reads what it
// asserts.
//
// It proves origin only. Whether the claims are the ones this service serves is
// authorize's decision, kept there so that one policy answers for every token
// format rather than each format carrying its own copy of the rules.
type verifier interface {
	verify(presented string, now time.Time) (access, error)
}

// devtokenVerifier accepts the architecture proof's fixture token, which the
// shell signs and this service verifies with a source credential they both
// hold. It is why the fixture is not a production design, and it is the only
// verifier that can read an invocation binding.
type devtokenVerifier struct {
	sourceCredential string
}

func (v devtokenVerifier) verify(presented string, now time.Time) (access, error) {
	claims, err := devtoken.Verify(v.sourceCredential, presented, now)
	switch {
	case errors.Is(err, devtoken.ErrExpired):
		return access{}, errAccessExpired
	case err != nil:
		return access{}, errAccessRejected
	}
	return access{
		Audiences:    []string{claims.Audience},
		Scopes:       claims.Scopes,
		Organization: claims.Organization,
		Invocation:   claims.Invocation,
	}, nil
}
```

- [ ] **Step 3: Give the service a verifier and rewrite authorize**

In `internal/statusservice/statusservice.go`, add the field to `Service`:

```go
// Service answers status requests for one audience and organization.
type Service struct {
	options  Options
	verifier verifier
}
```

In `New`, after the `options.Now` default, replace `return &Service{options: options}, nil` with:

```go
	return &Service{
		options:  options,
		verifier: devtokenVerifier{sourceCredential: options.SourceCredential},
	}, nil
```

Replace `authorize` entirely. Note the return type change: it no longer returns
claims, because what the service reports is now its own configuration rather
than anything the token said.

```go
// authorize proves the caller presented access this service accepts.
//
// Every check is against what the service itself serves, never against what the
// request asks for, so a caller cannot widen its access by asserting a
// different audience, scope, organization, or invocation.
func (s *Service) authorize(request *http.Request) *refusal {
	presented, found := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer ")
	if !found || strings.TrimSpace(presented) == "" {
		return &refusal{http.StatusUnauthorized, "unauthenticated",
			"the request presented no bearer token"}
	}
	invocation := request.Header.Get(InvocationHeader)
	if invocation == "" {
		return &refusal{http.StatusUnauthorized, "unauthenticated",
			"the request does not name the invocation it belongs to"}
	}

	granted, err := s.verifier.verify(strings.TrimSpace(presented), s.options.Now())
	switch {
	case errors.Is(err, errAccessExpired):
		return &refusal{http.StatusUnauthorized, "token_expired",
			"the presented access has expired"}
	case err != nil:
		return &refusal{http.StatusUnauthorized, "token_rejected",
			"the presented access was not issued for this service"}
	}

	switch {
	case !granted.serves(s.options.Audience):
		return notAccepted("audience")
	case !granted.allows(s.options.RequiredScope):
		return notAccepted("scope")
	// An organization the token names is checked; a token that names none is
	// bound to this organization by its issuer instead. Task 2's verifier
	// refuses any token whose iss is not the one this service was configured
	// with, and that configuration is the deployment's statement that this
	// issuer speaks for this organization. The alternative — demanding a claim
	// — would refuse every token Asgardeo issues outside a sub-organization
	// setup, because it mints none.
	case granted.Organization != "" && granted.Organization != s.options.Organization:
		return notAccepted("organization")
	// Only the fixture token binds an invocation. The header is required of
	// every caller regardless, so a run always states which invocation it
	// claims to be part of even when nothing can hold it to the claim.
	case granted.Invocation != "" && granted.Invocation != invocation:
		return notAccepted("invocation")
	}
	return nil
}
```

Change `notAccepted` to return only the refusal:

```go
func notAccepted(claim string) *refusal {
	return &refusal{http.StatusForbidden, "access_not_accepted",
		fmt.Sprintf("the presented access is not for the %s this service serves", claim)}
}
```

In `ServeHTTP`, replace the authorize call and the organization it reports:

```go
	if failure := s.authorize(request); failure != nil {
		s.fail(writer, failure.status, failure.code, failure.message)
		return
	}
```

```go
	checkedAt := s.options.Now().UTC()
	s.write(writer, http.StatusOK, map[string]string{
		// The organization reported is the one this service was configured to
		// serve, not one read out of the token. They are equal by the time
		// this line runs, and reporting the configured value keeps it that way
		// for a token format that carries no organization at all.
		"organization": s.options.Organization,
		"service":      ServiceName,
		"status":       "operational",
		"checkedAt":    checkedAt.Format(time.RFC3339),
	})
```

Remove the now-unused `devtoken` import from `statusservice.go` — it moved to
`verifier.go`.

- [ ] **Step 4: Run every test that touches this**

Run: `go test ./internal/statusservice/ ./test/acceptance/ -count=1`
Expected: PASS, with no test file edited. A failure here is a real behaviour
change and must be understood, not patched around.

- [ ] **Step 5: Vet and lint**

Run: `make vet lint`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/statusservice/verifier.go internal/statusservice/statusservice.go
git commit -m "refactor(statusservice): put one policy over a verifier seam"
```

---

### Task 2: Verify an issuer-minted token

> **Correction, recorded after the task shipped.** The body below is left as
> written, because this plan is a record of what was intended. Three things in
> it are wrong, and the shipped code is the authority on all three.
>
> - It says `SupportedSigningAlgs: []string{oidc.RS256}` "is what makes `alg:
>   none` unreachable". It is not. go-oidc filters `none` and the HMAC
>   algorithms out of an issuer's advertised set before any caller's config is
>   read (`oidc.go:179-190` and `337-341`), so a symmetric algorithm never
>   reaches the verifier however the issuer advertises it. What the setting
>   actually does is narrow that ten-algorithm asymmetric allowlist to the one
>   this service accepts.
> - Its `serveJWKS` advertises RS256 alone and publishes one RSA key. The
>   shipped fixture advertises RS256 and ES256 and publishes a key for each.
> - Its `mintJWT` takes a single RSA key, so it could sign in one algorithm
>   only. The shipped helper takes the algorithm and picks the matching key.
>
> The last two are what let the shipped ES256 subtest prove what the plan's
> design could not: that a token signed with an algorithm this service does not
> accept is refused even when the issuer advertises it and publishes the key
> that checks it.

**Files:**
- Create: `internal/statusservice/jwks.go`
- Modify: `internal/statusservice/statusservice.go` (the `Options` struct and `New`)
- Test: `internal/statusservice/jwks_test.go`

**Interfaces:**
- Consumes: Task 1's `access`, `verifier`, `errAccessExpired`, `errAccessRejected`.
- Produces: `Options.Issuer string` and `Options.HTTPClient *http.Client`; a
  service built with `Issuer` set verifies issuer-signed JWTs. Task 4 configures
  the acceptance harness through these.

- [ ] **Step 1: Write the failing test**

Create `internal/statusservice/jwks_test.go`. It is an external test package
(`package statusservice_test`), so it exercises the service through `New` and
`ServeHTTP` — which is the right level anyway: what matters is what a caller
gets, not what an unexported function returns.

The helper mints arbitrary JWTs because `fakeissuer` deliberately cannot: the
cases that matter here are tokens no correct issuer would produce.

```go
package statusservice_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/wso2/wso2-cli/internal/statusservice"
)

const (
	jwtAudience     = "reference-status"
	jwtScope        = "reference:status:read"
	jwtOrganization = "reference-org"
)

// serveJWKS starts an issuer that publishes one RSA key and the minimum
// discovery document go-oidc reads, and returns its URL and signing key. It is
// separate from fakeissuer because these tests mint the tokens themselves.
func serveJWKS(t *testing.T) (issuerURL string, key *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating a signing key: %v", err)
	}
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	mux.HandleFunc("GET /.well-known/openid-configuration",
		func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, map[string]any{
				"issuer":                                server.URL,
				"authorization_endpoint":                server.URL + "/authorize",
				"token_endpoint":                        server.URL + "/token",
				"jwks_uri":                              server.URL + "/jwks",
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
		})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(t, w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key: key.Public(), KeyID: "test-key", Algorithm: string(jose.RS256), Use: "sig",
		}}})
	})
	return server.URL, key
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, body any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Errorf("encoding a test response: %v", err)
	}
}

// mintJWT signs whatever claims a test asks for, including claim sets a correct
// issuer would never produce.
func mintJWT(t *testing.T, key *rsa.PrivateKey, algorithm jose.SignatureAlgorithm, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshalling claims: %v", err)
	}
	var signingKey any = key
	if algorithm == jose.HS256 {
		signingKey = []byte("a-symmetric-key-the-service-must-not-accept")
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: algorithm, Key: signingKey},
		(&jose.SignerOptions{}).WithHeader("kid", "test-key").WithType("JWT"))
	if err != nil {
		t.Fatalf("building a signer: %v", err)
	}
	signature, err := signer.Sign(payload)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	serialized, err := signature.CompactSerialize()
	if err != nil {
		t.Fatalf("serializing: %v", err)
	}
	return serialized
}

// validClaims are the claims a correct issuer produces, which each test then
// breaks in exactly one way. It names no organization, because that is what
// Asgardeo mints outside a sub-organization setup and therefore what the
// ordinary case looks like.
func validClaims(issuerURL string) map[string]any {
	now := time.Now()
	return map[string]any{
		"iss":   issuerURL,
		"sub":   "client-1",
		"aud":   []string{"some-client-id", jwtAudience},
		"scope": jwtScope,
		"exp":   now.Add(5 * time.Minute).Unix(),
		"iat":   now.Unix(),
	}
}

// callWith presents a token to a service and returns the HTTP status.
func callWith(t *testing.T, service *statusservice.Service, token string) int {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, statusservice.StatusPath, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set(statusservice.InvocationHeader, "invocation-1")
	recorder := httptest.NewRecorder()
	service.ServeHTTP(recorder, request)
	return recorder.Code
}

func newJWKSService(t *testing.T, issuerURL string) *statusservice.Service {
	t.Helper()
	service, err := statusservice.New(statusservice.Options{
		Audience:      jwtAudience,
		RequiredScope: jwtScope,
		Organization:  jwtOrganization,
		Issuer:        issuerURL,
	})
	if err != nil {
		t.Fatalf("statusservice.New returned %v", err)
	}
	return service
}

func TestAnIssuerMintedTokenIsAcceptedOnItsMerits(t *testing.T) {
	// The audience half of the boundary: a token this service never saw
	// minted, verified against keys the issuer publishes, accepted because
	// everything it asserts is what this service serves. The aud claim carries
	// a client identifier alongside the audience, which is the shape Identity
	// Server 7.3.0 produces; membership, not equality, is what admits it.
	issuerURL, key := serveJWKS(t)
	service := newJWKSService(t, issuerURL)

	status := callWith(t, service, mintJWT(t, key, jose.RS256, validClaims(issuerURL)))

	if status != http.StatusOK {
		t.Fatalf("a valid issuer-minted token was answered %d, want %d", status, http.StatusOK)
	}
}

func TestAnAudienceStatedAsOneStringIsRead(t *testing.T) {
	// RFC 7519 section 4.1.3 lets aud be a single string or an array, and an
	// Asgardeo token that names only the client identifier arrives as the
	// former. Reading one shape and not the other would make the audience
	// check depend on how an issuer chose to encode it.
	issuerURL, key := serveJWKS(t)
	service := newJWKSService(t, issuerURL)
	claims := validClaims(issuerURL)
	claims["aud"] = jwtAudience

	status := callWith(t, service, mintJWT(t, key, jose.RS256, claims))

	if status != http.StatusOK {
		t.Fatalf("a token naming its audience as one string was answered %d, want %d",
			status, http.StatusOK)
	}
}

func TestAnIssuerMintedTokenIsRefusedWhenItsClaimsAreNotServed(t *testing.T) {
	issuerURL, key := serveJWKS(t)
	other, otherKey := serveJWKS(t)

	for name, breakage := range map[string]struct {
		claims    func(map[string]any)
		algorithm jose.SignatureAlgorithm
		signWith  string // "issuer" or "other"
		want      int
	}{
		"the token names another issuer": {
			claims: func(c map[string]any) { c["iss"] = other }, want: http.StatusUnauthorized,
		},
		"the token was signed by another issuer's key": {
			signWith: "other", want: http.StatusUnauthorized,
		},
		"the token has expired": {
			claims: func(c map[string]any) { c["exp"] = time.Now().Add(-time.Minute).Unix() },
			want:   http.StatusUnauthorized,
		},
		"the token states no lifetime": {
			claims: func(c map[string]any) { delete(c, "exp") }, want: http.StatusUnauthorized,
		},
		"the token is signed with a symmetric algorithm": {
			algorithm: jose.HS256, want: http.StatusUnauthorized,
		},
		"the token names no audience this service serves": {
			claims: func(c map[string]any) { c["aud"] = []string{"some-client-id"} },
			want:   http.StatusForbidden,
		},
		"the token does not carry the required permission": {
			claims: func(c map[string]any) { c["scope"] = "reference:status:write" },
			want:   http.StatusForbidden,
		},
		"the token states no permissions": {
			claims: func(c map[string]any) { delete(c, "scope") }, want: http.StatusUnauthorized,
		},
		"the token names another organization": {
			claims: func(c map[string]any) { c["org_id"] = "another-organization" },
			want:   http.StatusForbidden,
		},
	} {
		t.Run(name, func(t *testing.T) {
			service := newJWKSService(t, issuerURL)
			claims := validClaims(issuerURL)
			if breakage.claims != nil {
				breakage.claims(claims)
			}
			algorithm := jose.RS256
			if breakage.algorithm != "" {
				algorithm = breakage.algorithm
			}
			signingKey := key
			if breakage.signWith == "other" {
				signingKey = otherKey
			}

			status := callWith(t, service, mintJWT(t, signingKey, algorithm, claims))

			if status != breakage.want {
				t.Errorf("status = %d, want %d", status, breakage.want)
			}
		})
	}
}

func TestATokenNamingNoOrganizationIsBoundByItsIssuer(t *testing.T) {
	// Asgardeo mints no organization claim outside a sub-organization setup,
	// so a token that names none has to be accepted or nothing it issues would
	// work. What binds such a token to one organization is the issuer: this
	// service is configured with the issuer that speaks for its organization,
	// and a token minted by any other is refused however correct its claims.
	issuerURL, key := serveJWKS(t)
	foreignURL, foreignKey := serveJWKS(t)
	service := newJWKSService(t, issuerURL)

	own := mintJWT(t, key, jose.RS256, validClaims(issuerURL))
	if status := callWith(t, service, own); status != http.StatusOK {
		t.Errorf("a token from this service's own issuer was answered %d, want %d",
			status, http.StatusOK)
	}

	foreign := mintJWT(t, foreignKey, jose.RS256, validClaims(foreignURL))
	if status := callWith(t, service, foreign); status != http.StatusUnauthorized {
		t.Errorf("a token from another organization's issuer was answered %d, want %d",
			status, http.StatusUnauthorized)
	}
}

func TestAServiceMustSayHowItEstablishesTrust(t *testing.T) {
	issuerURL, _ := serveJWKS(t)
	base := statusservice.Options{
		Audience: jwtAudience, RequiredScope: jwtScope, Organization: jwtOrganization,
	}
	for name, options := range map[string]statusservice.Options{
		"neither a credential nor an issuer": base,
		"both a credential and an issuer": func() statusservice.Options {
			both := base
			both.SourceCredential, both.Issuer = "a-credential", issuerURL
			return both
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := statusservice.New(options); err == nil {
				t.Error("statusservice.New accepted a service that cannot say how it verifies tokens")
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/statusservice/ -run 'JWKS|IssuerMinted|BoundByItsIssuer|EstablishesTrust' -count=1`
Expected: FAIL to compile — `unknown field Issuer in struct literal of type statusservice.Options`.

- [ ] **Step 3: Add the options**

In `internal/statusservice/statusservice.go`, add to `Options` after
`SourceCredential`:

```go
	// Issuer is the OpenID issuer whose signature this service accepts. A
	// service is configured with either this or a source credential, never
	// both: they are two ways to answer the same question, and a service that
	// would take either answer accepts a token that satisfies the weaker one.
	//
	// It is also the organization binding for a deployment that mints no
	// organization claim. Configuring an issuer here is the statement that
	// this issuer speaks for the organization this service serves, so a token
	// from anywhere else is refused whatever it claims.
	Issuer string
	// HTTPClient reaches the issuer for its configuration and keys. It
	// defaults to http.DefaultClient; a live run against a deployment serving
	// its own certificate replaces it.
	HTTPClient *http.Client
```

- [ ] **Step 4: Write the JWKS verifier**

Create `internal/statusservice/jwks.go` (after the licence header):

```go
package statusservice

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	oidc "github.com/coreos/go-oidc/v3/oidc"
)

// jwksVerifier accepts an access token an OpenID issuer signed, checked against
// the keys that issuer publishes.
//
// It is deliberately not the check the shell makes. internal/auth/claims.go
// reads a token's claims without verifying them, and says why it may: that
// token arrived over the issuer's own connection in answer to the shell's own
// request. A service receiving a bearer token from a caller it knows nothing
// about has no such standing, so nothing the token says about itself is trusted
// until the signature is.
type jwksVerifier struct {
	tokens *oidc.IDTokenVerifier
}

// newJWKSVerifier reads the issuer's OpenID configuration and builds a verifier
// against the keys it publishes.
//
// Discovery happens here rather than per request, so a service that cannot read
// its issuer's configuration refuses to start. That is New's existing rule: a
// service that cannot say what it accepts would accept anything.
func newJWKSVerifier(issuer string, client *http.Client, now func() time.Time) (jwksVerifier, error) {
	ctx := context.Background()
	if client != nil {
		ctx = oidc.ClientContext(ctx, client)
	}
	// NewProvider reads the document and refuses one whose own issuer member
	// disagrees with the URL it came from, so a redirected host cannot
	// substitute its own keys.
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return jwksVerifier{}, fmt.Errorf(
			"statusservice: cannot read the issuer's OpenID configuration: %w", err)
	}
	return jwksVerifier{tokens: provider.VerifierContext(ctx, &oidc.Config{
		// The audience check belongs to authorize, not here. An Asgardeo token
		// names the client identifier alone and an Identity Server 7.3.0 token
		// names the client identifier and the API resource, so membership is
		// the only check both deployments can satisfy — and this library's own
		// check is equality against a single configured value.
		SkipClientIDCheck: true,
		// One algorithm, stated rather than negotiated. An issuer that offers
		// another is refused instead of being left to choose how its own
		// tokens are checked, which is what makes "alg: none" unreachable.
		SupportedSigningAlgs: []string{oidc.RS256},
		// The service's clock, so a test that moves it moves expiry with it.
		Now: now,
	})}, nil
}

// verify proves the issuer minted this token and reads what it asserts.
//
// The clock is the one this verifier was built with rather than the argument,
// because the library holds it; the parameter is the seam's, and honoring it
// twice would let the two disagree.
func (v jwksVerifier) verify(presented string, _ time.Time) (access, error) {
	token, err := v.tokens.Verify(context.Background(), presented)
	var expired *oidc.TokenExpiredError
	switch {
	case errors.As(err, &expired):
		// A token stating no lifetime at all also arrives here, because an
		// absent exp reads as the zero time and every moment is after it.
		// Refusing it as expired rather than as malformed is the right answer
		// to the only question that matters: it is not access this service
		// will act on.
		return access{}, errAccessExpired
	case err != nil:
		return access{}, errAccessRejected
	}

	var claims struct {
		// Scope is the space-delimited permission list RFC 9068 section 2.2.3
		// describes, which is what both measured deployments emit.
		Scope string `json:"scope"`
		// Organization is the claim Asgardeo mints for a sub-organization and
		// the API Portal verifies on every request.
		Organization string `json:"org_id"`
	}
	if err := token.Claims(&claims); err != nil {
		return access{}, errAccessRejected
	}
	scopes := strings.Fields(claims.Scope)
	// A token whose permissions cannot be read is refused rather than read as
	// carrying none. Both would fail the coverage check today, but a service
	// that treats "I cannot tell" as "it has nothing" is one broken claim away
	// from treating it as "it has everything".
	if len(scopes) == 0 {
		return access{}, errAccessRejected
	}
	return access{
		Audiences:    token.Audience,
		Scopes:       scopes,
		Organization: claims.Organization,
	}, nil
}
```

- [ ] **Step 5: Choose the verifier in New**

Replace `New` in `internal/statusservice/statusservice.go`:

```go
// New builds a service, refusing to start without the policy it enforces. A
// service that cannot say what it accepts would accept anything.
func New(options Options) (*Service, error) {
	switch {
	case options.Audience == "":
		return nil, errors.New("statusservice: an audience is required")
	case options.RequiredScope == "":
		return nil, errors.New("statusservice: a required scope is required")
	case options.Organization == "":
		return nil, errors.New("statusservice: an organization is required")
	case options.SourceCredential == "" && options.Issuer == "":
		return nil, errors.New("statusservice: a source credential or an issuer is required to verify tokens")
	case options.SourceCredential != "" && options.Issuer != "":
		return nil, errors.New("statusservice: a service verifies tokens one way; give it a source credential or an issuer, not both")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Issuer == "" {
		return &Service{
			options:  options,
			verifier: devtokenVerifier{sourceCredential: options.SourceCredential},
		}, nil
	}
	tokens, err := newJWKSVerifier(options.Issuer, options.HTTPClient, options.Now)
	if err != nil {
		return nil, err
	}
	return &Service{options: options, verifier: tokens}, nil
}
```

Update the `SourceCredential` doc comment to say it is one of the two ways, and
add `"net/http"` to the imports if it is not already there.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/statusservice/ -count=1 -race`
Expected: PASS, every subtest.

- [ ] **Step 7: Prove nothing else moved**

Run: `go test ./... -count=1`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/statusservice/jwks.go internal/statusservice/jwks_test.go internal/statusservice/statusservice.go
git commit -m "feat(statusservice): verify an issuer-minted token against published keys"
```

---

### Task 3: Let the fake issuer mint an organization claim

**Files:**
- Modify: `internal/auth/fakeissuer/fakeissuer.go:53-106` (Options) and
  `:565-583` (mintAccessToken)
- Test: `internal/auth/fakeissuer/fakeissuer_test.go`

**Interfaces:**
- Produces: `fakeissuer.Options.OrganizationClaim string`. Task 6 uses it.

This file is shared with two sibling branches. Keep the diff additive and small;
do not reformat anything around it.

- [ ] **Step 1: Write the failing test**

`fakeissuer_test.go` is an external test package (`package fakeissuer_test`) and
already has `refresh(t, issuer, refreshToken, scope) (map[string]any, int)` at
line 265 and `text(body, key) string` at line 105. Use them; only
`claimFromToken` is new. Add to the file:

```go
func TestAnAccessTokenCarriesAnOrganizationClaimOnlyWhenAsked(t *testing.T) {
	// Asgardeo mints no organization claim outside a sub-organization setup,
	// so the default has to be a token that carries none. A deployment that
	// does mint one is the other case a resource server must handle, and the
	// option is how a test reaches it.
	for name, configured := range map[string]string{
		"the deployment states an organization": "reference-org",
		"the deployment states none":            "",
	} {
		t.Run(name, func(t *testing.T) {
			issuer := fakeissuer.New(t, fakeissuer.Options{
				Audience: "reference-status", OrganizationClaim: configured,
			})
			seeded := issuer.SeedSession([]string{"reference:status:read"})

			body, status := refresh(t, issuer, seeded, "")
			if status != http.StatusOK {
				t.Fatalf("the refresh grant answered %d, want %d", status, http.StatusOK)
			}

			if got := claimFromToken(t, text(body, "access_token"), "org_id"); got != configured {
				t.Errorf("org_id = %q, want %q", got, configured)
			}
		})
	}
}

// claimFromToken reads one string claim out of a JWT payload without verifying
// it. This is a test reading a fixture's own output, not a security decision.
func claimFromToken(t *testing.T, token, claim string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("the token is not a three-part JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decoding the token payload: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("the token payload is not JSON: %v", err)
	}
	value, present := claims[claim]
	if !present {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		t.Fatalf("the %q claim is %T, want a string", claim, value)
	}
	return text
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/auth/fakeissuer/ -run OrganizationClaim -count=1`
Expected: FAIL to compile — `unknown field OrganizationClaim`.

- [ ] **Step 3: Add the option**

In `Options`, after `Audience`:

```go
	// OrganizationClaim is the org_id an access token names. When it is empty
	// a token carries no organization claim at all, which is what Asgardeo
	// issues outside a sub-organization setup — and therefore what a resource
	// server has to accept, binding the token to an organization through the
	// issuer it trusts rather than through a claim.
	OrganizationClaim string
```

- [ ] **Step 4: Mint it**

Rewrite `mintAccessToken` so the claim set is built before it is signed:

```go
// mintAccessToken signs a real RS256 access token and records it for
// introspection.
func (i *Issuer) mintAccessToken(subject string, scopes []string) string {
	now := time.Now()
	claims := map[string]any{
		"iss":   i.URL,
		"sub":   subject,
		"aud":   i.opts.Audience,
		"scope": strings.Join(scopes, " "),
		"exp":   now.Add(5 * time.Minute).Unix(),
		"iat":   now.Unix(),
	}
	if i.opts.OrganizationClaim != "" {
		claims["org_id"] = i.opts.OrganizationClaim
	}
	token := i.sign(claims)
	i.mutex.Lock()
	i.accessTokens[token] = tokenRecord{
		scopes:   append([]string(nil), scopes...),
		audience: i.opts.Audience,
		subject:  subject,
	}
	i.mutex.Unlock()
	return token
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/auth/... ./test/acceptance/ -count=1`
Expected: PASS. Nothing else may change shape — the existing login tests assert
on the exact `aud` array, so a failure there means the diff was not additive.

- [ ] **Step 6: Commit**

```bash
git add internal/auth/fakeissuer/
git commit -m "test(fakeissuer): mint an organization claim when a test asks for one"
```

---

### Task 4: Give the acceptance harness an issuer-minted arm

**Files:**
- Modify: `test/acceptance/broker_test.go:34-129` (constants and helpers)
- Modify: `test/acceptance/acceptance_test.go:318-335` (`shellEnvironment`)

**Interfaces:**
- Consumes: `contextfixture.WriteV2`, `contexts.Document`, `fakeissuer.New`,
  Task 2's `Options.Issuer`, Task 3's `Options.OrganizationClaim`.
- Produces: `credentialKind` with `developmentCredential` and `issuerMinted`;
  `installation` with fields `kind`, `service`, `issuer`, `serviceIssuer`;
  `deployAs(t, installation) deployment`; `deployment` gaining `environment
  []string` and `fake *fakeissuer.Issuer`. `deploy(t, statusservice.Options)`
  keeps its signature so the thirteen existing call sites in `canary_test.go`,
  `status_test.go`, `failclosed_test.go` and `broker_test.go` are untouched.

This task adds the arm and one test that uses it. Tasks 5 and 6 then move the
rest of the suite onto it.

- [ ] **Step 1: Let a run carry extra environment**

In `test/acceptance/acceptance_test.go`, replace `shellEnvironment` with a base
plus a variadic addition:

```go
// shellEnvironment builds a minimal environment pointing the shell at the
// isolated state root, so the run cannot depend on developer configuration.
//
// It carries the development credential the isolated context names. Supplying
// it here is the point of the canary: it is present for every run, only the
// shell may read it, and no run may disclose it. Additional variables are for
// deployments whose identity names a different credential source; they are
// appended rather than replacing the canary, so every run still proves the
// credential it does not use stays undisclosed.
func shellEnvironment(stateRoot string, additional ...string) []string {
	environment := []string{
		state.RootEnvVar + "=" + stateRoot,
		credentialVariable + "=" + canaryCredential,
	}
	for _, name := range []string{"PATH", "SystemRoot", "TMP", "TEMP", "HOME", "USERPROFILE"} {
		if value, present := os.LookupEnv(name); present {
			environment = append(environment, name+"="+value)
		}
	}
	return append(environment, additional...)
}
```

Every existing call site passes one argument and keeps working.

- [ ] **Step 2: Add the deployment kinds**

In `test/acceptance/broker_test.go`, after the existing constant block, add:

```go
// The client-credentials identity the issuer-minted arm authenticates as. It
// mirrors login_test.go's inline deployment, which proves the same identity
// kind against an in-process shell; here it runs the built binary instead.
const (
	oauthIdentityName    = "reference-machine"
	oauthClientID        = "wso2cli-reference"
	oauthSecretVariable  = "WSO2_REFERENCE_CLIENT_SECRET"
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
```

- [ ] **Step 3: Rebuild deploy over installations**

Replace `deployment`, `deploy` and `installReferenceContext` in
`broker_test.go`. Keep `startStatusService` and `recordedService` as they are
apart from the credential default noted below.

```go
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
	// fake is the issuer behind an issuer-minted deployment, and nil for a
	// development one.
	fake *fakeissuer.Issuer
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
		service := startStatusService(t, install.service)
		installReferenceContext(t, stateRoot, service.server.URL, credentialVariable)
		return deployment{
			stateRoot:   stateRoot,
			service:     service.server,
			calls:       service.calls,
			environment: shellEnvironment(stateRoot),
		}
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
	service := startStatusService(t, install.service)
	installOAuthContext(t, stateRoot, issuer.URL, service.server.URL)

	return deployment{
		stateRoot:   stateRoot,
		service:     service.server,
		calls:       service.calls,
		environment: shellEnvironment(stateRoot, oauthSecretVariable+"="+oauthClientSecret),
		fake:        issuer,
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
```

In `startStatusService`, make the source-credential default conditional, so an
issuer-verifying service is not also handed a shared secret — `New` refuses
both:

```go
	if options.SourceCredential == "" && options.Issuer == "" {
		options.SourceCredential = canaryCredential
	}
```

Add `contextfixture`, `contexts`, and `fakeissuer` to the imports if the file
does not already have them.

- [ ] **Step 4: Route runs through the deployment's environment**

`runShell` (`acceptance_test.go:291`) and `tryShell` (`status_test.go:384`) both
derive the environment from a state root, so neither can carry a client secret.
Add deployment-aware wrappers to `broker_test.go` rather than changing either
helper, so no other call site moves. Both delegate to `runShellWith`
(`acceptance_test.go:308`), which already takes an environment:

```go
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
```

- [ ] **Step 5: Write the test that uses the new arm**

Add to `broker_test.go`:

```go
func TestTheModulesIssuerMintedAccessIsAcceptedByAVerifyingService(t *testing.T) {
	// The claim this whole slice exists to make. The shell obtains an access
	// token from a real issuer, the module presents it, and the service accepts
	// it only after verifying the issuer's signature against the keys that
	// issuer publishes — so nothing here turns on a secret the test planted on
	// both sides.
	shell := buildShell(t)
	deployed := deployAs(t, installation{kind: issuerMinted})

	stdout, stderr := deployed.run(t, shell, "reference", "status")

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
```

- [ ] **Step 6: Run it**

Run: `go test ./test/acceptance/ -run IssuerMintedAccessIsAccepted -count=1 -v`
Expected: PASS.

- [ ] **Step 7: Run the whole acceptance package**

Run: `go test ./test/acceptance/ -count=1`
Expected: PASS — the thirteen existing `deploy` call sites are unchanged.

- [ ] **Step 8: Commit**

```bash
git add test/acceptance/
git commit -m "test(acceptance): deploy the reference module against a real issuer"
```

---

### Task 5: Run the boundary tests under both credential kinds

**Files:**
- Modify: `test/acceptance/broker_test.go:131-170`, `:266-309`, `:330-358`

**Interfaces:**
- Consumes: Task 4's `installation`, `deployAs`, `deployment.run`,
  `deployment.try`.

Five tests move; four stay single-arm and gain a comment saying why.

- [ ] **Step 1: Parameterize the five boundary tests**

Convert each of `TestBrokeredReferenceStatusReportsTheServicesOwnAnswer`,
`TestBrokeredReferenceStatusRendersTheServicesAnswerAsJSON`,
`TestExpiredAccessIsRefusedByTheService`,
`TestAFailingServiceAndADeniedRequestEndInDifferentExitClasses` and
`TestTheModuleEnvironmentCarriesNoAmbientCredential` to run over both kinds.
The shape, using the expiry test as the worked example:

```go
// bothCredentialKinds are the two ways a deployment obtains access. Tests whose
// subject is the audience boundary run under both, because a boundary that
// holds only for the fixture credential is not a boundary.
var bothCredentialKinds = []credentialKind{developmentCredential, issuerMinted}

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

			stdout, stderr, err := expired.try(shell, "reference", "status")

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
```

For `TestTheModuleEnvironmentCarriesNoAmbientCredential`, the body builds its
own state root rather than calling `deploy`. Keep that structure but take the
environment from a `deployAs` deployment, and extend the sweep so the client
secret's variable is caught too — the existing loop already rejects any name
starting with `WSO2_`, which covers `WSO2_REFERENCE_CLIENT_SECRET`, so only the
deployment wiring changes.

- [ ] **Step 2: Mark the single-arm tests**

Add a sentence to each of `TestAnUndeclaredAudienceIsDenied` and
`TestAnExcessiveScopeIsDenied`:

```go
	// One kind only: the receipt is checked before any source is consulted, so
	// a second pass would prove the same refusal twice and say nothing about
	// where the token would have come from.
```

And to `TestAMissingCredentialIsDeniedWithSafeRecoveryGuidance` and
`TestAServiceThatRejectsTheAccessClaimsIsReported`:

```go
	// The development kind only. The issuer-minted equivalents are
	// TestAnInlineIdentityWithNoSecretTellsTheUserWhichVariableToSet in
	// login_test.go and the organization-binding tests below, which refuse for
	// reasons this kind has no counterpart for.
```

- [ ] **Step 3: Run them**

Run: `go test ./test/acceptance/ -count=1 -v -run 'Brokered|Expired|DifferentExitClasses|AmbientCredential'`
Expected: PASS, with each of the five reporting two subtests.

- [ ] **Step 4: Run everything**

Run: `go test ./... -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add test/acceptance/broker_test.go
git commit -m "test(acceptance): prove the boundary under both credential kinds"
```

---

### Task 6: Prove the organization binding

**Files:**
- Modify: `test/acceptance/broker_test.go` (append)

**Interfaces:**
- Consumes: Task 3's `fakeissuer.Options.OrganizationClaim`, Task 4's
  `installation.serviceIssuer`.

Three tests, one per way a deployment states an organization. The first is the
one a claim-only rule would wrongly accept, and it is the reason this task
exists rather than being folded into Task 5.

- [ ] **Step 1: Write the tests**

```go
func TestAccessFromAnotherOrganizationsIssuerIsRefused(t *testing.T) {
	// A deployment that mints no organization claim — Asgardeo's default —
	// binds a token to one organization through its issuer and nothing else.
	// So the service is pointed at an issuer that is not the one the shell
	// authenticates against, and the perfectly valid, correctly scoped,
	// correctly audienced token the shell obtains is refused anyway.
	//
	// This is the case a rule that only checked a claim when the token carried
	// one would accept, which is why it is here.
	shell := buildShell(t)
	foreign := fakeissuer.New(t, fakeissuer.Options{Audience: referenceAudience})
	deployed := deployAs(t, installation{kind: issuerMinted, serviceIssuer: foreign.URL})

	stdout, stderr, err := deployed.try(shell, "reference", "status")

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

	stdout, stderr, err := deployed.try(shell, "reference", "status")

	if exitCode(t, err) != exitProductService {
		t.Fatalf("exit status = %v, want the product-service class %d\nstderr:\n%s",
			err, exitProductService, stderr)
	}
	if !strings.Contains(stderr, "reference.status_access_rejected") {
		t.Errorf("stderr does not report the refused access:\n%s", stderr)
	}
	assertNoCredentialDisclosure(t, stdout, stderr)
}

func TestAccessNamingThisOrganizationIsAccepted(t *testing.T) {
	// The third shape a deployment can take: an issuer that does mint the
	// claim, for the organization this service serves. It is the only one of
	// the three where the claim itself admits the token, and it has to keep
	// working alongside the two refusals above.
	shell := buildShell(t)
	deployed := deployAs(t, installation{
		kind:   issuerMinted,
		issuer: fakeissuer.Options{OrganizationClaim: referenceOrganization},
	})

	stdout, stderr := deployed.run(t, shell, "reference", "status")

	if !strings.Contains(stdout, "operational") {
		t.Errorf("the table does not report the service's answer:\n%s", stdout)
	}
	if deployed.calls.Load() != 1 {
		t.Errorf("the status service was called %d times, want once", deployed.calls.Load())
	}
	assertNoCredentialDisclosure(t, stdout, stderr)
}
```

- [ ] **Step 2: Run them**

Run: `go test ./test/acceptance/ -count=1 -v -run 'AnotherOrganization|ThisOrganizationIsAccepted'`
Expected: PASS, all three.

- [ ] **Step 3: Prove the first test fails for the right reason**

Temporarily change `authorize` in `internal/statusservice/statusservice.go` to
skip the issuer binding — set `SkipIssuerCheck: true` in `newJWKSVerifier`'s
`oidc.Config`. Run
`go test ./test/acceptance/ -run AccessFromAnotherOrganizationsIssuerIsRefused -count=1`.
Expected: FAIL. Then revert the change and confirm it passes again. A test that
cannot fail is not evidence.

- [ ] **Step 4: Commit**

```bash
git add test/acceptance/broker_test.go
git commit -m "test(acceptance): bind issuer-minted access to one organization"
```

---

### Task 7: Record what the service now proves

**Files:**
- Modify: `internal/statusservice/statusservice.go:17-30` (package comment)
- Modify: `docs/research/asgardeo-redirect-uri-and-scope-narrowing.md` (append
  to section 3.1)

- [ ] **Step 1: Update the package comment**

The current comment says the service "accepts a request only when the presented
fixture token is the one this invocation was granted". That is now one of two
paths. Rewrite the second paragraph to say the service establishes trust either
through a source credential it shares with the shell or through an issuer's
published keys, that the second is what a production service does, and that an
invocation binding exists only on the first because no OAuth issuer mints such a
claim.

- [ ] **Step 2: Record the consequence of the audience finding**

The research document's section 3.1 established that `aud` is the client
identifier on Asgardeo and a list on Identity Server 7.3.0, and left open "what
the broker's policy should say when one supported product can satisfy a clause
and another structurally cannot". Append a short paragraph recording what the
audience side now does about it: membership rather than equality, which is the
only check both shapes satisfy, implemented in `internal/statusservice` and
exercised by `TestAnIssuerMintedTokenIsAcceptedOnItsMerits`. Keep the file's
hard-wrapping.

- [ ] **Step 3: Commit**

```bash
git add internal/statusservice/statusservice.go docs/research/asgardeo-redirect-uri-and-scope-narrowing.md
git commit -m "docs: say how the reference audience establishes trust"
```

---

### Task 8: Full gate and pull request

- [ ] **Step 1: Run the whole gate**

Run: `make test vet lint acceptance`
Expected: all clean. Do not proceed on a failure; diagnose it.

- [ ] **Step 2: Confirm the smoke package still builds**

Run: `make smoke-build`
Expected: clean. Nothing in this branch touches the smoke package, but
`statusservice.Options` changed shape and the smoke package is invisible to the
default gate.

- [ ] **Step 3: Push and open the pull request**

```bash
git push -u origin test/reference-oauth-boundary
gh pr create --repo wso2/wso2-cli --base main \
  --title "test(acceptance): prove the reference module against an issuer-minted token" \
  --body "Closes #47

The reference module already received an issuer-minted access token, but nothing on the other end of the wire ever checked it: the recording service in login_test.go answers 200 to any bearer, and internal/statusservice could only verify the shared-secret fixture token. This branch gives the service a second way to establish trust — an OpenID issuer's RS256 signature, verified against the keys that issuer publishes — and moves the boundary tests onto it, so that expired access, an audience the service does not serve, and access from another organization's issuer are now refusals a real audience makes about a real OAuth token."
```

Write the body as one unwrapped line per paragraph; GitHub renders the wrapping.

- [ ] **Step 4: Review the branch**

Run `/code-review` against `main`. The skill ends with a review for a reason;
skipping it has previously let defects reach a pull request.

---

## Definition of Done

- `internal/statusservice` verifies an issuer-signed RS256 token against keys
  the issuer publishes, and refuses one that is expired, unsigned by that
  issuer, signed with any other algorithm, bound to no audience it serves,
  short of the permission it requires, unreadable in its permissions, or
  naming another organization.
- A service is configured with a source credential or an issuer, never both and
  never neither.
- The organization a run reports is the one the service was configured to
  serve, not a value read out of a token.
- The reference module, launched as a real subprocess by the built shell, has
  its issuer-minted access accepted by a service that verified it.
- Five boundary tests run under both credential kinds; the four that do not say
  why in a comment.
- Access from another organization's issuer is refused with no organization
  claim in play, and `make test vet lint acceptance` passes.
