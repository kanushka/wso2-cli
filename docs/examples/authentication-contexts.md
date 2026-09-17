# Authentication context examples

**Status:** Illustrative examples
**Authoritative:** [Architecture](../architecture.md) §4.6, §4.7 ·
[Product requirements](../product-requirements.md) §7.2, §7.3
**Evidence:** [Authentication landscape](../research/wso2-authentication-landscape.md) ·
[Product authentication compatibility](../research/product-authentication-compatibility.md)

This document illustrates decisions recorded in the architecture and product
requirements. It does not extend them. Where this document and those disagree,
they win and this is wrong.

The field names below are implemented: `internal/contexts` serializes and
validates them, and `schemaVersion` is how the shell tells one shape from
another. A document naming an unknown version fails closed rather than being
guessed at, so changing a field name is a schema change and needs a version.
The current schema is version 4 ([ADR 0016](../adr/0016-a-context-owns-its-login-and-sessions.md)):
one list of contexts, each holding its own login, products and sessions.

## 1. Context

A **context** is one named target: how the shell logs in (its `login`), every
product it can reach from that login (its `products`), the secure-store
entries its sessions live under (its `credentialRef`), and optionally the
organization and project to act within.

> **Each context owns its login and its sessions. No two contexts share a
> `credentialRef`.**

Earlier schemas split this into an *account* (the login and products) and a
context naming it, so that several contexts could share one login. That
sharing is what made one context's change end or rebind another's sessions,
and it is gone: two targets that need the same login are two contexts that
each log in, or one context whose organization `wso2 org use` switches. See
ADR 0016.

| Question | Answered by |
| --- | --- |
| Which login is this? | the context's `login` |
| Which products can that login reach? | the context's `products` |
| Which organization and project am I working in? | the context's `organization` and `project` |

### Sharing an issuer is not sharing a login

Two products configured against the same issuer URL do **not** share a login
unless that session can actually produce access each of them accepts. The
deciding property is whether a product delegates token validation to a
configurable external issuer or bundles its own resident one. A product that
validates only its own resident issuer cannot be pointed at a shared session at
all, whatever the configuration says.

This is a property of the running deployment, not of the file. The
configuration records an assertion; a wrong assertion surfaces as a typed
authentication or authorization failure when a command needs access, never as a
malformed document. Nothing here should be validated by attempting to prove
reachability at parse time.

### One login is not one token

One login means one *sign-on*, not one credential handed around and not one
token. The person enters credentials once; the shell then runs one
authorization per product the context records, each answered from that same
sign-on, and stores the result as that product's own session under
`<credentialRef>.<product>`. No session ever carries another product's scopes.

How a product's session is obtained is decided per product, from the shape of
its record and from what the deployment supports, in six strategies:

- `direct` — the login session itself already covers the product's scopes and
  resource; nothing further is stored.
- `sibling` — the login provider answers for the product too, but under a
  scope set or resource the login session does not already cover, so a second
  session is obtained there, silently, in the same sign-on.
- `exchanged` — the product names an `exchange` grant: it accepts the login
  provider's own tokens bound to its audience, so no session of its own is
  kept; the login session's access token is exchanged (RFC 8693) per command.
- `derived` — the product names a jwt-bearer grant: its own session is
  obtained at the login issuer, through one browser tab on first use, for the
  grant's assertion scopes; an identity token from that session is presented
  at the product's own issuer per command.
- `federated` — the product names a federated grant: its own issuer is a
  public client federated to the login provider, so its session is obtained
  through one browser tab, answered by the same sign-on.
- `inline` — a client-credentials context holds no session at all; access is
  a grant per command, at the product's own issuer when its record names one.

A stored session records what it was authorized for — issuer, client, scopes,
resource and strategy — and is presented only for a record that still asks for
exactly that. A command that changes a record ends the sessions it no longer
matches before it writes.

## 2. Two files, two roles

The **local document** (`contexts.json`) is complete: every value a login and
a command need is written out, and the shell reads nothing else at command
time. The **input file** is short and shareable: it leaves out whatever the
installed products' descriptors already know, and `wso2 context apply -f`
fills it in and writes the complete record. A later product update therefore
changes no authentication behaviour until the file is applied again
("frozen defaults"); `wso2 doctor` and `wso2 context show` say when a record
differs from what the installed product would write now.

### The local document

```yaml
schemaVersion: 4
defaultContext: <context name>   # optional; absent selects nothing
contexts:
  - name: <context name>         # required, unique
    type: cloud | onprem         # selects defaults and wording, never structure
    credentialRef: <ref>         # interactive kinds; unique; never follows a rename
    login:
      kind: oauth-browser | oauth-device | client-credentials | pat
      issuer: <url>
      clientId: <id>             # the client this context logs in as
      tenant: <home tenant>      # optional; where the login lives
      provider: asgardeo | identity-server | thunder   # optional
      narrowing: scoped-refresh | token-resource       # optional; wins over provider
      clientSecretVariable: <VAR>   # client-credentials only; a name, never a value
      product: <namespace>       # the direct product the login runs for
    organization: <org id>       # targeting
    project: <project id>        # targeting
    products:                    # what this login reaches
      <namespace>:
        url: <url>
        audience: <resource id>  # what the broker binds derived access to
        scopes: [<scope>, ...]
        grant:                   # optional: when the login session does not cover it
          kind: exchange | jwt-bearer | federated
          issuer: <url>          # jwt-bearer, federated: the product's own issuer
          clientId: <id>         # jwt-bearer, federated: the public client there
          scopes: [<scope>, ...] # jwt-bearer only: assertion scopes
          resource: <uri>        # resource indicator, when required
        clientIdVariable: <VAR>     # product credential: client-credentials
        clientSecretVariable: <VAR> # contexts only
        gateway:                 # optional: the product's gateway record
          url: <url>
          audience: <uri>
          scopes: [<scope>, ...]
```

(The shell reads JSON; YAML is used here for the annotations.)

### The input file

```yaml
contexts:
  - name: <context name>
    login:
      product: <namespace>       # a login provider among products; or
      issuer: <url>              # an issuer and client, for a login with no product
      clientId: <id>
    products:
      <namespace>:
        url: <url>               # the one required member
        version: <version>       # optional: the version an install pins
        gateway: { url: <url> }
```

Every member of the local document may also appear, and is taken as written.
Two may not: `credentialRef` and `defaultContext` belong to one machine, and a
file naming either is refused. Unknown members are refused too, because the
file is written by hand and a misspelling silently ignored would be a default
nobody asked for.

Notes:

- `type` selects **defaults and wording, not structure**. It is derived from
  the issuer when not given.
- `login.tenant` is the home tenant the login belongs to at the issuer. It is
  deliberately *not* the context's `organization`, which is what commands
  target. Two different things, two different names.
- `login.product` is required once a context reaches a direct product, so that
  recording a product which sorts earlier can never move the login session from
  under the sessions already stored. The writing commands set it.
- `grant` names how one product is reached when the login session does not
  already cover it: `exchange` exchanges the login session per command,
  `jwt-bearer` presents an identity token at the product's own issuer, and
  `federated` signs in there as its own public client through the same browser
  sign-on. Omitting `grant` leaves the product direct or sibling.

## 3. Security rules

- Contexts hold target metadata, an authentication kind, and non-secret
  references only.
- A `credentialRef` is an opaque reference to an entry in the OS secure store.
  It is not the credential, and it is not a capability.
- Fields ending in `Variable` hold environment-variable *names*, not values.
- Access tokens, refresh tokens, personal access tokens, passwords, client
  secrets, and private keys never appear in these files.
- CI injects secrets from its secret store. The shell reads them into job
  memory and does not persist them.
- A URL may not embed user information: `https://user:pass@host` is rejected
  rather than stored, and the rejected value is never echoed.

## 4. Everything in WSO2 Cloud

All products behind one cloud identity provider. One login, one context.

```yaml
contexts:
  - name: acme
    type: cloud
    credentialRef: acme
    login:
      kind: oauth-browser
      issuer: https://api.asgardeo.io/t/acme/oauth2/token
      clientId: <published client>
    organization: acme
    project: retail
defaultContext: acme
```

```shell
wso2 login              # one browser login
wso2 agent status
wso2 api status
```

`products` is omitted: the target is that the control plane resolves the
reachable set at login (WSO2 Cloud login is not in this release). Each command
receives its own audience-bound token derived from the one session. Another
organization in the same cloud is `wso2 org use <org>` on this context, not a
second context.

## 5. Everything on-premises behind one identity provider

The customer runs the products themselves, with one identity provider in
front. A platform team writes the short form once:

```json
{
  "contexts": [
    {
      "name": "local",
      "login": { "product": "identity" },
      "products": {
        "identity": { "url": "http://localhost:8501" },
        "api": { "url": "http://localhost:9251", "gateway": { "url": "http://localhost:9091" } }
      }
    }
  ]
}
```

```shell
wso2 context apply -f team-context.json --use local
wso2 login
wso2 identity users list
wso2 api gateway apis list
```

`apply` installs `identity` and `api` when they are missing, and writes the
complete record the installed descriptors produce:

```json
{
  "name": "local",
  "type": "onprem",
  "credentialRef": "local",
  "login": {
    "kind": "oauth-browser",
    "issuer": "http://localhost:8501",
    "clientId": "wso2-cli",
    "provider": "thunder",
    "product": "identity"
  },
  "products": {
    "identity": { "url": "http://localhost:8501", "audience": "https://localhost:8090/mcp", "scopes": ["system"] },
    "api": {
      "url": "http://localhost:9251", "audience": "http://localhost:9251",
      "grant": { "kind": "exchange" },
      "gateway": { "url": "http://localhost:9091", "audience": "http://localhost:9091" }
    }
  }
}
```

Two things this example asserts, both of which can be wrong at runtime:

- every listed product accepts access derived from that session. If one
  validates only its own resident issuer, it does not belong here;
- the user is *authorized* for each. One login authenticates for all of them;
  it does not authorize. A user who is authenticated but not entitled gets a
  typed authorization problem from that product, not a login prompt.

## 6. Some on-premises, some cloud, one legacy product

The realistic mixed estate: an on-premises Agent Manager behind its own
identity provider, an on-premises API Manager that only understands its own
credentials, and integration running in WSO2 Cloud. Three logins, so three
contexts.

```yaml
contexts:
  - name: own-agent
    type: onprem
    credentialRef: own-agent
    login:
      kind: oauth-browser
      issuer: https://thunder.own.example
      clientId: wso2-cli
      provider: thunder
      product: agent
    products:
      agent:
        url: https://agent.own.example
        audience: https://agent.own.example
        scopes: [agent:read, agent:write]
      agent-gateway:
        url: https://gateway.own.example
        audience: https://gateway.own.example
        scopes: [gateway:invoke]

  - name: own-api
    type: onprem
    credentialRef: own-api
    login:
      kind: pat                        # compatibility adapter; see §10
    products:
      api:
        url: https://api.own.example

  - name: acme
    type: cloud
    credentialRef: acme
    login:
      kind: oauth-browser
      issuer: https://api.asgardeo.io/t/acme/oauth2/token
      clientId: <published client>
    organization: acme

defaultContext: acme
```

```shell
wso2 login --context own-agent
wso2 login                        # acme, the default
wso2 --context own-agent agent status
wso2 integration status           # acme
```

Three logins is the truth of this estate; the configuration says so rather
than hiding it behind one name. `own-agent` shows that a Thunder-bound context
is not limited to one product: Thunder accepts only one resource indicator per
authorization, but that binds one *session* to one resource, not one context to
one product. `agent` is `direct` and `agent-gateway` is `sibling`, a second
session obtained from the same provider under its own resource, in the same
`wso2 login`.

## 7. Several targets

Where one login covers several organizations, that is one context and
`wso2 org use`:

```shell
wso2 login
wso2 org use acme-partner         # the same context, another organization
```

Where two targets must be selectable by name, they are two contexts, and each
logs in. Their sessions are separate by design: a change to one never ends or
rebinds the other's, and logging out of one leaves the other signed in.

```shell
wso2 context apply -f team-context.json   # declares retail-dev and retail-prod
wso2 login --context retail-dev
wso2 login --context retail-prod          # a second sign-on, usually silent
wso2 --context retail-prod api list
```

## 8. Selection and login

### Which context

Highest precedence first:

1. `--context <name>`;
2. `WSO2_CONTEXT`;
3. `defaultContext`;
4. none. The command runs with no context, and the broker refuses anything
   needing access, with recovery guidance.

A named context that does not exist is a typed error listing the configured
contexts. `wso2 context apply` never changes the selection unless `--use` is
given, so a team file does not move an existing user's active environment.

### Which product

The namespace is always explicit in the command, so the only question is whether
the selected context reaches it. If not, the command **fails**, naming the
command that records it. The shell does not switch contexts on the user's
behalf, because a different context is a different login.

### What login does

`wso2 login` authenticates the selected context: its login session, then one
session per further product it records, from the same sign-on.

`wso2 login --url <issuer> --client-id <id>` creates the context it
authenticates, so a first run needs no file. `wso2 context create <name>
--login-product <product> --url <url>` and `wso2 context product add` write a
context from a product's descriptor; `wso2 context apply -f` writes contexts
from a shared file. None of them makes a network call besides installing a
missing product, and none grants anything: writing a context records public
configuration only ([ADR 0012](../adr/0012-writing-a-context-or-identity-grants-nothing.md)).

## 9. CI

CI is non-interactive and uses client credentials. There is no reusable session
to establish, so there is **no separate login step**. The shell acquires access
inline during the command.

```json
{
  "contexts": [
    {
      "name": "ci-release",
      "type": "onprem",
      "login": {
        "kind": "client-credentials",
        "issuer": "https://idp.acme.example/oauth2/token",
        "clientId": "ci-release",
        "clientSecretVariable": "WSO2_CI_CLIENT_SECRET"
      },
      "organization": "acme",
      "products": {
        "api": { "url": "https://api.acme.example", "audience": "https://api.acme.example", "scopes": ["api:read", "api:write"] }
      }
    }
  ]
}
```

```shell
wso2 context apply -f ci-context.json --use ci-release --no-input
# The secret is injected by the CI secret store. No wso2 login.
wso2 api deploy ./api.yaml
```

The shell holds the client secret, performs the token exchange itself, and hands
the module only the resulting short-lived access token. The secret never reaches
the module, the filesystem, the OS secure store, or a module environment.

Browser and device authorization are invalid here and fail with a stable
configuration error rather than waiting for approval.

## 10. Authentication kinds

The legal kinds are recorded in [Architecture](../architecture.md) §4.7. Their
availability is per deployment, not universal:

| Kind | Where it is valid | Today |
| --- | --- | --- |
| `oauth-browser` | supported by every OIDC backend | implemented |
| `oauth-device` | only where the backend advertises the grant; the broker refuses otherwise | implemented |
| `client-credentials` | supported by every OIDC backend; the preferred CI method | implemented |
| `pat` | only for products that accept product-issued long-lived tokens | validates, refuses at use with `auth.kind_not_implemented` |

The last column is what the shell implements, not a property of the kind. A
document naming a deferred kind loads and validates, deliberately, so that
configuration written ahead of the shell stays readable. It refuses only when
a context using it is actually selected. Examples below that use `pat`
therefore describe intended shape, not something to run today.

Browser and device are **login modes for one interactive OIDC context**, not
two stored kinds. `oauth-device` appears as a kind only where a context can
*only* be established that way; otherwise the mode is chosen at login with
`--device-code`.

That flag is not in this release. Until it arrives, a context that could be
established either way declares `oauth-browser` and is established that way, and
`oauth-device` is the kind for a context where the browser mode is not
available at all: a deployment that cannot register the loopback callback URLs,
or one whose users are only ever on machines with no reachable browser. A
developer who merely *happens* to be on a headless machine today is served by a
second context, not by this kind; that is the gap `--device-code` closes.

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
context shape. Every kind obeys §3 whatever it adds.

Unknown members are tolerated on read, so a newer shell can record non-secret
facts an older one ignores, and one unsupported kind never makes a whole
document unreadable. They are **not** preserved on write: until a preservation
mechanism exists, an older shell rewriting a newer document drops what it did
not understand. A new *required* field is a schema revision, not an addition
within a version.

## 11. Architecture-proof development credential

Not a production kind and not a production shape. It exists only for the
non-production `wso2 reference status` proof, in the reference namespace alone,
and predates this model: a single flat `endpoint`. It is kept here because the
shell still reads it (and never rewrites it), and because it obeys
the one rule everything above obeys: it names a credential source and never
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

It is read into the §2 shape in memory and never written back.

## 12. Open questions

- The on-disk format and path, and the split between this document and
  `~/.wso2/config.yaml` ([Architecture](../architecture.md) §8).
- Atomic-write and Windows-replace mechanics, which the research makes sharper:
  rotated refresh tokens need atomic single-writer persistence.
- Client provisioning. The research is explicit that a client identifier cannot
  be assumed to exist. It needs published public clients, per-tenant
  registration at context creation, or dynamic registration.
- Whether contexts need grouping, so "everything in staging" is expressible
  where one environment spans several logins.
- Deployment discovery, so a bare URL can produce an input file
  (docs/plans/deployment-discovery-handoff.md).
- Preservation of unknown members on write, which becomes a data-loss path as
  soon as commands can write configuration.
