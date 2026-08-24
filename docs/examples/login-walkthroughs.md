# Login walkthroughs

**Status:** Proposal, pending review — **target user experience, not slice 1**
**Date:** 2026-08-05
**Authoritative constraints:** [Architecture](../architecture.md) §4.6, §4.7 ·
[Product requirements](../product-requirements.md) §7.2, §7.3
**Companion:** [Authentication context examples](authentication-contexts.md)
describes the configuration *shape*. This document describes the *journey* that
produces it.

[Authentication context examples](authentication-contexts.md) answers "what does
a correct configuration look like". This document answers the question a
developer asks first: **"what do I type, and what happens?"**

> **These walkthroughs describe the intended end state, not what the first
> implementation slice delivers.** Slice 1 requires a hand-authored context and
> implements browser PKCE plus inline client credentials only. Login-created
> contexts, fresh-machine cloud tenant resolution, a WSO2-published CLI client,
> and organization switch are all deferred or are backend asks — each is listed
> in [Gaps](#gaps-these-walkthroughs-depend-on). See the
> [login first slice plan](../plans/login-first-slice.md)
> for what is in scope now.

## How to read this

Commands carry one of two marks:

| Mark | Meaning |
| --- | --- |
| **decided** | The surface is recorded in the architecture, the requirements, or the [login first slice plan](../plans/login-first-slice.md) |
| **proposed** | New surface introduced by this document for review. Not yet agreed |

Configuration blocks show the file *after* the commands above them ran.

Two rules hold in every walkthrough:

- **Every YAML block below is non-secret in full.** Refresh tokens, access
  tokens, personal access tokens, and client secrets live *only* in the OS
  secure store, never in these files. `credentialRef: keychain://wso2/<name>` is
  an opaque lookup key — not the credential, and not a capability: naming an
  entry the invoking OS user cannot read fails as an authentication problem.
  Fields ending in `Variable` hold environment-variable *names*, never values.
  Being credential-free is not the same as being public: these files still carry
  internal hostnames, tenant names, and organization identifiers, which many
  deployments treat as sensitive. Whether a given configuration may be committed
  or exported is a deployment policy decision, not a property of the format.
- **Nothing appears in configuration that the shell was not told or did not
  learn.** Where a value comes from the login response it is called out; where
  it must be supplied, a command supplies it. The shell never invents an
  endpoint, an audience, or a client ID.

## Why the file has two blocks

Every configuration below has an `identities:` list and a `contexts:` list. In
the simplest case — one login, one target — that looks like overhead, and it is:
A.1's file would be shorter as a single block.

The split pays for itself the moment **one login serves several targets**, which
is the normal case as soon as a team has more than one environment. Merged, the
same three targets look like this:

```yaml
# NOT the design — what merging costs
contexts:
  - name: retail-dev
    issuer: https://api.asgardeo.io/t/acme/oauth2/token
    credentialRef: keychain://wso2/acme-cloud
    organization: acme
  - name: retail-prod
    issuer: https://api.asgardeo.io/t/acme/oauth2/token    # repeated
    credentialRef: keychain://wso2/acme-cloud              # repeated
    organization: acme
  - name: partner
    issuer: https://api.asgardeo.io/t/acme/oauth2/token    # repeated
    credentialRef: keychain://wso2/acme-cloud              # repeated
    organization: acme-partner
```

Three problems, in increasing order of seriousness:

1. **Duplication drifts.** Rotate the client, or move the issuer, and three
   entries must change together.
2. **The file stops saying how many logins there are.** Three
   `credentialRef` values that happen to be equal *look* like three stored
   sessions. Split, one identity with three contexts states plainly that there
   is one login.
3. **Equality becomes load-bearing.** The shell would have to infer "these are
   the same session" from string-matching issuer and credential fields. A typo
   silently becomes a second login. Referencing a named identity makes sharing
   explicit and a typo an error.

This is also the direction the industry moved, not away from: AWS CLI v2
replaced its one-credential-per-profile shape with `[sso-session]` blocks that
any number of `[profile]` blocks reference — the same split under different
names. kubectl separates `clusters`, `users`, and `contexts` for the same
reason.

**What this means for the person using the CLI:** they think in contexts.
`wso2 login` writes the identity; `wso2 context use` switches targets. The
identity block is machine-managed, and most users never edit it by hand — the
same way most people never hand-edit the `users:` block of a kubeconfig.

## Scenario A — WSO2 Cloud

### A.1 First run: products that share the cloud identity provider

```console
$ wso2 login                                                    # decided
Opening your browser to sign in to WSO2 Cloud.
If it does not open, visit:
  https://cloud.wso2.com/cli/authorize?...        # see gap 1 — entry point TBD

✓ Signed in as kanushka@acme.com
  Home tenant: acme
  Issuer:      https://api.asgardeo.io/t/acme/oauth2/token

  You can act in 2 organizations:
    acme            (root)
    acme-partner

Created context "acme" targeting organization acme.
Set as the default context.
```

The tenant is not known before the flow starts — Asgardeo discovery is
tenant-qualified, so the tenant-qualified issuer can only be *reported* once
authentication has resolved it. How WSO2 Cloud resolves a user to a tenant
without being told is [gap 1](#gaps-these-walkthroughs-depend-on).

```console
$ wso2 api list                                                 # decided
NAME                VERSION   STATUS
retail-checkout     1.4.0     published
retail-inventory    2.0.1     published
```

No new user login. The command may still call the network — refreshing the
stored session, and deriving an audience- and scope-bound token for `api` — but
nothing was asked of the person at the keyboard.

**What the shell wrote:**

```yaml
identities:
  - name: acme-cloud
    type: cloud
    auth:
      kind: oauth-browser
      tenant: acme                        # learned from the login response
      credentialRef: keychain://wso2/acme-cloud

contexts:
  - name: acme
    identity: acme-cloud
    organization: acme

defaultContext: acme
```

`products` is absent. `clientId` is absent because the cloud identity uses the
WSO2-published CLI client — which does not exist yet
([gap 2](#gaps-these-walkthroughs-depend-on)).

`tenant` is the home tenant the login belongs to. It is deliberately not the
context's `organization`, which is what commands target.

#### Which products does this login actually reach?

**The CLI does not know, and login does not find out.** Login proves who you
are at an issuer. It does not enumerate entitlements, and it deliberately does
not probe each product — architecture §4.6 makes reachability a property of the
running deployment discovered at first use, not a promise recorded at login.

So `wso2 api list` is *attempted*, and there are three distinct ways it can
fail. They need different remedies, so the CLI must not collapse them into one
message:

| What is wrong | Who detects it | What the user does |
| --- | --- | --- |
| The `api` module is not installed | the shell, locally, before any network call | install the module |
| The shell cannot derive a token for that audience | the broker | log in to the identity that reaches it, or fix the context |
| The product rejects a valid token | the product | request entitlement — this is **authorization**, not authentication |

The third row is the one most easily mistaken for a login problem. One login
**authenticates** for every product in its identity domain; it **authorizes**
nothing. A user who is signed in but not entitled must get an authorization
failure that does not invite them to log in again:

```console
$ wso2 api list                                                 # decided
Error: not authorized for "api" in organization acme

  You are signed in as kanushka@acme.com. The API Platform rejected an
  otherwise valid token for this organization.

  This is an entitlement problem, not a login problem. Signing in again will
  not change it.
```

For cloud, whether the control plane can enumerate a subscribed product set at
all — turning "attempt and find out" into "know in advance" — is **unverified**
([gap 7](#gaps-these-walkthroughs-depend-on)). If such an API exists, login
could record the reachable set and give a much better first-run experience; this
document does not assume it does.

An explicit, user-invoked check is the middle ground worth considering, because
it probes only when asked rather than slowing every login:

```console
$ wso2 status                                                   # proposed
Context "acme"  ·  identity "acme-cloud"  ·  signed in as kanushka@acme.com

  api           ✓ reachable
  integration   ✓ reachable
  agent         ✗ not authorized (entitlement)
```

### A.2 A second target on the same login

```console
$ wso2 context create retail-prod \                             # proposed
    --organization acme --project retail-prod
Created context "retail-prod" on identity "acme-cloud".
No login required: "acme-cloud" is already authenticated.

$ wso2 context use retail-prod                                  # decided
Now using context "retail-prod".
```

No browser opened. This is the case the identity/context split exists for: one
login, several targets.

**On `--project`.** The value is supplied by the user, who knows their own
project name. That is different from *discovering* which projects exist — a
project is a product-domain object, so enumeration requires an installed product
module and belongs to that product's command surface, not to generic login or
context creation. **Project discovery has no agreed flow**
([gap 5](#gaps-these-walkthroughs-depend-on)); this walkthrough shows only
persisting a value the user already has.

```yaml
identities:
  - name: acme-cloud
    type: cloud
    auth:
      kind: oauth-browser
      tenant: acme
      credentialRef: keychain://wso2/acme-cloud

contexts:
  - name: acme
    identity: acme-cloud
    organization: acme
  - name: retail-prod
    identity: acme-cloud
    organization: acme
    project: retail-prod

defaultContext: retail-prod
```

### A.3 Another organization on the same login

```console
$ wso2 context create partner --organization acme-partner       # proposed
Created context "partner" on identity "acme-cloud".
Organization acme-partner is reached by organization switch on the existing
session. No login required.
```

`acme-partner` is a sub-organization of the `acme` root tenant, so the broker
reaches it with an `organization_switch` exchange on the stored session, lazily,
during the command that needs access — not at `context create`, and not at
`context use`.

**The boundary that decides whether a new login is required:**

| Move | Mechanism | New login? |
| --- | --- | --- |
| Root tenant → sub-organization of the same root | `organization_switch` exchange | No |
| Root tenant → a different root tenant | Different issuer, different discovery document | **Yes** — a different identity |

**Evidence status.** Asgardeo and IS both advertise the `organization_switch`
grant, and `iamctl` uses it in production — but with a *confidential* client
authenticating by client credentials
([setup.go](https://github.com/wso2-extensions/identity-tools-cli/blob/master/iamctl/pkg/utils/setup.go)).
Two things this flow depends on are untested:

1. whether a **public client** holding a PKCE-obtained *user* token may perform
   the switch at all;
2. whether the switch response carries a **refresh token for the sub-organization**,
   or whether the broker must re-exchange from the root session each time —
   which decides whether the secure store holds one entry per identity or one
   per context.

Both need a live-tenant test before this walkthrough is implementable.

### A.4 API Platform and Agent Manager, both hosted in WSO2 Cloud

A.1 assumes every product validates the cloud identity provider. Today it does
not, so an estate that is entirely *hosted* in WSO2 Cloud may still span several
identity domains:

| Product | Validates | Same identity as A.1? |
| --- | --- | --- |
| API Platform | the configured cloud issuer | Yes |
| Agent Manager | its own provisioned Thunder | **No** — shown below |
| Choreo / WDP | proprietary control-plane sessions | **No** — not modelled yet |

Evidence:
[product authentication compatibility](../research/product-authentication-compatibility.md) §3.

**This subscenario is deliberately not the complete current all-cloud flow.** It
covers API Platform and Agent Manager. Choreo and WDP authenticate against
proprietary control planes whose token formats and APIs are not publicly
documented, so they need a compatibility adapter that no design has specified
yet. An estate using them cannot be fully expressed in this configuration model
today, and this document does not pretend otherwise.

For the two products that can be expressed, the flow is two credential
establishments, not one:

```console
$ wso2 login                                                    # decided
✓ Signed in as kanushka@acme.com
  Home tenant: acme
Created context "acme" targeting organization acme.
Set as the default context.

$ wso2 login --url https://thunder.acme.wso2.cloud \            # decided
    --client-id wso2-cli --context acme-agent
✓ Signed in as kanushka@acme.com
  Issuer: https://thunder.acme.wso2.cloud
Created identity "acme-agent" and context "acme-agent".

$ wso2 identity add-product acme-agent agent \                  # proposed
    --endpoint https://agent.acme.wso2.cloud \
    --audience https://agent.acme.wso2.cloud \
    --scopes agent:read,agent:write
Added product "agent" to identity "acme-agent".
```

```yaml
identities:
  - name: acme-cloud
    type: cloud
    auth:
      kind: oauth-browser
      tenant: acme
      credentialRef: keychain://wso2/acme-cloud

  - name: acme-agent
    type: cloud
    auth:
      kind: oauth-browser
      issuer: https://thunder.acme.wso2.cloud   # from --url
      clientId: wso2-cli                        # from --client-id
      credentialRef: keychain://wso2/acme-agent
    products:
      agent:
        endpoint: https://agent.acme.wso2.cloud
        audience: https://agent.acme.wso2.cloud
        scopes: [agent:read, agent:write]

contexts:
  - name: acme
    identity: acme-cloud
    organization: acme
  - name: acme-agent
    identity: acme-agent

defaultContext: acme
```

```console
$ wso2 api list                                                 # acme, default
$ wso2 agent status --context acme-agent
```

Two browser logins, because Agent Manager's Thunder issues its own tokens and
the cloud session cannot produce access it accepts. As deployments converge on
one issuer this collapses back to A.1 — the configuration shrinks, and no
concept changes.

**Two things this flow assumes that are not available yet.**

- **The Thunder issuer is supplied, not discovered.** `--url` here names the
  issuer directly. Deriving it from an Agent Manager *instance* URL — the
  RFC 9728 → RFC 8414 resource-first chain `amctl` uses — is explicitly out of
  scope for login slice 1, so until it lands the operator supplies the issuer.
- **`clientId: wso2-cli` must be registered on that Thunder.** Agent Manager
  seeds a client named `amctl`, but this CLI binds different loopback callback
  ports, and a registration's redirect URIs are exact. The `amctl` client
  therefore cannot be assumed to accept `wso2` callbacks. Either the operator
  registers a client, or Agent Manager seeds a `wso2-cli` client alongside
  `amctl` — a **backend ask, owner: Agent Manager / Thunder**.

## Scenario B — Everything on-premises behind one identity provider

The customer runs the products with one identity provider in front. One login,
one context — differing from A in that nothing is discoverable, and the shell
must never assume a self-hosted deployment supports cloud SSO.

### B.1 First login

```console
$ wso2 login --url https://idp.customer.example \               # decided
    --client-id wso2-cli
Opening your browser to sign in.
If it does not open, visit:
  https://idp.customer.example/oauth2/authorize?response_type=code&...

✓ Signed in as ops@customer.example
  Issuer: https://idp.customer.example

Created identity "customer-idp" and context "customer".
Set as the default context.

No products are configured for this identity. Self-hosted deployments are not
discoverable, so each product's endpoint must be recorded:

  wso2 identity add-product customer-idp <namespace> \
      --endpoint <url> --audience <resource-id> --scopes <list>
```

`--client-id` is required. No WSO2-published client exists for self-hosted
deployments, so the operator registers an application and supplies its ID; the
shell does not invent one. Omitting the flag prompts for it in an interactive
terminal and is a typed error under `--no-input`.

### B.2 Recording what the login reaches

```console
$ wso2 identity add-product customer-idp api \                  # proposed
    --endpoint https://api.customer.example \
    --audience https://api.customer.example \
    --scopes api:read,api:write
Added product "api" to identity "customer-idp".

$ wso2 identity add-product customer-idp integration \          # proposed
    --endpoint https://esb.customer.example \
    --audience https://esb.customer.example \
    --scopes integration:read
Added product "integration" to identity "customer-idp".

$ wso2 api list                                                 # decided
NAME                VERSION   STATUS
orders              3.1.0     published
```

**What the shell wrote** — every value came from a flag or the login response:

```yaml
identities:
  - name: customer-idp
    type: onprem
    auth:
      kind: oauth-browser
      issuer: https://idp.customer.example      # from --url
      clientId: wso2-cli                        # from --client-id
      credentialRef: keychain://wso2/customer-idp
    products:
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

defaultContext: customer
```

The context carries no `organization`. Nothing in the login response supplied
one, and this deployment's identity provider was not asked for an organization
model — so the field is absent rather than invented. Where a self-hosted IS does
expose organizations, `wso2 context create --organization <id>` records one
explicitly.

### B.3 What this configuration asserts, and how it fails

Listing a product under an identity is the operator's **assertion** that the
shared session reaches it. The shell does not verify it at write time or at
login. A wrong assertion surfaces where it matters:

```console
$ wso2 integration deploy ./flow.xml                            # decided
Error: authentication failed for product "integration"

  The integration service did not accept access derived from the
  "customer-idp" login. It may validate a different issuer.

  If this product requires its own login, it belongs to a separate identity
  and context. See: wso2 login --url <its issuer> --context <name>
```

A typed authentication failure at first use — never a malformed document, and
never a silent fallback to another credential.

## Scenario C — Mixed estate

An on-premises Agent Manager behind its own identity provider, an on-premises
API Manager configured for its own credentials, and integration in WSO2 Cloud.
**Three identities and three credential establishments** — two interactive
logins and one stored product token. "Three logins" would be wrong: the adapter
identity has no session to establish.

### C.1 Two logins and one adapter

```console
$ wso2 login                                                    # decided
✓ Signed in as kanushka@acme.com
  Home tenant: acme
Created context "acme" targeting organization acme.
Set as the default context.

$ wso2 login --url https://thunder.own.example \                # decided
    --client-id wso2-cli --context own-agent
✓ Signed in as ops@own.example
  Issuer: https://thunder.own.example
Created identity "onprem-agent" and context "own-agent".

No products are configured for this identity.

$ wso2 identity add-product onprem-agent agent \                # proposed
    --endpoint https://agent.own.example \
    --audience https://agent.own.example \
    --scopes agent:read,agent:write
Added product "agent" to identity "onprem-agent".
```

The third product refuses interactive login:

```console
$ wso2 login --url https://api.own.example --context own-api    # decided
Error: this deployment advertises no interactive login method the CLI supports

  The API Manager management API at https://api.own.example authenticates
  against its own resident key manager, and its metadata advertises no grant
  this CLI can use interactively.

  An API Manager deployment can be configured to trust an external issuer
  through its Key Manager framework. If yours is, log in against that issuer
  instead and add "api" to its identity.

  Otherwise, configure a compatibility-adapter identity:
    wso2 identity create own-api --kind pat \
        --product api --endpoint https://api.own.example
```

The refusal is scoped to what was observed — *this deployment advertises no
supported path* — not a claim that API Manager can never accept IdP-issued
tokens. Its Key Manager framework supports named and custom OIDC connectors, so
an operator can wire it deliberately.

### C.2 The adapter identity

```console
$ wso2 identity create own-api --kind pat \                     # proposed
    --product api --endpoint https://api.own.example
Created identity "own-api" and context "own-api".

Store the product-issued token:
  wso2 identity set-credential own-api

$ wso2 identity set-credential own-api                          # proposed
Paste the token (read from stdin, not echoed, never stored in configuration):
✓ Stored in the OS secure store as keychain://wso2/own-api
```

Read from stdin, never from a flag — matching Choreo's shipping `--with-token`
practice and requirements §7.2's rule against secrets in arguments.

```yaml
identities:
  - name: acme-cloud
    type: cloud
    auth:
      kind: oauth-browser
      tenant: acme
      credentialRef: keychain://wso2/acme-cloud

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

  - name: own-api
    type: onprem
    auth:
      kind: pat                           # compatibility adapter; no session
      credentialRef: keychain://wso2/own-api
    products:
      api:
        endpoint: https://api.own.example

contexts:
  - name: acme
    identity: acme-cloud
    organization: acme
  - name: own-agent
    identity: onprem-agent
  - name: own-api
    identity: own-api

defaultContext: acme
```

`own-api` carries a different trust property from the other two: its token is
presented unchanged and cannot be narrowed or derived from. The configuration
says so through `kind: pat` rather than presenting it as equivalent.

### C.3 Using it, and where it hurts today

```console
$ wso2 integration deploy ./flow.xml        # acme, the default   # decided
$ wso2 agent status --context own-agent                          # decided
$ wso2 api list --context own-api                                # decided
```

**Two of three commands need an explicit `--context`.** That is the honest state
of the design today: selection resolves `--context` → `WSO2_CONTEXT` → default,
and nothing else.

This is the developer-experience cost of a mixed estate, and it is what the
planned **workspace** layer exists to remove — a named project routing each
namespace to the context that serves it:

```yaml
# Proposed; not yet designed in detail
workspace: acme-prod
routes:
  integration: acme
  agent: own-agent
  api: own-api
```

**On `namespaceContexts`.** [Architecture](../architecture.md) §4.7 records a
global per-namespace binding as target behavior, and
[authentication context examples](authentication-contexts.md) §6 and §8 use it
for this routing. It is deliberately not used here: it is out of scope for login
slice 1, and workspace is expected to supersede it, so shipping the map first
would mean migrating configuration users had already authored. It remains
architecturally recorded until workspace is designed and the architecture
updated — this document defers it rather than deleting it.

## Scenario D — CI

CI is non-interactive, so there is **no login step at all**. The shell acquires
access inline during the invoking command.

```console
$ export WSO2_CONTEXT=ci
$ export WSO2_CI_CLIENT_SECRET=...          # injected by the CI secret store
$ wso2 api import ./api.yaml                                    # decided
✓ Imported "orders" 3.1.0
```

```console
$ wso2 login --context ci                                       # decided
Error: context "ci" needs no login

  Identity "ci-release" uses client credentials. Access is acquired during the
  command that needs it, so there is no session to establish.
```

An interactive method under `--no-input` refuses rather than waiting on a
browser that will never open:

```console
$ wso2 login --no-input                                         # decided
Error: interactive login is not available in non-interactive mode

  Identity "acme-cloud" authenticates through a browser. Configure client
  credentials for automation.
```

**The configuration this runs against** — authored once, no login ever:

```yaml
identities:
  - name: ci-release
    type: onprem
    auth:
      kind: client-credentials
      issuer: https://idp.customer.example
      clientId: wso2-ci
      clientSecretVariable: WSO2_CI_CLIENT_SECRET   # a NAME, never a value
    products:
      api:
        endpoint: https://api.customer.example
        audience: https://api.customer.example
        scopes: [api:write]

contexts:
  - name: ci
    identity: ci-release
    organization: customer
```

The secret reaches the shell from the CI secret store through the named
variable, stays in job memory, and is never written to disk, the secure store,
or the module environment.

## What each command writes

| Command | Identity | Context | Secure store | Network |
| --- | --- | --- | --- | --- |
| `wso2 login` | creates or reuses | may create | writes session | yes |
| `wso2 login --url <issuer>` | creates or reuses | may create | writes session | yes |
| `wso2 context create` | never | creates | never | no |
| `wso2 context use` | never | selection only | never | **no** |
| `wso2 identity add-product` | modifies | never | never | no |
| `wso2 identity set-credential` | never | never | writes | no |
| `wso2 logout` | never | never | removes entry | best effort |

`wso2 logout` is the one row whose network column is not a yes or a no. It asks
the deployment to revoke the session's refresh token when the deployment
publishes a `revocation_endpoint`, and it removes the secure-store entry whether
or not that request was made or accepted — reporting which of the three
happened rather than implying the strongest one.
[ADR 0010](../adr/0010-best-effort-revocation-on-session-end.md) records
why. It does not end the browser single-sign-on session either way.

The row that matters most is `context use`: a local write and nothing else.
Architecture §4.7 requires it, and it is what makes several targets over one
login cheap.

## Gaps these walkthroughs depend on

Stated plainly, because several of these flows do not work today.

1. **Bare `wso2 login` needs a tenant before it can start.** Asgardeo discovery
   is tenant-qualified (`/t/{org}/...`), so A.1 requires WSO2 Cloud to expose a
   common entry point that resolves the organization during authentication.
   Choreo and WDP evidently do this; the mechanism is not public. **Open —
   needs an internal answer.** The URL shown in A.1 is a placeholder for it.
2. **No WSO2-published public client exists for a CLI in Asgardeo or IS.**
   Agent Manager seeds `amctl` on the Thunder side; cloud has no equivalent, so
   A.1 requires a seeded client and B requires the operator to register one.
   **Backend ask — owner: Asgardeo service team**
   ([evidence](../research/product-authentication-compatibility.md) §1.1).
3. **Organization switch is not in slice 1, and is unverified for this client
   type.** A.3 depends on it. Until it ships, a context may target only its
   identity's home tenant; anything else is refused with
   `auth.organization_switch_unsupported`. The two live-test unknowns are in
   A.3.
4. **Whether per-product narrowing works on Asgardeo is unverified.** If
   refresh-grant scope narrowing is not honored, the broker refuses and the
   product commands in A.1 fail after a successful login. **Needs a live-tenant
   test before implementation**
   ([evidence](../research/asgardeo-redirect-uri-and-scope-narrowing.md)).
5. **Project discovery has no flow.** A project is a product-domain object, so
   it cannot be enumerated at login — on a fresh machine no product module is
   installed to ask. `--project` persists a value the user supplies; discovering
   valid values is per-product work with no agreed command surface.
6. **Login-created contexts and identities are deferred.** Slice 1 requires a
   hand-authored context, so every "Created identity / Created context" line
   above is target behavior.
7. **Whether WSO2 Cloud can enumerate a subscribed product set is unverified.**
   A.1 omits `products` for `type: cloud`. That is only tenable if either the
   control plane can report which products an organization has, or the CLI
   accepts "attempt and find out" as the permanent answer. No public source
   establishes such an API. **Open — needs an internal answer**, and it
   determines whether first-run can tell a user what they can do.
8. **Resource-first discovery is deferred.** RFC 9728 → RFC 8414 resolution
   from a product instance URL — how `amctl` finds its issuer — is out of scope
   for slice 1, so A.4 supplies the Thunder issuer directly instead of deriving
   it from the Agent Manager instance.
9. **A `wso2-cli` client on Agent Manager's Thunder does not exist.** A.4
   requires one, because this CLI's loopback callback ports differ from
   `amctl`'s and redirect URIs match exactly. **Backend ask — owner: Agent
   Manager / Thunder.**

Items 1, 2, 4, and 7 gate the flagship path in A.1. Items 3, 5, 6, 8, and 9
shape later slices.

## Open questions for review

1. Are `wso2 identity create`, `identity add-product`, and
   `identity set-credential` the right surface, or should identities only ever
   be produced by `login` plus hand-editing? Scenario B needs *some* way to
   record endpoints that cannot be discovered.
2. Should `wso2 login` create a context when it can, as shown, or authenticate
   only and require an explicit `context create`? These walkthroughs assume the
   former, per requirements §7.3's P1.
3. In C.1, should a refused login offer the adapter command as shown? Offering
   it is friendlier; refusing plainly is harder to misread as an endorsement of
   the adapter tier.
4. Should `--project` be accepted on `context create` at all before a project
   discovery flow exists, or should project targeting wait for the product
   command that can validate it?
