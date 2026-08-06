# `wso2 login` First Slice Implementation Plan

**Status:** Implemented. Tasks 1-12 are merged into `feature/login` through PRs
#24-#29, #33 and #34, and every step below is ticked. One definition-of-done
check is still open and says why in place: the live smoke run's scope check
cannot fail as written.

**Goal:** Implement `wso2 login` (browser Authorization Code + PKCE) and inline client-credentials acquisition, on a version-2 identities/contexts schema, with OS-keychain session storage and a token-source seam in the broker, so the reference module receives a real issuer-minted access token.

**Architecture:** The shell gains schema v2 (`identities` + contexts referencing them) with a compatibility read for the v1 architecture-proof documents. A new `internal/auth/session` package owns keychain persistence; a new `internal/auth/oauthflow` package owns the browser PKCE flow; `internal/auth` gains an unexported `source` seam resolved per identity kind (dev fixture, oauth-browser scoped-refresh, client-credentials). Every failure is a typed `auth_policy` problem with a stable code.

**Tech Stack:** Go 1.25, `golang.org/x/oauth2`, `github.com/coreos/go-oidc/v3`, `github.com/zalando/go-keyring`, `github.com/go-jose/go-jose/v4` (test JWT signing only).

**Spec:** the body of [issue #17](https://github.com/wso2/wso2-cli/issues/17) — read it before starting any task.

## Global Constraints

- Every new `.go` file starts with the repo's 16-line Apache license header (copy it verbatim from `internal/auth/auth.go` lines 1–15 plus the blank line).
- Module path `github.com/wso2/wso2-cli`; Go `1.25.0`.
- New direct dependencies are exactly: `golang.org/x/oauth2`, `github.com/coreos/go-oidc/v3`, `github.com/zalando/go-keyring`. `github.com/go-jose/go-jose/v4` may appear as a test dependency (it is already transitive via go-oidc).
- Loopback callback ports, in order: **10425, 10426, 10427, 10428**, path `/callback`, host `127.0.0.1`.
- Keychain service name: **`wso2-cli`**.
- Legal identity kinds: `oauth-browser`, `oauth-device`, `client-credentials`, `pat`. Only the first and third are implemented; the others refuse at use with `auth.kind_not_implemented`.
- Context selection order: `--context` flag → `WSO2_CONTEXT` env var → `defaultContext`. No namespace binding in this slice.
- Non-interactive mode: `--non-interactive` flag or non-empty `WSO2_NON_INTERACTIVE` env var.
- `WSO2_NO_BROWSER` (non-empty) suppresses the browser-open attempt; the URL is always printed either way.
- Secrets (tokens, secrets, credentials) never appear in: command-line arguments, context documents, problems, logs, module environment, or stdout/stderr. Tests assert this.
- Problem codes are stable API. New codes introduced by this plan: `auth.kind_not_implemented`, `auth.login_required`, `auth.login_not_required`, `auth.discovery_failed`, `auth.session_issuer_mismatch`, `auth.keyring_unavailable`, `auth.organization_switch_unsupported`, `auth.narrowing_unavailable`, `auth.non_interactive`, `auth.product_not_configured`. Existing codes (`auth.context_not_selected`, `auth.method_unsupported`, `auth.audience_not_declared`, `auth.scope_not_declared`, `auth.already_granted`, `auth.credential_unavailable`, `auth.namespace_not_brokered`, `contexts.*`) keep their meaning.
- All problems raised by auth code use `problem.CategoryAuthPolicy`; malformed-document problems use `problem.CategoryUsage` (matching `internal/contexts` today).
- Run `go test ./...` from the repo root; it must pass at the end of every task. Also run `go vet ./...` before each commit.
- Commit messages follow the repo's conventional style (`feat:`, `test:`, `docs:`, lower-case, imperative, no trailing period).

## File Structure

```
internal/contexts/contexts.go        MODIFY  document, load/decode/encode, selection (v2)
internal/contexts/identity.go        CREATE  Identity/IdentityAuth/Product types + validation
internal/contexts/legacy.go          CREATE  v1 compatibility read → synthetic identities
internal/contexts/fixture/           MODIFY  add v2 fixture writer next to the v1 one
internal/auth/session/session.go     CREATE  keychain blob + issuer check
internal/auth/session/lock.go        CREATE  advisory file lock for rotation safety
internal/auth/fakeissuer/fakeissuer.go CREATE test OIDC issuer (discovery/authorize/token/JWKS/introspect)
internal/auth/oauthflow/login.go     CREATE  browser PKCE flow (discovery, loopback, exchange)
internal/auth/oauthflow/browser.go   CREATE  best-effort browser opening
internal/auth/discovery.go           CREATE  token-endpoint discovery helper
internal/auth/claims.go              CREATE  unverified JWT payload claim extraction (aud, scope)
internal/auth/source.go              CREATE  source interface + kind resolution
internal/auth/source_dev.go          CREATE  dev fixture source (extracted from auth.go)
internal/auth/source_browser.go      CREATE  scoped-refresh source
internal/auth/source_clientcred.go   CREATE  client-credentials source
internal/auth/auth.go                MODIFY  Broker policy checks delegate to sources
internal/app/app.go                  MODIFY  register login builtin
internal/app/login.go                CREATE  wso2 login command
internal/app/invoke.go               MODIFY  selection (flag/env), v2 wiring into broker
test/acceptance/login_test.go        CREATE  in-process login + broker chain tests
test/smoke/login_smoke_test.go       CREATE  flag-gated live smoke (build tag)
test/smoke/asgardeo_empirical_test.go CREATE flag-gated empirical experiments (build tag)
docs/guides/login.md                 CREATE  walkthrough
docs/research/product-authentication-compatibility.md MODIFY record the seeded-client backend ask
Makefile (or new make target file)   MODIFY  smoke-login target
```

---

### Task 1: Schema v2 — identity types and validation

**Files:**
- Create: `internal/contexts/identity.go`
- Modify: `internal/contexts/contexts.go`
- Test: `internal/contexts/contexts_test.go` (extend)

**Interfaces:**
- Consumes: existing `problem` package, existing `namePattern`/`variablePattern`.
- Produces (later tasks rely on these exact names):

```go
const SchemaVersion = 2
const (
    KindOAuthBrowser      = "oauth-browser"
    KindOAuthDevice       = "oauth-device"
    KindClientCredentials = "client-credentials"
    KindPAT               = "pat"
)

type Document struct {
    SchemaVersion  int        `json:"schemaVersion"`
    DefaultContext string     `json:"defaultContext"`
    Identities     []Identity `json:"identities"`
    Contexts       []Context  `json:"contexts"`
}

type Identity struct {
    Name     string             `json:"name"`
    Type     string             `json:"type"` // "cloud" | "onprem"
    Auth     IdentityAuth       `json:"auth"`
    Products map[string]Product `json:"products,omitempty"`
    // synthetic marks an identity manufactured by the v1 compatibility read.
    // It is never encodable. (unexported)
    synthetic bool
}

type IdentityAuth struct {
    Kind                 string `json:"kind"`
    Issuer               string `json:"issuer,omitempty"`
    ClientID             string `json:"clientId,omitempty"`
    Tenant               string `json:"tenant,omitempty"`
    CredentialRef        string `json:"credentialRef,omitempty"`
    ClientSecretVariable string `json:"clientSecretVariable,omitempty"`
    // CredentialVariable exists only on synthetic v1 identities. Never encoded.
    CredentialVariable string `json:"-"`
}

type Product struct {
    Endpoint string   `json:"endpoint"`
    Audience string   `json:"audience,omitempty"`
    Scopes   []string `json:"scopes,omitempty"`
}

type Context struct {
    Name         string `json:"name"`
    Identity     string `json:"identity"`
    Organization string `json:"organization,omitempty"`
    Project      string `json:"project,omitempty"`
}

func (i Identity) Synthetic() bool
```

The v1 `Auth`, `MethodDevelopmentCredential`, and the old `Context` fields (`OrganizationID`, `Endpoint`, `Auth`) are **removed from the exported surface** in this task's end state — Task 2 restores v1 *reading* via a legacy decode path, and Task 3/8 update the callers. Until Task 8 lands the tree will not compile green between tasks unless you follow the step order below, which keeps a temporary shim.

- [x] **Step 1: Write failing tests for v2 decode and validation**

Add to `internal/contexts/contexts_test.go` (follow the existing test style in that file — read it first):

```go
func validV2() string {
    return `{
  "schemaVersion": 2,
  "defaultContext": "acme-dev",
  "identities": [
    {
      "name": "acme-cloud",
      "type": "cloud",
      "auth": {
        "kind": "oauth-browser",
        "issuer": "https://issuer.example.test/t/acme/oauth2/token",
        "clientId": "client-123",
        "tenant": "acme",
        "credentialRef": "acme-cloud-login"
      },
      "products": {
        "reference": {
          "endpoint": "https://api.example.test",
          "audience": "reference-status",
          "scopes": ["reference:status:read"]
        }
      }
    }
  ],
  "contexts": [
    {"name": "acme-dev", "identity": "acme-cloud", "organization": "acme"}
  ]
}`
}

func TestDecodeV2(t *testing.T) {
    document, err := contexts.Decode([]byte(validV2()))
    if err != nil {
        t.Fatalf("decode: %v", err)
    }
    if len(document.Identities) != 1 || document.Identities[0].Auth.Kind != contexts.KindOAuthBrowser {
        t.Fatalf("identity not decoded: %+v", document.Identities)
    }
    if document.Contexts[0].Identity != "acme-cloud" {
        t.Fatalf("context does not reference its identity: %+v", document.Contexts[0])
    }
}

func TestValidateV2(t *testing.T) {
    cases := []struct {
        name    string
        mutate  func(doc string) string
        code    string // expected problem code, "" for valid
    }{
        {"unknown kind rejected", replace(`"kind": "oauth-browser"`, `"kind": "password"`), "contexts.document_malformed"},
        {"context referencing unknown identity", replace(`"identity": "acme-cloud"`, `"identity": "ghost"`), "contexts.document_malformed"},
        {"credentialRef holding a JWT-shaped value", replace(`"credentialRef": "acme-cloud-login"`, `"credentialRef": "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJ4In0.c2ln"`), "contexts.document_malformed"},
        {"clientSecretVariable on browser kind", replace(`"credentialRef": "acme-cloud-login"`, `"clientSecretVariable": "MY_SECRET"`), "contexts.document_malformed"},
        {"missing issuer on browser kind", replace(`"issuer": "https://issuer.example.test/t/acme/oauth2/token",`, ``), "contexts.document_malformed"},
        {"missing clientId on browser kind", replace(`"clientId": "client-123",`, ``), "contexts.document_malformed"},
        {"product endpoint with embedded credentials", replace(`"endpoint": "https://api.example.test"`, `"endpoint": "https://user:pass@api.example.test"`), "contexts.document_malformed"},
        {"duplicate identity name", /* duplicate the identity object */ duplicateIdentity, "contexts.document_malformed"},
        {"invalid product namespace", replace(`"reference":`, `"Not A Namespace!":`), "contexts.document_malformed"},
        {"invalid identity type", replace(`"type": "cloud"`, `"type": "hybrid"`), "contexts.document_malformed"},
    }
    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            _, err := contexts.Decode([]byte(testCase.mutate(validV2())))
            assertProblemCode(t, err, testCase.code)
        })
    }
}
```

Write the small helpers `replace(old, new string) func(string) string`, `duplicateIdentity(doc string) string`, and `assertProblemCode(t *testing.T, err error, code string)` (uses `errors.As` into `problem.Problem` and compares `.Code`) in the test file.

Also add a client-credentials validity test:

```go
func TestValidateClientCredentialsIdentity(t *testing.T) {
    doc := strings.NewReplacer(
        `"kind": "oauth-browser"`, `"kind": "client-credentials"`,
        `"credentialRef": "acme-cloud-login"`, `"clientSecretVariable": "WSO2_ACME_SECRET"`,
    ).Replace(validV2())
    if _, err := contexts.Decode([]byte(doc)); err != nil {
        t.Fatalf("client-credentials identity should validate: %v", err)
    }
    // A lowercase variable name is a value-shaped mistake, not a name.
    bad := strings.Replace(doc, `"clientSecretVariable": "WSO2_ACME_SECRET"`, `"clientSecretVariable": "actual-secret-value"`, 1)
    _, err := contexts.Decode([]byte(bad))
    assertProblemCode(t, err, "contexts.document_malformed")
}
```

- [x] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/contexts/ -run 'TestDecodeV2|TestValidateV2|TestValidateClientCredentials' -v`
Expected: FAIL — compile errors for the new identifiers (`KindOAuthBrowser`, `Identities`, …).

- [x] **Step 3: Implement the v2 types and validation**

Create `internal/contexts/identity.go` with the types from the Interfaces block plus validation:

```go
// legalKinds are the authentication kinds a v2 document may declare. Which of
// them this release implements is broker policy; a legal-but-unimplemented
// kind stays readable and is refused when a command needs access.
var legalKinds = map[string]bool{
    KindOAuthBrowser: true, KindOAuthDevice: true,
    KindClientCredentials: true, KindPAT: true,
}

// refPattern constrains a credential reference to one readable word, exactly
// as context names are constrained. A credential value pasted where a
// reference belongs — a JWT, anything with dots, equals signs, or upper-case
// runs — fails this pattern by construction and is rejected rather than stored.
var refPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

func (i Identity) Synthetic() bool { return i.synthetic }

func (i Identity) validate() error {
    if !namePattern.MatchString(i.Name) {
        return malformed(fmt.Sprintf("declares an invalid identity name %q", i.Name))
    }
    if i.Type != "cloud" && i.Type != "onprem" {
        return malformed(fmt.Sprintf("declares an identity type for %q that is neither cloud nor onprem", i.Name))
    }
    if err := i.Auth.validate(i.Name); err != nil {
        return err
    }
    for namespace, product := range i.Products {
        if !namePattern.MatchString(namespace) {
            return malformed(fmt.Sprintf("declares an invalid product namespace on the identity %q", i.Name))
        }
        if err := product.validate(i.Name); err != nil {
            return err
        }
    }
    return nil
}

func (a IdentityAuth) validate(identity string) error {
    if !legalKinds[a.Kind] {
        return malformed(fmt.Sprintf("declares an authentication kind for the identity %q that this shell does not read", identity))
    }
    switch a.Kind {
    case KindOAuthBrowser, KindOAuthDevice:
        if a.Issuer == "" || a.ClientID == "" {
            return malformed(fmt.Sprintf("declares the interactive identity %q without an issuer and client identifier", identity))
        }
        if a.CredentialRef == "" || !refPattern.MatchString(a.CredentialRef) {
            // The rejected value is not echoed: what was pasted where a
            // reference belongs may be a credential.
            return contextProblem("contexts.document_malformed",
                fmt.Sprintf("the identity %q does not name a secure-store reference as its credential source", identity),
                "Name the secure-store entry, not a credential value. A reference is one lower-case word.")
        }
        if a.ClientSecretVariable != "" {
            return malformed(fmt.Sprintf("declares a client secret source on the interactive identity %q", identity))
        }
    case KindClientCredentials:
        if a.Issuer == "" || a.ClientID == "" {
            return malformed(fmt.Sprintf("declares the identity %q without an issuer and client identifier", identity))
        }
        if a.ClientSecretVariable == "" || !variablePattern.MatchString(a.ClientSecretVariable) {
            return contextProblem("contexts.document_malformed",
                fmt.Sprintf("the identity %q does not name an environment variable as its client secret source", identity),
                "Name the environment variable holding the client secret, not the secret itself.")
        }
        if a.CredentialRef != "" {
            return malformed(fmt.Sprintf("declares a secure-store reference on the non-interactive identity %q", identity))
        }
    case KindPAT:
        if a.CredentialRef == "" || !refPattern.MatchString(a.CredentialRef) {
            return contextProblem("contexts.document_malformed",
                fmt.Sprintf("the identity %q does not name a secure-store reference as its credential source", identity),
                "Name the secure-store entry, not a credential value. A reference is one lower-case word.")
        }
    }
    // The issuer URL, like an endpoint, may not embed user information.
    if a.Issuer != "" {
        parsed, err := url.Parse(a.Issuer)
        if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
            return malformed(fmt.Sprintf("declares an issuer for the identity %q that this shell cannot read", identity))
        }
        if parsed.User != nil {
            return malformed(fmt.Sprintf("declares an issuer for the identity %q that embeds credentials in its URL", identity))
        }
    }
    return nil
}

func (p Product) validate(identity string) error {
    // Same endpoint rules the v1 context enforced, including the
    // credentials-in-URL rejection. Reuse the same message shapes.
    if p.Endpoint == "" {
        return malformed(fmt.Sprintf("declares a product without an endpoint on the identity %q", identity))
    }
    parsed, err := url.Parse(p.Endpoint)
    if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
        return malformed(fmt.Sprintf("declares a product endpoint on the identity %q that this shell cannot read", identity))
    }
    if parsed.User != nil {
        return contextProblem("contexts.document_malformed",
            fmt.Sprintf("a product endpoint on the identity %q embeds credentials in its URL", identity),
            "Remove the user information from the endpoint. A context names a credential source; it never carries a credential.")
    }
    return nil
}
```

In `contexts.go`: change `SchemaVersion` to `2`, replace the `Context` struct with the v2 shape, add `Identities []Identity` to `Document`, and rewrite `Document.validate()`:

```go
func (d Document) validate() error {
    if d.SchemaVersion != SchemaVersion {
        return contextProblem("contexts.schema_unsupported", ...) // unchanged message
    }
    identities := make(map[string]struct{}, len(d.Identities))
    for _, identity := range d.Identities {
        if _, duplicate := identities[identity.Name]; duplicate {
            return malformed(fmt.Sprintf("declares the identity %q more than once", identity.Name))
        }
        identities[identity.Name] = struct{}{}
        if err := identity.validate(); err != nil {
            return err
        }
    }
    seen := make(map[string]struct{}, len(d.Contexts))
    for _, candidate := range d.Contexts {
        if !namePattern.MatchString(candidate.Name) {
            return malformed(fmt.Sprintf("declares an invalid context name %q", candidate.Name))
        }
        if _, duplicate := seen[candidate.Name]; duplicate {
            return malformed(fmt.Sprintf("declares the context %q more than once", candidate.Name))
        }
        seen[candidate.Name] = struct{}{}
        if _, found := identities[candidate.Identity]; !found {
            return malformed(fmt.Sprintf("the context %q references the identity %q, which the document does not declare",
                candidate.Name, candidate.Identity))
        }
    }
    if len(d.Contexts) == 0 {
        return nil
    }
    if _, found := seen[d.DefaultContext]; !found {
        return malformed(fmt.Sprintf("selects the context %q, which it does not declare", d.DefaultContext))
    }
    return nil
}
```

Temporarily keep `MethodDevelopmentCredential` (Task 2 uses it) and add a **temporary shim** so `internal/auth` and `internal/app` still compile: keep the old `Auth` type and add back-compat accessor fields nowhere — instead, expect compile breakage in `internal/auth`/`internal/app`/fixtures and fix it *minimally* in this task by updating those call sites to the v2 shape with an empty-identity behavior (the broker keeps refusing as before; its real v2 policy lands in Task 8). Follow the compiler. Keep the acceptance suite green by updating `internal/contexts/fixture` to emit a v2 document:

```go
// fixture writes the v2 equivalent of the old proof context: one synthetic-free
// document whose identity carries the development-credential kind is not legal
// in v2, so the fixture writes a v1 document and relies on the compatibility
// read (Task 2). Until Task 2 lands, keep the fixture emitting v1 and skip the
// acceptance suite locally with -run if needed — the suite must be green again
// at the end of Task 2.
```

(That comment is the actual guidance: **Task 1 and Task 2 are one commit window** — do not commit between them if the acceptance suite is red; commit after Task 2 makes it green. Unit tests inside `internal/contexts` must pass at the end of Task 1.)

- [x] **Step 4: Run the package tests**

Run: `go test ./internal/contexts/ -v`
Expected: new tests PASS; pre-existing v1-shaped tests now FAIL — rewrite those v1 unit tests to the v2 shape (same properties: trailing-document refusal, unknown-member tolerance, endpoint credential rejection now on products, name patterns). The v1 *document* tests move to Task 2's legacy tests.

- [x] **Step 5: Do not commit yet** — proceed straight to Task 2 (they share one commit window because the fixture and acceptance suite straddle both).

---

### Task 2: v1 compatibility read and synthetic identities

**Files:**
- Create: `internal/contexts/legacy.go`
- Modify: `internal/contexts/contexts.go` (decode dispatch, encode refusal)
- Modify: `internal/contexts/fixture/` (keep v1 writer, add v2 writer)
- Test: `internal/contexts/legacy_test.go`

**Interfaces:**
- Produces:

```go
// In legacy.go (all unexported except the constant):
const SchemaVersionLegacy = 1
// decodeLegacy maps a v1 document onto the v2 in-memory shape.
func decodeLegacy(data []byte) (Document, error)
```

Mapping, exactly: each v1 context `{name, organizationId, endpoint, auth{method, credentialVariable}}` becomes

- `Identity{Name: <context name>, Type: "onprem", Auth: IdentityAuth{Kind: <v1 method>, CredentialVariable: <v1 credentialVariable>}, Products: {"reference": {Endpoint: <v1 endpoint>}}, synthetic: true}` (omit the `products` entry entirely when the v1 endpoint is empty), and
- `Context{Name: <context name>, Identity: <context name>, Organization: <v1 organizationId>}`.

`DefaultContext` carries over. The v1 validation rules (name pattern, variable pattern, endpoint credential rejection, duplicate names, default-names-a-context) are enforced on the legacy document with the same problem codes as today.

- [x] **Step 1: Write failing tests**

`internal/contexts/legacy_test.go`:

```go
func validV1() string {
    return `{
  "schemaVersion": 1,
  "defaultContext": "reference-local",
  "contexts": [
    {
      "name": "reference-local",
      "organizationId": "reference-org",
      "endpoint": "https://service.example.test",
      "auth": {"method": "development-credential", "credentialVariable": "WSO2_REFERENCE_DEV_CREDENTIAL"}
    }
  ]
}`
}

func TestLegacyReadMapsToSyntheticIdentity(t *testing.T) {
    document, err := contexts.Decode([]byte(validV1()))
    if err != nil {
        t.Fatalf("legacy decode: %v", err)
    }
    selection, err := document.Select("")
    if err != nil {
        t.Fatalf("select: %v", err)
    }
    identity := selection.Identity
    if !identity.Synthetic() {
        t.Fatal("v1 identity must be marked synthetic")
    }
    if identity.Auth.Kind != contexts.MethodDevelopmentCredential {
        t.Fatalf("kind = %q", identity.Auth.Kind)
    }
    if identity.Auth.CredentialVariable != "WSO2_REFERENCE_DEV_CREDENTIAL" {
        t.Fatalf("credential variable = %q", identity.Auth.CredentialVariable)
    }
    if identity.Products["reference"].Endpoint != "https://service.example.test" {
        t.Fatalf("products = %+v", identity.Products)
    }
    if selection.Context.Organization != "reference-org" {
        t.Fatalf("organization = %q", selection.Context.Organization)
    }
}

func TestSyntheticIdentityIsNotEncodable(t *testing.T) {
    document, _ := contexts.Decode([]byte(validV1()))
    if _, err := document.Encode(); err == nil {
        t.Fatal("encoding a compatibility document must fail: no automatic rewrite")
    }
}

func TestUnknownSchemaVersionFailsClosed(t *testing.T) {
    _, err := contexts.Decode([]byte(`{"schemaVersion": 3}`))
    assertProblemCode(t, err, "contexts.schema_unsupported")
}
```

Note this introduces `Document.Select(name string) (Selection, error)` — declared here, implemented in Task 3. For this task, add the minimal form: `Select("")` returns the default context and its identity (empty selection when no contexts exist, exactly like today's `Selected`).

- [x] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/contexts/ -run 'TestLegacy|TestSynthetic|TestUnknownSchema' -v`
Expected: FAIL (v1 decodes rejected by v2 validation; `Select` undefined).

- [x] **Step 3: Implement**

In `contexts.go`, dispatch inside `Decode` on a version probe:

```go
func Decode(data []byte) (Document, error) {
    var probe struct {
        SchemaVersion int `json:"schemaVersion"`
    }
    if err := json.Unmarshal(data, &probe); err != nil {
        return Document{}, malformed("is not valid JSON")
    }
    switch probe.SchemaVersion {
    case SchemaVersionLegacy:
        return decodeLegacy(data)
    case SchemaVersion:
        return decodeCurrent(data) // the existing strict single-document decode + validate
    default:
        return Document{}, contextProblem("contexts.schema_unsupported",
            fmt.Sprintf("context document schema version %d is not supported by this shell", probe.SchemaVersion),
            "Update the WSO2 CLI, or write a context document this shell owns.")
    }
}
```

`legacy.go` declares private mirror structs of the v1 shape (`legacyDocument`, `legacyContext`, `legacyAuth` — copy the old field names and JSON tags exactly), performs the old validation (copy the old `validate` logic for those structs, same problem messages), then maps to the v2 `Document` per the Interfaces block. Only `MethodDevelopmentCredential` is *eligible*: any other v1 method still maps (readable document) — the broker refuses it at use, which Task 8 covers.

`Encode` refuses synthetic content before validating:

```go
func (d Document) Encode() ([]byte, error) {
    for _, identity := range d.Identities {
        if identity.synthetic {
            return nil, contextProblem("contexts.document_malformed",
                "a compatibility-read context document cannot be written back",
                "Author a schema version 2 document. The shell does not rewrite version 1 documents in place.")
        }
    }
    if err := d.validate(); err != nil { ... } // unchanged
}
```

Add `Selection` and minimal `Select`:

```go
// Selection is one resolved context together with its identity.
type Selection struct {
    Context  Context
    Identity Identity
}

func (d Document) Select(name string) (Selection, error) {
    if len(d.Contexts) == 0 {
        if name != "" {
            return Selection{}, contextProblem("contexts.unknown_context",
                fmt.Sprintf("no context named %q is configured", name),
                "Select a configured context, or remove the context document to run without one.")
        }
        return Selection{Context: Context{Name: DefaultName}}, nil
    }
    wanted := name
    if wanted == "" {
        wanted = d.DefaultContext
    }
    for _, candidate := range d.Contexts {
        if candidate.Name == wanted {
            return Selection{Context: candidate, Identity: d.identity(candidate.Identity)}, nil
        }
    }
    return Selection{}, contextProblem("contexts.unknown_context",
        fmt.Sprintf("no context named %q is configured", wanted),
        "Select a configured context, or remove the context document to run without one.")
}

func (d Document) identity(name string) Identity {
    for _, candidate := range d.Identities {
        if candidate.Name == name {
            return candidate
        }
    }
    return Identity{} // unreachable for a validated document
}
```

Delete the old `Selected` method and update its callers to `Select("")`. Update `internal/auth` and `internal/app` call sites minimally (broker holds a `contexts.Selection`; its checks read `Selection.Identity.Auth.Kind`, `Selection.Identity.Auth.CredentialVariable`, `Selection.Context.Organization` — behavior identical to today for the dev path). Update `internal/contexts/fixture` so the existing v1 writer still writes v1 JSON (acceptance suite exercises the compatibility read from now on), and add a v2 writer used by later tasks:

```go
// WriteV2 writes a schema version 2 document into the state root.
func WriteV2(stateRoot string, document contexts.Document) error
```

- [x] **Step 4: Run the full suite**

Run: `go test ./...`
Expected: PASS everywhere, including the acceptance suite (which now exercises v1-compat read end to end). Fix expectations that referenced removed API.

- [x] **Step 5: Vet and commit (Tasks 1+2 together)**

```bash
go vet ./...
git add internal/contexts internal/auth internal/app test
git commit -m "feat: read identities as context schema v2 with a v1 compatibility read"
```

---

### Task 3: Context selection — flag, environment, default

**Files:**
- Modify: `internal/app/invoke.go` (parseProductArgs, selectedContext)
- Modify: `internal/app/app.go` (thread the flag value)
- Test: `internal/app/invoke_test.go` (extend)

**Interfaces:**
- Consumes: `contexts.Document.Select(name string) (contexts.Selection, error)` from Task 2.
- Produces:

```go
// parseProductArgs gains --context handling and returns the context name:
func parseProductArgs(namespace string, args []string) (command, arguments []string, mode output.Mode, contextName string, err error)

// selection resolves flag → WSO2_CONTEXT → default.
func (s Shell) selection(flagName string) (contexts.Selection, error)
```

- [x] **Step 1: Write failing tests**

In `internal/app/invoke_test.go` (read the existing tests first and follow their harness style):

```go
func TestContextSelectionOrder(t *testing.T) {
    // A document with two contexts, default = "first".
    // Three-source resolution, most specific wins:
    cases := []struct {
        name     string
        args     []string // e.g. {"--context", "second"}
        env      string   // WSO2_CONTEXT value, "" unset
        expected string
    }{
        {"flag wins over env and default", []string{"--context", "second"}, "first", "second"},
        {"env wins over default", nil, "second", "second"},
        {"default when nothing is set", nil, "", "first"},
        {"flag with equals form", []string{"--context=second"}, "", "second"},
    }
    // assert via the selection the broker sees or via a --output json probe,
    // matching however the existing invoke tests observe the selected context.
}

func TestUnknownContextFlagIsTypedProblem(t *testing.T) {
    // --context ghost → problem code contexts.unknown_context, category usage.
}

func TestMissingContextFlagValue(t *testing.T) {
    // "--context" with nothing after it → shell.missing_flag_value.
}
```

- [x] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/app/ -run TestContext -v`
Expected: FAIL.

- [x] **Step 3: Implement**

In `parseProductArgs`, add cases alongside `--output` (same shape) for `--context <name>` and `--context=<name>`, returning the name. In `invoke.go`:

```go
func (s Shell) selection(flagName string) (contexts.Selection, error) {
    name := flagName
    if name == "" {
        name = os.Getenv("WSO2_CONTEXT")
    }
    root, err := s.stateRoot()
    if err != nil {
        return contexts.Selection{}, err
    }
    document, err := contexts.Load(root)
    if err != nil {
        return contexts.Selection{}, err
    }
    return document.Select(name)
}
```

`invokeModule` uses the returned `contextName` and passes the `Selection` into the broker and the endpoint from `selection.Identity.Products[namespace].Endpoint` into `rpc.InvocationContext`.

- [x] **Step 4: Run tests**

Run: `go test ./internal/app/ ./test/acceptance/ -v`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
go vet ./... && git add internal/app && git commit -m "feat: resolve the invocation context from flag, environment, then default"
```

---

### Task 4: Session store — keychain blob and rotation lock

**Files:**
- Create: `internal/auth/session/session.go`, `internal/auth/session/lock.go`
- Test: `internal/auth/session/session_test.go`

**Interfaces:**
- Produces:

```go
package session

// Session is one identity's interactive login state, stored as a single
// keychain entry. It exists only inside the shell and the OS secure store.
type Session struct {
    Issuer       string    `json:"issuer"`
    RefreshToken string    `json:"refreshToken"`
    AccessToken  string    `json:"accessToken,omitempty"`
    ExpiresAt    time.Time `json:"expiresAt,omitempty"`
}

// Store reads and writes sessions in the OS secure store.
type Store struct {
    // StateRoot hosts the advisory lock files, never session content.
    StateRoot string
}

const Service = "wso2-cli"

// Load returns the stored session for a credential reference.
// Missing entry → auth.login_required. Backend unavailable → auth.keyring_unavailable.
// Unreadable/undecodable entry → auth.login_required (stale entries are re-logged-in, not repaired).
func (s Store) Load(ref string) (Session, error)

// Save writes the session, replacing any previous entry.
func (s Store) Save(ref string, value Session) error

// WithLock runs fn while holding the per-reference advisory file lock.
// It is how refresh-token rotation stays single-writer across processes.
func (s Store) WithLock(ref string, fn func() error) error
```

Dependency step: `go get github.com/zalando/go-keyring@latest` (record the version go.mod picks).

- [x] **Step 1: Write failing tests**

```go
package session_test

import (
    "errors"
    "sync"
    "testing"
    "time"

    keyring "github.com/zalando/go-keyring"

    "github.com/wso2/wso2-cli/internal/auth/session"
    "github.com/wso2/wso2-cli/sdk/problem"
)

func TestSaveAndLoadRoundTrip(t *testing.T) {
    keyring.MockInit()
    store := session.Store{StateRoot: t.TempDir()}
    saved := session.Session{Issuer: "https://issuer.example.test", RefreshToken: "rt-1", AccessToken: "at-1", ExpiresAt: time.Now().Add(time.Hour).UTC()}
    if err := store.Save("acme-cloud-login", saved); err != nil {
        t.Fatalf("save: %v", err)
    }
    loaded, err := store.Load("acme-cloud-login")
    if err != nil {
        t.Fatalf("load: %v", err)
    }
    if loaded.RefreshToken != "rt-1" || loaded.Issuer != saved.Issuer {
        t.Fatalf("round trip lost data: %+v", loaded)
    }
}

func TestMissingEntryIsLoginRequired(t *testing.T) {
    keyring.MockInit()
    store := session.Store{StateRoot: t.TempDir()}
    _, err := store.Load("never-logged-in")
    var typed problem.Problem
    if !errors.As(err, &typed) || typed.Code != "auth.login_required" {
        t.Fatalf("expected auth.login_required, got %v", err)
    }
}

func TestKeyringUnavailableIsTyped(t *testing.T) {
    keyring.MockInitWithError(errors.New("no secret service"))
    store := session.Store{StateRoot: t.TempDir()}
    _, err := store.Load("acme-cloud-login")
    var typed problem.Problem
    if !errors.As(err, &typed) || typed.Code != "auth.keyring_unavailable" {
        t.Fatalf("expected auth.keyring_unavailable, got %v", err)
    }
}

func TestWithLockSerializesWriters(t *testing.T) {
    keyring.MockInit()
    store := session.Store{StateRoot: t.TempDir()}
    var inside, maxInside int32
    var group sync.WaitGroup
    for range 8 {
        group.Add(1)
        go func() {
            defer group.Done()
            _ = store.WithLock("acme-cloud-login", func() error {
                now := atomic.AddInt32(&inside, 1)
                if now > atomic.LoadInt32(&maxInside) {
                    atomic.StoreInt32(&maxInside, now)
                }
                time.Sleep(5 * time.Millisecond)
                atomic.AddInt32(&inside, -1)
                return nil
            })
        }()
    }
    group.Wait()
    if maxInside != 1 {
        t.Fatalf("lock admitted %d concurrent writers", maxInside)
    }
}
```

- [x] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/auth/session/ -v`
Expected: FAIL (package does not exist).

- [x] **Step 3: Implement**

`session.go`: JSON-encode the blob into `keyring.Set(Service, ref, string(data))`; decode on load. Error mapping:

```go
func (s Store) Load(ref string) (Session, error) {
    value, err := keyring.Get(Service, ref)
    switch {
    case errors.Is(err, keyring.ErrNotFound):
        return Session{}, problem.New(problem.CategoryAuthPolicy, "auth.login_required",
            "no stored login session exists for the selected context").
            WithRecovery("Run wso2 login to establish a session for this context.")
    case err != nil:
        return Session{}, problem.New(problem.CategoryAuthPolicy, "auth.keyring_unavailable",
            "the OS secure store is not available to the shell").
            WithRecovery("Enable the OS keychain or secret service for this user, then retry. The shell does not store credentials in files.")
    }
    var stored Session
    if json.Unmarshal([]byte(value), &stored) != nil || stored.RefreshToken == "" {
        // A stale or foreign entry is indistinguishable from no session.
        return Session{}, problem.New(problem.CategoryAuthPolicy, "auth.login_required",
            "the stored login session for the selected context cannot be read").
            WithRecovery("Run wso2 login to establish a fresh session for this context.")
    }
    return stored, nil
}
```

`lock.go`: portable lock via `os.OpenFile(path, os.O_CREATE|os.O_EXCL, 0o600)` with retry every 25 ms, deadline 5 s, staleness takeover after 30 s (check the lock file's mtime; remove and retry once). Lock path: `filepath.Join(stateRoot, "cli", "locks", ref+".lock")`, `os.MkdirAll(dir, 0o700)` first. Release removes the file in a deferred call. Deadline exhaustion returns `problem.New(problem.CategoryAuthPolicy, "auth.login_required", "another WSO2 CLI invocation is updating this session")` with recovery "Retry the command." — it is recoverable by retry, and the spec's code list stays closed (no new busy code).

- [x] **Step 4: Run tests**

Run: `go test ./internal/auth/session/ -race -v`
Expected: PASS (the lock test specifically must pass with `-race`).

- [x] **Step 5: Commit**

```bash
go vet ./... && git add go.mod go.sum internal/auth/session && git commit -m "feat: store interactive sessions in the OS secure store with rotation-safe locking"
```

---

### Task 5: Fake OIDC issuer test helper

**Files:**
- Create: `internal/auth/fakeissuer/fakeissuer.go`
- Test: `internal/auth/fakeissuer/fakeissuer_test.go`

**Interfaces:**
- Produces:

```go
package fakeissuer

// Options configure how the issuer behaves at its token endpoint.
type Options struct {
    // RefreshScopeMode is how the refresh grant treats a narrower scope request:
    // "honor" narrows the issued token (IS-source behavior), "ignore" returns
    // the original full scope set, "reject" answers invalid_scope.
    RefreshScopeMode string // default "honor"
    // Audience is the aud claim minted into access tokens.
    Audience string
    // RotateRefreshTokens issues a new refresh token on every refresh.
    RotateRefreshTokens bool
}

type Issuer struct {
    URL string // issuer identifier == server base URL
    // ...
}

// New starts the issuer on an httptest server and closes it on test cleanup.
func New(t *testing.T, opts Options) *Issuer

// SeedSession mints a live refresh token directly, so broker tests need no
// browser step. Returned value goes into session.Session.RefreshToken.
func (i *Issuer) SeedSession(scopes []string) string

// Introspect answers whether the issuer minted this exact access token, with
// its scopes and audience. Backed by the issuer's /introspect endpoint.
func (i *Issuer) Introspect(t *testing.T, token string) (active bool, scopes []string, audience []string)
```

Endpoints served: `/.well-known/openid-configuration` (issuer, `authorization_endpoint`, `token_endpoint`, `jwks_uri`, `introspection_endpoint`, `code_challenge_methods_supported: ["S256"]`, `grant_types_supported: ["authorization_code","refresh_token","client_credentials"]`), `/jwks`, `/authorize` (auto-approves: verifies `client_id`, `redirect_uri` is `http://127.0.0.1:{10425..10428}/callback`, stores the `code_challenge`, 302-redirects to `redirect_uri?code=...&state=...`), `/token` (grants: `authorization_code` with PKCE S256 verification, `refresh_token` honoring `Options.RefreshScopeMode` and rotation, `client_credentials` requiring `client_secret_post` or basic), `/introspect`.

Access tokens are real RS256 JWTs signed with a per-issuer `rsa.PrivateKey` via `github.com/go-jose/go-jose/v4`, claims: `iss`, `sub` (`"user-1"` for interactive, `"client-1"` for client credentials), `aud` (Options.Audience), `scope` (space-joined), `exp` (+5 min), `iat`. ID tokens likewise with `email: "dev@example.test"` and `nonce` echo. Refresh responses include the `scope` field (except add a knob later if a test needs its absence: `OmitRefreshScopeField bool` — include it now, default false).

- [x] **Step 1: Write failing tests** — in `fakeissuer_test.go`, drive the issuer with plain `http.Client` calls: discovery returns S256; full code+PKCE exchange round-trips and the access token verifies against `/jwks` (use `go-oidc` provider + verifier in the test); refresh with `RefreshScopeMode: "honor"` narrows `scope`; `"ignore"` returns the full set; `"reject"` returns HTTP 400 `{"error":"invalid_scope"}`; rotation returns a new refresh token and invalidates the old one; `Introspect` reports issued tokens active with their scopes.

- [x] **Step 2: Run tests, verify they fail** — `go test ./internal/auth/fakeissuer/ -v` → FAIL (package missing).

- [x] **Step 3: Implement** the issuer as a single `http.ServeMux` on `httptest.NewServer`, with an internal `sync.Mutex`-guarded state: `codes map[string]codeGrant{challenge, scopes, redirectURI, nonce}`, `refreshTokens map[string][]string` (token → scopes), `accessTokens map[string]tokenRecord{scopes, audience}`. Keep it under ~350 lines; it is a test fixture, not a product.

- [x] **Step 4: Run tests** — `go test ./internal/auth/fakeissuer/ -v` → PASS.

- [x] **Step 5: Commit**

```bash
go vet ./... && git add go.mod go.sum internal/auth/fakeissuer && git commit -m "test: add a fake OIDC issuer for deterministic authentication tests"
```

---

### Task 6: Browser PKCE flow (`oauthflow`)

**Files:**
- Create: `internal/auth/oauthflow/login.go`, `internal/auth/oauthflow/browser.go`
- Test: `internal/auth/oauthflow/login_test.go`

**Interfaces:**
- Consumes: `fakeissuer` (Task 5).
- Produces:

```go
package oauthflow

// LoopbackPorts is the fixed callback port sequence, tried in order. All four
// URLs are part of the documented app registration.
var LoopbackPorts = []int{10425, 10426, 10427, 10428}

// Login runs one browser Authorization Code + PKCE login.
type Login struct {
    Issuer   string
    ClientID string
    // Scopes beyond openid/offline_access (the identity's product scope union).
    Scopes []string
    // HTTPClient serves discovery and exchange. Defaults to http.DefaultClient.
    HTTPClient *http.Client
    // OpenBrowser opens the authorization URL. Defaults to the OS opener;
    // a failure to open is not a failure to log in (the URL is printed).
    OpenBrowser func(url string) error
    // Out receives the always-printed authorization URL and progress lines.
    Out io.Writer
    // Ports overrides LoopbackPorts in tests.
    Ports []int
}

// Result is a completed login.
type Result struct {
    Token   *oauth2.Token // includes RefreshToken; Extra("id_token") verified
    Subject string
    Email   string
}

func (l Login) Run(ctx context.Context) (Result, error)
```

Problem mapping inside `Run`: discovery failure or a discovery document without `S256` → `auth.discovery_failed`. A loopback bind failure (all four ports busy) also returns `auth.discovery_failed` with the message "no loopback callback port is available for the browser login" and a recovery naming the four ports — the spec's code list stays closed, and the message states the real cause. (Record this in the walkthrough's troubleshooting section.)

- [x] **Step 1: Write the failing happy-path test**

```go
func TestBrowserLoginRoundTrip(t *testing.T) {
    issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status"})
    var printed bytes.Buffer
    login := oauthflow.Login{
        Issuer:     issuer.URL,
        ClientID:   "client-123",
        Scopes:     []string{"reference:status:read"},
        HTTPClient: issuer.HTTPClient(),
        Out:        &printed,
        Ports:      []int{0}, // 0 = ephemeral port in tests; fake issuer accepts any 127.0.0.1 redirect when Options.AllowAnyLoopbackPort is set
        OpenBrowser: func(authURL string) error {
            // The "browser": follow the URL; the issuer auto-approves and
            // redirects to the loopback listener.
            go func() {
                response, err := issuer.HTTPClient().Get(authURL)
                if err == nil {
                    response.Body.Close()
                }
            }()
            return nil
        },
    }
    result, err := login.Run(context.Background())
    if err != nil {
        t.Fatalf("login: %v", err)
    }
    if result.Token.RefreshToken == "" {
        t.Fatal("no refresh token issued")
    }
    if result.Subject != "user-1" || result.Email != "dev@example.test" {
        t.Fatalf("identity claims: %+v", result)
    }
    if !strings.Contains(printed.String(), "http") {
        t.Fatal("authorization URL was not printed")
    }
    if strings.Contains(printed.String(), result.Token.RefreshToken) {
        t.Fatal("refresh token leaked into output")
    }
}
```

Add `AllowAnyLoopbackPort bool` to `fakeissuer.Options` and an `HTTPClient()` accessor for this test (extend Task 5's fixture — that is in-scope drift, fold it in here).

Also write: `TestLoginFailsWithoutS256` (fake issuer knob `OmitS256 bool` → `auth.discovery_failed`), `TestStateMismatchRejected` (drive the callback URL manually with a wrong `state`, expect the flow to keep waiting/reject — assert the exchange fails and no Result is produced), `TestBrowserOpenFailureStillCompletes` (OpenBrowser returns an error; the test then fetches the printed URL from `printed` and the login still completes).

- [x] **Step 2: Run tests, verify they fail** — `go test ./internal/auth/oauthflow/ -v` → FAIL.

- [x] **Step 3: Implement `login.go`**

Flow, using go-oidc + x/oauth2 exactly:

```go
ctx = oidc.ClientContext(ctx, l.httpClient())
provider, err := oidc.NewProvider(ctx, l.Issuer)                    // discovery
var capabilities struct {
    CodeChallengeMethods []string `json:"code_challenge_methods_supported"`
}
_ = provider.Claims(&capabilities)
// require "S256" in capabilities.CodeChallengeMethods → else auth.discovery_failed

listener, port := bindLoopback(l.ports())                            // net.Listen("tcp4", "127.0.0.1:port")
config := oauth2.Config{
    ClientID:    l.ClientID,
    Endpoint:    provider.Endpoint(),
    RedirectURL: fmt.Sprintf("http://127.0.0.1:%d/callback", port),
    Scopes:      append([]string{oidc.ScopeOpenID, "offline_access"}, l.Scopes...),
}
verifier := oauth2.GenerateVerifier()
state := randomState()                                               // 32 bytes crypto/rand, base64url
authURL := config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
fmt.Fprintf(l.Out, "Open this URL to log in:\n%s\n", authURL)
_ = l.openBrowser(authURL)                                           // best effort
code := waitForCallback(ctx, listener, state)                        // http.Server on the listener; /callback checks state, replies a small HTML page, sends code on a channel
token, err := config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
rawIDToken, _ := token.Extra("id_token").(string)
idToken, err := provider.Verifier(&oidc.Config{ClientID: l.ClientID}).Verify(ctx, rawIDToken)
var claims struct {
    Email string `json:"email"`
}
_ = idToken.Claims(&claims)
return Result{Token: token, Subject: idToken.Subject, Email: claims.Email}, nil
```

`waitForCallback` details: handler checks `r.URL.Query().Get("state") == state` (mismatch → 400 page, keep listening), success → write `<html><body>Login complete. You can close this tab and return to the terminal.</body></html>`, send the code, then the caller shuts the server down. Respect `ctx.Done()` with a clean error.

`browser.go`:

```go
// Open opens a URL in the user's default browser, best effort.
func Open(target string) error {
    if os.Getenv("WSO2_NO_BROWSER") != "" {
        return nil
    }
    switch runtime.GOOS {
    case "darwin":
        return exec.Command("open", target).Start()
    case "windows":
        return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
    default:
        return exec.Command("xdg-open", target).Start()
    }
}
```

- [x] **Step 4: Run tests** — `go test ./internal/auth/oauthflow/ -race -v` → PASS.

- [x] **Step 5: Commit**

```bash
go vet ./... && git add go.mod go.sum internal/auth/oauthflow internal/auth/fakeissuer && git commit -m "feat: implement the browser authorization code login with PKCE"
```

---

### Task 7: The `wso2 login` command

**Files:**
- Create: `internal/app/login.go`
- Modify: `internal/app/app.go` (register builtin)
- Test: `internal/app/login_test.go`

**Interfaces:**
- Consumes: `oauthflow.Login` (Task 6), `session.Store` (Task 4), `contexts.Selection` (Task 2), `Shell.selection` (Task 3).
- Produces: builtin `login`; `Shell.login(args []string) error`. Test seam on `Shell`:

```go
// Shell gains one optional test hook (zero value = production behavior):
type Shell struct {
    StateRoot string
    Streams   output.Streams
    // OpenBrowser overrides how login opens the authorization URL. Tests use
    // it to drive the flow without a display. nil = oauthflow.Open.
    OpenBrowser func(url string) error
}
```

Login flags parsed by hand in `login.go` (same idiom as `parseProductArgs`): `--context <name>`/`--context=`, `--non-interactive`. Unknown flags → `shell.unknown_flag` usage problem (`problem.CategoryUsage`, message naming the flag, recovery "Run wso2 login [--context <name>] [--non-interactive].").

Behavior table (each row is a test):

| Situation | Outcome |
| --- | --- |
| `--non-interactive` or `WSO2_NON_INTERACTIVE` set, kind `oauth-browser` | `auth.non_interactive`: "browser login cannot run non-interactively" + recovery naming client credentials |
| No context document / empty selection | `auth.context_not_selected` |
| Kind `client-credentials` | `auth.login_not_required`: "this context acquires access inline; no login step exists" |
| Kind `development-credential` (synthetic v1) | `auth.login_not_required` |
| Kind `oauth-device` or `pat` | `auth.kind_not_implemented` |
| Kind `oauth-browser`, happy path | PKCE flow; save session; print subject, email, and product namespaces |

- [x] **Step 1: Write failing tests**

`internal/app/login_test.go`, in-process with `keyring.MockInit()` and a v2 fixture document pointing at a `fakeissuer`:

```go
func TestLoginRefusals(t *testing.T) {
    cases := []struct {
        name string
        doc  func(issuerURL string) contexts.Document // nil = no document
        args []string
        env  map[string]string
        code string
    }{
        {"no context", nil, nil, nil, "auth.context_not_selected"},
        {"non-interactive flag", browserDoc, []string{"--non-interactive"}, nil, "auth.non_interactive"},
        {"non-interactive env", browserDoc, nil, map[string]string{"WSO2_NON_INTERACTIVE": "1"}, "auth.non_interactive"},
        {"client credentials", clientCredentialsDoc, nil, nil, "auth.login_not_required"},
        {"device kind", deviceDoc, nil, nil, "auth.kind_not_implemented"},
        {"pat kind", patDoc, nil, nil, "auth.kind_not_implemented"},
    }
    // run app.Shell{StateRoot: root, Streams: captured}.Run(append([]string{"login"}, args...))
    // assert exit code exit.ForProblem class auth_policy (77) and stderr contains the code's message.
}

func TestLoginHappyPathStoresSessionAndReportsIdentity(t *testing.T) {
    keyring.MockInit()
    issuer := fakeissuer.New(t, fakeissuer.Options{Audience: "reference-status", AllowAnyLoopbackPort: true})
    root := t.TempDir()
    writeBrowserFixture(t, root, issuer.URL) // v2 doc, credentialRef "acme-cloud-login", product "reference"
    var out, errOut bytes.Buffer
    shell := app.Shell{
        StateRoot: root,
        Streams:   output.Streams{Out: &out, Err: &errOut},
        OpenBrowser: func(authURL string) error {
            go func() {
                response, fetchErr := issuer.HTTPClient().Get(authURL)
                if fetchErr == nil {
                    response.Body.Close()
                }
            }()
            return nil
        },
    }
    if code := shell.Run([]string{"login"}); code != exit.OK {
        t.Fatalf("login failed: exit %d, stderr %s", code, errOut.String())
    }
    stored, err := session.Store{StateRoot: root}.Load("acme-cloud-login")
    if err != nil {
        t.Fatalf("session not stored: %v", err)
    }
    if stored.Issuer != issuer.URL || stored.RefreshToken == "" {
        t.Fatalf("stored session incomplete: %+v", stored)
    }
    for _, expected := range []string{"user-1", "dev@example.test", "reference"} {
        if !strings.Contains(out.String(), expected) {
            t.Fatalf("login report missing %q in: %s", expected, out.String())
        }
    }
    for _, secret := range []string{stored.RefreshToken, stored.AccessToken} {
        if secret != "" && (strings.Contains(out.String(), secret) || strings.Contains(errOut.String(), secret)) {
            t.Fatal("token material leaked into login output")
        }
    }
}
```

Note the login flow must use the fake issuer's HTTP client for discovery/exchange: give `Shell` one more test seam? No — the fake issuer is an `httptest.Server` reachable over real HTTP on 127.0.0.1, so the default client works. Ensure `fakeissuer` uses `httptest.NewServer` (HTTP, not TLS) so no client injection is needed here; `HTTPClient()` then just returns `http.DefaultClient` — simplify Task 5/6 accordingly if TLS was used.

- [x] **Step 2: Run tests, verify they fail** — `go test ./internal/app/ -run TestLogin -v` → FAIL.

- [x] **Step 3: Implement `login.go`**

```go
// login establishes the selected context's interactive session.
func (s Shell) login(args []string) error {
    flags, err := parseLoginArgs(args) // {contextName string; nonInteractive bool}
    if err != nil {
        return err
    }
    selected, err := s.selection(flags.contextName)
    if err != nil {
        return err
    }
    kind := selected.Identity.Auth.Kind
    if kind == "" {
        return problem.New(problem.CategoryAuthPolicy, "auth.context_not_selected",
            "no WSO2 CLI context is selected to log in to").
            WithRecovery("Author a context document and select a context, then run wso2 login. See docs/guides/login.md.")
    }
    if flags.nonInteractive || os.Getenv("WSO2_NON_INTERACTIVE") != "" {
        return problem.New(problem.CategoryAuthPolicy, "auth.non_interactive",
            "browser login cannot run in non-interactive mode").
            WithRecovery("Use a client-credentials identity for automation; it acquires access inline without a login step.")
    }
    switch kind {
    case contexts.KindClientCredentials, contexts.MethodDevelopmentCredential:
        return problem.New(problem.CategoryAuthPolicy, "auth.login_not_required",
            fmt.Sprintf("the %q context acquires access inline and has no login step", selected.Context.Name)).
            WithRecovery("Run the product command directly; the shell authenticates during it.")
    case contexts.KindOAuthDevice, contexts.KindPAT:
        return problem.New(problem.CategoryAuthPolicy, "auth.kind_not_implemented",
            fmt.Sprintf("the %q context uses an authentication kind this release does not implement", selected.Context.Name)).
            WithRecovery("Use a browser or client-credentials identity. Device and personal-access-token login are planned.")
    case contexts.KindOAuthBrowser:
        // fall through
    default:
        return problem.New(problem.CategoryAuthPolicy, "auth.method_unsupported",
            fmt.Sprintf("the %q context uses an authentication method this shell does not implement", selected.Context.Name)).
            WithRecovery("Select a context with a supported authentication kind.")
    }

    root, err := s.stateRoot()
    if err != nil {
        return err
    }
    result, err := oauthflow.Login{
        Issuer:      selected.Identity.Auth.Issuer,
        ClientID:    selected.Identity.Auth.ClientID,
        Scopes:      productScopeUnion(selected.Identity),
        OpenBrowser: s.OpenBrowser,
        Out:         s.Streams.Out,
    }.Run(context.Background())
    if err != nil {
        return err
    }
    store := session.Store{StateRoot: root}
    err = store.WithLock(selected.Identity.Auth.CredentialRef, func() error {
        return store.Save(selected.Identity.Auth.CredentialRef, session.Session{
            Issuer:       selected.Identity.Auth.Issuer,
            RefreshToken: result.Token.RefreshToken,
            AccessToken:  result.Token.AccessToken,
            ExpiresAt:    result.Token.Expiry.UTC(),
        })
    })
    if err != nil {
        return err
    }
    return s.reportLogin(selected, result) // prints subject, email, sorted product namespaces; never token material
}
```

`productScopeUnion` returns the sorted, de-duplicated union of `identity.Products[*].Scopes`. Register the builtin in `app.go`: `{name: "login", summary: "Log in to the selected context's identity.", run: Shell.login}`.

- [x] **Step 4: Run tests** — `go test ./internal/app/ -race -v` → PASS.

- [x] **Step 5: Commit**

```bash
go vet ./... && git add internal/app && git commit -m "feat: add wso2 login for browser-based interactive identities"
```

---

### Task 8: Broker source seam and v2 policy

**Files:**
- Create: `internal/auth/source.go`, `internal/auth/source_dev.go`
- Modify: `internal/auth/auth.go`
- Test: `internal/auth/auth_test.go` (extend; keep existing tests passing)

**Interfaces:**
- Produces:

```go
// source mints access material after broker policy has admitted the request.
type source interface {
    mint(request Request, now time.Time) (Grant, error)
}

// Broker fields change to:
type Broker struct {
    Namespace    string
    Capabilities modules.Capabilities
    Selection    contexts.Selection
    InvocationID string
    // Credentials reads a named environment variable (dev + client-credentials).
    Credentials func(name string) (string, bool)
    // StateRoot hosts session locks for the oauth-browser source.
    StateRoot string
    // HTTPClient serves issuer traffic. nil = http.DefaultClient.
    HTTPClient *http.Client
    Now         func() time.Time
    granted     bool
}
```

`Acquire` order (each refusal a test):

1. `granted` → `auth.already_granted` (unchanged).
2. `checkDeclared` against receipt capabilities (unchanged codes).
3. Kind resolution from `Selection.Identity.Auth.Kind`:
   - `""` → `auth.context_not_selected`.
   - `MethodDevelopmentCredential` → namespace must be `ProofNamespace` else `auth.namespace_not_brokered`; organization required (`auth.organization_not_selected`); credential variable resolution (existing `auth.credential_unavailable` paths) → `devSource`.
   - `KindOAuthBrowser` / `KindClientCredentials` → production checks below, then the matching source (Tasks 9–10; in this task they return a placeholder refusal — see Step 3).
   - `KindOAuthDevice` / `KindPAT` → `auth.kind_not_implemented`.
   - anything else → `auth.method_unsupported`.
4. Production checks (browser + client credentials):
   - `Selection.Identity.Products[Namespace]` missing → `auth.product_not_configured` ("the selected identity does not configure the %q product").
   - Product audience non-empty and `request.Audience != product.Audience` → `auth.product_not_configured` (the deployment registration does not match what the module needs; message says the requested audience is not the configured one, without echoing values beyond the audience names — audiences are not secrets).
   - Product scopes non-empty and any requested scope ∉ product scopes → `auth.product_not_configured`.
   - `Selection.Context.Organization != ""` and `Selection.Context.Organization != Selection.Identity.Auth.Tenant` → `auth.organization_switch_unsupported` ("this release cannot switch to the target organization; the identity's session stays in its home tenant").

- [x] **Step 1: Write failing tests** — extend `auth_test.go` with a table covering every row above, using a helper that builds a `Broker` with a v2 `contexts.Selection`. Existing dev-path tests keep passing unmodified except for the `Broker` field rename (`Context` → `Selection`); update them mechanically. One ordering test: a non-reference namespace with **no** identity now refuses `auth.context_not_selected` (previously `auth.namespace_not_brokered`) — check whether an existing test pinned the old order; if so, update it and note the change in the commit message body.

- [x] **Step 2: Run tests, verify failures** — `go test ./internal/auth/ -v`.

- [x] **Step 3: Implement** — move the devtoken minting body of today's `Acquire` into `source_dev.go` (`devSource{credential string, claims devtoken.Claims}` built by the broker after its checks; `mint` calls `devtoken.Mint` exactly as today). `source.go` holds the kind-resolution switch. For this task only, `KindOAuthBrowser`/`KindClientCredentials` resolve to a placeholder `source` returning `auth.narrowing_unavailable` with message "this build cannot derive access for this identity yet" — Tasks 9–10 replace it; the placeholder keeps this task shippable and its checks testable.

- [x] **Step 4: Run the whole suite** — `go test ./... -race` → PASS.

- [x] **Step 5: Commit**

```bash
go vet ./... && git add internal/auth internal/app && git commit -m "feat: resolve broker access through per-kind token sources"
```

---

### Task 9: oauth-browser source — scoped refresh with verification

**Files:**
- Create: `internal/auth/source_browser.go`, `internal/auth/discovery.go`, `internal/auth/claims.go`
- Test: `internal/auth/source_browser_test.go`

**Interfaces:**
- Consumes: `session.Store` (Task 4), `fakeissuer` (Task 5), source seam (Task 8).
- Produces:

```go
// discovery.go
// tokenEndpoint resolves the issuer's token endpoint via OIDC discovery.
func tokenEndpoint(ctx context.Context, client *http.Client, issuer string) (string, error) // auth.discovery_failed on failure

// claims.go
// bearerClaims extracts aud and scope from a JWT's payload without verifying
// its signature: the token arrived over TLS from the issuer itself, and the
// claims steer shell policy only — the audience service re-verifies.
type bearerFacts struct {
    Audiences []string
    Scopes    []string
    ExpiresAt time.Time
}
func bearerClaims(token string) (bearerFacts, error)

// source_browser.go
type browserSource struct {
    identity contexts.Identity
    product  contexts.Product
    sessions session.Store
    client   *http.Client
}
func (s browserSource) mint(request Request, now time.Time) (Grant, error)
```

`mint` algorithm (the scoped-refresh strategy from spec §6):

1. `sessions.WithLock(ref, ...)` around everything below (single-writer rotation).
2. `sessions.Load(ref)`; `stored.Issuer != identity.Auth.Issuer` → `auth.session_issuer_mismatch`.
3. Discover the token endpoint. POST `application/x-www-form-urlencoded`: `grant_type=refresh_token`, `refresh_token`, `client_id`, `scope=` space-joined `request.Scopes` (exact requested subset — the module's request, not the product union).
4. HTTP 400 with `{"error":"invalid_scope"}` → `auth.narrowing_unavailable` ("the deployment refused to narrow this session to the requested permissions"). Other non-200 → `auth.login_required` ("the stored session was not accepted by the issuer; log in again").
5. Parse the token response. Effective scopes = response `scope` field if present, else the JWT `scope` claim via `bearerClaims`; neither present → `auth.narrowing_unavailable` (cannot prove narrowing — the spec's "refuse rather than degrade").
6. Verify: effective scope **set equals** the requested set → else `auth.narrowing_unavailable` (message states what the deployment issued instead, scopes are not secrets). Verify `bearerClaims(access_token).Audiences` contains `request.Audience` → else `auth.narrowing_unavailable` ("the issued token is not bound to the requested audience; check the deployment's API-resource registration").
7. If the response contains a rotated `refresh_token`, `sessions.Save` the new session **before** returning (this is inside the lock).
8. Return `Grant{Token: accessToken, ExpiresAt: from expires_in (fallback: JWT exp)}`.

- [x] **Step 1: Write failing tests** — table against the fake issuer:

| Test | Issuer options / setup | Expected |
| --- | --- | --- |
| happy path narrows | `RefreshScopeMode: "honor"`, seeded session, request one of two granted scopes | Grant issued; introspection shows exactly the requested scope; aud matches |
| rotation persisted | `RotateRefreshTokens: true` | second `mint` in a new broker succeeds using the rotated token; old token rejected by issuer |
| issuer ignores scope | `RefreshScopeMode: "ignore"` | `auth.narrowing_unavailable`, no grant |
| issuer rejects scope | `RefreshScopeMode: "reject"` | `auth.narrowing_unavailable` |
| wrong audience registered | `Options.Audience: "other-api"` | `auth.narrowing_unavailable` |
| no stored session | keychain empty | `auth.login_required` |
| issuer mismatch | stored session's issuer ≠ identity issuer | `auth.session_issuer_mismatch` |
| revoked/garbage refresh token | seed then tamper | `auth.login_required` |

Use `keyring.MockInit()` per test; seed sessions via `fakeissuer.SeedSession` + `session.Store.Save`.

- [x] **Step 2: Run tests, verify failures.** — `go test ./internal/auth/ -run TestBrowserSource -v`

- [x] **Step 3: Implement** the three files per the interfaces; wire the real `browserSource` into Task 8's kind switch (replacing the placeholder). `bearerClaims`: split on `.`, `base64.RawURLEncoding` decode part 1, JSON into `{Aud any; Scope string; Exp int64}`, normalize `aud` string-or-array.

- [x] **Step 4: Run tests** — `go test ./internal/auth/... -race` → PASS.

- [x] **Step 5: Commit**

```bash
go vet ./... && git add internal/auth && git commit -m "feat: derive module access from the stored session by scoped refresh"
```

---

### Task 10: client-credentials source — inline CI acquisition

**Files:**
- Create: `internal/auth/source_clientcred.go`
- Test: `internal/auth/source_clientcred_test.go`

**Interfaces:**
- Consumes: `tokenEndpoint`, `bearerClaims` (Task 9), source seam (Task 8).
- Produces:

```go
type clientCredentialsSource struct {
    identity contexts.Identity
    product  contexts.Product
    lookup   func(name string) (string, bool) // Broker.Credentials
    client   *http.Client
}
func (s clientCredentialsSource) mint(request Request, now time.Time) (Grant, error)
```

`mint`: read the secret from the env var named by `identity.Auth.ClientSecretVariable` via `lookup` — absent/blank → the existing two-part `auth.credential_unavailable` denial shape (module-safe problem + user guidance naming the variable, exactly like the dev source's `credential()` does today). Then `clientcredentials.Config{ClientID, ClientSecret, TokenURL, Scopes: request.Scopes}` → `.Token(ctx)`. Verify effective scopes and audience with the same helpers and the same `auth.narrowing_unavailable` refusals as Task 9 (shared verification function `verifyIssued(request Request, accessToken string, responseScopes string) error` — extract it in this task and reuse it from `source_browser.go`). Nothing is persisted anywhere.

- [x] **Step 1: Write failing tests** — happy path (grant issued, introspection confirms scopes/aud, nothing written to the keychain mock or state dir), missing env var → `auth.credential_unavailable` with guidance naming the variable, scope-ignore issuer → `auth.narrowing_unavailable`, secret never in any problem message (assert the secret string is absent from every error and output surface in the test).

- [x] **Step 2: Run tests, verify failures.**

- [x] **Step 3: Implement and wire into the kind switch.**

- [x] **Step 4: Run tests** — `go test ./internal/auth/... -race` → PASS.

- [x] **Step 5: Commit**

```bash
go vet ./... && git add internal/auth && git commit -m "feat: acquire client-credentials access inline during the invoking command"
```

---

### Task 11: Acceptance — the reference module receives a real token

**Files:**
- Create: `test/acceptance/login_test.go`
- Test: itself

**Interfaces:**
- Consumes: everything above; the existing acceptance harness helpers in `test/acceptance/acceptance_test.go` (deployment setup, module fixture build); `app.Shell` in-process.

These tests run the shell **in-process** (`app.Shell{...}.Run(...)`) because the OS keychain must be the go-keyring mock; the module is still launched as a real subprocess by the shell, so the full shell→broker→module chain is exercised. State the reason in a file comment.

- [x] **Step 1: Write the failing chain test**

```go
func TestLoginThenModuleReceivesRealToken(t *testing.T) {
    keyring.MockInit()
    issuer := fakeissuer.New(t, fakeissuer.Options{
        Audience:            referenceAudience,
        RefreshScopeMode:    "honor",
        RotateRefreshTokens: true,
        AllowAnyLoopbackPort: true,
    })
    root := t.TempDir()
    installReferenceModule(t, root)            // reuse the existing fixture installer
    writeV2BrowserDocument(t, root, issuer.URL) // identity products: reference → {endpoint: statusService.URL, audience: referenceAudience, scopes: [referenceReadScope]}

    shell := app.Shell{StateRoot: root, Streams: captured(), OpenBrowser: autoApprove(issuer)}
    if code := shell.Run([]string{"login"}); code != exit.OK {
        t.Fatalf("login: %d", code)
    }
    // The reference module's status command asks the broker for
    // {referenceAudience, referenceReadScope} and reports its grant.
    if code := shell.Run([]string{"reference", "status", "--output", "json"}); code != exit.OK {
        t.Fatalf("status: %d", code)
    }
    token := grantTokenFromStatusOutput(t, stdout) // however the reference module surfaces it today — if it redacts the token (it should), have the status service capture the bearer token it received instead and read it from there
    active, scopes, audience := issuer.Introspect(t, token)
    if !active {
        t.Fatal("module presented a token the issuer did not mint")
    }
    if len(scopes) != 1 || scopes[0] != referenceReadScope {
        t.Fatalf("token scopes = %v, want exactly the requested scope", scopes)
    }
    if !slices.Contains(audience, referenceAudience) {
        t.Fatalf("token audience = %v", audience)
    }
}
```

Read `test/acceptance/status_test.go` and the reference module first: the right capture point is the **status service** (`internal/statusservice` httptest server records the `Authorization` header it receives) — the module must present the broker's token to its endpoint; assert from the service side, never by printing the token.

Additional tests in the same file:

```go
func TestModuleRunWithoutLoginIsLoginRequired(t *testing.T)   // v2 browser doc, no login → exit 77, stderr has "wso2 login"
func TestClientCredentialsInlineAcquisition(t *testing.T)     // client-credentials doc + env secret → module call succeeds with introspectable token; no session stored
func TestNonInteractiveGuardInCI(t *testing.T)                // WSO2_NON_INTERACTIVE=1 + browser doc → login refuses; module run still refuses auth.login_required (not a hang)
func TestRotationSurvivesConsecutiveInvocations(t *testing.T) // two module runs; second uses rotated refresh token
func TestNoTokenMaterialOnAnyOutputSurface(t *testing.T)      // sweep stdout+stderr of login and status runs for refresh/access token substrings (canary style, mirroring failclosed_test.go)
```

- [x] **Step 2: Run tests, verify failures** — `go test ./test/acceptance/ -run 'TestLogin|TestModuleRun|TestClientCredentials|TestNonInteractive|TestRotation|TestNoToken' -v`

- [x] **Step 3: Fix whatever the chain surfaces.** This is the integration task: expect wiring gaps (broker HTTP client, endpoint pass-through, status service audience). No new features — only completing the wiring the earlier tasks defined.

- [x] **Step 4: Full suite** — `go test ./... -race` → PASS.

- [x] **Step 5: Commit**

```bash
go vet ./... && git add test/acceptance && git commit -m "test: prove the reference module receives issuer-minted narrowed access"
```

---

### Task 12: Live smoke, empirical experiments, walkthrough, backend ask

**Files:**
- Create: `test/smoke/login_smoke_test.go`, `test/smoke/asgardeo_empirical_test.go` (both `//go:build smoke`)
- Create: `docs/guides/login.md`
- Modify: `docs/research/product-authentication-compatibility.md` (backend-ask note)
- Modify: `Makefile` (or create if absent — check first with `ls Makefile`)

**Interfaces:**
- Consumes: `oauthflow.Login`, `session.Store`, broker sources — via `app.Shell` in-process.
- Produces: `make smoke-login`, `make empirical-asgardeo`.

- [x] **Step 1: Smoke test** — `login_smoke_test.go` reads env: `WSO2_SMOKE_ISSUER`, `WSO2_SMOKE_CLIENT_ID`, `WSO2_SMOKE_AUDIENCE`, `WSO2_SMOKE_SCOPE`; skips (`t.Skip`) when unset. It writes a temp v2 document, runs `wso2 login` in-process (real browser opens — this is a human-driven test), then runs the broker acquisition path directly (`auth.Broker` with the real selection) and reports the outcome. Works identically against Asgardeo and a local IS container.

- [x] **Step 2: Empirical experiments** — `asgardeo_empirical_test.go`, same env gating plus `WSO2_EMPIRICAL=1`:
  - **Experiment A (any-port loopback):** run `oauthflow.Login` with `Ports: []int{16000}` while the registered app only lists 10425–10428. Success ⇒ IS-parity port-agnostic matching; redirect-URI-mismatch error ⇒ exact-match only. Print a one-line verdict: `ASGARDEO ANY-PORT LOOPBACK: {supported|rejected}`.
  - **Experiment B (refresh scope narrowing):** log in with two scopes, then refresh with one via the browser source; print `ASGARDEO REFRESH NARROWING: {honored|ignored|rejected}` from which refusal (if any) the source produced.
  - The test always passes; its output is evidence. After running it for real, **record both verdicts** in `docs/research/asgardeo-redirect-uri-and-scope-narrowing.md` §3 (replace the "unknown — needs empirical test" cells) — that recording step is manual and part of executing this task against a real tenant.

- [x] **Step 3: Makefile targets**

```make
smoke-login:
	go test -tags smoke ./test/smoke/ -run TestLoginSmoke -v

empirical-asgardeo:
	WSO2_EMPIRICAL=1 go test -tags smoke ./test/smoke/ -run TestAsgardeoEmpirical -v
```

- [x] **Step 4: Walkthrough** — `docs/guides/login.md` covering, in order: registering the public client in Asgardeo (standard-based app, PKCE mandatory, the four callback URLs listed explicitly) and in IS 7.x (same, or one `regexp=(...)` entry OR-ing the four); authoring the v2 identity/context JSON by hand (complete copy-paste example matching Task 1's `validV2()` shape, with the real Asgardeo issuer URL shape `https://api.asgardeo.io/t/{org}/oauth2/token`); first `wso2 login`; the CI client-credentials setup (M2M app, `clientSecretVariable`, no login step); troubleshooting (ports busy, keyring unavailable, `auth.narrowing_unavailable` meaning, `auth.organization_switch_unsupported` meaning). Follow the repo's doc conventions (status header, authoritative links to the spec and architecture).

- [x] **Step 5: Backend ask** — append to `docs/research/product-authentication-compatibility.md` §1.1's backend-gap paragraph a dated note: the wso2-cli slice ships with per-tenant manual registration; the standing ask to the Asgardeo service team is a seeded, well-known `wso2cli` public client (PKCE-mandatory, loopback callbacks 10425–10428), after which the walkthrough's registration section collapses to a default `clientId`.

- [x] **Step 6: Full suite, vet, commit**

```bash
go test ./... && go vet ./...
git add test/smoke docs Makefile
git commit -m "docs: add the login walkthrough, live smoke gates, and empirical experiments"
```

---

## Definition of done (from the spec, verbatim checks)

- [x] `wso2 login` completes PKCE against a real Asgardeo trial tenant and a local IS 7.x (`make smoke-login`, run twice with the two issuer configs). — Asgardeo tenant 2026-08-06; `wso2/wso2is:7.3.0` 2026-08-06. The nonce echo is checked by both, since `oauthflow/login.go` refuses on a mismatch and neither login could otherwise have completed.
- [x] The refresh token lands in the OS secure store (smoke run; deterministic equivalent in Task 7). — both smoke runs read it back out and compare its issuer.
- [ ] The reference module receives a real short-lived access token through the broker against a backend proven to satisfy the scope/audience policy, and a test introspects that token (Task 11 deterministic; smoke against whichever real backend passes the empirical narrowing test).

  **Deterministic half done, live half incomplete.** `TestLoginThenTheModuleReceivesIssuerMintedNarrowedAccess` launches the real module subprocess, introspects what it presented, and proves refresh rotation — against the fake issuer. Live, `login_smoke_test.go` acquires through the broker but asks for `config.Scopes`, every scope the session already holds, so `sameScopeSet` compares a set against itself: the audience check is real and the scope check cannot fail. A deployment that flatly ignored narrowing would still print `granted`. `config.NarrowTarget()` exists for exactly this and is unused by the smoke run. The module half needs nothing further — the module never learns which issuer minted its token, so running it live proves nothing the deterministic chain does not.
- [x] If Asgardeo fails the narrowing experiment: login and session persistence still pass; broker acquisition refuses `auth.narrowing_unavailable`; the research doc records the verdict (Task 12). — moot in the favorable direction: both deployments honor narrowing (`make empirical-asgardeo`, 2026-08-06). The refusal path itself is pinned by `TestADeploymentThatCannotNarrowIsRefusedRatherThanGrantedMore`.
- [x] CI path acquires a client-credentials token inline in an acceptance test (Task 11). — `TestAnInlineIdentityAuthenticatesACommandWithNoLoginStep`, plus the missing-secret, cannot-narrow, non-interactive and secret-disclosure cases beside it.
- [x] `docs/research/asgardeo-redirect-uri-and-scope-narrowing.md` empirical cells filled in (Task 12). — Asgardeo §3 filled 2026-08-06; Identity Server 7.3.0 recorded in §3.1.

**One finding outran this plan.** Asgardeo binds an access token's `aud` to the
client ID and exposes no way to change it, while Identity Server 7.3.0 adds the
API resource identifier once it is registered as an audience. So the spec's
"the issued token's audience covers the requested product audience" is
satisfiable on one supported product and structurally not on the other. Nothing
in the broker needs to change — it verifies rather than assumes — but what
`products.<namespace>.audience` can *mean* differs by product, and that is a
decision for issue #17 rather than a task here.
