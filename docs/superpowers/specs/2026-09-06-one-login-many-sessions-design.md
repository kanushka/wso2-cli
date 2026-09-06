# Design: one login, one session per product, acquired through shared sign-on

**Status:** Approved design, 2026-09-06
**Builds on:** the measured journeys in the exercise directories
(`iam-apim-journey-findings.md`, `HANDOFF-auth-derived-grant.md`), the
research in `docs/research/2026-09-05-single-login-feasibility.md` and
`docs/research/2026-09-06-single-login-product-setup.md`, the derived-grant
design `2026-09-06-single-login-derived-grant-design.md` on
`claude/wso2-cli-auth-handoff-043a89`, and the broker in `internal/auth`.
**Decides:** the open question in section 7b of the derived-grant design,
and issue #43.

## 1. Goal

A person signs in once and every product command works, with no second
credential entry, no `--context`, no scope or audience typed by hand, and
no client secret on the machine.

```sh
wso2 iam connect http://localhost:8492            # records the product
wso2 apim connect https://localhost:9443           # records the product
wso2 login                                          # one browser sign-in
wso2 iam users list
wso2 apim apis list                                 # a tab opens, signs on, closes
wso2 apim gateway invoke /mockapi/1.0.0/status      # same
wso2 whoami                                         # per product: how access is obtained
```

The login provider is the user's choice: ThunderID, Identity Server or
Asgardeo. ThunderID is the reference deployment.

## 2. What was measured, and what it rules out

- ThunderID binds one authorization to one resource server and issues only
  that resource's scopes. Its token exchange rebinds the audience but never
  widens scopes.
- ThunderID and Identity Server both permanently narrow a refresh token's
  grant to the smallest scope set ever requested with it.
- API Manager's management plane accepts only tokens its own key manager
  issued, unless an upstream issuer is federated or trusted for jwt-bearer.
- ThunderID's single sign-on is a property of the authentication flow. An
  application on a flow with the session nodes gets a second authorization
  answered from the browser cookie with no credentials; the console flow
  family has them and `iam bootstrap` already places the CLI client there.
- The jwt-bearer derivation (Identity Server session to an API Manager
  token) works end to end through the shell for one derived product.

Together these rule out one refresh token serving several products with
different scopes. They do not rule out one *sign-in* doing so.

## 3. The rule that changes

Today an identity is one login and one session, and every product under it
is reached by narrowing that session per command. The new rule:

> An identity is one login provider and one person. It holds **one session
> per product**, and every session is obtained from the same browser
> sign-on. The person enters credentials once; the shell runs as many
> authorizations as the products need, and the provider's session answers
> the rest.

Each session is used for exactly one scope set, so the refresh-narrowing
behaviour of the providers is harmless, and the shell's existing proof that
a module receives exactly the scopes it asked for, bound to the product's
audience, is unchanged. No token ever carries another product's scopes.

The union-scope alternative, one refresh token for everything with the
audience bound per command, is rejected. It hands every module a token
carrying every product's scopes, and on ThunderID it cannot be obtained at
all.

## 4. Acquisition strategies

How a product's session is obtained follows from the shape of its record;
nothing pins it. A product with no `grant` is `direct` when it shares the
login product's scope set and resource, and `sibling` otherwise. A product
naming a `grant` is `derived` for a jwt-bearer grant and `federated` for a
federated grant, regardless of what the login session already covers. A
client-credentials identity has no session at all, and every product under
it is `inline`: a grant per command, at the product's own issuer when its
record names one. The shell shows the resulting strategy in `wso2 whoami`.

| Strategy | When it applies | Browser | What is stored |
| --- | --- | --- | --- |
| `direct` | the product has no grant and shares the login product's scope set and resource | none | the login session itself |
| `sibling` | the product has no grant but does not share the login product's scope set and resource (ThunderID's second resource server; Identity Server with a different scope set) | one tab, answered by sign-on | a second refresh token from the login provider |
| `derived` | the product record names a jwt-bearer grant at the product's own issuer, and the login session can yield the assertion | one tab, answered by sign-on, on first use | a refresh token from the login provider under the grant's assertion scopes, presented per command as a jwt-bearer assertion |
| `federated` | the product record names a federated grant: its own issuer is a public client federated to the login provider | one tab, answered by sign-on, at first use and again whenever the issuer will not renew the session | the product issuer's access and refresh tokens; the access token is served while valid |
| `inline` | the identity is client-credentials | none | nothing; a grant per command |

`direct` is today's `sessionSource`. `derived` is the `assertionSource`
from the derived-grant branch, given its own session under the product's
own key rather than sharing the login session's: it authorizes at the
login issuer for the grant's assertion scopes on first use, then presents
that session's identity token as a jwt-bearer assertion per command.
`federated` and `sibling` are the same code: an authorization code flow
with PKCE at an issuer, with a resource indicator and scope set taken from
the product record, storing the result under the product's own session
key. They differ only in which issuer and client ID they present.

A product whose record supports none of these is refused with
`auth.product_not_configured`, naming the `connect` command.

## 5. Sessions

The session store is keyed by credential reference **and product
namespace**. The login session keeps today's key so nothing a v2 shell
wrote becomes unreadable; product sessions are stored beside it as
`<credentialRef>.<namespace>` — a dot, which a credential reference can
never contain, so the entry can never collide with another identity's.
Each carries the strategy that produced it, the issuer and client ID it
belongs to, and its scope set.

`wso2 login` runs the login authorization, then, for every recorded
product whose strategy needs its own session, runs that authorization too,
in sequence, in the same browser. It reports one line per product.
`wso2 login --only <namespace>` acquires one; `wso2 login --no-products`
acquires only the login session.

A command whose product has no session acquires one on first use. Before
the browser opens the shell prints one line naming the product and the
issuer. Under `--no-input` or `WSO2_NO_INPUT` it refuses instead with
`auth.session_required`, naming `wso2 login --only <namespace>`.

A federated session is served from the access token its authorization
returned while that token is valid and carries the request's scopes; API
Manager, measured, refuses to renew a management scope on a refresh at
all. When the token has expired and the refresh comes back refused or
narrower than asked, the shell authorizes the product once more through
the sign-on, exactly as on first use, and refuses only if that still cannot
serve the request.

`wso2 logout` revokes every session the identity holds, best effort, as
ADR 0010 already allows for one, and then opens each identity provider's
end-session page, once per provider and client, with the client id and the
identity token the session recorded (ThunderID requires the client;
API Manager shows a consent page without the token). That is what makes
the next `wso2 login` prompt for credentials again. `--keep-browser-session`
leaves the providers' sessions in place, as does `--no-input` or
`WSO2_NO_INPUT`. `wso2 whoami` and `wso2 doctor` report each product:
strategy, session present or absent, expiry when disclosed.

The rotation lock stays per session key; two products never contend.

## 6. Product records and setup

The eight-flag `wso2 identity create` and `wso2 identity add-product`
remain for hand-written documents, but the supported path is product
first:

```sh
wso2 <namespace> connect <url> [flags]
```

`connect` is a shell command, not a module one. The shell resolves a
product namespace before the module runs and reads `connect` off the front
of the arguments there, exactly as it reads `--context` and `--output` off
a product command line: the module never sees it, and the document is
written by the shell, which writes nothing to the secure store (ADR 0012).
A module enables it by declaring a **product descriptor** in its manifest
(`modules/<namespace>/module.json`), inside `capabilities`, so the
descriptor travels with the capabilities through the catalog, the release
record and the installed receipt without any of those growing a second
carrier. A module whose receipt carries no descriptor has `connect`
refused with `shell.connect_unsupported`, naming `wso2 identity
add-product`. The descriptor says:

| Member | Meaning |
| --- | --- |
| `provider` | the identity provider this product *is*, when a login can run against it (`thunder`, `identity-server`, `asgardeo`); empty for a product that is not a login provider |
| `issuerPath` | what is appended to the `connect` URL to name the issuer (ThunderID: nothing; API Manager: `/oauth2/token`) |
| `clientId` | the public client the shell presents at that issuer, as the product's bootstrap registers it; empty when the deployment assigns one and `--client-id` must carry it |
| `audience` | `resource` when the token audience is a resource server URI (ThunderID, with `defaultAudience` naming the seeded system server) or `client` when it is the client id (API Manager) |
| `scopes` | the scopes the product's commands need; the session is authorized for exactly these |
| `grant` | the grant kind the product needs when it is not the login provider (`federated` for API Manager); empty for a product only its own provider serves |
| `machine` | the strategies a client-credentials identity may use: `inline` (the machine client at the login provider is minted per product) or `credential` (the product needs a credential of its own) |

What `connect` records follows from the descriptor and the flags:

- A product whose descriptor names a `provider` is a login provider. With
  no identity selected, or `--identity <name>` naming one that does not
  exist, `connect` creates the identity, named after the provider unless
  `--identity` says otherwise, with a same-named context, selected when
  none is; the product is recorded direct. With `--client-id` and
  `--client-secret-variable` the identity is a client-credentials one,
  for a pipeline, and the descriptor's `machine` must allow `inline`. When
  the selected identity already has that issuer, the product is recorded
  on it instead.
- A product whose descriptor names no `provider` attaches to the selected
  identity, under the grant its descriptor names: the grant's issuer is
  the `connect` URL plus `issuerPath`, its client the descriptor's or
  `--client-id`, the audience the client id or `--audience`. With several
  identities, `--login-provider <issuer-url>` picks the one with that
  issuer. With none, `connect` is refused with
  `shell.login_provider_required`, naming the provider's own `connect`
  command to run first; the shell does not ask for an issuer it could
  build no identity from.
- On a client-credentials identity, a product whose `machine` list has no
  `inline` is refused at write time with `auth.product_not_configured`
  unless `--client-id-variable` and `--client-secret-variable` name the
  product's own credential (spec section 8's fallback); the refusal names
  those two flags.
- `--scopes` and `--audience` override the descriptor's defaults;
  `--replace` overwrites a recorded product, which is otherwise refused
  with `contexts.product_exists`.

The first direct product `connect` records on an identity is pinned as
the identity's `loginProduct`, and `wso2 login` authorizes it first.
Without the pin the login product is the first direct product by
namespace, so recording a product that sorts earlier would move the login
session from under the sessions already stored; the pin is what keeps it
where it was. Documents that predate the field keep the namespace order.

A module bootstrap that registers the CLI on the product (as `iam
bootstrap` and `apim bootstrap` do) prints the `connect` line to run next,
rather than the identity create line it printed before. It keeps printing
a command rather than returning a record the shell writes: a structured
result the shell acted on would be a protocol change for one line of
output, and the module contract stays as it is.

`wso2 login` with no product recorded on a ThunderID identity is refused
before the browser opens, naming `connect`. This closes the first-login
failure recorded in the gap analysis.

## 7. Command surface

- A module request with no scopes means the product's recorded scopes; the
  record stays the ceiling. Modules stop needing `--scope` on every command.
- `--no-input` reaches product commands, as `WSO2_NO_INPUT` already does:
  the shell reads it off the product command line as its own flag, the
  broker's first-use acquisition refuses under it, and the module is handed
  `WSO2_NO_INPUT` so it needs no second spelling.
- `wso2 whoami`, `wso2 doctor` and `wso2 logout` treat a client-credentials
  identity as healthy without a session, and `logout` exits 0.
- `wso2 org use` is refused on a provider without organization switch,
  naming the provider, rather than accepted and then breaking every call.
- `--context` is needed only when several identities exist;
  `namespaceContexts` keeps working for the mixed estate.

## 8. Machines: CI and headless hosts

A pipeline has no browser and no sign-on cookie, so nothing called a
session is shared there. What is shared is a credential every product
honours, or a token one product accepts from another's issuer.

- **Client credentials** is the CI method. One machine client at the login
  provider, given roles on every product. There is no session, so every
  product's strategy is `inline`; the inline source mints **per product**,
  on ThunderID a token request carrying that product's resource indicator,
  on Identity Server and Asgardeo that product's scopes, measured on
  ThunderID in exercise 1. The derived-from-machine-token route — presenting
  the machine token as a jwt-bearer assertion at a product's own issuer — is
  withdrawn: API Manager refuses every `at+jwt` assertion, measured in
  `docs/research/2026-09-06-single-login-spikes.md`. A product reached at
  its own issuer under a machine identity instead carries a product
  credential, `clientIdVariable` and `clientSecretVariable`; the pipeline
  then holds two secrets, still on one identity. A product with a grant and
  no credential of its own is refused with `auth.kind_not_implemented`: the
  target issuer does not accept the identity's machine client, and this
  release reads no other credential for it without being told.
- **Device code** is browser login for a host without a browser, not a CI
  method. It yields a refresh token like browser login, so `direct` and
  `derived` are unchanged; `sibling` and `federated` each show one more
  code, approved once, and the sessions then persist. Exchange at the login
  issuer may later reduce that to one code on Identity Server and Asgardeo;
  on ThunderID it cannot.
- **Personal access tokens** are product-issued and opaque. They stay the
  compatibility adapter: one per product, no derivation, no narrowing
  proof.

`wso2 whoami`, `wso2 doctor` and `wso2 logout` treat a client-credentials
identity as healthy with no session, and `logout` exits 0. Under
`--no-input` a product with no session and no inline credential is refused
with `auth.session_required`.

Two unmeasured links, each with a fallback: whether API Manager maps a
machine client to the roles carrying `apim:*` scopes (fallback: the
product-level secret above), and whether ThunderID grants `system` to a
client-credentials client (fallback: `iam` administration from CI keeps
the user-login script ThunderID's own pipelines use).

## 9. Product-side setup

No product code changes are required. Each needs configuration the CLI
applies where it can and documents where it cannot:

- **ThunderID**: the CLI client on a flow with the sign-on nodes. Done by
  `iam bootstrap` today; a seeded `wso2-cli` client on such a flow is the
  product ask.
- **API Manager**: the login provider as a federated OpenID identity
  provider with group-to-role mapping, and a dedicated public CLI service
  provider with mandatory PKCE and authentication without a client secret,
  using that provider. `apim bootstrap` applies it given an administrator
  password. Alternatively the jwt-bearer trust the derived-grant branch
  proved.
- **Agent Manager**: untested. If it validates an external issuer's tokens
  directly it needs only trust configuration; if it has a resident issuer
  it needs the same federation as API Manager.

## 10. Testing

- `internal/contexts`: a product record with a pinned strategy round-trips;
  an unknown strategy is refused as malformed; a ThunderID identity may
  record several products.
- `internal/auth/session`: product-keyed sessions save, load and delete
  beside the login session; a legacy store with only the login session
  still loads.
- `internal/auth`: the fake issuer gains a second resource server, a
  second client and a federation to a second fake issuer. Tests cover each
  strategy being chosen and refused, a product session used with exactly
  its scopes, first-use acquisition refused under no-input, and no denial
  carrying a token.
- `internal/app`: `connect` writes the record from a descriptor; `login`
  acquires every product and reports each; `whoami` shows per-product
  state.
- Live, counting credential prompts and browser redirects separately, from
  an empty home, then again after a shell restart and after access-token
  expiry, then as a user without management rights:
  1. ThunderID login serving `iam` (direct) and the gateway (sibling).
  2. The same plus API Manager management (federated, or derived).
  3. Identity Server login serving the same three.
  4. Asgardeo, when a tenant is available.
  5. CI: one machine client, from an empty home with no browser,
     `iam users list`, the gateway, and `apim apis list`.

## 11. Order of work

0. **Spike:** a dedicated public CLI service provider on API Manager,
   federated to ThunderID, answering an authorization code flow from the
   shell's loopback with a management-scoped token. This is the one
   unmeasured link; everything else in section 4 is measured. Its result
   decides whether `federated` or `derived` is API Manager's default.
   Beside it: a ThunderID machine token accepted by API Manager's
   management plane, and `system` on a ThunderID client-credentials
   token; these decide how many secrets a pipeline holds (section 8).
   **Measured** (`docs/research/2026-09-06-single-login-spikes.md`):
   federation answers with a management-scoped token and no prompt, so
   `federated` is API Manager's interactive default; ThunderID grants
   `system` to a machine client; API Manager refuses every `at+jwt`
   assertion, so CI reaches API Manager management with the product-level
   secret.
1. ADR: one login, one session per product, no union tokens. Close #43.
2. Product-keyed sessions and the `sibling`/`federated` source; `login`
   acquires products; `whoami`/`doctor`/`logout` per product.
3. Fold in the derived-grant branch as the `derived` strategy.
4. Product descriptors, `connect`, bootstrap results the shell writes.
5. Section 7's command-surface fixes.
6. The live matrix in section 9.

## 12. Security notes

- No token carries more than one product's scopes. The exact-scope and
  audience proof applies to every strategy.
- Product sessions are refresh tokens in the OS secure store, like the
  login session; modules never see them.
- The CLI holds no client secret. Every client it presents is public with
  PKCE. A server-side federation secret lives on the product server.
- A tab opened for acquisition is the same loopback flow as login: state
  checked, one-use callback, fixed registered ports.
- A product that maps no role for the user issues no management scope, and
  the exact-scope check refuses it before the module runs.
