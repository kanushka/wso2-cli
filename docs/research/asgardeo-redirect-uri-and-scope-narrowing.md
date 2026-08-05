# Asgardeo: loopback redirect URIs and refresh-grant scope narrowing

**Status:** Research
**Research date:** 2026-08-04
**Scope:** Targeted follow-up to
[Product authentication compatibility §1.1 and §1.5.1](product-authentication-compatibility.md),
which marked two questions "unknown from public sources": (1) whether
Asgardeo accepts loopback (`127.0.0.1`) redirect URIs — fixed-port and/or
RFC 8252 §7.3 any-port — for a public/native OAuth2 client, and (2) whether
Asgardeo honors a narrower `scope` parameter on the `refresh_token` grant per
RFC 6749 §6. This feeds the broker decision on whether to refuse a
downscoping request outright or issue the broadest token available and
disclose the limitation.
**Source policy:** Same as the parent document — only public primary
sources: official product documentation (fetched live, not from training
data), public GitHub source code, and OpenAPI/REST reference content. Every
claim below is labeled **confirmed** (Asgardeo's own docs/API reference say
this explicitly), **inferred from WSO2 IS** (found in WSO2 Identity Server
docs or source, not stated by Asgardeo, but IS and Asgardeo share
`carbon-identity-framework`/`identity-inbound-auth-oauth` lineage per the
parent document's landscape research), or **unknown from public sources**
(no authoritative statement found; an empirical test is described instead).
**Rebrand note:** During this research, `wso2.com/asgardeo/docs/...` URLs
began redirecting to `wso2.com/identity-platform/docs/...` — the docs site
now carries a banner "Asgardeo is now WSO2 Identity Platform." Citations
below use the original `asgardeo/docs` paths (matching the parent document's
convention and still live via redirect); the rendered page title/URL is
"WSO2 Identity Platform" where noted.

## 1. Loopback redirect URI support

### 1.1 Fixed-port loopback (`http://127.0.0.1:<port>/callback`)

- **Confirmed, indirectly — a working example, not a documented rule.** The
  parent document already established that Asgardeo's React quickstart
  registers `http://localhost:5173` as an authorized redirect URL, proving a
  plain-HTTP, fixed-port loopback-hostname callback is registrable and
  functional on the hosted service.
  [React quickstart](https://wso2.com/asgardeo/docs/quick-starts/react/)
  This uses `localhost`, not the literal IP `127.0.0.1`; Asgardeo's docs
  never state whether the two are validated identically.
- **Confirmed absence of an explicit rule.** Asgardeo's own app-registration
  guide gives only generic, protocol-scheme-agnostic guidance and never
  mentions `127.0.0.1`, `localhost`, or "loopback" anywhere on the page
  (checked by full-text search of the live rendered page, not just a static
  fetch): "Web-based applications: Use exact URLs or implement logic to
  dynamically register specific redirect URIs as needed." / "Mobile apps
  with deep links: Wildcard support may be acceptable, but it must be
  implemented securely and restricted to well-defined patterns to limit its
  scope." Native/CLI/desktop apps using a loopback listener are not
  addressed as their own category.
  [Register a standard-based app](https://wso2.com/asgardeo/docs/guides/applications/register-standard-based-app/)
- **Confirmed — the REST API schema imposes no format restriction that would
  block it.** The Application Management API's `callbackURLs` field is typed
  as a plain `Array of strings` with description "This is the callback
  location where the tokens should be sent" — no scheme allowlist, no
  pattern restriction beyond the optional `regexp=` prefix convention (§1.3).
  Nothing in the schema disallows the string `http://127.0.0.1:8080/callback`
  as a registered value.
  [Application management API](https://wso2.com/asgardeo/docs/apis/application-management/),
  [OpenAPI schema (`applications.yaml`, `callbackURLs`)](https://github.com/wso2/identity-api-server/blob/master/components/org.wso2.carbon.identity.api.server.application.management/org.wso2.carbon.identity.api.server.application.management.v1/src/main/resources/applications.yaml)
- **Net verdict for 1.1:** a fixed-port loopback URI is very likely
  registrable and usable (nothing in the docs or schema forbids it, and the
  `localhost:5173` quickstart example demonstrates the closely related
  `localhost` case working end-to-end), but Asgardeo's own docs never say so
  explicitly for the literal `127.0.0.1` form. This is a documentation gap,
  not a contradiction.

### 1.2 Wildcard/any-port loopback (RFC 8252 §7.3 style)

- **Confirmed absence in Asgardeo's own docs.** Full-text search of the live
  rendered pages for "loopback", "127.0.0.1", and "RFC 8252" across the
  app-registration guide, the Application Management API reference, and the
  OAuth2 grant-types reference returned no matches. Asgardeo does not
  document any any-port or wildcard-port loopback behavior.
- **Inferred from WSO2 IS, not confirmed for Asgardeo.** WSO2 Identity
  Server's "Advanced Configurations" guide has a dedicated, explicit
  (collapsible, easy to miss on a static fetch) section titled "Click for
  information on configuring loopback callback URLs," which states verbatim:
  > "From IS 6.0.0 onwards, to comply with [RFC 8252 section 7.3], the
  > callback URL in the authorization request does not need to have an
  > exact port match to the callback URL registered here if it is a
  > loopback callback IP address. Exact port matches are not required if
  > you use loopback IP addresses (`127.0.0.1` and `[::1]`) only."

  With a concrete example: registering `http://127.0.0.1:8090/callback`
  also makes the following valid at request time:
  `http://127.0.0.1:8090/callback`, `http://127.0.0.1:16000/callback`, and
  `http://127.0.0.1:7500/callback`. It further states: "When registering
  multiple callback URLs using a regex pattern, do not specify the port
  number for the loopback callback URL either as a single port or as a
  capture group," with example
  `regexp=(https://myapp.com/callback|https://127.0.0.1/callback)`.
  [IS Advanced Configurations](https://is.docs.wso2.com/en/6.0.0/guides/login/oauth-app-config-advanced/)
  This is IS-version-gated documentation ("From IS 6.0.0 onwards") and was
  added in response to a doc-request issue tagged `Affected-6.0.0`, with no
  mention of Asgardeo.
  [wso2/product-is issue #14274](https://github.com/wso2/product-is/issues/14274)
- **Mechanism, for precision:** IS does not support a literal wildcard
  character in the registered redirect URI (e.g. no `http://127.0.0.1:*/callback`
  syntax exists anywhere in the docs). Instead, the *validator* special-cases
  any *registered* loopback URI to ignore the port when matching an
  *incoming* authorization request's `redirect_uri` — RFC 8252 §7.3
  compliance is achieved by relaxing the match rule, not by a wildcard
  registration value.
- **Unknown from public sources:** whether Asgardeo's hosted SaaS token/authorize
  endpoints run this same port-agnostic loopback matching. Asgardeo does not
  publish a mapping from its SaaS build to an IS version number, and its own
  docs are silent on the feature. This is the single biggest open question
  for the broker's ephemeral-port loopback pattern (RFC 8252 §7.3) on
  Asgardeo specifically.
  **Empirical test needed:** in a free Asgardeo trial tenant, register a
  standard-based (public/native) application with authorized redirect URL
  `http://127.0.0.1:8090/callback`. Start a local listener bound to a
  *different* port (e.g. `16000`), run the authorization code + PKCE flow
  with `redirect_uri=http://127.0.0.1:16000/callback`, and observe whether
  Asgardeo accepts the redirect (IS-parity) or rejects it with a
  redirect-URI-mismatch error (exact-match only, no IS parity).

### 1.3 Exact redirect URI validation rules

- **Confirmed by Asgardeo's own REST API reference and OpenAPI schema.**
  Default matching is exact string match against one of the registered
  `callbackURLs` array entries. A registered entry may instead use the
  literal prefix `regexp=(...)`, in which case the remainder is treated as a
  regular expression matched against the incoming `redirect_uri` — this is
  Asgardeo's documented mechanism for allowing more than one concrete
  callback URL (not for wildcarding a single URL's port or path). Documented
  example: `"regexp=(https://app.example.com/callback1|https://app.example.com/callback2)"`.
  [Application management API](https://wso2.com/asgardeo/docs/apis/application-management/)
  — the same example string appears verbatim in the upstream OpenAPI
  schema's `callbackURLs` field definition.
  [`applications.yaml`, `OpenIDConnectConfiguration.callbackURLs`](https://github.com/wso2/identity-api-server/blob/master/components/org.wso2.carbon.identity.api.server.application.management/org.wso2.carbon.identity.api.server.application.management.v1/src/main/resources/applications.yaml#L3921)
- **Confirmed by Asgardeo's app-registration guide, but only as loose
  prose, no concrete syntax.** "Web-based applications: Use exact URLs...";
  "Mobile apps with deep links: Wildcard support may be acceptable, but it
  must be implemented securely and restricted to well-defined patterns to
  limit its scope." No wildcard character or pattern syntax is given for
  Asgardeo itself here — this reads as general security guidance, not a
  syntax reference. The concrete regex mechanism lives only in the API
  reference (previous bullet).
  [Register a standard-based app](https://wso2.com/asgardeo/docs/guides/applications/register-standard-based-app/)
- **Inferred from WSO2 IS, not confirmed for Asgardeo:** both the regex
  prefix and the loopback port-agnostic behavior are version-gated in IS's
  own docs — "From IS 5.2.0 onwards, regex-based consumer URLs are
  supported when defining the callback URL" and "From IS 6.0.0 onwards..."
  for loopback (§1.2). Whether Asgardeo's validator implementation is at
  API/behavior parity with a specific IS version for either feature is
  unknown from public sources.
  [IS Advanced Configurations](https://is.docs.wso2.com/en/6.0.0/guides/login/oauth-app-config-advanced/)
- **Net verdict for 1.3:** exact match is the confirmed default; the
  `regexp=(...)` prefix for registering multiple concrete alternative URLs
  is confirmed and documented directly by Asgardeo. A true wildcard/pattern
  syntax for a *single* registered URL (as opposed to OR-ing several exact
  URLs via regex) is not documented by Asgardeo at all.

## 2. Scope narrowing on `refresh_token` grant

- **Confirmed absence of documentation in Asgardeo's own docs — and a
  telling asymmetry.** The official OAuth2 grant-types reference page's
  "Refresh token grant" section shows a complete, literal sample request and
  response:

  ```
  curl -k https://api.asgardeo.io/t/<organization_name>/oauth2/token \
  --header "Content-Type: application/x-www-form-urlencoded" \
  --header "Authorization: Basic <Base64Encoded(CLIENT_ID:CLIENT_SECRET)>" \
  --data-urlencode "grant_type=refresh_token" \
  --data-urlencode "refresh_token=<REFRESH_TOKEN>"
  ```

  No `scope` parameter appears in the request, the response
  (`access_token`/`refresh_token`/`token_type`/`expires_in`, itself notably
  missing a `scope` field), or the surrounding prose. By contrast, the
  **client_credentials** and **password** grant sections on the *same page*
  both show a documented `scope=<scopes>` request parameter and a `scope`
  field in the example token response. This asymmetry means Asgardeo's docs
  neither confirm nor deny refresh-time scope narrowing — the topic is
  simply never raised for this grant.
  [OAuth2 grant types — Refresh token grant](https://wso2.com/asgardeo/docs/references/grant-types/#refresh-token-grant)
- **Inferred from WSO2 IS source code, not confirmed for Asgardeo
  directly.** `RefreshGrantHandler.validateScope()` in
  `wso2-extensions/identity-inbound-auth-oauth` — the literal OAuth2
  grant-handler implementation underlying WSO2 IS, and per the parent
  document's landscape research the same lineage Asgardeo is built on —
  implements RFC 6749 §6 verbatim, per its own Javadoc:
  > "The requested scope MUST NOT include any scope not originally granted
  > by the resource owner, and if omitted is treated as equal to the scope
  > originally granted by the resource owner."

  The code (`validateScope`, reading `OAuth2AccessTokenReqDTO.getScope()` as
  `requestedScopes` and the token context's `getScope()` /
  `getAuthorizedInternalScopes()` as the granted set):
  - If `requestedScopes` is a subset of the originally granted scopes
    (regular + internal), it calls `tokReqMsgCtx.setScope(requestedScopes)`
    — the newly issued token is narrowed to exactly the requested subset.
    **This directly answers the "honor a narrower scope" question: yes, in
    this code path.**
  - If any requested scope is not in the originally granted set, the method
    returns `false`, which fails validation and surfaces as an
    `invalid_scope` error rather than silently widening or ignoring the
    request.
  - If no `scope` parameter is sent at all, the method returns `true`
    without narrowing, so the new token retains the original full granted
    scope set (RFC 6749 §6's "if omitted" clause, implemented as specified).
  [`RefreshGrantHandler.java`, `validateScope`](https://github.com/wso2-extensions/identity-inbound-auth-oauth/blob/master/components/org.wso2.carbon.identity.oauth/src/main/java/org/wso2/carbon/identity/oauth2/token/handlers/grant/RefreshGrantHandler.java)
- **Corroboration that this exact code path is live, shipping behavior (not
  dead code), with one known edge-case defect.**
  [wso2/product-is issue #19474](https://github.com/wso2/product-is/issues/19474)
  reports a real `invalid_scope` error ("Invalid Scope!") when re-requesting
  an *already-granted* internal management scope (e.g.
  `internal_mgt_user_update`) via `refresh_token` grant. The root cause
  described in the issue is that internal scopes are stripped from the
  comparison list *before* `validateScope` runs, so a legitimately-granted
  internal scope fails the subset check. This confirms the
  subset-scope-check / `invalid_scope`-rejection mechanism described above
  is real, currently-exercised behavior in a shipping WSO2 IS release — it
  is a reported bug in the internal-scope edge case specifically, not
  evidence against the general (non-internal) narrowing behavior.
- **Genuinely unknown from public sources:** whether Asgardeo's hosted
  token endpoint runs this same `RefreshGrantHandler` code path unmodified
  for ordinary (non-internal) API scopes, given Asgardeo's own docs never
  demonstrate or discuss a `scope` parameter on this grant.
  **Empirical test needed:** against a real Asgardeo tenant, obtain an
  access token with `scope=A B C` (e.g. via authorization code grant or
  client credentials to a suitable API resource), then call
  `POST /t/<org>/oauth2/token` with
  `grant_type=refresh_token&refresh_token=<...>&scope=A`. Inspect the
  returned access token's `scope` claim (decode the JWT, or call
  `/oauth2/introspect` with the new token) to determine which of three
  outcomes occurs: (a) the new token is narrowed to `scope=A` only
  (RFC 6749 §6 / IS-source behavior), (b) the `scope` parameter is ignored
  and the full `A B C` is returned, or (c) the request is rejected with
  `invalid_scope`.

## 3. Summary for the broker decision

The fourth column is the empirical one. Every cell in it is answered by a run
against a live deployment, and section 4 says exactly how to produce and record
one. A cell marked **pending live run** has not been measured — it is not a
negative finding, and nothing in the shell should be designed as though it were.

| Question | Confirmed (Asgardeo docs) | Inferred (WSO2 IS only) | Empirical verdict |
|---|---|---|---|
| Fixed-port loopback (`127.0.0.1:<port>`) registrable | `localhost:<port>` proven registrable via quickstart; REST schema imposes no blocking restriction | — | **Pending live run.** Any successful `make smoke-login` answers this incidentally: the walkthrough registers the literal `127.0.0.1` form on all four ports and the login binds one of them. Record: verdict, date, deployment. |
| Any-port loopback (RFC 8252 §7.3) | Not documented at all | IS 6.0.0+: exact port match waived for loopback IPs | **Pending live run.** `make empirical-asgardeo`, experiment A. Record the `ASGARDEO ANY-PORT LOOPBACK: {supported\|rejected}` verdict, its date, and the `deployment:` line the run printed under it. |
| Redirect URI validation rules | Exact match by default; `regexp=(url1\|url2)` prefix for OR-ing multiple exact URLs | Regex support IS-version-gated (5.2.0+); loopback flexibility IS-version-gated (6.0.0+) | **Not measured, and no experiment planned.** The open part is whether a true single-URL wildcard syntax exists, and an experiment can only ever fail to find one — absence of a syntax is not observable by trying one. This stays a documentation question. |
| Refresh-grant scope narrowing | Docs show no `scope` param on refresh_token grant at all (asymmetric vs. client_credentials/password sections, which do show one) | `RefreshGrantHandler.validateScope()`: subset requests honored and narrow the token; over-broad requests rejected with `invalid_scope`; omitted scope keeps full original grant | **Pending live run.** `make empirical-asgardeo`, experiment B. Record the `ASGARDEO REFRESH NARROWING: {honored\|ignored\|rejected}` verdict, its date, and the `deployment:` line. |

Both questions remain genuinely open for Asgardeo specifically; the WSO2 IS
evidence is suggestive (shared codebase lineage, per the parent document's
landscape findings) but not a substitute for a live test against an Asgardeo
tenant. The broker decision does not assume Asgardeo parity with IS on either
point: the shell verifies the narrowing it asked for and refuses
(`auth.narrowing_unavailable`) when it cannot prove it, which is the behavior
that is correct under every one of the three possible verdicts rather than the
behavior that bets on one.

## 4. Producing and recording the verdicts

**Added 2026-08-05.** The experiments described in §1.2 and §2 are implemented
and runnable. They were not runnable when this document was first written, which
is why the cells above still say pending: the harness exists, the tenant run does
not.

The runs live in `test/smoke/asgardeo_empirical_test.go` behind the `smoke`
build tag, so they never execute in the default test gate. To produce the
verdicts, against a real Asgardeo tenant and again against a local Identity
Server 7.x:

```sh
export WSO2_SMOKE_ISSUER='https://api.asgardeo.io/t/<org>/oauth2/token'
export WSO2_SMOKE_CLIENT_ID='<client id>'
export WSO2_SMOKE_AUDIENCE='<api resource identifier>'
export WSO2_SMOKE_SCOPE='<scope-a> <scope-b>'   # at least two

make empirical-asgardeo
```

Registering the application the variables describe is covered by
[the login walkthrough](../guides/login.md). What the verdict words mean, and
the one case that needs corroborating from the browser before it is believed
(`rejected` on the any-port experiment, which is observed as a flow that never
returns), is covered by
[`test/smoke/RUNNING.md`](../../test/smoke/RUNNING.md).

Recording is a manual edit to the fourth column above, and is part of executing
a live run rather than something the harness does. Record three things per cell
and nothing less: the verdict, the date, and the deployment the run printed
under it. The verdict lines are prefixed `ASGARDEO` because that names the
question; the `deployment:` line under each one is what says where the answer
came from, and a verdict recorded without it cannot be told apart from one
measured against an Identity Server.

### What each verdict would mean for the shell

- **Any-port `supported`:** Asgardeo is at IS parity, and the four-port
  registration in the walkthrough could in principle collapse to one. It should
  not, while IS deployments older than 6.0.0 remain in scope.
- **Any-port `rejected`:** exact-match only. The four registered ports are load-
  bearing, and the shell's refusal to fall back to an unregistered port is what
  keeps the failure legible.
- **Narrowing `honored`:** the broker's scoped refresh works as designed on
  Asgardeo, and a module receives exactly what it asked for.
- **Narrowing `ignored` or `rejected`:** brokered acquisition refuses with
  `auth.narrowing_unavailable` on Asgardeo. Login and session persistence are
  unaffected and still pass. That refusal is the designed outcome — the shell
  does not hand a module more authority than it requested — and is documented as
  such in the walkthrough's troubleshooting section. It is not a fallback and
  must not be relaxed into one.
