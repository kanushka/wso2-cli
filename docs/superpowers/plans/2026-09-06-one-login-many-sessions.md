# One Login, One Session per Product: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** After one `wso2 login`, every product an identity records has its
own session, obtained through the login provider's sign-on, and a command
whose product has no session yet acquires one on first use; a pipeline
mints per product from one machine client, or from a product-level
credential where the product cannot map the machine client.

**Architecture:** The context document already records products under an
identity. This plan gives each product a computed *access plan* (which
issuer, which client, which scopes, which resource, which session entry,
which strategy) in `internal/contexts`, keys sessions in the OS secure
store by credential reference *and* product, and makes the broker's
session source read the product's own session, establishing it through a
shell-supplied hook when it is missing. `wso2 login` runs one
authorization per product session; `whoami`, `doctor` and `logout` act per
product. A client-credentials identity mints per product inline and may
carry a per-product credential.

**Tech Stack:** Go 1.24, cobra, `golang.org/x/oauth2`, `go-oidc/v3`,
`zalando/go-keyring`, the in-repo `internal/auth/fakeissuer` test issuer.

**Spec:** `docs/superpowers/specs/2026-09-06-one-login-many-sessions-design.md`
(sections 3 to 8). ADR: `docs/adr/0014-one-login-one-session-per-product.md`.
Measurements: `docs/research/2026-09-06-single-login-spikes.md`.

## Global Constraints

- Commit messages: one line, `type(scope): description`, no body, no
  attribution trailers.
- Repo `.md` files are hard-wrapped at about 78 columns. Never name agent
  tooling in a committed file.
- No credential value ever reaches a context document, a log line, a
  problem message, or a module. Variables hold names; the secure store
  holds tokens.
- Every refusal from the broker is a `problem.Problem` in
  `problem.CategoryAuthPolicy` with a recovery the user can act on.
  Problem codes are a closed list: reuse `auth.login_required`,
  `auth.narrowing_unavailable`, `auth.product_not_configured`,
  `auth.credential_unavailable`, `auth.trust_not_configured`; this plan
  adds exactly one code, `auth.session_required`.
- Tests: `go test ./internal/... ./cmd/...` must stay green except the
  pre-existing environmental failure in `internal/boundaries`
  (`TestShellReaderIsAssignedOnlyInCmdWso2`, caused by stale worktrees
  under `.claude/worktrees/`). Run a package's tests with
  `go test ./internal/<pkg>/ -run <Name> -v`.
- The keychain is mocked in tests with `keyring.MockInit()`.
- Deviations from the spec made by this plan, to be written back into the
  spec in Task 8: (a) a product session's secure-store entry is
  `<credentialRef>.<namespace>` (a dot, which a credential reference can
  never contain), not `<credentialRef>/<namespace>`; (b) there is no
  `strategy` pin on the product record: the strategy follows from the
  record's shape (no grant, jwt-bearer grant, federated grant) and from
  whether the product shares the login product's scope set and resource;
  (c) the strategy for a client-credentials identity is reported as
  `inline`.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/contexts/access.go` (create) | `ProductAccess` and `Identity.Access`, `Identity.LoginAccess`, `Identity.Accesses`: the one place that decides how a product is reached. |
| `internal/contexts/identity.go` (modify) | `GrantFederated` kind, `Grant.Resource`, `Product.ClientIDVariable` / `ClientSecretVariable`, relaxed resource-bound validation. |
| `internal/contexts/access_test.go` (create), `derivation_test.go`, `grant_test.go` (modify) | Tests for the above. |
| `internal/auth/session/session.go` (modify) | `Session.Strategy`, `Session.ClientID`, `Session.Scopes`; `ProductRef`. |
| `internal/auth/source_session.go` (modify) | `sessionSource` takes `ref`, `issuer`, `clientID`, and an `establish` hook; first-use acquisition. |
| `internal/auth/source.go`, `source_assertion.go`, `source_clientcred.go`, `auth.go` (modify) | Resolve through `Access`; `Broker.EstablishSession`; `SessionRequired`; inline source per product credential. |
| `internal/auth/source_product_test.go` (create) | Sibling, federated, first-use, inline-per-product tests. |
| `internal/app/login.go` (modify) | `establishProduct`, per-product login, `--only`, `--no-products`, per-product report. |
| `internal/app/invoke.go` (modify) | Wires `EstablishSession` with the no-input gate and the notice line. |
| `internal/app/whoami.go`, `doctor.go`, `logout.go`, `sessionkind.go` (modify) | Per-product reporting; client-credentials identities healthy. |
| `internal/app/login_products_test.go`, `whoami_products_test.go`, `doctor_products_test.go`, `logout_products_test.go` (create) | App-level tests. |
| `docs/reference/commands.md`, `docs/examples/authentication-contexts.md`, the spec (modify) | Documentation. |

---

### Task 1: The access plan of a product (`internal/contexts`)

**Files:**
- Create: `internal/contexts/access.go`
- Modify: `internal/contexts/identity.go` (Grant, Product, validation)
- Create: `internal/contexts/access_test.go`
- Modify: `internal/contexts/derivation_test.go:104-136`, `internal/contexts/grant_test.go`

**Interfaces:**
- Produces:
  - `const StrategyDirect = "direct"`, `StrategySibling = "sibling"`,
    `StrategyDerived = "derived"`, `StrategyFederated = "federated"`,
    `StrategyInline = "inline"`.
  - `const GrantFederated = "federated"` (a second legal `Grant.Kind`).
  - `Grant.Resource string` (json `resource`, optional).
  - `Product.ClientIDVariable`, `Product.ClientSecretVariable` (json
    `clientIdVariable`, `clientSecretVariable`, optional, both or neither,
    legal only on a client-credentials identity).
  - `type ProductAccess struct { Namespace, Strategy, Issuer, ClientID string; Scopes []string; Resource, SessionRef, Audience string }`
  - `func (i Identity) LoginAccess() ProductAccess`
  - `func (i Identity) Access(namespace string) (ProductAccess, bool)`
  - `func (i Identity) Accesses() []ProductAccess` (login access first,
    then every product whose `SessionRef` differs, sorted by namespace;
    for a client-credentials identity every product with `StrategyInline`
    and an empty `SessionRef`)
  - `func ProductSessionRef(credentialRef, namespace string) string` =
    `credentialRef + "." + namespace`.

- [ ] **Step 1: Write the failing tests**

`internal/contexts/access_test.go`:

```go
package contexts_test

import (
	"slices"
	"testing"

	"github.com/wso2/wso2-cli/internal/contexts"
)

func thunderIdentity() contexts.Identity {
	return contexts.Identity{
		Name: "thunder", Type: "onprem",
		Auth: contexts.IdentityAuth{
			Kind: contexts.KindOAuthBrowser, Issuer: "http://localhost:8492",
			ClientID: "wso2-cli", CredentialRef: "thunder", Provider: contexts.ProviderThunder,
		},
		Products: map[string]contexts.Product{
			"iam": {Endpoint: "http://localhost:8492", Audience: "https://localhost:8090/mcp",
				Scopes: []string{"system"}},
			"gateway": {Endpoint: "https://localhost:8243", Audience: "http://localhost:18080/mockapi",
				Scopes: []string{"orders:read"}},
			"apim": {Endpoint: "https://localhost:9443", Audience: "DgP2V4Arw9KYeo2ltIm4r8r19vca",
				Scopes: []string{"apim:api_view"},
				Grant: &contexts.Grant{Kind: contexts.GrantFederated,
					Issuer: "https://localhost:9443/oauth2/token", ClientID: "DgP2V4Arw9KYeo2ltIm4r8r19vca"}},
		},
	}
}

func TestTheLoginProductIsTheFirstDirectProductByNamespace(t *testing.T) {
	access := thunderIdentity().LoginAccess()
	if access.Namespace != "gateway" || access.Strategy != contexts.StrategyDirect {
		t.Fatalf("login access = %+v, want the gateway product, direct", access)
	}
	if access.SessionRef != "thunder" || access.Resource != "http://localhost:18080/mockapi" ||
		access.Issuer != "http://localhost:8492" || access.ClientID != "wso2-cli" {
		t.Fatalf("login access = %+v", access)
	}
}

func TestASecondResourceIsASiblingWithItsOwnSession(t *testing.T) {
	access, ok := thunderIdentity().Access("iam")
	if !ok || access.Strategy != contexts.StrategySibling {
		t.Fatalf("iam access = %+v, %v", access, ok)
	}
	if access.SessionRef != "thunder.iam" || access.Resource != "https://localhost:8090/mcp" ||
		!slices.Equal(access.Scopes, []string{"system"}) {
		t.Fatalf("iam access = %+v", access)
	}
}

func TestAFederatedGrantIsReachedAtItsOwnIssuerAsItsOwnClient(t *testing.T) {
	access, _ := thunderIdentity().Access("apim")
	if access.Strategy != contexts.StrategyFederated || access.Issuer != "https://localhost:9443/oauth2/token" ||
		access.ClientID != "DgP2V4Arw9KYeo2ltIm4r8r19vca" || access.SessionRef != "thunder.apim" ||
		access.Resource != "" || !slices.Equal(access.Scopes, []string{"apim:api_view"}) ||
		access.Audience != "DgP2V4Arw9KYeo2ltIm4r8r19vca" {
		t.Fatalf("apim access = %+v", access)
	}
}

func TestAJWTBearerGrantIsDerivedFromItsOwnAssertionSession(t *testing.T) {
	identity := thunderIdentity()
	identity.Products["apim"] = contexts.Product{Endpoint: "https://localhost:9443", Audience: "cid",
		Scopes: []string{"apim:api_view"},
		Grant: &contexts.Grant{Kind: contexts.GrantJWTBearer, Issuer: "https://localhost:9443/oauth2/token",
			ClientID: "cid", Scopes: []string{"email", "groups"}, Resource: "https://localhost:9443/oauth2/token"}}
	access, _ := identity.Access("apim")
	if access.Strategy != contexts.StrategyDerived || access.Issuer != "http://localhost:8492" ||
		access.ClientID != "wso2-cli" || access.SessionRef != "thunder.apim" ||
		access.Resource != "https://localhost:9443/oauth2/token" ||
		!slices.Equal(access.Scopes, []string{"email", "groups", "openid"}) {
		t.Fatalf("derived access = %+v", access)
	}
}

func TestAProductSharingTheLoginScopeSetIsDirect(t *testing.T) {
	identity := contexts.Identity{Name: "is", Type: "onprem",
		Auth: contexts.IdentityAuth{Kind: contexts.KindOAuthBrowser, Issuer: "https://is.example",
			ClientID: "wso2-cli", CredentialRef: "is"},
		Products: map[string]contexts.Product{
			"a": {Endpoint: "https://a.example", Audience: "a", Scopes: []string{"x", "y"}},
			"b": {Endpoint: "https://b.example", Audience: "b", Scopes: []string{"y", "x"}},
			"c": {Endpoint: "https://c.example", Audience: "c", Scopes: []string{"x"}},
		}}
	b, _ := identity.Access("b")
	c, _ := identity.Access("c")
	if b.Strategy != contexts.StrategyDirect || b.SessionRef != "is" {
		t.Fatalf("b = %+v, want direct on the login session", b)
	}
	if c.Strategy != contexts.StrategySibling || c.SessionRef != "is.c" {
		t.Fatalf("c = %+v, want a sibling", c)
	}
}

func TestAccessesListsTheLoginSessionFirstThenEachOtherSession(t *testing.T) {
	var names []string
	for _, access := range thunderIdentity().Accesses() {
		names = append(names, access.Namespace+":"+access.Strategy)
	}
	want := []string{"gateway:direct", "apim:federated", "iam:sibling"}
	if !slices.Equal(names, want) {
		t.Fatalf("accesses = %v, want %v", names, want)
	}
}

func TestAnIdentityWithoutProductsStillHasALoginAccess(t *testing.T) {
	identity := thunderIdentity()
	identity.Auth.Provider = ""
	identity.Products = nil
	access := identity.LoginAccess()
	if access.Namespace != "" || access.Strategy != contexts.StrategyDirect ||
		access.SessionRef != "thunder" || len(access.Scopes) != 0 || access.Resource != "" {
		t.Fatalf("login access = %+v", access)
	}
	if got := identity.Accesses(); len(got) != 1 {
		t.Fatalf("accesses = %+v, want the login access alone", got)
	}
}

func TestAClientCredentialsIdentityMintsEveryProductInline(t *testing.T) {
	identity := thunderIdentity()
	identity.Auth.Kind = contexts.KindClientCredentials
	identity.Auth.CredentialRef = ""
	identity.Auth.ClientSecretVariable = "WSO2_CI_SECRET"
	for _, access := range identity.Accesses() {
		if access.Strategy != contexts.StrategyInline || access.SessionRef != "" {
			t.Fatalf("%s = %+v, want inline with no session", access.Namespace, access)
		}
	}
	apim, _ := identity.Access("apim")
	if apim.Issuer != "https://localhost:9443/oauth2/token" {
		t.Fatalf("a federated product is minted at its own issuer, got %+v", apim)
	}
}

func TestAProductCredentialIsBothVariablesOrNeither(t *testing.T) {
	identity := thunderIdentity()
	identity.Auth.Kind = contexts.KindClientCredentials
	identity.Auth.CredentialRef = ""
	identity.Auth.ClientSecretVariable = "WSO2_CI_SECRET"
	identity.Products["apim"] = contexts.Product{Endpoint: "https://localhost:9443", Audience: "cid",
		ClientIDVariable: "WSO2_APIM_CLIENT_ID"}
	document := contexts.Document{SchemaVersion: contexts.SchemaVersion, Identities: []contexts.Identity{identity},
		Contexts: []contexts.Context{{Name: "ci", Identity: "thunder"}}, DefaultContext: "ci"}
	if _, err := document.Encode(); err == nil {
		t.Fatal("a product naming only a client id variable was accepted")
	}
	identity.Products["apim"] = contexts.Product{Endpoint: "https://localhost:9443", Audience: "cid",
		ClientIDVariable: "WSO2_APIM_CLIENT_ID", ClientSecretVariable: "WSO2_APIM_CLIENT_SECRET"}
	if _, err := document.Encode(); err != nil {
		t.Fatalf("a product naming both variables was refused: %v", err)
	}
	identity.Auth.Kind = contexts.KindOAuthBrowser
	identity.Auth.CredentialRef = "thunder"
	identity.Auth.ClientSecretVariable = ""
	if _, err := document.Encode(); err == nil {
		t.Fatal("a product credential on a browser identity was accepted")
	}
}
```

Also change `internal/contexts/derivation_test.go`:
`TestAThunderIdentityServingSeveralProductsIsRefused` becomes
`TestAThunderIdentityMayServeSeveralProducts` asserting the document
decodes without error (keep the same fixture). Keep
`TestAResourceBoundIdentityWithoutAProductIsRefused` and the audience
tests unchanged. In `grant_test.go`, `TestAThunderIdentityMayServeASecondProductByGrant`
stays; add a case to `TestAMalformedGrantIsRefused`: a jwt-bearer grant
on a Thunder identity whose `resource` is not an absolute URI is refused,
and a federated grant with a `resource` that is not an absolute URI is
refused.

Check how `Document.Encode` validates: `internal/contexts/contexts.go:199`
calls `d.validate()`; if it does not, use `contexts.Decode` of the encoded
bytes instead in the credential test.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/contexts/ -run 'Access|LoginProduct|Sibling|Federated|Inline|ProductCredential|SeveralProducts' -v`
Expected: compile errors for undefined `contexts.StrategyDirect`,
`GrantFederated`, `Access`, etc.

- [ ] **Step 3: Implement**

`internal/contexts/access.go`:

```go
// Copyright header as in every other file.

package contexts

import (
	"maps"
	"slices"
)

// The strategies by which a product's access is obtained. They are what
// wso2 whoami reports and what a session records about itself.
const (
	// StrategyDirect: the login session itself covers the product.
	StrategyDirect = "direct"
	// StrategySibling: a second session at the login issuer, under the
	// product's own resource or scope set.
	StrategySibling = "sibling"
	// StrategyDerived: an assertion session at the login issuer, presented to
	// the product's own issuer under the jwt-bearer grant per command.
	StrategyDerived = "derived"
	// StrategyFederated: a session at the product's own issuer, obtained as
	// the product's public client through the login provider's sign-on.
	StrategyFederated = "federated"
	// StrategyInline: no session; a client-credentials grant per command.
	StrategyInline = "inline"
)

// ProductAccess is how one product under an identity is reached. Everything
// in it is public configuration; nothing is a credential.
type ProductAccess struct {
	Namespace  string
	Strategy   string
	Issuer     string
	ClientID   string
	Scopes     []string
	Resource   string
	SessionRef string
	Audience   string
}

// ProductSessionRef is the secure-store entry a product's own session lives
// under. The separator is a dot, which refPattern never admits in a
// credential reference, so it can never collide with another identity's.
func ProductSessionRef(credentialRef, namespace string) string {
	return credentialRef + "." + namespace
}

// LoginAccess is the authorization wso2 login runs first: the first direct
// product by namespace, or a bare session when the identity records none.
func (i Identity) LoginAccess() ProductAccess {
	access := ProductAccess{
		Strategy: StrategyDirect, Issuer: i.Auth.Issuer, ClientID: i.Auth.ClientID,
		SessionRef: i.Auth.CredentialRef,
	}
	for _, namespace := range slices.Sorted(maps.Keys(i.Products)) {
		product := i.Products[namespace]
		if !product.Direct() {
			continue
		}
		access.Namespace = namespace
		access.Audience = product.Audience
		access.Scopes = sortedScopes(product.Scopes)
		if i.Auth.Derivation() == DerivationTokenResource {
			access.Resource = product.Audience
		}
		return access
	}
	return access
}

// Access is how the named product is reached, and whether it is recorded.
func (i Identity) Access(namespace string) (ProductAccess, bool) {
	product, recorded := i.Products[namespace]
	if !recorded {
		return ProductAccess{}, false
	}
	if i.Auth.Kind == KindClientCredentials {
		return i.inlineAccess(namespace, product), true
	}
	login := i.LoginAccess()
	access := ProductAccess{
		Namespace: namespace, Issuer: i.Auth.Issuer, ClientID: i.Auth.ClientID,
		Audience: product.Audience, SessionRef: ProductSessionRef(i.Auth.CredentialRef, namespace),
	}
	switch {
	case product.Direct():
		access.Scopes = sortedScopes(product.Scopes)
		if i.Auth.Derivation() == DerivationTokenResource {
			access.Resource = product.Audience
		}
		if namespace == login.Namespace ||
			(slices.Equal(access.Scopes, login.Scopes) && access.Resource == login.Resource) {
			access.Strategy = StrategyDirect
			access.SessionRef = login.SessionRef
		} else {
			access.Strategy = StrategySibling
		}
	case product.Grant.Kind == GrantJWTBearer:
		access.Strategy = StrategyDerived
		access.Scopes = product.Grant.AssertionScopes()
		access.Resource = product.Grant.Resource
	case product.Grant.Kind == GrantFederated:
		access.Strategy = StrategyFederated
		access.Issuer = product.Grant.Issuer
		access.ClientID = product.Grant.ClientID
		access.Scopes = sortedScopes(product.Scopes)
		access.Resource = product.Grant.Resource
	}
	return access, true
}

// inlineAccess is a client-credentials identity's plan for one product: no
// session, one grant per command, at the product's own issuer when it names
// one.
func (i Identity) inlineAccess(namespace string, product Product) ProductAccess {
	access := ProductAccess{
		Namespace: namespace, Strategy: StrategyInline, Issuer: i.Auth.Issuer,
		ClientID: i.Auth.ClientID, Audience: product.Audience, Scopes: sortedScopes(product.Scopes),
	}
	if product.Grant != nil {
		access.Issuer = product.Grant.Issuer
		access.Resource = product.Grant.Resource
	} else if i.Auth.Derivation() == DerivationTokenResource {
		access.Resource = product.Audience
	}
	return access
}

// Accesses is every authorization the identity needs, the login one first
// and then one per further session, in namespace order. A
// client-credentials identity lists every product, each inline.
func (i Identity) Accesses() []ProductAccess {
	if i.Auth.Kind == KindClientCredentials {
		var all []ProductAccess
		for _, namespace := range slices.Sorted(maps.Keys(i.Products)) {
			access, _ := i.Access(namespace)
			all = append(all, access)
		}
		return all
	}
	all := []ProductAccess{i.LoginAccess()}
	for _, namespace := range slices.Sorted(maps.Keys(i.Products)) {
		access, _ := i.Access(namespace)
		if access.SessionRef != all[0].SessionRef {
			all = append(all, access)
		}
	}
	return all
}

func sortedScopes(scopes []string) []string {
	sorted := slices.Clone(scopes)
	slices.Sort(sorted)
	return slices.Compact(sorted)
}
```

In `internal/contexts/identity.go`:

```go
// GrantFederated obtains the product's session at the product's own issuer,
// as the public client the grant names, through the login provider's
// sign-on. The session is the product issuer's own refresh token.
const GrantFederated = "federated"

var legalGrants = map[string]bool{GrantJWTBearer: true, GrantFederated: true}
```

Add to `Grant`:

```go
	// Resource is the RFC 8707 resource indicator the authorization for this
	// grant's session carries, when the issuer it runs at requires one. For a
	// jwt-bearer grant that issuer is the identity's; for a federated grant it
	// is the product's own. Optional.
	Resource string `json:"resource,omitempty"`
```

Add to `Product`:

```go
	// ClientIDVariable and ClientSecretVariable name the environment
	// variables holding a credential of the product's own, for a
	// client-credentials identity whose machine client the product cannot
	// map to its roles. Both or neither; names, never values.
	ClientIDVariable     string `json:"clientIdVariable,omitempty"`
	ClientSecretVariable string `json:"clientSecretVariable,omitempty"`
```

Replace `validateDerivation`'s "exactly one direct product" block with:

```go
	if len(i.Products) == 0 {
		return malformed(fmt.Sprintf(
			"declares the identity %q against a deployment that binds a login to a product, "+
				"and gives it none", i.Name))
	}
```

and keep the per-product audience loop as it is. After that loop add, for
grant products on a token-resource identity:

```go
	for _, namespace := range slices.Sorted(maps.Keys(i.Products)) {
		grant := i.Products[namespace].Grant
		if grant == nil || grant.Kind != GrantJWTBearer {
			continue
		}
		if !absoluteURI(grant.Resource) {
			return malformed(fmt.Sprintf(
				"declares the %q product on the identity %q with a jwt-bearer grant and no "+
					"resource for its assertion session, which its deployment binds access by",
				namespace, i.Name))
		}
	}
```

with a helper `func absoluteURI(value string) bool` (parse, scheme non-empty,
no fragment) that the existing audience check also uses. In
`Grant.validate`, after the issuer check, add: a non-empty `Resource`
must satisfy `absoluteURI`.

`Product.validate` gains, before the grant branch:

```go
	if (p.ClientIDVariable == "") != (p.ClientSecretVariable == "") {
		return malformed(fmt.Sprintf(
			"declares a product credential on the identity %q with one variable and not the other", identity))
	}
	if p.ClientIDVariable != "" {
		if !variablePattern.MatchString(p.ClientIDVariable) || !variablePattern.MatchString(p.ClientSecretVariable) {
			return contextProblem("contexts.document_malformed",
				fmt.Sprintf("a product on the identity %q does not name environment variables as its credential source", identity),
				"Name the environment variables holding the product's client id and secret, not the values.")
		}
	}
```

and `Identity.validate` refuses a product credential unless
`i.Auth.Kind == KindClientCredentials`:

```go
		if i.Products[namespace].ClientSecretVariable != "" && i.Auth.Kind != KindClientCredentials {
			return malformed(fmt.Sprintf(
				"declares a product credential on the interactive identity %q; a product credential "+
					"belongs to a client-credentials identity", i.Name))
		}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/contexts/ -v`
Expected: PASS, including the fixture and documentation-example tests.

- [ ] **Step 5: Commit**

```bash
git add internal/contexts
git commit -m "feat(contexts): compute how each product is reached and let a Thunder identity record several"
```

---

### Task 2: Product-keyed sessions (`internal/auth/session`)

**Files:**
- Modify: `internal/auth/session/session.go:40-59`
- Modify: `internal/auth/session/session_test.go`

**Interfaces:**
- Produces: `Session.Strategy string` (json `strategy`), `Session.ClientID string` (json `clientId`), `Session.Scopes []string` (json `scopes`), all `omitempty`. No API change to `Store`; a product session is stored under `contexts.ProductSessionRef`.

- [ ] **Step 1: Write the failing test**

Append to `internal/auth/session/session_test.go`:

```go
func TestAProductSessionRoundTripsItsStrategyClientAndScopes(t *testing.T) {
	keyring.MockInit()
	store := Store{StateRoot: t.TempDir()}
	saved := Session{Issuer: "https://apim.example", RefreshToken: "rt", Strategy: "federated",
		ClientID: "cli-sso", Scopes: []string{"apim:api_view"}}
	if err := store.Save("thunder.apim", saved); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load("thunder.apim")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Strategy != "federated" || loaded.ClientID != "cli-sso" || len(loaded.Scopes) != 1 {
		t.Fatalf("loaded %+v", loaded)
	}
	// The login session under the bare reference is untouched by the product one.
	if _, err := store.Load("thunder"); err == nil {
		t.Fatal("a product session answered for the login reference")
	}
}

func TestASessionWrittenBeforeStrategiesExistedStillLoads(t *testing.T) {
	keyring.MockInit()
	if err := keyring.Set(Service, "legacy", `{"issuer":"https://is.example","refreshToken":"rt"}`); err != nil {
		t.Fatal(err)
	}
	loaded, err := Store{StateRoot: t.TempDir()}.Load("legacy")
	if err != nil || loaded.Strategy != "" || loaded.RefreshToken != "rt" {
		t.Fatalf("loaded %+v, %v", loaded, err)
	}
}
```

(Match the file's package name: it is `package session` or `session_test`;
adjust the `Store`/`Session` qualifiers accordingly.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/auth/session/ -run 'ProductSession|BeforeStrategies' -v`
Expected: compile error, unknown field `Strategy`.

- [ ] **Step 3: Implement**

Add to `Session` after `SessionExpiresAt`:

```go
	// Strategy is how this session was obtained (a contexts.Strategy* value),
	// ClientID the client it was obtained as, and Scopes what it was
	// authorized for. All three are informational: wso2 whoami reports them
	// and nothing grants access from them. omitempty, so an entry written
	// before they existed decodes with them empty.
	Strategy string   `json:"strategy,omitempty"`
	ClientID string   `json:"clientId,omitempty"`
	Scopes   []string `json:"scopes,omitempty"`
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/auth/session/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/auth/session
git commit -m "feat(session): record the strategy, client and scopes a session was obtained with"
```

---

### Task 3: The broker reads each product's own session and can establish a missing one

**Files:**
- Modify: `internal/auth/source_session.go` (fields and first-use acquisition)
- Modify: `internal/auth/source.go:60-100` (resolve through `Access`)
- Modify: `internal/auth/source_assertion.go` (unchanged logic; the `session` it holds now carries the product ref)
- Modify: `internal/auth/auth.go:100-129` (`Broker.EstablishSession`), add `SessionRequired`
- Create: `internal/auth/source_product_test.go`
- Modify: `internal/auth/source_assertion_test.go` (seed the assertion session under the product ref)

**Interfaces:**
- Consumes: `contexts.ProductAccess`, `Identity.Access`, `session.Session.Strategy`.
- Produces:
  - `Broker.EstablishSession func(access contexts.ProductAccess) error`: called when a product's own session is absent; nil means refuse.
  - `func SessionRequired(namespace string) Denial` with code `auth.session_required`, message `the %q product has no session under this identity yet`, recovery `Run wso2 login --only <namespace> to authorize it, or wso2 login to authorize every product.`
  - `sessionSource` fields: `namespace, ref, issuer, clientID, audience string; sessions session.Store; client *http.Client; establish func() error`.

- [ ] **Step 1: Write the failing tests**

`internal/auth/source_product_test.go`:

```go
package auth_test

import (
	"errors"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
)

const (
	siblingNamespace = "iam"
	siblingAudience  = "https://localhost:8090/mcp"
	siblingScope     = "system"
)

// thunderLikeDeployment is a resource-bound issuer with the login session
// seeded for the reference product and, optionally, a sibling session for
// a second resource.
func thunderLikeDeployment(t *testing.T, seedSibling bool) browserDeployment {
	t.Helper()
	deployment := seedBrowserSession(t, fakeissuer.Options{RequireResource: true})
	if seedSibling {
		seeded := deployment.issuer.SeedSessionFor([]string{siblingScope}, siblingAudience)
		store := session.Store{StateRoot: deployment.stateRoot}
		if err := store.Save(contexts.ProductSessionRef(sessionRef, siblingNamespace),
			session.Session{Issuer: deployment.issuer.URL, RefreshToken: seeded}); err != nil {
			t.Fatal(err)
		}
	}
	return deployment
}

// siblingBroker is the broker the iam module would build on that deployment.
func siblingBroker(t *testing.T, deployment browserDeployment) *auth.Broker {
	t.Helper()
	broker := deployment.broker(t)
	broker.Selection.Identity.Auth.Provider = contexts.ProviderThunder
	broker.Selection.Identity.Products[siblingNamespace] = contexts.Product{
		Endpoint: deployment.issuer.URL, Audience: siblingAudience, Scopes: []string{siblingScope},
	}
	broker.Namespace = siblingNamespace
	broker.Capabilities.AuthAudiences = []string{siblingAudience}
	broker.Capabilities.AuthScopes = []string{siblingScope}
	return broker
}

func TestASiblingProductIsAnsweredFromItsOwnSession(t *testing.T) {
	deployment := thunderLikeDeployment(t, true)
	broker := siblingBroker(t, deployment)
	grant, err := broker.Acquire(auth.Request{Audience: siblingAudience, Scopes: []string{siblingScope}})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	active, scopes, audiences := deployment.issuer.Introspect(t, grant.Token)
	if !active || len(scopes) != 1 || scopes[0] != siblingScope || audiences[0] != siblingAudience {
		t.Fatalf("token scopes %v audiences %v", scopes, audiences)
	}
	// The login session is untouched: it still holds the seeded refresh token.
	if deployment.storedSession(t).RefreshToken != deployment.seeded {
		t.Fatal("the sibling derivation rotated the login session")
	}
}

func TestAMissingSiblingSessionIsEstablishedOnFirstUse(t *testing.T) {
	deployment := thunderLikeDeployment(t, false)
	broker := siblingBroker(t, deployment)
	var asked contexts.ProductAccess
	broker.EstablishSession = func(access contexts.ProductAccess) error {
		asked = access
		seeded := deployment.issuer.SeedSessionFor(access.Scopes, access.Resource)
		return session.Store{StateRoot: deployment.stateRoot}.Save(access.SessionRef,
			session.Session{Issuer: access.Issuer, RefreshToken: seeded, Strategy: access.Strategy})
	}
	if _, err := broker.Acquire(auth.Request{Audience: siblingAudience, Scopes: []string{siblingScope}}); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if asked.Namespace != siblingNamespace || asked.Strategy != contexts.StrategySibling ||
		asked.Resource != siblingAudience || asked.SessionRef != contexts.ProductSessionRef(sessionRef, siblingNamespace) {
		t.Fatalf("the hook was asked for %+v", asked)
	}
}

func TestAMissingSiblingSessionWithNoWayToEstablishItIsRefused(t *testing.T) {
	deployment := thunderLikeDeployment(t, false)
	broker := siblingBroker(t, deployment)
	_, err := broker.Acquire(auth.Request{Audience: siblingAudience, Scopes: []string{siblingScope}})
	var denial auth.Denial
	if !errors.As(err, &denial) || denial.Problem.Code != "auth.session_required" {
		t.Fatalf("got %v, want auth.session_required", err)
	}
	if !contains(denial.Problem.Recovery, "wso2 login --only iam") {
		t.Fatalf("recovery %q does not name the login to run", denial.Problem.Recovery)
	}
}

func TestAMissingLoginSessionIsNeverEstablishedByTheBroker(t *testing.T) {
	// The bare login session is wso2 login's to establish; the broker only
	// fills in a product's own session beside an existing login.
	keyring.MockInit()
	deployment := thunderLikeDeployment(t, false)
	if _, err := session.Store{StateRoot: deployment.stateRoot}.Delete(sessionRef); err != nil {
		t.Fatal(err)
	}
	broker := deployment.broker(t)
	broker.EstablishSession = func(contexts.ProductAccess) error {
		t.Fatal("the broker tried to establish the login session")
		return nil
	}
	_, err := broker.Acquire(auth.Request{Audience: audience, Scopes: []string{readScope}})
	var denial auth.Denial
	if !errors.As(err, &denial) || denial.Problem.Code != "auth.login_required" {
		t.Fatalf("got %v, want auth.login_required", err)
	}
}

func TestAFederatedProductIsRefreshedAtItsOwnIssuerAsItsOwnClient(t *testing.T) {
	deployment := seedBrowserSession(t, fakeissuer.Options{})
	const apimAudience = "apim-cli-client"
	product := fakeissuer.New(t, fakeissuer.Options{Audience: apimAudience})
	seeded := product.SeedSession([]string{"apim:api_view"})
	ref := contexts.ProductSessionRef(sessionRef, "apim")
	if err := (session.Store{StateRoot: deployment.stateRoot}).Save(ref,
		session.Session{Issuer: product.URL, RefreshToken: seeded, Strategy: contexts.StrategyFederated}); err != nil {
		t.Fatal(err)
	}
	broker := deployment.broker(t)
	broker.Selection.Identity.Products["apim"] = contexts.Product{
		Endpoint: product.URL, Audience: apimAudience, Scopes: []string{"apim:api_view"},
		Grant: &contexts.Grant{Kind: contexts.GrantFederated, Issuer: product.URL, ClientID: apimAudience},
	}
	broker.Namespace = "apim"
	broker.Capabilities.AuthAudiences = []string{apimAudience}
	broker.Capabilities.AuthScopes = []string{"apim:api_view"}
	broker.HTTPClient = product.HTTPClient()
	grant, err := broker.Acquire(auth.Request{Audience: apimAudience, Scopes: []string{"apim:api_view"}})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	active, scopes, audiences := product.Introspect(t, grant.Token)
	if !active || scopes[0] != "apim:api_view" || audiences[0] != apimAudience {
		t.Fatalf("token scopes %v audiences %v", scopes, audiences)
	}
	if issuedBy, _, _ := deployment.issuer.Introspect(t, grant.Token); issuedBy {
		t.Fatal("the login issuer minted the federated product's token")
	}
}

func contains(text, want string) bool { return len(want) > 0 && strings.Contains(text, want) }
```

(Add `"strings"` to the imports.) Both fake issuers serve plain HTTP on
loopback with `httptest.Server` clients; `product.HTTPClient()` reaches
either. If `deployment.broker` sets `HTTPClient` to the login issuer's
client and that client refuses the second server, use
`http.DefaultClient` for the broker in the federated test.

In `internal/auth/source_assertion_test.go`, wherever the assertion test
seeds the session under `sessionRef` for the grant product, seed it under
`contexts.ProductSessionRef(sessionRef, <namespace>)` instead, and give
the grant a `Resource` when the deployment `RequireResource`s. Read that
file's helper (`seedGrantDeployment` or similar) and make the one change
there.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/auth/ -run 'Sibling|Federated|MissingLoginSession' -v`
Expected: compile error on `broker.EstablishSession`; then, after adding
the field only, failures with `auth.login_required` where
`auth.session_required` is expected.

- [ ] **Step 3: Implement**

`internal/auth/auth.go`, add to `Broker` after `Now`:

```go
	// EstablishSession obtains a product's own session when a command finds
	// none. The shell supplies it with the login flow; nil refuses with
	// auth.session_required. It is never asked for the login session itself:
	// that is wso2 login's, and a command that finds none is told to run it.
	EstablishSession func(access contexts.ProductAccess) error
```

and:

```go
// SessionRequired refuses a product whose own session is absent and cannot
// be established in this invocation.
func SessionRequired(namespace string) Denial {
	return denial("auth.session_required",
		fmt.Sprintf("the %q product has no session under this identity yet", namespace),
		fmt.Sprintf("Run wso2 login --only %s to authorize it, or wso2 login to authorize every product.",
			namespace))
}
```

`internal/auth/source.go`, in `resolveSource`'s interactive branch replace
the `sessionSource{...}` construction and the `!product.Direct()` check
with:

```go
		access, _ := b.Selection.Identity.Access(b.Namespace)
		source := sessionSource{
			namespace: b.namespace(),
			ref:       access.SessionRef,
			issuer:    access.Issuer,
			clientID:  access.ClientID,
			audience:  access.Audience,
			sessions:  session.Store{StateRoot: b.StateRoot},
			client:    b.httpClient(),
		}
		if access.Strategy != contexts.StrategyDirect {
			source.establish = b.establishFor(access)
		}
		if access.Strategy == contexts.StrategyDerived {
			return assertionSource{session: source, grant: *product.Grant}, nil
		}
		return source, nil
```

where `product` is `b.Selection.Identity.Products[b.Namespace]` (already
in scope: `checkProduct` reads it; bind it to a variable). Add:

```go
// establishFor is what a source calls when the product's own session is
// absent: the shell's login hook, or a refusal naming the login to run.
func (b *Broker) establishFor(access contexts.ProductAccess) func() error {
	return func() error {
		if b.EstablishSession == nil {
			return SessionRequired(b.namespace())
		}
		return b.EstablishSession(access)
	}
}
```

`internal/auth/source_session.go`: change the struct to

```go
type sessionSource struct {
	namespace string
	// ref is the secure-store entry this product's session lives under: the
	// identity's own for a direct product, the product's for every other.
	ref string
	// issuer and clientID are where and as whom the session is renewed.
	issuer, clientID string
	audience         string
	sessions         session.Store
	client           *http.Client
	// establish obtains the session when none is stored. nil for the login
	// session, which only wso2 login establishes.
	establish func() error
}
```

`mint` becomes:

```go
func (s sessionSource) mint(request Request, now time.Time) (Grant, error) {
	if err := s.ensureSession(); err != nil {
		return Grant{}, err
	}
	var granted Grant
	err := s.sessions.WithLock(s.ref, func() error { ... unchanged, s.derive ... })
	...
}

// ensureSession establishes the product's own session when it is absent,
// before the rotation lock is taken: a browser round trip must not hold
// the lock other invocations wait on.
func (s sessionSource) ensureSession() error {
	if s.establish == nil {
		return nil
	}
	_, err := s.sessions.Load(s.ref)
	if err == nil || !isLoginRequired(err) {
		return err
	}
	return s.establish()
}

func isLoginRequired(err error) bool {
	var typed problem.Problem
	return errors.As(err, &typed) && typed.Code == "auth.login_required"
}
```

Replace every `s.identity.Auth.CredentialRef` with `s.ref`,
`s.identity.Auth.Issuer` with `s.issuer`, `s.identity.Auth.ClientID` with
`s.clientID`, and `s.identity.Name` in the issuer-mismatch denial with
`s.namespace`. Remove the `identity` field. In `source_assertion.go`
replace `s.session.identity.Auth.CredentialRef` with `s.session.ref` and
the two `s.session.identity.Auth.*` references in `refusedGrant` with
`s.session.issuer` and `s.session.clientID`. Import `sdk/problem` in
`source_session.go`.

`assertionSource.mint` must also call `s.session.ensureSession()` before
taking the lock.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/auth/... -v 2>&1 | grep -E '^(--- FAIL|FAIL|ok)'`
Expected: all `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/auth
git commit -m "feat(auth): answer each product from its own session and establish a missing one on first use"
```

---

### Task 4: `wso2 login` establishes every product session

**Files:**
- Modify: `internal/app/login.go` (all of it below `login`)
- Modify: `internal/app/command.go` where `loginCommand` declares flags (search for `"client-id"`)
- Modify: `internal/app/login_create.go` if it calls `establishAndStore` (keep the call; the signature is unchanged)
- Create: `internal/app/login_products_test.go`

**Interfaces:**
- Consumes: `contexts.ProductAccess`, `Identity.Accesses`, `Identity.Access`.
- Produces:
  - `func (s Shell) establishProduct(selected contexts.Selection, access contexts.ProductAccess) (oauthflow.Result, error)`: runs one authorization (browser or device per the identity kind) at `access.Issuer` as `access.ClientID` for `access.Scopes` and `access.Resource`, and stores the session under `access.SessionRef` with `Strategy`, `ClientID`, `Scopes` recorded.
  - `loginFlags.only string` (`--only <namespace>`), `loginFlags.noProducts bool` (`--no-products`).
  - Report: after `Logged in to the %q context.`, one field per access: label the namespace (or `Session` for a bare login), value `<strategy>, established`.

- [ ] **Step 1: Write the failing tests**

`internal/app/login_products_test.go`:

```go
package app_test

import (
	"net/http"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
)

// thunderDoc is a resource-bound identity with two direct products and one
// federated product at a second issuer.
func thunderDoc(loginIssuer, productIssuer string) contexts.Document {
	document := browserDoc(loginIssuer)
	document.Identities[0].Auth.Provider = contexts.ProviderThunder
	document.Identities[0].Products = map[string]contexts.Product{
		"iam": {Endpoint: loginIssuer, Audience: "https://localhost:8090/mcp", Scopes: []string{"system"}},
		"gateway": {Endpoint: "https://gw.example", Audience: "http://localhost:18080/mockapi",
			Scopes: []string{"orders:read"}},
		"apim": {Endpoint: productIssuer, Audience: "apim-cli", Scopes: []string{"apim:api_view"},
			Grant: &contexts.Grant{Kind: contexts.GrantFederated, Issuer: productIssuer, ClientID: "apim-cli"}},
	}
	return document
}

// followBrowser answers every printed authorization URL by fetching it,
// as a browser holding the sign-on cookie would.
func followBrowser(shell *app.Shell) {
	shell.OpenBrowser = func(authURL string) error {
		go func() {
			if response, err := http.Get(authURL); err == nil {
				_ = response.Body.Close()
			}
		}()
		return nil
	}
}

func TestLoginEstablishesOneSessionPerProduct(t *testing.T) {
	keyring.MockInit()
	login := fakeissuer.New(t, fakeissuer.Options{RequireResource: true})
	product := fakeissuer.New(t, fakeissuer.Options{Audience: "apim-cli"})
	shell, out, errOut := newLoginShell(t)
	installLogin(t, shell, thunderDoc(login.URL, product.URL))
	followBrowser(&shell)

	if code := shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	store := session.Store{StateRoot: shell.StateRoot}
	bare, err := store.Load(credentialRef)
	if err != nil || bare.Strategy != contexts.StrategyDirect {
		t.Fatalf("login session %+v, %v", bare, err)
	}
	iam, err := store.Load(contexts.ProductSessionRef(credentialRef, "iam"))
	if err != nil || iam.Strategy != contexts.StrategySibling || iam.Issuer != login.URL {
		t.Fatalf("iam session %+v, %v", iam, err)
	}
	apim, err := store.Load(contexts.ProductSessionRef(credentialRef, "apim"))
	if err != nil || apim.Strategy != contexts.StrategyFederated || apim.Issuer != product.URL ||
		apim.ClientID != "apim-cli" {
		t.Fatalf("apim session %+v, %v", apim, err)
	}
	if _, err := store.Load(contexts.ProductSessionRef(credentialRef, "gateway")); err == nil {
		t.Fatal("the login product got a session of its own beside the login session")
	}
	for _, line := range []string{"gateway", "direct", "iam", "sibling", "apim", "federated"} {
		if !strings.Contains(out.String(), line) {
			t.Fatalf("report lacks %q:\n%s", line, out)
		}
	}
	if got := strings.Count(errOut.String(), "/authorize?"); got != 3 {
		t.Fatalf("printed %d authorization URLs, want 3:\n%s", got, errOut)
	}
}

func TestLoginOnlyEstablishesTheNamedProduct(t *testing.T) {
	keyring.MockInit()
	login := fakeissuer.New(t, fakeissuer.Options{RequireResource: true})
	product := fakeissuer.New(t, fakeissuer.Options{Audience: "apim-cli"})
	shell, _, errOut := newLoginShell(t)
	installLogin(t, shell, thunderDoc(login.URL, product.URL))
	followBrowser(&shell)
	if code := shell.Run([]string{"login", "--only", "iam"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	store := session.Store{StateRoot: shell.StateRoot}
	if _, err := store.Load(contexts.ProductSessionRef(credentialRef, "iam")); err != nil {
		t.Fatalf("iam session: %v", err)
	}
	if _, err := store.Load(credentialRef); err == nil {
		t.Fatal("--only iam established the login session too")
	}
}

func TestLoginNoProductsEstablishesTheLoginSessionAlone(t *testing.T) {
	keyring.MockInit()
	login := fakeissuer.New(t, fakeissuer.Options{RequireResource: true})
	product := fakeissuer.New(t, fakeissuer.Options{Audience: "apim-cli"})
	shell, _, errOut := newLoginShell(t)
	installLogin(t, shell, thunderDoc(login.URL, product.URL))
	followBrowser(&shell)
	if code := shell.Run([]string{"login", "--no-products"}); code != exit.OK {
		t.Fatalf("login failed: exit %d, stderr %s", code, errOut)
	}
	if got := strings.Count(errOut.String(), "/authorize?"); got != 1 {
		t.Fatalf("printed %d authorization URLs, want 1", got)
	}
}

func TestLoginOnlyRefusesAProductTheIdentityDoesNotRecord(t *testing.T) {
	keyring.MockInit()
	login := fakeissuer.New(t, fakeissuer.Options{RequireResource: true})
	shell, _, errOut := newLoginShell(t)
	installLogin(t, shell, thunderDoc(login.URL, login.URL))
	if code := shell.Run([]string{"login", "--only", "nothing"}); code != exit.Usage {
		t.Fatalf("exit %d, want usage; stderr %s", code, errOut)
	}
	if !strings.Contains(errOut.String(), "auth.product_not_configured") &&
		!strings.Contains(errOut.String(), "shell.invalid_argument") {
		t.Fatalf("stderr %s", errOut)
	}
}
```

Check `exit.Usage`'s actual name in `internal/exit` (it may be
`exit.Usage` or `exit.UsageError`) and the code `shell.invalid_argument`
in `internal/app/identity.go`; use `shell.invalid_argument` with
`problem.CategoryUsage`.

The fake issuer's `handleAuthorize` does not check `client_id` against a
registration and `exchangeCode` accepts whichever client presented the
code, so the second issuer needs no client setup.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/app/ -run 'LoginEstablishes|LoginOnly|LoginNoProducts' -v`
Expected: FAIL (unknown flag `--only`; one session only).

- [ ] **Step 3: Implement**

In `command.go`'s `loginCommand`, beside `--client-id`:

```go
	command.Flags().StringVar(&flags.only, "only", "", "Authorize only the named product.")
	command.Flags().BoolVar(&flags.noProducts, "no-products", false, "Authorize only the login session, not the products.")
```

and pass them through to `s.login(flags)`. Refuse `--only` together with
`--no-products` with `shell.conflicting_arguments`.

`login.go`: add `only string; noProducts bool` to `loginFlags`. Replace
`establishAndStore`'s body after the gates with:

```go
	accesses, err := s.loginAccesses(selected, flags)
	if err != nil {
		return oauthflow.Result{}, err
	}
	var first oauthflow.Result
	var established []contexts.ProductAccess
	for index, access := range accesses {
		result, err := s.establishProduct(selected, access)
		if err != nil {
			return oauthflow.Result{}, err
		}
		if index == 0 {
			first = result
		}
		established = append(established, access)
	}
	s.established = established   // NO: Shell methods take a value receiver.
```

Since `Shell` is passed by value, return the established accesses instead:
change the signature to
`func (s Shell) establishAndStore(selected contexts.Selection, flags loginFlags) (loginOutcome, error)`
with

```go
// loginOutcome is what one wso2 login established: the first authorization's
// verified identity and every access it stored a session for.
type loginOutcome struct {
	first       oauthflow.Result
	established []contexts.ProductAccess
}
```

and update `login`, `loginCreating` (in `login_create.go`) and
`reportLogin` to take `loginOutcome`. `reportLogin` appends one field per
established access after `Products`:

```go
	for _, access := range outcome.established {
		label := "Session"
		if access.Namespace != "" {
			label = access.Namespace
		}
		fields = append(fields, [2]string{label, access.Strategy + ", established"})
	}
```

`loginAccesses`:

```go
// loginAccesses is what this login authorizes: every session by default,
// the login session alone under --no-products, one product under --only.
func (s Shell) loginAccesses(selected contexts.Selection, flags loginFlags) ([]contexts.ProductAccess, error) {
	switch {
	case flags.only != "":
		access, recorded := selected.Identity.Access(flags.only)
		if !recorded {
			return nil, problem.New(problem.CategoryUsage, "shell.invalid_argument",
				fmt.Sprintf("the %q identity records no %q product to authorize",
					selected.Identity.Name, flags.only)).
				WithRecovery("Name a product the identity records; wso2 identity list shows them.")
		}
		return []contexts.ProductAccess{access}, nil
	case flags.noProducts:
		return []contexts.ProductAccess{selected.Identity.LoginAccess()}, nil
	default:
		return selected.Identity.Accesses(), nil
	}
}
```

`establishProduct` is the old `establishAndStore` body from the debug line
onward, parametrized:

```go
func (s Shell) establishProduct(selected contexts.Selection, access contexts.ProductAccess) (oauthflow.Result, error) {
	root, err := s.stateRoot()
	if err != nil {
		return oauthflow.Result{}, err
	}
	s.log.Debug("starting a login",
		"context", selected.Context.Name, "product", access.Namespace, "strategy", access.Strategy,
		"grant_kind", selected.Identity.Auth.Kind, "issuer", access.Issuer, "client_id", access.ClientID,
		"scopes", strings.Join(access.Scopes, " "), "resource", access.Resource)
	result, err := s.establishSession(selected, access)
	if err != nil {
		return oauthflow.Result{}, err
	}
	if result.Token.RefreshToken == "" { ... unchanged refusal ... }
	s.log.Debug("the login completed", "subject", result.Subject,
		"access_expires_at", result.Token.Expiry.UTC().Format(time.RFC3339), "credential_ref", access.SessionRef)
	store := session.Store{StateRoot: root}
	sessionExpiresAt := ... unchanged ...
	err = store.WithLock(access.SessionRef, func() error {
		return store.Save(access.SessionRef, session.Session{
			Issuer: access.Issuer, RefreshToken: result.Token.RefreshToken,
			AccessToken: result.Token.AccessToken, ExpiresAt: result.Token.Expiry.UTC(),
			Subject: result.Subject, SessionExpiresAt: sessionExpiresAt,
			Strategy: access.Strategy, ClientID: access.ClientID, Scopes: access.Scopes,
		})
	})
	if err != nil {
		return oauthflow.Result{}, err
	}
	return result, nil
}
```

`establishSession(selected, access)` uses `access.Issuer`, `access.ClientID`,
`access.Scopes`, `access.Resource` in place of the identity fields and
`productScopeUnion`/`productResource`. Delete `productScopeUnion` and
`productResource` and their tests (`login_resource_test.go` asserts the
resource is sent: rewrite its assertion against `LoginAccess().Resource`,
or keep it if it only inspects the printed URL). Keep `productNamespaces`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/app/ -run 'Login' -v 2>&1 | grep -E '^(--- FAIL|FAIL|ok|PASS)'`
Expected: PASS. Then `go test ./internal/app/` for the whole package.

- [ ] **Step 5: Commit**

```bash
git add internal/app
git commit -m "feat(login): establish one session per product, with --only and --no-products"
```

---

### Task 5: A command acquires a missing product session on first use

**Files:**
- Modify: `internal/app/invoke.go:130-145`
- Modify: `internal/app/invoke_test.go` (one test)

**Interfaces:**
- Consumes: `Broker.EstablishSession`, `Shell.establishProduct`, `Shell.nonInteractiveControl`, `auth.SessionRequired`.

- [ ] **Step 1: Write the failing test**

In `internal/app/invoke_test.go`, find the existing test that invokes an
installed module against a browser identity with a seeded session (search
for `SeedSession` or `browserDoc`). Add:

```go
func TestAProductCommandAcquiresItsOwnSessionOnFirstUse(t *testing.T) {
	keyring.MockInit()
	login := fakeissuer.New(t, fakeissuer.Options{RequireResource: true})
	shell, _, errOut := newShell(t)
	t.Setenv("WSO2_NO_INPUT", "")
	document := thunderDoc(login.URL, login.URL)
	installLogin(t, shell, document)
	installFixture(t, shell, referenceModuleFixture()) // the fixture the other invoke tests install
	// The login session exists for the login product; the module's product
	// (iam) has none.
	seeded := login.SeedSessionFor([]string{"orders:read"}, "http://localhost:18080/mockapi")
	if err := (session.Store{StateRoot: shell.StateRoot}).Save(credentialRef,
		session.Session{Issuer: login.URL, RefreshToken: seeded}); err != nil {
		t.Fatal(err)
	}
	followBrowser(&shell)
	// Invoke the module command that asks for the iam audience and scope.
	code := shell.Run([]string{"iam", "status"})
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(errOut.String(), `The "iam" product has no session yet`) {
		t.Fatalf("no notice before the browser opened:\n%s", errOut)
	}
	if _, err := (session.Store{StateRoot: shell.StateRoot}).Load(contexts.ProductSessionRef(credentialRef, "iam")); err != nil {
		t.Fatalf("iam session not stored: %v", err)
	}
}

func TestAProductCommandRefusesToOpenABrowserUnderNoInput(t *testing.T) {
	// Same setup, with WSO2_NO_INPUT=1 and OpenBrowser failing the test if reached.
	// Expect exit.AuthPolicy and "auth.session_required" naming "wso2 login --only iam" on stderr.
}
```

Adapt the module fixture: the existing invoke tests install a fixture
module whose receipt declares the reference audience and scope. Make a
fixture (or reuse `fixture.Module` with edited capabilities) that declares
audience `https://localhost:8090/mcp` and scope `system` under namespace
`iam`, and whose command asks the broker for them. Read
`internal/contexts/fixture` and `internal/app/invoke_test.go`'s first test
to see how the fixture module and its `status` command are built; copy that
pattern with the iam values.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/app/ -run 'AcquiresItsOwnSession|RefusesToOpenABrowser' -v`
Expected: FAIL with `auth.session_required` on the first, and the second
failing because no notice/refusal is produced yet (or passing trivially:
verify it fails before the change by asserting the notice text).

- [ ] **Step 3: Implement**

In `invoke.go`, after constructing the `auth.Broker`:

```go
	broker.EstablishSession = func(access contexts.ProductAccess) error {
		if control := s.nonInteractiveControl(false); control != "" {
			refusal := auth.SessionRequired(namespace)
			refusal.Guidance = fmt.Sprintf("Run wso2 login --only %s before this command; %s asked that no browser open.",
				namespace, control)
			return refusal
		}
		if _, err := fmt.Fprintf(s.Streams.Err,
			"The %q product has no session yet. Opening the browser to authorize it at %s.\n",
			namespace, access.Issuer); err != nil {
			return err
		}
		_, err := s.establishProduct(selection, access)
		return err
	}
```

(bind the broker to a variable before putting it in the `Launcher`). Add
`"strategy", productStrategy(selection.Identity, namespace)` to the
"brokering module access" debug line, where `productStrategy` reads
`identity.Access(namespace)` and returns its `Strategy` or `""`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/app/ -run 'Invoke|AcquiresItsOwnSession|RefusesToOpenABrowser' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app
git commit -m "feat(invoke): authorize a product on first use, and refuse to under no-input"
```

---

### Task 6: `whoami`, `doctor` and `logout` act per product; client-credentials identities are healthy

**Files:**
- Modify: `internal/app/whoami.go`, `internal/app/doctor.go:175-225`, `internal/app/logout.go`, `internal/app/sessionkind.go:98-110`
- Create: `internal/app/whoami_products_test.go`, `internal/app/doctor_products_test.go`, `internal/app/logout_products_test.go`
- Modify: `internal/app/logout_test.go` if a test pins the client-credentials refusal (`auth.logout_not_required`); `internal/app/whoami_test.go` / `doctor_test.go` if one pins the client-credentials "no session" outcome.

**Interfaces:**
- Produces:
  - `whoamiReport.Products []whoamiProduct` (json `products`), `whoamiProduct{Namespace, Strategy, Session, SessionExpiry string}` (json `namespace`, `strategy`, `session`, `sessionExpiry`); table field `Products` rendering `iam: sibling, present; apim: federated, none`.
  - `const whoamiSessionInline = "inline"`: a client-credentials identity reports `Session: inline`, no recovery, and each product `inline`.
  - doctor session check: passes when every access with a session ref has one stored; fails naming the missing products and `wso2 login --only <ns>`; not applicable for a client-credentials identity (`"the selected context acquires access inline and holds no session"`).
  - logout: revokes and deletes every session of the identity; result gains `productSessions` (label `Product sessions`) as `iam ended, apim ended` or `none`; a client-credentials identity exits 0 with `Session none` and `Revocation not-attempted`.

- [ ] **Step 1: Write the failing tests**

`internal/app/whoami_products_test.go`:

```go
package app_test

import (
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
)

func TestWhoamiReportsEveryProductSession(t *testing.T) {
	keyring.MockInit()
	shell, out, errOut := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	installLogin(t, shell, thunderDoc("http://login.example", "http://apim.example"))
	store := session.Store{StateRoot: shell.StateRoot}
	if err := store.Save(credentialRef, session.Session{Issuer: "http://login.example", RefreshToken: "rt", Subject: "user-1"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(contexts.ProductSessionRef(credentialRef, "iam"),
		session.Session{Issuer: "http://login.example", RefreshToken: "rt2", Strategy: contexts.StrategySibling}); err != nil {
		t.Fatal(err)
	}
	if code := shell.Run([]string{"whoami"}); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, want := range []string{"gateway: direct, present", "iam: sibling, present", "apim: federated, none"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	out.Reset()
	if code := shell.Run([]string{"whoami", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	report := decodeWhoamiReport(t, out.Bytes())
	if len(report.Products) != 3 || report.Products[0].Namespace != "apim" || report.Products[0].Session != "none" {
		t.Fatalf("products %+v", report.Products)
	}
}

func TestWhoamiReportsAClientCredentialsIdentityAsInline(t *testing.T) {
	keyring.MockInit()
	shell, out, errOut := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	installLogin(t, shell, identityDoc(contexts.KindClientCredentials)("http://login.example"))
	if code := shell.Run([]string{"whoami"}); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out.String(), "inline") || strings.Contains(out.String(), "wso2 login") {
		t.Fatalf("a client-credentials identity was told to log in:\n%s", out)
	}
}
```

Extend the test-side `whoamiReport` struct in `whoami_test.go` with
`Products []struct{ Namespace, Strategy, Session, SessionExpiry string }`.

`internal/app/doctor_products_test.go`:

```go
func TestDoctorFailsTheSessionCheckNamingTheProductsWithoutOne(t *testing.T) {
	keyring.MockInit()
	shell, out, _ := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	installLogin(t, shell, thunderDoc("http://login.example", "http://apim.example"))
	store := session.Store{StateRoot: shell.StateRoot}
	if err := store.Save(credentialRef, session.Session{Issuer: "http://login.example", RefreshToken: "rt"}); err != nil {
		t.Fatal(err)
	}
	code := shell.Run([]string{"doctor", "--output", "json"})
	if code == exit.OK {
		t.Fatal("doctor passed with two products lacking sessions")
	}
	finding := decodeDoctorReport(t, out.Bytes()).findingFor(t, "session")
	if finding.Status != "fail" || !strings.Contains(finding.Detail, "apim") || !strings.Contains(finding.Detail, "iam") ||
		!strings.Contains(finding.Detail, "wso2 login --only") {
		t.Fatalf("finding %+v", finding)
	}
}

func TestDoctorTreatsAClientCredentialsIdentityAsHealthyWithoutASession(t *testing.T) {
	keyring.MockInit()
	shell, out, errOut := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	installLogin(t, shell, identityDoc(contexts.KindClientCredentials)("http://login.example"))
	if code := shell.Run([]string{"doctor", "--output", "json"}); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if finding := decodeDoctorReport(t, out.Bytes()).findingFor(t, "session"); finding.Status != "not-applicable" {
		t.Fatalf("finding %+v", finding)
	}
}
```

(Use the field names `doctorReport.findingFor` actually returns; read
`doctor_test.go:52-68`.)

`internal/app/logout_products_test.go`:

```go
func TestLogoutEndsEveryProductSession(t *testing.T) {
	keyring.MockInit()
	login := fakeissuer.New(t, fakeissuer.Options{RequireResource: true})
	product := fakeissuer.New(t, fakeissuer.Options{Audience: "apim-cli"})
	shell, out, errOut := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	installLogin(t, shell, thunderDoc(login.URL, product.URL))
	store := session.Store{StateRoot: shell.StateRoot}
	for ref, issuer := range map[string]string{
		credentialRef: login.URL,
		contexts.ProductSessionRef(credentialRef, "iam"):  login.URL,
		contexts.ProductSessionRef(credentialRef, "apim"): product.URL,
	} {
		if err := store.Save(ref, session.Session{Issuer: issuer, RefreshToken: "rt-" + ref}); err != nil {
			t.Fatal(err)
		}
	}
	if code := shell.Run([]string{"logout"}); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, ref := range []string{credentialRef, contexts.ProductSessionRef(credentialRef, "iam"),
		contexts.ProductSessionRef(credentialRef, "apim")} {
		if _, err := store.Load(ref); err == nil {
			t.Fatalf("%s survived logout", ref)
		}
	}
	if !strings.Contains(out.String(), "apim ended") || !strings.Contains(out.String(), "iam ended") {
		t.Fatalf("report:\n%s", out)
	}
}

func TestLogoutOnAClientCredentialsIdentityExitsCleanly(t *testing.T) {
	keyring.MockInit()
	shell, out, errOut := newShell(t)
	t.Setenv("WSO2_CONTEXT", "")
	installLogin(t, shell, identityDoc(contexts.KindClientCredentials)("http://login.example"))
	if code := shell.Run([]string{"logout"}); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out.String(), "none") {
		t.Fatalf("report:\n%s", out)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/app/ -run 'Products|ClientCredentialsIdentity' -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

`whoami.go`: after the existing bare-session read, build products:

```go
	if selected.Identity.Auth.Kind == contexts.KindClientCredentials {
		report.Session, report.SessionExpiry, report.Recovery = whoamiSessionInline, "", ""
		report.Subject = ""
	}
	for _, access := range selected.Identity.Accesses() {
		if access.Namespace == "" {
			continue
		}
		entry := whoamiProduct{Namespace: access.Namespace, Strategy: access.Strategy, Session: whoamiSessionNone}
		if access.Strategy == contexts.StrategyInline {
			entry.Session = whoamiSessionInline
		} else if stored, err := store.Load(access.SessionRef); err == nil {
			entry.Session, entry.SessionExpiry, _ = sessionExpiryState(stored, time.Now())
		} else if !isNoSession(err) {
			return err
		}
		report.Products = append(report.Products, entry)
	}
```

`Accesses` lists the login access first and then only accesses with a
different session ref, so a direct product sharing the login session is
not listed by `Accesses`; iterate `slices.Sorted(maps.Keys(Products))`
and call `Access(ns)` for each instead, so every product is shown. The
table field:

```go
func (w whoamiReport) productsField() string {
	if len(w.Products) == 0 {
		return "none configured"
	}
	parts := make([]string, 0, len(w.Products))
	for _, p := range w.Products {
		parts = append(parts, fmt.Sprintf("%s: %s, %s", p.Namespace, p.Strategy, p.Session))
	}
	return strings.Join(parts, "; ")
}
```

appended to `fields()` as `{"Products", w.productsField()}` before
`Recovery`. For a client-credentials identity do not load the bare session
(there is none) and do not set the login recovery.

`doctor.go` session check, default branch:

```go
		switch {
		case selected.Identity.Auth.Kind == contexts.KindClientCredentials:
			findings = append(findings, notApplicableFinding(checkSession,
				"the selected context acquires access inline and holds no session"))
		default:
			var missing []string
			for _, access := range selected.Identity.Accesses() {
				if _, err := store.Load(access.SessionRef); err != nil {
					if !isNoSession(err) {
						typed := doctorProblem(err)
						failures[checkSession] = typed
						findings = append(findings, failFinding(checkSession, typed))
						missing = nil
						break
					}
					name := access.Namespace
					if name == "" {
						name = "the login session"
					}
					missing = append(missing, name)
				}
			}
			if len(missing) > 0 {
				typed := problem.New(problem.CategoryAuthPolicy, "auth.login_required",
					"no stored session exists for "+strings.Join(missing, ", ")).
					WithRecovery("Run wso2 login to authorize every product, or wso2 login --only <product> for one.")
				failures[checkSession] = typed
				findings = append(findings, failFinding(checkSession, typed))
			} else if _, failed := failures[checkSession]; !failed {
				findings = append(findings, passFinding(checkSession,
					"a stored session exists for every product of the selected context"))
			}
		}
```

`logout.go`: for a client-credentials identity, skip the gate refusal
(`logoutKindGate.inline` returns nil: change the `kindGate.check` to call
`g.inline` only when it is non-nil, and set logout's `inline` to nil) and
report `Session none`, `Revocation not-attempted`, `Product sessions none`.
Otherwise loop over `selected.Identity.Accesses()`; for each, run the
existing lock/load/revoke/delete block with `access.SessionRef`,
`access.Issuer`, `access.ClientID`; collect `ended` per namespace. The
bare session's outcome fills the existing `session` and `revocation`
fields; product outcomes fill `productSessions`:

```go
	var products []string
	for _, access := range accesses[1:] {
		state := "none"
		if outcome.sessionEnded { state = "ended" }
		products = append(products, access.Namespace+" "+state)
	}
	// With("productSessions", "Product sessions", strings.Join(products, ", ") or "none")
```

Keep the debug lines per session.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/app/ 2>&1 | tail -3`
Expected: `ok`. Fix any existing test that pinned `auth.logout_not_required`
for a client-credentials identity (it now exits 0) or a whoami/doctor
"run wso2 login" outcome for one.

- [ ] **Step 5: Commit**

```bash
git add internal/app
git commit -m "feat(app): report and end sessions per product, and treat a machine identity as healthy without one"
```

---

### Task 7: A pipeline mints per product, with a product-level credential where needed

**Files:**
- Modify: `internal/auth/source.go` (`inlineSource`), `internal/auth/source_clientcred.go` (issuer and resource from the access plan)
- Modify: `internal/auth/source_clientcred_test.go`

**Interfaces:**
- Consumes: `Product.ClientIDVariable/ClientSecretVariable`, `Identity.Access` with `StrategyInline`.
- Produces: `clientCredentialsSource` gains `issuer string` and `resource string` fields replacing the `identity`-derived ones; `inlineSource` reads the product's variables when set.

- [ ] **Step 1: Write the failing tests**

Append to `internal/auth/source_clientcred_test.go` (follow the file's
existing helper for a client-credentials broker, likely
`clientCredentialsBroker(t, issuer)` with `broker.Credentials` stubbed):

```go
func TestAProductCredentialIsPresentedAtTheProductIssuer(t *testing.T) {
	login := fakeissuer.New(t, fakeissuer.Options{ClientSecret: "machine-secret", RequireResource: true})
	product := fakeissuer.New(t, fakeissuer.Options{ClientSecret: "apim-secret", Audience: "apim-cli"})
	broker := clientCredentialsBroker(t, login) // identity secret in WSO2_ACME_CLIENT_SECRET
	broker.Selection.Identity.Auth.Provider = contexts.ProviderThunder
	broker.Selection.Identity.Products["apim"] = contexts.Product{
		Endpoint: product.URL, Audience: "apim-cli", Scopes: []string{"apim:api_view"},
		Grant:            &contexts.Grant{Kind: contexts.GrantFederated, Issuer: product.URL, ClientID: "unused"},
		ClientIDVariable: "WSO2_APIM_CLIENT_ID", ClientSecretVariable: "WSO2_APIM_CLIENT_SECRET",
	}
	broker.Namespace = "apim"
	broker.Capabilities.AuthAudiences = []string{"apim-cli"}
	broker.Capabilities.AuthScopes = []string{"apim:api_view"}
	broker.Credentials = func(name string) (string, bool) {
		return map[string]string{
			"WSO2_ACME_CLIENT_SECRET": "machine-secret",
			"WSO2_APIM_CLIENT_ID":     "apim-machine",
			"WSO2_APIM_CLIENT_SECRET": "apim-secret",
		}[name], true
	}
	broker.HTTPClient = product.HTTPClient()
	grant, err := broker.Acquire(auth.Request{Audience: "apim-cli", Scopes: []string{"apim:api_view"}})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if active, _, _ := product.Introspect(t, grant.Token); !active {
		t.Fatal("the product issuer did not mint the token")
	}
}

func TestAMissingProductCredentialNamesItsVariable(t *testing.T) {
	// Same setup with WSO2_APIM_CLIENT_SECRET unset: expect a Denial with code
	// auth.credential_unavailable whose Guidance contains "WSO2_APIM_CLIENT_SECRET".
}

func TestAMachineIdentityMintsEachProductWithItsOwnResource(t *testing.T) {
	issuer := fakeissuer.New(t, fakeissuer.Options{ClientSecret: "machine-secret", RequireResource: true})
	broker := clientCredentialsBroker(t, issuer)
	broker.Selection.Identity.Auth.Provider = contexts.ProviderThunder
	broker.Selection.Identity.Products["iam"] = contexts.Product{
		Endpoint: issuer.URL, Audience: "https://localhost:8090/mcp", Scopes: []string{"system"}}
	broker.Namespace = "iam"
	broker.Capabilities.AuthAudiences = []string{"https://localhost:8090/mcp"}
	broker.Capabilities.AuthScopes = []string{"system"}
	grant, err := broker.Acquire(auth.Request{Audience: "https://localhost:8090/mcp", Scopes: []string{"system"}})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if _, scopes, audiences := issuer.Introspect(t, grant.Token); scopes[0] != "system" || audiences[0] != "https://localhost:8090/mcp" {
		t.Fatalf("scopes %v audiences %v", scopes, audiences)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/auth/ -run 'ProductCredential|EachProductWithItsOwnResource' -v`
Expected: the product-credential test fails (the identity's secret is
presented at the login issuer); the per-resource test may already pass.

- [ ] **Step 3: Implement**

`inlineSource`:

```go
func (b *Broker) inlineSource() (source, error) {
	access, _ := b.Selection.Identity.Access(b.Namespace)
	product := b.Selection.Identity.Products[b.Namespace]
	clientID := b.Selection.Identity.Auth.ClientID
	secretVariable := b.Selection.Identity.Auth.ClientSecretVariable
	if product.ClientSecretVariable != "" {
		id, err := b.namedSecret(product.ClientIDVariable, "the product's client id")
		if err != nil {
			return nil, err
		}
		clientID = id
		secretVariable = product.ClientSecretVariable
	} else if product.Grant != nil {
		return nil, denial("auth.kind_not_implemented",
			fmt.Sprintf("the %q product is reached through its own issuer, which does not accept this "+
				"identity's machine client", b.namespace()),
			"Record the product's own client credential on its entry with clientIdVariable and "+
				"clientSecretVariable, or select an interactive identity.")
	}
	secret, err := b.namedSecret(secretVariable, "the client secret")
	if err != nil {
		return nil, err
	}
	return clientCredentialsSource{
		namespace: b.namespace(), contextName: b.Selection.Context.Name,
		issuer: access.Issuer, clientID: clientID, resource: access.Resource, audience: access.Audience,
		secret: secret, secretVariable: secretVariable, client: b.httpClient(),
	}, nil
}
```

In `clientCredentialsSource` replace the `identity` field with `issuer,
clientID, resource string`; `mint` discovers `s.issuer`, sets
`form.Set("resource", s.resource)` when non-empty, and presents
`clientAuth{id: s.clientID, secret: s.secret}`. Remove the
`TestAClientCredentialsIdentityCannotUseAGrant` expectation in
`source_assertion_test.go` if it asserts the old wording; keep the code
`auth.kind_not_implemented`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/auth/... ./internal/app/ ./internal/contexts/`
Expected: `ok` for each.

- [ ] **Step 5: Commit**

```bash
git add internal/auth
git commit -m "feat(auth): mint a machine identity per product, with a product-level credential where recorded"
```

---

### Task 8: Documentation and the spec deviations

**Files:**
- Modify: `docs/reference/commands.md` (rows for `wso2 login`, `wso2 logout`, `wso2 whoami`, `wso2 doctor`, `wso2 identity add-product`)
- Modify: `docs/examples/authentication-contexts.md` section 1 ("One login is not one token") and section 2 (the `grant` and product credential members)
- Modify: `docs/superpowers/specs/2026-09-06-one-login-many-sessions-design.md` sections 4, 5, 6 and 8

- [ ] **Step 1: Update `commands.md`**

- `wso2 login`: add "Establishes the login session, then one session per further product the identity records, each through the same browser sign-on; `--only <namespace>` authorizes one product, `--no-products` only the login session. The report lists each session with its strategy."
- `wso2 logout`: "ends every session the identity holds, the login session and each product's".
- `wso2 whoami`: "lists each product with its strategy (`direct`, `sibling`, `derived`, `federated`, `inline`) and whether a session is stored; a client-credentials identity reports `inline` and no recovery".
- `wso2 doctor`: "the session check passes only when every product has a session, naming the ones without; not applicable for a client-credentials identity".
- `wso2 identity add-product`: "`--grant federated` with `--grant-issuer` and `--grant-client-id` records a product reached at its own issuer; `--grant-resource` names the resource indicator for the grant's session" (add these flags in the next plan; document only the document members here if the flags are not yet built: say "recorded in the document today; flags follow").
- Add `auth.session_required` to the problem-code list if the file has one.

- [ ] **Step 2: Update `authentication-contexts.md`**

Rewrite "One login is not one token" to state the new rule (one sign-on,
one session per product, strategies), and add to the shape in section 2:

```yaml
      <namespace>:
        endpoint: <url>
        audience: <resource id>
        scopes: [<scope>, ...]
        grant:                    # optional: how this product is reached when its issuer is not the login's
          kind: jwt-bearer | federated
          issuer: <url>
          clientId: <id>
          scopes: [<scope>, ...]  # jwt-bearer only: assertion scopes
          resource: <uri>         # resource indicator for the grant's session, when required
        clientIdVariable: <VAR>     # client-credentials identities only, both or neither
        clientSecretVariable: <VAR>
```

- [ ] **Step 3: Update the spec**

Section 4: state that `direct` and `sibling` are decided by whether the
product shares the login product's scope set and resource; that the
strategy follows from the record and there is no pin (remove the last
paragraph of section 6 about `strategy`); add `inline` for
client-credentials identities. Section 5: the entry is
`<credentialRef>.<namespace>`. Section 8: the derived-from-machine-token
route is withdrawn (spike result); a product with its own issuer under a
machine identity carries a product credential.

- [ ] **Step 4: Commit**

```bash
git add docs
git commit -m "docs: describe per-product sessions, strategies and the product credential"
```

---

## Self-review

**Spec coverage.** Section 3 (rule): Tasks 1, 3, 4. Section 4
(strategies): Task 1 decides them, Task 3 executes `direct`, `sibling`,
`federated`, keeps `derived`. Section 5 (sessions, login, first use,
logout, whoami, doctor, lock per key): Tasks 2, 4, 5, 6. Section 6
(`connect`, descriptors, bootstrap results, refusing login with no product
on ThunderID): **not in this plan**, except that the document already
refuses a ThunderID identity with no product (Task 1 keeps that rule); the
`connect` command and descriptors are the next plan. Section 7 (command
surface fixes): the client-credentials health commands are in Task 6;
empty-scope requests, `--no-input` passthrough and `org use` are the next
plan. Section 8 (CI): Tasks 1, 6, 7. Section 10 tests: covered per task;
the live matrix is a separate step after this plan.

**Placeholders.** Task 5's second test and Task 7's second test are
described rather than written in full: their bodies are the first test's
with the stated change and the stated assertion. Task 6's logout loop is
sketched; the implementer copies the existing lock/load/revoke/delete
block into the loop body.

**Type consistency.** `contexts.ProductAccess` fields and
`ProductSessionRef` are used identically in Tasks 1, 3, 4, 5, 6.
`Broker.EstablishSession func(contexts.ProductAccess) error` in Tasks 3
and 5. `auth.SessionRequired(namespace string) Denial` in Tasks 3 and 5.
`session.Session.Strategy/ClientID/Scopes` in Tasks 2, 4, 6.
`Shell.establishProduct(selected, access) (oauthflow.Result, error)` in
Tasks 4 and 5. `thunderDoc`, `followBrowser` defined in Task 4 and used
in Tasks 5 and 6.
