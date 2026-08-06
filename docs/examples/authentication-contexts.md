# Authentication context examples

**Status:** Illustrative examples
**Authoritative:** [Architecture](../architecture.md) §4.6, §4.7 ·
[Product requirements](../product-requirements.md) §7.2, §7.3
**Evidence:** [Authentication landscape](../research/wso2-authentication-landscape.md) ·
[Product authentication compatibility](../research/product-authentication-compatibility.md)

This document illustrates decisions recorded in the architecture and product
requirements. It does not extend them. Where this document and those disagree,
they win and this is wrong.

Field names are proposed until the schema is implemented.

## 1. Identity and context

- An **identity** is one login session, together with every product for which
  the shell can derive valid access from that session without another login or
  an independently supplied credential.
- A **context** is one named target, referencing exactly one identity.

> **Each context references exactly one identity. One identity may back several
> contexts.**

The two questions are separate, and keeping them separate is what makes the
model work:

| Question | Answered by |
| --- | --- |
| Which login is this? | the identity |
| Which products can that login reach? | the identity's `products` |
| Which organization and project am I working in? | the context |

### Sharing an issuer is not sharing an identity

Two products configured against the same issuer URL do **not** share an
identity unless that session can actually produce access each of them accepts.
The deciding property is whether a product delegates token validation to a
configurable external issuer or bundles its own resident one. A product that
validates only its own resident issuer cannot be pointed at a shared session at
all, whatever the configuration says.

This is a property of the running deployment, not of the file. The
configuration records an assertion; a wrong assertion surfaces as a typed
authentication or authorization failure when a command needs access, never as a
malformed document. Nothing here should be validated by attempting to prove
reachability at parse time.

### One login is not one token

One identity means one *login*, not one credential handed around. The broker
derives a separate short-lived token per product invocation, bound to that
product's audience/resource and to scopes.

How narrowly it can bind is a per-backend capability — full audience and scope
narrowing on some backends, scope selection only on others — so the broker
resolves a strategy per deployment and refuses rather than silently issuing
broader access. The context shape is identical either way: this is a broker
capability, not a configuration difference.

### As deployments improve, contexts collapse

A product that needs its own login today becomes part of a shared identity the
day its backend accepts the shared issuer. That is a configuration change with
no schema change: an identity disappears and its products move into the
remaining one. The model describes the estate as it is without baking today's
fragmentation into the design.

## 2. Shape

```yaml
identities:
  - name: <identity name>       # required, unique
    type: cloud | onprem        # selects defaults, never structure
    auth:                       # required, exactly one per identity
      kind: oauth-browser | oauth-device | client-credentials | pat
      issuer: <url>             # interactive OIDC kinds
      clientId: <id>            # the client this identity authenticates as
      tenant: <home tenant>     # optional; where the login lives, not what it targets
      credentialRef: <ref>      # OR a *Variable field; never a value
    products:                   # what this one login reaches
      <namespace>:
        endpoint: <url>
        audience: <resource id> # what the broker binds derived access to
        scopes: [<scope>, ...]

contexts:
  - name: <context name>        # required, unique
    identity: <identity name>   # required, exactly one
    organization: <org id>      # targeting
    project: <project id>       # targeting

defaultContext: <context name>
namespaceContexts:              # optional per-product bindings
  <namespace>: <context name>
```

Notes:

- `type` selects **defaults, not structure**. There is no `cloud:` block and no
  separate on-premises layout. It exists because cloud is the default and
  on-premises targeting is explicit, and because the shell must never infer that
  an on-premises endpoint supports shared SSO.
- `auth.tenant` is the home tenant the login belongs to at the issuer. It is
  deliberately *not* the context's `organization`, which is what the command
  targets and which the broker may reach through an organization-switch
  exchange on the same session. Two different things, two different names.
- `clientId` sits on the identity, because it is what the session authenticates
  as. A product needing its own separately authenticated client is not the same
  login and belongs to its own identity.
- `products` may be omitted for `type: cloud`, where the control plane resolves
  the set at login. It is required for `type: onprem`.
- Contexts are always stored explicitly. There is no implicit context, so
  `context list`, `context delete`, and export all operate on the same set.
  `wso2 context create` and `wso2 login` generate the one-identity/one-context
  case so that the common path costs no hand-editing.

## 3. Security rules

- Identities and contexts hold target metadata, an authentication kind, and
  non-secret references only.
- A `credentialRef` is an opaque reference to an entry in the OS secure store.
  It is not the credential, and it is not a capability.
- Fields ending in `Variable` hold environment-variable *names*, not values.
- Access tokens, refresh tokens, personal access tokens, passwords, client
  secrets, and private keys never appear in these files.
- CI injects secrets from its secret store. The shell reads them into job
  memory and does not persist them.
- An endpoint may not embed user information: `https://user:pass@host` is
  rejected rather than stored (`internal/contexts` enforces this today).

## 4. Everything in WSO2 Cloud

All products behind one cloud identity provider. One login, one context.

```yaml
identities:
  - name: acme-cloud
    type: cloud
    auth:
      kind: oauth-browser
      credentialRef: keychain://wso2/acme-cloud

contexts:
  - name: acme
    identity: acme-cloud
    organization: acme
    project: retail

defaultContext: acme
```

```shell
wso2 login              # one browser login
wso2 agent status
wso2 api status
wso2 integration status
```

`products` is omitted: the control plane resolves the reachable set at login.
Each command receives its own audience-bound token derived from the one
session.

This shape is correct exactly when every product validates that cloud issuer.
Where one of them still validates a different issuer, it is not part of this
identity, and the arrangement is §6 rather than this one.

## 5. Everything on-premises behind one identity provider

The customer runs the products themselves, with one shared identity provider in
front. Structurally identical to §4 — one login, one context — differing only in
`type` and in listing endpoints the CLI has no way to resolve.

```yaml
identities:
  - name: customer-idp
    type: onprem
    auth:
      kind: oauth-browser
      issuer: https://idp.customer.example
      clientId: wso2-cli
      credentialRef: keychain://wso2/customer-idp
    products:
      agent:
        endpoint: https://agent.customer.example
        audience: https://agent.customer.example
        scopes: [agent:read, agent:write]
      api:
        endpoint: https://api.customer.example
        audience: https://api.customer.example
        scopes: [api:read, api:write]
      integration:
        endpoint: https://esb.customer.example
        audience: https://esb.customer.example
        scopes: [integration:read]

contexts:
  - name: customer
    identity: customer-idp
    organization: customer

defaultContext: customer
```

```shell
wso2 login --context customer     # may also create the context; see §8
wso2 agent status
wso2 api status
wso2 integration status
```

Two things this example asserts, both of which can be wrong at runtime:

- every listed product accepts access derived from that shared session — if one
  validates only its own resident issuer, it does not belong here;
- the user is *authorized* for each. One login authenticates for all three; it
  does not authorize. A user who is authenticated but not entitled gets a typed
  authorization problem from that product, not a login prompt.

## 6. Some on-premises, some cloud, one legacy product

The realistic mixed estate: an on-premises Agent Manager behind its own
identity provider, an on-premises API Manager that only understands its own
credentials, and integration running in WSO2 Cloud. Three logins, so three
identities and three contexts.

```yaml
identities:
  - name: onprem-agent
    type: onprem
    auth:
      kind: oauth-browser
      issuer: https://thunder.own.example
      clientId: wso2-cli
      credentialRef: keychain://wso2/onprem-agent
    products:
      agent:
        endpoint: https://agent.own.example
        audience: https://agent.own.example
        scopes: [agent:read, agent:write]

  - name: onprem-api
    type: onprem
    auth:
      kind: pat                       # compatibility adapter; see §10
      credentialRef: keychain://wso2/onprem-api
    products:
      api:
        endpoint: https://api.own.example

  - name: acme-cloud
    type: cloud
    auth:
      kind: oauth-browser
      credentialRef: keychain://wso2/acme-cloud

contexts:
  - name: own-agent
    identity: onprem-agent
  - name: own-api
    identity: onprem-api
  - name: acme
    identity: acme-cloud
    organization: acme
    project: retail

defaultContext: acme
namespaceContexts:
  agent: own-agent
  api: own-api
```

```shell
wso2 agent login --url https://thunder.own.example   # creates own-agent, binds agent
wso2 api login   --url https://api.own.example       # creates own-api,   binds api
wso2 login                                           # creates acme, the default

wso2 agent status         # own-agent, via its binding
wso2 api status           # own-api
wso2 integration status   # acme, the default context
```

This is the arrangement the earlier per-product-authentication design handled by
accident and this one handles by rule. Three logins is the truth of this estate;
the configuration says so rather than hiding it behind one name.

Note what `namespaceContexts` is doing. Without it, every `agent` and `api`
command would need an explicit `--context`, because the default context cannot
reach those products. With it, each namespace resolves from a decision the
login commands recorded — not from the shell guessing which context looks
plausible.

`onprem-api` is a compatibility-adapter identity. Its access cannot be narrowed
or derived from, so a module reached through it does not carry the same trust
property as one reached through the other two. §10 states what that costs.

## 7. Several targets over one login

Where one login covers several projects or organizations, that is one identity
and several contexts. This is the case that makes the split earn its keep.

```yaml
identities:
  - name: acme-cloud
    type: cloud
    auth:
      kind: oauth-browser
      credentialRef: keychain://wso2/acme-cloud

contexts:
  - name: retail-dev
    identity: acme-cloud
    organization: acme
    project: retail-dev
  - name: retail-prod
    identity: acme-cloud
    organization: acme
    project: retail-prod
  - name: partner
    identity: acme-cloud
    organization: acme-partner      # reached by organization switch on the same session

defaultContext: retail-dev
```

```shell
wso2 login                                  # once, for all three
wso2 --context retail-prod api list         # no further authentication
wso2 context use partner                    # no further authentication
```

One `credentialRef`, one browser flow, three targets. Adding a fourth adds four
lines and no login. Reaching another organization is an exchange on the existing
session, not another credential.

## 8. Selection and login

### Which context

Highest precedence first:

1. `--context <name>`;
2. `WSO2_CONTEXT`;
3. `namespaceContexts[<invoked namespace>]`, if recorded;
4. `defaultContext`;
5. none — the command runs with no context, and anything needing access is
   refused by the broker with recovery guidance.

Step 3 is a recorded decision, not an inference. A binding exists only because
a command wrote it, and `wso2 context list` shows it. The shell never scans for
a context that happens to provide the namespace.

A named context that does not exist is a typed error listing the configured
contexts. A context naming an undeclared identity is a malformed document.

### Which product

The namespace is always explicit in the command, so the only question is whether
the selected context's identity reaches it. If not, the command **fails**,
naming the contexts that do and the flag or binding that would select one. The
shell does not switch identities on the user's behalf, because a different
identity is a different login.

### What login does

`wso2 login` authenticates the **identity** of the selected context. Where
several contexts share an identity, one login covers all of them.

`wso2 login --url <issuer>` and `wso2 <namespace> login --url <issuer>` may
create the context they authenticate, so a first run does not require
hand-written configuration. Creation names the context explicitly — from
`--context <name>` or a deterministic derivation — reports what it created, and
never silently replaces an existing context. The namespace form also records
the `namespaceContexts` binding.

`wso2 context use` writes the selection and stops: no network call, no login.
Creating or importing a context grants nothing;
[Architecture](../architecture.md) §4.7 records the five properties that make
that testable.

## 9. CI

CI is non-interactive and uses client credentials. There is no reusable session
to establish, so there is **no separate login step** — the shell acquires access
inline during the command.

```yaml
identities:
  - name: ci-release
    type: onprem
    auth:
      kind: client-credentials
      tokenEndpoint: https://idp.acme.example/oauth2/token
      clientIdVariable: WSO2_CI_CLIENT_ID
      clientSecretVariable: WSO2_CI_CLIENT_SECRET
    products:
      api:
        endpoint: https://api.acme.example
        audience: https://api.acme.example
        scopes: [api:read, api:write]
      integration:
        endpoint: https://esb.acme.example
        audience: https://esb.acme.example
        scopes: [integration:deploy]

contexts:
  - name: ci-release
    identity: ci-release
    organization: acme

defaultContext: ci-release
```

```shell
# Both variables are injected by the CI secret store. No wso2 login.
wso2 api deploy ./api.yaml
```

The shell holds the client secret, performs the token exchange itself, and hands
the module only the resulting short-lived access token. The secret never reaches
the module, the filesystem, the OS secure store, or a module environment. The
variable names, token endpoint, audiences, and scopes are non-secret metadata.

Browser and device authorization are invalid here and fail with a stable
configuration error rather than waiting for approval.

## 10. Authentication kinds

The legal kinds are recorded in [Architecture](../architecture.md) §4.7. Their
availability is per deployment, not universal:

| Kind | Where it is valid |
| --- | --- |
| `oauth-browser` | supported by every identity backend |
| `oauth-device` | only where the backend advertises the grant; the broker refuses otherwise |
| `client-credentials` | supported by every identity backend; the preferred CI method |
| `pat` | only for products that accept product-issued long-lived tokens |

Browser and device are **login modes for one interactive OIDC identity**, not
two stored kinds. `oauth-device` appears as a kind only where an identity can
*only* be established that way; otherwise the mode is chosen at login with
`--device-code`.

### The adapter tier

A kind is first-class only if the shell can derive short-lived, non-renewable
access from it. `oauth-browser`, `oauth-device`, and `client-credentials` all
qualify: the shell holds the long-lived material and mints something narrower.

A `pat` that a product accepts directly as bearer material does not. There is
nothing to derive, so the module receives material it can reuse after the
invocation ends and that does not expire on the broker's schedule. Such a
product is compatibility-adapter territory. It is supported, and the reduced
trust property is stated rather than hidden. Where a token *can* be exchanged
for a product session token, the derivation step is restored and it is
first-class again.

### Adding a kind

A kind may add its own non-secret fields; adding one does not change the
identity or context shape. Every kind obeys §3 whatever it adds.

Unknown members are tolerated on read, so a newer shell can record non-secret
facts an older one ignores, and one unsupported kind never makes a whole
document unreadable. They are **not** preserved on write: until a preservation
mechanism exists, an older shell rewriting a newer document drops what it did
not understand. A new *required* field is a schema revision, not an addition
within a version.

## 11. Architecture-proof development credential

Not a production kind and not a production shape. It exists only for the
non-production `wso2 reference status` proof, in the reference namespace alone,
and predates this model: no identity list, and a single flat `endpoint`. It is
kept here because it is the document the shell reads today, and because it obeys
the one rule everything above obeys — it names a credential source and never
holds a credential.

```json
{
  "schemaVersion": 1,
  "defaultContext": "reference-local",
  "contexts": [
    {
      "name": "reference-local",
      "organizationId": "reference-org",
      "endpoint": "http://127.0.0.1:8080",
      "auth": {
        "method": "development-credential",
        "credentialVariable": "WSO2_REFERENCE_DEV_CREDENTIAL"
      }
    }
  ]
}
```

The shell reads `WSO2_REFERENCE_DEV_CREDENTIAL` into memory, applies broker
policy, and exchanges it for a short-lived fixture token bound to the requested
audience and scope, the context's organization, and the current invocation. The
reference module receives that token and nothing else: not the credential, not
its source, and no way to renew what it was given.

Migrating it to the shape in §2 is schema-version work.

## 12. Open questions

- The on-disk format and path, and the split between this document and
  `~/.wso2/config.yaml` ([Architecture](../architecture.md) §8).
- Atomic-write and Windows-replace mechanics, which the research makes sharper:
  rotated refresh tokens need atomic single-writer persistence.
- Client provisioning. The research is explicit that a client identifier cannot
  be assumed to exist — it needs published public clients, per-tenant
  registration at context creation, or dynamic registration.
- Whether contexts need grouping across identities, so "everything in staging"
  is expressible where one environment spans several logins.
- Which of `context list | show | use | create | delete | import | export` are
  in the next slice, and whether identities need their own verbs.
- Preservation of unknown members on write, which becomes a data-loss path as
  soon as commands can write configuration.
