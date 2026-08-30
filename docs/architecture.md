# WSO2 CLI architecture

**Status:** Working draft  
**Date:** 2026-07-24  
**Related:** [Product requirements](product-requirements.md),
[first CLI vertical slice](plans/first-cli-vertical-slice.md)

## 1. Design summary

The `wso2` CLI will be a Go shell with a managed module runtime. Each product
module is an independently versioned native executable owned by a WSO2 product
team. The shell resolves modules from a CLI-managed local versioned store and
launches them out of process.

This is an **SDK-first hybrid architecture**:

- execution stays out of process, preserving independent releases and avoiding
  Go plugin ABI coupling;
- every production module implements a mandatory control contract;
- a shared Go SDK hides the process protocol and supplies common authentication,
  context, help, output, and error behavior;
- existing CLIs may use a temporary migration adapter, but raw passthrough is
  not considered fully conformant.

The architecture deliberately does not copy kubectl's arbitrary `PATH`
discovery. It adopts Krew's useful versioned-store and receipt ideas while
adding a shell-owned store, integrity-checked artifacts, protocol and platform
compatibility gates, and a first-class offline model.

## 2. Architectural principles

1. **The shell owns policy; products own workflows.**
2. **One top-level product namespace maps to one official module.**
3. **Common behavior is implemented once in the SDK and platform services.**
4. **Modules release independently of the shell.**
5. **Installed state is explicit, receipt-backed, and reproducible.**
6. **The active installation changes only after complete verification.**
7. **Long-lived credentials never enter module storage.**
8. **Native process isolation is not represented as a security sandbox.**
9. **Automation never depends on terminal formatting or implicit network
   access.**

## 3. System context

```mermaid
flowchart LR
    U["User, CI, or coding agent"] --> H["wso2 shell"]
    H --> C["Context and configuration"]
    H --> K["OS secure store for interactive sessions"]
    J["CI secret store"] --> H
    H --> M["Module manager"]
    M --> R["Official WSO2 module registry"]
    M --> S["Managed versioned module store"]
    H --> P["Product module process"]
    P <--> A["Authentication broker"]
    A --> K
    P --> W["WSO2 product API"]
    H --> O["Consistent terminal or machine output"]
```

## 4. Major components

### 4.1 Root shell

The root shell owns:

- root commands and reserved namespace precedence;
- root argument parsing and product-namespace dispatch;
- context selection and configuration;
- authentication and secure credential storage;
- module discovery and lifecycle entry points;
- root help, version, diagnostics, and completion;
- invocation policy such as interactivity and network allowance.

The root must remain small enough that product command changes do not require a
root release.

### 4.2 Module manager

The module manager is a deep subsystem behind a narrow interface:

```text
Resolve(namespace, constraint, platform) -> Release
Install(release, policy) -> Receipt
Activate(name, version) -> ActiveInstallation
Verify(name, version) -> VerificationReport
Rollback(name) -> ActiveInstallation
Remove(name, version) -> Result
```

It owns catalog metadata, dependency-free release resolution, downloading,
verification, safe extraction, receipts, activation, retention, and rollback.
It does not parse product commands or store product credentials.

### 4.3 Module registry

The official registry maps an assigned product namespace to signed release
metadata. It contains metadata, not mutable executable state.

The registry must support:

- authorized publisher delegation by namespace;
- platform-specific artifacts;
- semantic module versions and release channels;
- host and module-protocol compatibility;
- digests, signatures, provenance, and SBOM references;
- release revocation;
- expiring and rollback-protected catalog metadata;
- mirrors and signed offline export.

### 4.4 Product module process

A product module:

- owns one top-level product namespace;
- implements product resources, actions, validation, and API calls;
- uses the shared SDK for the mandatory contract and common behavior;
- is distributed as a signed platform-specific executable/archive;
- can be updated without rebuilding the root shell.

Product modules are trusted WSO2 software. Running them in another process
isolates failures and release cycles, but does not remove their access to
resources available to the user account.

### 4.5 Shared Go SDK

The SDK is the author-facing interface. Module authors should not need to know
the wire protocol.

Conceptual usage:

```go
func main() {
    sdk.Run(sdk.Module{
        Name:     "api",
        Version:  build.Version,
        Commands: NewCommands,
    })
}
```

The SDK provides:

- protocol handshake and module identity;
- standard Cobra construction and help templates;
- common flags such as `--context`, `--output`, `--no-input`, `--quiet`, and
  `--verbose`;
- access to selected context and invocation policy;
- authentication-broker client;
- typed result and problem definitions with authoring helpers;
- automatic secret redaction;
- command-schema description and completion metadata;
- health and conformance hooks;
- test fixtures and golden-output helpers.

The shell owns final table, JSON, and YAML rendering plus problem-to-exit-code
mapping. The public SDK API should remain substantially smaller than its
implementation.

### 4.6 Authentication broker

The root process owns authentication policy and long-lived interactive state.
A module communicates with an invocation-scoped broker through the module
contract on its inherited standard input and standard output streams.

The broker:

- identifies the already integrity-checked module through the launch
  handshake;
- checks the module's declared audience and scope capabilities;
- resolves the selected context, its identity, and the relevant product
  session;
- refreshes credentials without exposing refresh tokens;
- returns short-lived or invocation-scoped access material and restricts
  audience and scope when the deployment supports them;
- refuses rather than silently issuing broader or incorrectly targeted access
  when a requested narrowing is unavailable;
- redacts secrets and records security-relevant diagnostics without recording
  the secret value.

The IPC endpoint must not be a world-discoverable local port.

#### Identity

An **identity** is one reusable login session, together with every product for
which the broker can derive valid access from that session without another
login or an independently supplied credential.

An identity is not an issuer. Two products configured against the same issuer
URL do not share an identity unless that session can actually produce access
each of them accepts. A product that validates only its own resident issuer, or
that requires its own separately authenticated OAuth client, personal access
token, or password, belongs to a different identity and therefore to a
different context.

Whether a session can derive access for a product is a property of the running
deployment, not of a configuration file. The configuration records the
operator's assertion. A wrong assertion surfaces as a typed authentication or
authorization failure when a command needs access, never as a malformed
document.

The broker derives product-specific access bound to the product's
audience/resource and to scopes. One identity therefore does not mean one
access token: a separate short-lived token may be derived for each product
invocation. How narrowly that derivation can be bound is a per-backend
capability, so the broker resolves a downscoping strategy per deployment and
exposes what it can enforce instead of degrading silently.

Product-specific legacy authentication that cannot yield derived, short-lived
access remains compatibility-adapter territory: a management password grant,
or a long-lived token the shell can only pass through unchanged. A module
reached that way does not carry the same trust property, and the design states
that rather than presenting it as equivalent.

#### Interactive login modes

> **What ships today.** This section describes the target architecture. The
> shell implements browser Authorization Code with PKCE, the Device
> Authorization Grant, and inline client credentials. Personal access tokens
> validate as legal configuration and refuse at use with the stable code
> `auth.kind_not_implemented`. They are accepted so that a document written for
> them stays readable, not executed. See
> [the login first slice](plans/login-first-slice.md).
>
> The device grant is reached through the `oauth-device` **kind**, not yet
> through a login-time flag: `wso2 login --device-code` is not in this release.
> So the mode-not-kind rule below states the target, and today an identity that
> can only be established by device says so in its kind.

Browser Authorization Code with PKCE and the Device Authorization Grant are two
**login modes for the same interactive OIDC identity**, not two stored
authentication methods. An identity records that it authenticates interactively
against an issuer; the mode is chosen at login time, by the machine the user is
sitting at.

`wso2 login` uses the OAuth 2.0/OIDC Authorization Code flow with PKCE, opening
a browser for the user to authenticate and approve the request. This is the
default interactive login.

`wso2 login --device-code` uses the Device Authorization Grant for a developer
on a headless or remote machine. The CLI displays the verification instructions
and the user approves the request in a browser on another device.

Device authorization is available only where the backend advertises it, through
the discovery document's `device_authorization_endpoint` or the corresponding
grant type. Where the advertisement is absent, the broker refuses with a stable
error rather than falling back to a browser the user cannot open.

The shell completes either exchange and stores any resulting long-lived
interactive credential, such as a refresh token, in the OS secure store. Device
authorization is still interactive and must not be used by CI.

#### On-premises login

An on-premises context explicitly configures its identity: the products it
reaches, their endpoints, and the authentication method. The shell uses only
mechanisms that deployment supports.

The CLI must not infer that an on-premises endpoint supports WSO2 Cloud SSO,
WSO2 Identity Server, shared authentication, or device authorization. Products
that one login cannot reach belong to separate identities, and therefore to
separate contexts. A single identity never mixes authentication methods across
its products.

#### CI authentication

CI is always non-interactive. It authenticates with:

- client credentials, which are the preferred CI method;
- a personal access token, where the product issues one;
- a future workload-identity mechanism.

Browser Authorization Code and Device Authorization flows are invalid in CI
or any invocation using non-interactive mode. The CLI must fail with a stable
configuration error instead of waiting for user approval.

A non-interactive method establishes no reusable session, so there is nothing
for a separate login step to persist. The shell acquires access inline during
the invoking command, and CI does not require a preceding `wso2 login`.

With client credentials the shell holds the client secret, performs the token
exchange itself, and passes the module only the resulting short-lived access
token; the secret never leaves the shell. A personal access token that a
product accepts directly as bearer material cannot be narrowed or derived from,
so it is compatibility-adapter territory under the identity rules above rather
than an equivalent CI method.

The CI platform injects the secret from its secret store. The shell reads it
from the configured variable name or stdin, keeps it only in job-process
memory, and does not write it to the filesystem, OS secure store, context
configuration, or module environment.

### 4.7 Context and configuration store

The configuration store contains non-secret data:

- named identities, as defined in §4.6;
- named cloud and on-premises contexts;
- default context, and any per-namespace context bindings;
- module pins and update policy;
- mirror/offline policy;
- output and interaction defaults.

#### Identities and contexts

**Each context references exactly one identity. One identity may back several
contexts.** Several projects or organizations reached through the same login
are several contexts over one identity, not several logins.

Configuration divides along that boundary:

- an **identity** carries the authentication kind, the issuer, the client
  identifier, and an opaque OS-secure-store reference or CI variable name;
- a **product entry** on an identity carries the endpoint plus the
  audience/resource metadata the broker needs to target access;
- a **context** carries targeting only, the organization and project, plus the
  name of its identity.

A context therefore never mixes authentication methods across products. Where
an identity's authentication cannot reach a product, that product belongs to
another identity and another context.

Where authentication itself needs a tenant, meaning a home organization at the
issuer, that belongs to the identity's authentication configuration and is named
distinctly from the context's target organization, which the broker may reach
through an organization-switch exchange on the same session.

The legal authentication kinds are `oauth-browser`, `oauth-device`,
`client-credentials`, and `pat`. Availability is per deployment, not universal:
`oauth-device` is valid only where the backend advertises the grant, and `pat`
only for products that accept product-issued long-lived tokens. Evidence for
this set is recorded in
[the authentication landscape research](research/wso2-authentication-landscape.md)
and [product authentication compatibility](research/product-authentication-compatibility.md).

#### Selection

A command resolves its context in this order:

1. the `--context` flag;
2. the `WSO2_CONTEXT` environment variable;
3. a context bound to the invoked product namespace, if one is recorded;
4. the configured default context;
5. none, which produces a typed refusal when a command requires access.

Step 3 is a recorded decision, not an inference: a per-namespace binding exists
only because a command wrote it, and it is reported by context listings like
any other selection. Without it, a deployment whose products require separate
logins would force `--context` onto nearly every invocation.

Selecting a context never authenticates. `wso2 context use` writes the
selection and performs no network call. A selected but unauthenticated context
produces an authentication-class problem at first use.

Login may create the context it authenticates, so that a first-run user is not
required to write configuration by hand. Creation is explicit about the name it
assigns and is reported; it never silently replaces an existing context.

#### Credentials

The store never contains access tokens, refresh tokens, personal access tokens,
client secrets, passwords, or private keys. Importing or exporting an identity
or context therefore moves target and authentication configuration but never a
credential.

Interactive long-lived credentials are stored in the OS keychain or another
approved secure store and referenced by opaque identifiers. CI credentials
remain owned by the CI secret store and exist in the CLI only in job memory.

#### Writing grants nothing

The architecture proof holds the invariant that no shell command can write a
context, so no shell command can grant itself access. A production
`wso2 context create` ends that invariant, and replaces it with one that
survives a writable store: **writing a context or an identity grants nothing.**
Why the invariant is stated this way, rather than as a rule about which
command is allowed to write, is recorded in
[ADR 0012](adr/0012-writing-a-context-or-identity-grants-nothing.md).

It holds through five properties, each of which is testable:

1. the types have nowhere to put a credential, and a value supplied where a
   reference or variable name belongs is rejected rather than stored;
2. a created identity and context, with no login, yield an
   authentication-class refusal on first use;
3. an imported identity and context grant the importer nothing they did not
   already hold in their own OS secure store;
4. a secure-store reference is a lookup key, not a capability: naming an entry
   the invoking OS user cannot read fails as an authentication problem;
5. export is credential-free by construction, because the document has no
   credential to remove.

#### Schema evolution

The document carries a schema version. Unknown members are tolerated on read,
so a newer shell may record non-secret facts an older one ignores, and a single
unsupported authentication kind never renders a whole document unreadable.

Unknown members are **not** preserved on write. Until a preservation mechanism
exists, an older shell that rewrites a newer document drops what it did not
understand, which matters as soon as commands can write configuration. A new
*required* field is a schema revision, not an addition within a version.

Concrete, non-secret examples are maintained in
[Authentication context examples](examples/authentication-contexts.md), which
illustrates the decisions recorded here and does not extend them.

### 4.8 Output and problem model

Product handlers return typed values. The SDK owns their presentation:

```text
Result
  data
  table columns
  warnings
  pagination metadata

Problem
  stable code
  category
  human message
  safe details
  recovery suggestion
  exit class
```

JSON and YAML output must preserve the same semantic fields. Table output may
select a readable subset but must not change the operation's meaning. Diagnostic
logs go to standard error; machine output goes to standard output.

## 5. Module contract

Every production module implements one protocol version through the SDK: the
version of the SDK release it was built against. The shell supports a window of
two, so a module built against either negotiates successfully. See
[section 10](#10-version-model).

The shell and a module exchange length-delimited Protobuf messages over the
module process's inherited standard input and standard output. Module standard
output is protocol-only, module standard error is reserved for bounded
diagnostics, and only the shell writes user-facing standard output. See
[ADR 0002](adr/0002-module-transport.md) and
[ADR 0003](adr/0003-shell-owned-output.md).

### 5.1 Identity

The module reports:

- namespace;
- module version;
- protocol versions supported;
- build identity;
- declared capabilities.

The root checks this identity against the signed manifest and receipt before
the product operation proceeds.

### 5.2 Description

The module exposes command metadata generated from its actual command tree.
Authors must not maintain a separate handwritten help schema.

The description includes:

- commands and aliases;
- arguments and flags;
- short and long descriptions;
- examples;
- output shapes where declared;
- interactivity and authentication requirements.

### 5.3 Health

The module provides a non-mutating startup health check used during installation
and diagnostics. It validates that the artifact can start, negotiate the
protocol, and construct its command tree. It must not require a live product
endpoint.

### 5.4 Invocation

For normal execution:

1. the root parses built-in flags and identifies the product namespace;
2. it resolves and verifies the active module;
3. it selects the invocation context and creates an invocation-scoped broker
   session;
4. it launches the module with sanitized environment and original product
   arguments;
5. the SDK completes the identity/protocol handshake;
6. the module runs its product command and requests authentication if required;
7. the module returns a typed result or problem;
8. the shell renders output or diagnostics and exits with the centrally
   defined exit class.

This retains normal CLI-process execution rather than converting every product
operation into a generic JSON-RPC method.

## 6. Release manifest

A published module version carries enough information to resolve and verify an
artifact without executing it. That information is not a reviewed document
submitted by a publisher: it is one entry in the
[module catalog](reference/module-catalog.md), which the release job generates
from the module tags that exist.

The catalog is two files, served over HTTPS from the origin that already
serves the install scripts:

- `index.json` names, for every product namespace, the latest version on each
  channel. Its size is bounded by namespaces and channels rather than by
  release history, so an update check is one request whose cost does not grow
  as products accumulate releases.
- `modules/<namespace>.json` carries the full version history of one
  namespace. Per version it records the channel, the protocol versions the
  module speaks, the shell range it declares, and for each platform an
  artifact URL, size, and SHA-256 digest.

The shell reads `index.json` to check for updates and fetches a namespace file
only when it has to select a specific version. A check therefore costs one
document, and an install costs two plus the artifact.

One version entry, as published:

```json
{
  "version": "0.9.0",
  "channel": "stable",
  "compatibility": {
    "shell": ">=0.4.0 <2.0.0",
    "protocolVersions": [2, 1]
  },
  "capabilities": {
    "authAudiences": ["api-platform"]
  },
  "artifacts": [
    {
      "os": "darwin",
      "arch": "arm64",
      "url": "https://.../api/v0.9.0/wso2-module-api-v0.9.0-darwin-arm64.tar.gz",
      "size": 12345678,
      "sha256": "4f3c...9a2e"
    }
  ]
}
```

The channel is derived from the version rather than declared: a version
carrying a prerelease identifier is a prerelease, and every other version is
stable. Nothing can therefore publish a prerelease labelled
stable, because nothing labels it.

There is no `publisher`, `signature`, `provenance`, `sbom`, or revocation
field, and no per-release document envelope for one to live in. With one
repository and one CODEOWNERS file, the question those fields existed to
answer, whether this publisher may claim this namespace, has one answer, and
carrying empty values would suggest a trust chain that does not exist.
What a digest does and does not prove is stated in section 9.2.

### 6.1 Module publishing and consumption flow

One repository holds the shell, the SDK, and every product module, and the
catalog is a build output of that repository rather than a curated artifact:

- the shell keeps plain `v*` tags, the SDK keeps `sdk/v*` tags, and a product
  module is tagged `<namespace>/v<version>`, so the three still release on
  their own schedules from one repository;
- a module tag push gates the release, builds one archive per supported
  platform, publishes them to GitHub Releases with a checksum file, then
  regenerates the catalog and deploys it to the origin;
- generation reads every module tag rather than the one just pushed and is
  deterministic, so regenerating over an unchanged tag set produces an
  unchanged file, there is no curation step to forget, and the catalog cannot
  disagree with what was released;
- a tag naming no module the checkout declares, or a release missing an
  archive for a supported platform, fails the release rather than publishing
  an entry that points at nothing;
- the shell reads the published catalog, downloads the artifact from GitHub
  Releases, and checks its size and digest before anything is activated.

The catalog entry is the link between a product namespace and a published
release. It records the version, channel, protocol and shell compatibility,
and per-platform artifact location, size, and digest, and nothing else. Among
the versions the user's channel and pin permit, the shell selects the newest
whose protocol versions intersect what the shell speaks and which publishes
an artifact for the user's platform. It does not assume the numerically
newest release is usable.

Independent module releases do not come from separate repositories and never
required them. What one repository buys is that a change spanning the shell,
the SDK, and a module is one review rather than a cross-repository sequence,
and that an SDK change which breaks a module is visible in the pull request
that makes it. What it does not buy is atomic rollout. A green build proves
the head shell against the head module; the pair that breaks a user is an old
installed shell against a new module, and no amount of source-level atomicity
addresses it. That is what the protocol window in section 10 and the release
gate exist for.

The ordering is enforced rather than hoped for. A module release is refused
when the protocol versions the module declares do not intersect what the
released shell speaks, and the refusal names the module's requirement and the
shell's window rather than failing opaquely. The shell ships first.

```mermaid
flowchart LR
    REPO["wso2-cli repository<br/>shell, SDK, product modules"]
    TAG["Module tag<br/>namespace/vX.Y.Z"]
    MR["Module release workflow<br/>gate, build, publish, generate"]
    GR["GitHub Releases<br/>module archives and checksums"]
    OR["Catalog origin<br/>index.json and modules/&lt;namespace&gt;.json"]
    CLI["Installed wso2 shell"]
    LS["Managed module store"]
    MP["Product module process"]
    API["WSO2 product API"]

    REPO -->|"push a module tag"| TAG
    TAG --> MR
    MR -->|"publish platform archives"| GR
    MR -->|"generate from all module tags"| OR

    CLI -->|"read the catalog"| OR
    CLI -->|"download the selected artifact"| GR
    CLI -->|"check the digest and activate"| LS
    CLI -->|"launch per command"| MP
    LS --> MP
    MP -->|"product operation"| API
```

Publishing a module and deploying a product workload are separate operations.
The flow above publishes a CLI module for installation by the shell. Commands
such as `wso2 integration component deploy` remain product operations
implemented by the corresponding module after it has been installed.

For a user invocation such as `wso2 api gateway list`:

1. The shell resolves `api` from its local active receipt.
2. If the module is missing, an interactive invocation may offer to install the
   official module. Non-interactive execution never installs implicitly unless
   policy explicitly permits it.
3. Installation or update reads the catalog, selects a compatible release,
   downloads its artifact, and follows the verification and activation
   procedure in section 7.
4. Normal commands use the installed local module and do not require a catalog
   request on every invocation.
5. The shell launches the installed module, negotiates the protocol, supplies
   non-secret context, and brokers invocation-scoped authentication as
   described in sections 4.6 and 5.4.

Module releases are decoupled from shell releases. A product team can publish
a compatible module without rebuilding the root binary, and moving artifact or
catalog hosting does not change the module protocol. There are no signing
keys to keep out of source repositories, because nothing is signed; the trust
that publishing to the catalog origin concentrates instead, and what is done
about it, is section 9.2.

## 7. Installation and activation

### 7.1 Online installation

The numbered path below is the original design. What is built is described by
the [module catalog](reference/module-catalog.md) reference: steps 3 and 5's
publisher, signature, provenance, and revocation checks are not performed,
because the catalog carries no such fields, and step 7's health check is not
performed at installation. What remains is the size and digest check, safe
extraction, the receipt, and atomic activation, and any failure before
activation leaves no executable and no receipt behind.

1. Load and verify fresh-enough signed catalog metadata.
2. Resolve exact namespace, version/channel, host, protocol, OS, and
   architecture.
3. Confirm that the publisher is authorized for the namespace.
4. Download the artifact into a private quarantine directory.
5. Verify expected size, digest, artifact signature, provenance, and revocation
   status.
6. Safely extract it, rejecting absolute paths, traversal, links escaping the
   destination, unexpected files, and unsafe permissions.
7. Verify module identity and run the non-mutating health check.
8. Write an immutable receipt containing the signed release metadata and
   verification result.
9. Atomically update the active-version state.
10. Retain the previous verified version according to the rollback policy.

Any failure before activation leaves the old active version unchanged.

### 7.2 Update

An update performs the same verification path as installation. A module version
is immutable; publishing changed bytes requires a new version. CI can pin exact
versions and disable implicit metadata refresh or installation.

#### 7.2.1 Update discovery and notification

The rules below are the original design. What is built is described by the
[module catalog](reference/module-catalog.md) reference: an explicit
`wso2 module list` or `wso2 module update` reads the catalog index when it
runs, the shell caches no catalog metadata and performs no background refresh,
and rule 3 filters by channel, pin, protocol, and platform only, because the
catalog carries no publisher or revocation field. Rules 4 to 6 are not built.

The shell uses the catalog to distinguish the installed version from newer
available releases. Update discovery follows these rules:

1. Explicit lifecycle commands such as `wso2 module list`,
   `wso2 module update <name>`, and `wso2 module update --all` may refresh
   catalog metadata when network policy allows it.
2. Interactive use may perform a background catalog refresh no more than once
   per configurable interval, initially 24 hours. Fresh verified metadata is
   cached locally, and a failed refresh does not delay or fail the product
   command.
3. The shell filters catalog releases by channel, pinning policy, host version,
   protocol version, operating system, architecture, revocation, and publisher
   trust before reporting an update. "Available" therefore means the newest
   compatible verified release, not merely the highest version number.
4. After a successful interactive command, the shell may write a concise update
   notice to standard error. It never writes update notices into standard
   output used for table, JSON, or YAML results.
5. Non-interactive, offline, or explicitly network-disabled execution performs
   no implicit catalog refresh and emits no update notice based on stale or
   unavailable network metadata. CI uses pinned, pre-installed modules unless
   an explicit lifecycle command permits network access.
6. Updating remains explicit by default. Any future automatic-update mode is
   opt-in and uses the same staged verification, atomic activation, and
   rollback behavior as an explicit update.

If a newer release requires a newer shell or protocol, the notice identifies
that dependency and keeps the current compatible verified module active. If an
update download, verification, health check, or activation fails, the current
active version remains unchanged.

Revocation is out of scope (section 9.2), so the paragraph below is the
original design and describes nothing that is built.

Revocation is not an ordinary update notification. When fresh verified metadata
marks the active release as revoked, the shell records a security diagnostic
and blocks that module according to policy, with guidance to install a safe
compatible version. Offline policy must define how long cached revocation
metadata remains acceptable; it must not silently represent stale metadata as
current.

### 7.3 Verification

The list below is the original design. What is checked is the receipt's own
integrity facts, shell and protocol compatibility, the platform, and the
executable's digest; there is no signature to check and no revocation state to
consult, and the health handshake happens at launch rather than at
verification.

Verification checks:

- the receipt's signed release metadata and local verification evidence;
- release revocation;
- host/protocol compatibility;
- executable digest and secure file ownership/permissions;
- identity and health handshake.

The active executable's integrity is checked before launch. Performance
optimizations must not turn file timestamps alone into the trust decision.

### 7.4 Rollback

No rollback command is built. The retained-version behavior below is the
original design. What holds today is that an install or update failing at any
point before activation leaves the previously active version active.
Rollback changes the active pointer to a retained installation only after
rechecking integrity, compatibility, and revocation. A revoked release is not a
valid rollback target.

### 7.5 Offline installation

Nothing in this subsection is built, and `wso2 bundle` is deferred rather than
cancelled. It describes a signed model, which the rest of this document no
longer does; when it is picked up, its trust claims have to be reconciled with
section 9.2 first.

Offline mode never means unsigned mode. Every offline path follows the same
identity, compatibility, verification, activation, receipt, rollback, and
revocation-metadata policy as online installation.

An individual `.wso2module` file packages one module's signed release metadata,
artifact, provenance, and required trust metadata. It is imported by an
existing shell with `wso2 module install --file <module.wso2module>`.

A fresh machine uses a platform-specific, self-installing offline bundle. The
bundle contains a signed bootstrap installer, an exact root-shell release, the
selected compatible modules, a signed catalog snapshot, a bundle manifest,
digests, signatures, provenance, and the trust material required for offline
verification. The target does not need a preinstalled `wso2` command. After the
package is transferred through an approved process, Windows and macOS verify
the installer through their platform trust mechanisms before execution. A
Linux bundle is distributed with a detached signature and verification
instructions. Its bootstrap signature must be verified with an independently
trusted WSO2 public key before execution. The bootstrap then makes no network
requests; it verifies the remaining package contents, installs the shell, and
verifies and activates the included modules.

On a connected machine, `wso2 bundle create` can build a tailored,
platform-specific self-installing bundle from verified catalog releases. It
must not package arbitrary local executables. If the air-gapped target already
has the shell, `wso2 bundle inspect <file>` can report the contents and
verification state without installation, and `wso2 bundle install <file>` can
import the bundle to add or update modules. These commands are not prerequisites
for fresh-machine installation.

## 8. Storage layout

Conceptual user-level state:

```text
~/.wso2/
  config.yaml
  cli/
    trust/
    catalog/
    modules/
      api/
        versions/
          0.9.0/
            module
            receipt.json
        active.json
```

`active.json` is updated atomically and names an exact installed version and
receipt digest. A state file is preferred over relying only on symlink behavior
because the CLI must behave consistently on Windows.

Sensitive credentials do not appear in this tree.

## 9. Security model

### 9.1 Threats in scope

- compromised download storage;
- a substituted or corrupted artifact;
- archive traversal and unsafe extraction;
- local modification of an installed executable, and `PATH` shadowing;
- credential leakage through arguments, environment, output, or logs;
- a compromised build pipeline.

Three threats the earlier design claimed are not defended today: a forged or
rewritten catalog, a rollback or freeze of catalog metadata, and an
unauthorized publication. The first two follow from the catalog being
unsigned, and the third from there being no publisher identity to be
unauthorized. Section 9.2 states the position rather than implying one.

### 9.2 Publishing trust

Artifacts are integrity-checked and not signed. Every published version
records a size and a SHA-256 digest; the shell checks a download against both
before it writes anything into the module store, and a mismatch aborts the
install, leaving no executable, no receipt, and no change to the active
version. Every launch rechecks the installed executable against its receipt.
That is the whole of the cryptographic guarantee.

What a digest proves is narrow and worth stating exactly. It proves that the
artifact the shell downloaded is the artifact the catalog entry describes. It
does not prove that the entry is authentic. Nothing signs the catalog, so the
integrity of the manifest itself rests on HTTPS to the origin and on control
of what is published there, not on a signature the shell can check.

Publishing to the catalog origin is therefore a concentrated trust point.
That origin serves the install scripts, the catalog the shell reads to decide
what to install, and the digests it checks a download against, so whoever can
publish there controls the update channel for the shell and for every module,
and can rewrite both an artifact and the digest that would have caught it.
The exposure is not new, since it already existed for the install scripts, but
its blast radius grows from a first install to every update of every module.

The mitigation is process rather than cryptography: branch protection on the
default branch, and required review on the release and deployment workflows,
so that no single push reaches the origin. That is a repository setting rather
than a file in this checkout, which is worth saying plainly, because nothing
in the checkout can prove it is switched on.

Manifest signing is the piece that would make the catalog's authenticity
checkable rather than assumed. It is a tracked follow-up, recorded in
section 15, and not a silent omission: publisher signing was removed
deliberately, and this is the deferral it was traded for.

Out of scope, rather than pending: publisher authority and per-publisher
keys, artifact and code signing, notarization, build provenance, an SBOM, and
revocation of a published version or key. The earlier design's TUF-style
hierarchy was never built and is not planned: rotatable trust roots, threshold
root authority, delegated namespace publishing authority, and expiring
timestamp and snapshot metadata. Its purpose was to decide whether a given
publisher may claim a product namespace, and with one organization owning
every module that question has one answer. Registry admission checks for
ownership, policy, scans, and provenance do not exist; what admits a module
release is the protocol gate in section 6.1 and the checks in section 13.

### 9.3 Runtime trust boundary

An integrity-checked native module still runs with the user's operating-system
permissions. The v1 trust decision is therefore: only authorized WSO2 product
software may run through the managed module path.

The host can enforce access to host-owned facilities such as the authentication
broker. Capability declarations alone cannot prevent a native executable from
reading user-accessible files or opening a network connection. OS sandboxing is
future defense in depth, not a claimed v1 property.

### 9.4 Secret handling

- Interactive refresh tokens and long-lived credentials remain in the OS
  secure store.
- CI secrets are read from the CI secret source, remain only in job memory, and
  are never persisted by the CLI.
- Context configuration stores authentication methods and secret references or
  variable names, never secret values.
- Modules receive short-lived or invocation-scoped access material; audience
  and scope are narrowed whenever supported.
- Secret values are never placed in command arguments, receipts, context files,
  logs, or module environments.
- SDK secret types are redacted by default.
- Debug and error paths are tested with injected canary secrets.
- Authentication failures identify the missing context/session without
  including tokens or private request data.

## 10. Version model

The runtime has three independent versions:

- **Host version:** release of the `wso2` binary.
- **Protocol version:** compatibility contract between shell and modules.
- **Module version:** product module release owned by its product team.

The shell supports a protocol window of the current version and its
predecessor, so a user whose shell is one protocol generation behind is not cut
off from module releases: there is a full generation in which to update the
shell before a module release can outrun it. The window is declared once, in
the SDK's protocol package. The shell reads that declaration, and so does the
release gate that refuses to publish a module the released shell cannot
launch, so the two cannot come to disagree about what is supported. The first
generation has no predecessor, so the window is one version wide until there
is something to be behind.

The launch gate is the protocol window intersected with the platform, and
nothing else. **The shell never compares a module's version against its own**,
in either direction. A product module carries its product's version scheme,
chosen so its users recognise it, and that scheme does not track the shell's,
so a module numbered far above or far below the shell says nothing about
whether the two can speak. Comparing them would refuse modules that run
perfectly, and in terms a user cannot act on. Reintroducing the comparison
would look like defensive tightening; it is not, and there is no version pair
for which it is correct. The invariant is recorded here because it is not
self-evident from the code that omits the comparison: an absence leaves no
trace to read, and the next reader to notice it is missing has to be told
that it is missing on purpose.

What the shell does compare is a module's own declared shell range against
the running shell, which is a membership test on a range the module publishes,
not a comparison of two version numbers.

Output:

```text
$ wso2 version
WSO2 CLI   v0.1.0
Protocol   v2, v1
Platform   darwin/arm64

Installed modules
NAME   VERSION   PLATFORM
api    v0.8.1    darwin/arm64

$ wso2 module list
MODULE   INSTALLED   CHANNEL   UPDATE
api      v0.8.1      stable    v0.9.0 available

1 module(s) have an update available. Run wso2 module update --all to take
them.
```

`wso2 version` reads receipts only: it never launches a module and never opens
a network connection. `wso2 module list` reads the catalog index, and costs one
request whatever is installed, because a check selects no version and a version
history is what selecting is for. Neither report claims anything beyond the
integrity facts above: the executable matches the digest in its receipt. There
is no publisher or revocation state behind that to report, which is why
neither report carries a verification column.

## 11. Repository and release structure

The implementation is a multi-module monorepo with three kinds of public
deliverable: the user-facing CLI, the Go SDK used by product teams, and the
product modules themselves.

```text
.
├── go.work
├── go.mod
├── cmd/
│   ├── wso2/main.go
│   ├── wso2-catalog/
│   ├── wso2-catalog-input/
│   └── wso2-module-release/
├── internal/
│   └── {app,auth,boundaries,catalog,contexts,exit,install,modules,
│        output,release,rpc,semver,state,statusservice,version}/
├── modules/                    where product modules will live,
│   └── <namespace>/            one Go module per product namespace
├── examples/
│   └── reference-module/
└── sdk/
    ├── go.mod
    ├── module/
    ├── problem/
    ├── proto/
    ├── protocol/
    ├── result/
    └── testkit/
```

The root Go module builds the `wso2` CLI. `cmd/wso2/main.go` is its entry
point, and shell policy belongs in root `internal/` packages: the module
catalog client and generator, install and update, the managed module store,
the release gate, RPC, contexts, and authentication. The catalog commands are
in `cmd/` rather than in a workflow, so the generator and the gate can be run
and tested locally instead of only on a push.

The top-level `sdk/` directory is a separate public Go module. It contains the
command and module interfaces, the result and problem types, the wire schemas,
the conformance test kit, and the versioned `sdk/protocol` package, which
holds the single declaration of the supported protocol window. A product
module depends only on this public SDK and must never import the shell's
`internal/` packages.

`modules/<namespace>/` is where product module source is to live, one Go
module per product namespace, each to be owned by its product team through
CODEOWNERS. That ownership is a weaker guarantee than a separate repository,
and it is the substantive thing product teams are being asked to accept in
exchange for one review, one pipeline, and one queue. Nothing has moved yet:
the directory does not exist, CODEOWNERS still assigns the whole tree to the
repository's maintainers, and the only module in the checkout is the reference
module under `examples/`, which exists to prove the contract rather than to
ship a product.

There is no separate top-level `api/` directory. The complete versioned
subprocess and RPC contract lives in `sdk/protocol`, imported by both
shell-internal RPC code and product modules. If multi-language support or
schema generation becomes necessary, canonical schemas may move to a top-level
`api/` directory and generated Go bindings may return to the SDK, provided
wire compatibility is preserved.

### Tags

The three deliverables are versioned independently, and the tag namespace is
what separates them:

- the CLI uses plain repository tags such as `v1.2.0` and publishes platform
  archives for users;
- the SDK uses submodule-prefixed tags such as `sdk/v1.1.0` and publishes the
  `sdk/` directory as a tagged public Go module; users do not install it;
- a product module uses tags prefixed by its namespace, such as `api/v0.9.0`,
  and is free to carry its product's own version scheme rather than one
  imposed by this repository.

A module tag must be plain semantic versioning after the `v`. Build metadata
is refused rather than dropped, because a version that cannot round-trip
through a tag, a file path, and a receipt is a version the shell cannot
resolve later.

### Modules depend on the SDK by version, never by a committed `replace`

Every `go.mod` in the repository is prohibited from carrying a `replace`
directive, and the previous-protocol check refuses a pull request that adds
one. That check runs when the shell or the SDK changes, which is the case a
`replace` would be added in, but it is a gate on Go changes rather than an
unconditional one. Local composition belongs in `go.work`, which does not
travel with a release.

This rule is load-bearing rather than stylistic. The protocol window is only
worth declaring if something proves the older end of it still works, and the
only honest way to prove that is to reproduce a released module's dependency
graph: drop the workspace, resolve the published SDK for the previous protocol
by version from the module proxy, build the module against it, and launch it
under the shell built from the branch at hand. A committed `replace` would
silently pin that build back to the SDK in the checkout, so the gate would
pass while proving nothing. `go.work` carries one placeholder replacement for
the SDK, which is unpublished; it disappears on the first SDK release.

No SDK version is published yet, so that gate reports that it cannot prove
anything rather than passing quietly. It begins enforcing the window on the
first SDK release.

Detailed build, test, and release procedures live in the
[release artifacts](reference/release-artifacts.md) and
[module catalog](reference/module-catalog.md) references.

## 12. Existing CLI migration

Migration proceeds in increasing levels of conformance:

1. Move the existing CLI's source into `modules/<namespace>/`, declare its
   namespace and compatibility, and publish it through the catalog as a
   managed executable. There is no signing step: artifacts are
   integrity-checked, as section 9.2 states.
2. Add the SDK identity, description, health, and invocation bootstrap.
3. Replace independent credential handling with the authentication broker.
4. Replace custom output and errors with typed SDK results and problems.
5. Adopt common help, flags, exit codes, and secret redaction.
6. Pass the complete cross-platform conformance suite, including the previous
   protocol.

The compatibility adapter exists to reduce migration risk, not as a permanent
alternative contract. Conformance is the whole of what a completed migration
demonstrates; there is no separate supply-chain gate for it to pass. A module
that has reached level 6 is fully conformant, and that is the whole of the
claim: it says nothing about publisher authority, because there is no
publisher authority to claim.

The pilot should include:

- one clean factory-injected Cobra CLI, such as Agent Manager's `amctl`;
- one CLI with global initialization or legacy coupling, such as `apictl` or
  `mi`.

This validates both the ideal authoring model and the difficult migration path.

## 13. Testing strategy

This section defines the system-wide testing strategy. The ordered test gates
for the first architecture proof are maintained in the
[first CLI vertical-slice plan](plans/first-cli-vertical-slice.md).

### Unit tests

- manifest and compatibility resolution;
- trust and revocation decisions;
- safe archive extraction;
- receipt and atomic-state operations;
- context selection;
- output and problem serialization;
- secret redaction.

### Contract tests

Every module is tested against the same black-box suite:

- identity and protocol negotiation;
- help and command-schema generation;
- standard flags;
- table/JSON/YAML equivalence;
- exit codes and typed errors;
- authentication-broker behavior;
- non-interactive behavior;
- canary-secret non-disclosure.

### Integration tests

- fresh install, update, failure before activation, rollback, remove;
- offline bundle and mirror installation;
- expired, rolled-back, revoked, or tampered metadata;
- corrupted and malicious archives;
- incompatible host and protocol versions;
- cloud Authorization Code with PKCE as the default interactive login;
- Device Authorization as the explicit headless login mode, and its refusal
  with a stable error where the backend does not advertise the grant;
- rejection of browser and device authorization in CI/non-interactive mode;
- on-premises identities without assuming WSO2 Cloud SSO or Identity Server;
- one identity backing several contexts: a single login serving every context
  that references it, with no further authentication on switch;
- per-product access derived from one session and bound to each product's
  audience and scopes, rather than one token reused across products;
- refusal, rather than a broader grant, where a requested narrowing is
  unavailable;
- a product the selected context's identity cannot reach failing with guidance
  that names the contexts that can;
- recorded namespace bindings selecting a context, and no selection occurring
  without an explicit flag, variable, binding, or default;
- context switching performing no network call and no login;
- the five writing-grants-nothing properties of §4.7, including a created and
  an imported context yielding an authentication-class refusal without a login;
- client credentials completing inline with no separate login step, and the
  client secret never reaching a module;
- OS secure-store integration for interactive credentials;
- CI secret-variable and stdin inputs with no filesystem or secure-store
  persistence;
- identity and context import/export proving that no credential values are
  present.

### End-to-end tests

Run pilot product operations on every supported OS/architecture and validate
human and machine output. CI must also prove that pinned, pre-installed modules
work with network access disabled.

## 14. Operational behavior and recovery

- Catalog failure does not break already installed, non-revoked modules when
  policy permits local operation.
- Failed installation or update never changes the active version.
- Missing authentication returns a stable auth problem and the exact login
  command appropriate to the selected context.
- A non-interactive invocation configured with browser or device authorization
  fails immediately and identifies the accepted CI authentication methods.
- Incompatible modules are not launched; the error reports compatible
  alternatives from local receipt metadata.
- `wso2 doctor` reports context, secure-store, catalog, receipt, module
  integrity, compatibility, and protocol status without printing secrets.

## 15. Decisions and remaining design work

The architecture, SDK-first hybrid model, official-only trust scope, and
security principles are selected.

**The verified module term is retired.** It named a module whose publisher,
release metadata, artifact, compatibility, and revocation status had passed
the production trust policy. With publisher authority and revocation gone, the
definition was unreachable rather than merely rare, and the term's whole
load-bearing content was the supply-chain authority that is no longer there.
Retiring it was preferred to redefining it around integrity and compatibility:
keeping the label after removing what it asserted is exactly how a document
comes to imply a trust chain it does not have, which is the failure
section 9.2 exists to prevent. Integrity-checked already states what is true,
and conformant already describes the protocol property, so no third term was
coined. `CONTEXT.md` keeps one entry, and sections 10 and 12 and the module
lifecycle requirements now state the outcome instead of waiting on it.

The next design session should settle, in order:

1. exact module lifecycle command semantics;
2. remaining module message semantics and the SDK public API;
3. context/auth broker API;
4. output/problem schema and exit-code table;
5. namespace and pilot-module selection;
6. rollout and standalone-CLI compatibility policy.

One item is a tracked follow-up rather than an open design question:

- **Manifest signing.** Publisher signing was removed with the multi-repository
  model, and nothing replaced it, so the catalog's authenticity rests on HTTPS
  and on control of the origin rather than on a signature the shell can check.
  Section 9.2 states the residual risk, and the
  [module catalog](reference/module-catalog.md) reference states it where a
  reader meets the digest. Signing the catalog is the work that removes it.
  This entry is the record that it was deferred rather than dropped; naming an
  owner and opening the issue is the next step, and until that is done the
  deferral is tracked only here.

The approved architecture proof has its own bounded
[implementation plan](plans/first-cli-vertical-slice.md). Later delivery work
requires a separate reviewed plan. Phase lists in research documents remain
recommendations, not implementation authority.

## 16. Research

- [Archived original proposal](research/archive/original-proposal.md)
- [Public WSO2 CLI inventory](research/public-wso2-cli-inventory.md)
- [kubectl and Krew findings](research/kubectl-krew.md)
- [Azure CLI, AWS CLI, and Google Cloud CLI comparison](research/cloud-cli-comparison.md)
- [Module architecture options](research/module-architecture-options.md)
- [Root CLI installation and distribution](research/root-cli-installation-distribution.md)

## 17. Implementation plan

- [First CLI vertical-slice plan](plans/first-cli-vertical-slice.md)
