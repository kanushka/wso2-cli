# WSO2 CLI module architecture options

**Status:** Research recommendation  
**Date:** 2026-07-27  
**Scope:** How independently released WSO2 product modules extend the `wso2`
shell

## Executive recommendation

Use an **SDK-first, out-of-process module architecture**:

- each product team ships one signed native executable for its assigned
  top-level namespace;
- the shell discovers only modules installed in its managed, versioned store;
- the shell launches a module on demand for each command and communicates over
  a private inherited pipe using a small, versioned RPC protocol;
- the shared Go SDK implements that protocol and the standard command, output,
  error, context, and authentication behavior;
- the root shell retains long-lived credentials and gives the verified module
  only a short-lived, audience- and scope-restricted token through an
  invocation-scoped authentication broker;
- signed catalog metadata and installation receipts drive compatibility,
  version reporting, activation, update, and rollback.

This is a hybrid of the executable simplicity used by Git and `kubectl` with
the typed handshake, version negotiation, bidirectional calls, and crash
isolation demonstrated by HashiCorp's `go-plugin`. Git and `kubectl` show that
standalone executables make independently shipped commands easy, but both can
search `PATH`; `kubectl` documents that such plugins inherit the environment,
can conflict or shadow one another, and are arbitrary programs
([Git command dispatch](https://git-scm.com/docs/git#Documentation/git.txt-codePATHcode),
[kubectl plugins](https://kubernetes.io/docs/tasks/extend-kubectl/kubectl-plugins/)).
WSO2 should therefore keep executable modules while replacing `PATH` discovery
and ambient credential inheritance with a verified CLI-managed local store and
an explicit protocol.

The recommendation aligns with the current
[product requirements](../product-requirements.md),
[architecture](../architecture.md),
[authentication examples](../examples/authentication-contexts.md), and
[root command reference](../reference/commands.md). It preserves the fixed
boundary: authentication and other shared behavior belong to the shell;
project, deploy, runtime, and other product operations belong to modules.

## Evidence from existing extension systems

The following are source-supported observations, not WSO2-specific decisions:

- Git resolves non-core `git-<command>` executables from `PATH` and forwards
  the remaining arguments. This is a very small and durable extension seam,
  but it does not provide manifests, compatibility negotiation, structured
  output, or a credential boundary
  ([Git documentation](https://git-scm.com/docs/git#Documentation/git.txt-codePATHcode)).
- `kubectl` plugins are standalone executables, receive arguments and the
  inherited environment, cannot override built-ins, and may be implemented in
  any language. Kubernetes also warns that third-party plugins are arbitrary
  programs and that `PATH` discovery can produce shadowing conflicts
  ([Kubernetes documentation](https://kubernetes.io/docs/tasks/extend-kubectl/kubectl-plugins/)).
- Krew adds a manifest and package-manager layer to that executable model.
  Its manifests declare semantic version, platform selectors, archive URI,
  SHA-256 digest, extracted files, and entrypoint
  ([Krew manifest specification](https://krew.sigs.k8s.io/docs/developer-guide/plugin-manifest/)).
- HashiCorp `go-plugin` launches plugins as subprocesses and communicates by
  `net/rpc` or gRPC. It supports protocol versions, bidirectional calls,
  structured logging, TTY preservation, checksums, and cross-language gRPC;
  a plugin crash does not crash the host
  ([upstream repository](https://github.com/hashicorp/go-plugin)).
- Go's standard in-process `plugin` package is unavailable on Windows and
  warns that host and plugin builds can crash unless toolchain, build flags,
  and shared dependency sources match exactly. The Go documentation explicitly
  suggests IPC as a practical alternative despite its overhead
  ([Go package documentation](https://pkg.go.dev/plugin#hdr-Warnings)).
- Docker's plugin API demonstrates capability activation and a versioned,
  RPC-style JSON protocol over a local socket
  ([Docker Plugin API](https://docs.docker.com/engine/extend/plugin_api/)).
- WebAssembly components are self-describing, interact through typed WIT
  interfaces rather than shared memory, and offer a language-neutral canonical
  ABI. They are promising for capability-oriented isolation, but adopting them
  would also require WSO2 to ship a runtime and constrain or adapt existing Go
  CLI code
  ([Component Model overview](https://component-model.bytecodealliance.org/design/components.html),
  [Canonical ABI](https://component-model.bytecodealliance.org/advanced/canonical-abi.html)).
- OAuth 2.0 Token Exchange defines an STS request that can trade a subject
  token for a token intended for a particular resource or audience and scope.
  The RFC does not require every authorization server to implement it
  ([RFC 8693](https://datatracker.ietf.org/doc/rfc8693/)).
- The Update Framework defines signed repository roles, delegated trust,
  expiry, consistent snapshots, and protections against rollback, freeze,
  mix-and-match, and wrong-artifact attacks
  ([TUF specification](https://theupdateframework.github.io/specification/latest/)).
  Sigstore's Cosign can additionally verify artifact digest and publisher
  identity and can package verification material for offline use
  ([Cosign verification](https://docs.sigstore.dev/cosign/verifying/verify/)).

## Feasible architecture options

### Option 1: in-process libraries or Go plugins

Product commands run in the `wso2` process, either compiled into the shell or
loaded dynamically.

**Advantages**

- Direct function calls, shared Cobra tree, and lowest invocation overhead.
- Help, completion, output, and context objects integrate naturally.
- One debugger and no wire protocol.

**Disadvantages**

- Compiling modules into the shell destroys independent release cadence.
- Dynamic Go plugins impose strict toolchain and dependency coupling, lack
  Windows support, cannot be unloaded, and can crash or corrupt the shell.
- A module sees the shell's process memory, including credentials.
- Updating one product can force a shell rebuild or create an ABI matrix.

**Assessment:** reject for independently released production modules. Keep
ordinary Go libraries inside the shared SDK and inside each product executable.

### Option 2: simple executable subprocess plugins

The shell resolves a managed executable and forwards the product arguments,
stdin, stdout, stderr, and selected non-secret environment values.

**Advantages**

- Easiest migration path for existing CLIs.
- Language-neutral and independently releasable.
- A crash terminates only the product command.
- Native binaries preserve current CLI libraries and platform behavior.

**Disadvantages**

- Raw argument and terminal forwarding does not produce a coherent help tree,
  completion schema, structured errors, or typed output.
- Environment inheritance is too broad for secrets and policy.
- Compatibility failures appear only at execution time unless the package
  carries reliable metadata.
- Every module may reimplement common behavior and drift from the WSO2
  conventions.

**Assessment:** useful only as a temporary migration adapter. A fully compliant
module needs a control protocol and the shared SDK.

### Option 3: subprocess module with a versioned RPC contract

The shell launches the module on demand and establishes a private local RPC
channel. The module declares its identity and capabilities, accepts a typed
invocation request, calls shell services such as the auth broker, and returns
structured events/results.

**Advantages**

- Independent release and failure isolation without Go ABI coupling.
- Explicit compatibility negotiation and capability declaration.
- The shell can supply narrowly scoped services instead of ambient state.
- Structured results, errors, logs, help, and completion remain consistent.
- gRPC/protobuf permits future non-Go modules if needed.

**Disadvantages**

- More design and test work than argument passthrough.
- Process startup and serialization add modest latency.
- Interactive streaming, cancellation, and terminal ownership require an
  explicit contract.
- Native processes are isolated for reliability, not sandboxed from the user's
  filesystem or network.

**Assessment:** recommended production architecture. For CLI-scale operations,
process startup is normally insignificant compared with API calls; measure it
before adding a persistent process.

### Option 4: persistent daemon or central token broker

A background `wso2` service owns credentials, contexts, module processes, and
RPC endpoints. Each shell invocation becomes a client of that daemon.

**Advantages**

- Warm modules and token caches reduce repeated startup and refresh work.
- One place can coordinate concurrent refreshes and session changes.
- Strong broker lifecycle and audit point.

**Disadvantages**

- Introduces service installation, startup, upgrade, stale-socket, ownership,
  multi-user, and recovery complexity on every supported OS.
- Creates a long-lived high-value credential process and expands its attack
  surface.
- Makes containers, CI, SSH sessions, and portable installations harder.
- Daemon and CLI version skew becomes another compatibility problem.

**Assessment:** do not require a daemon in the first release. Use an
invocation-scoped broker hosted by the shell process. Add an optional user
service only if measurements later show a material need for concurrency or
latency.

### Option 5: WebAssembly component modules

The shell embeds a component runtime and product teams compile modules against
WIT-defined host interfaces.

**Advantages**

- Portable artifact and language-neutral typed interfaces.
- Host-controlled capability surface can be narrower than a native process.
- Stronger isolation potential than native modules.

**Disadvantages**

- Existing Go/Cobra CLIs cannot be reused without substantial adaptation.
- Filesystem, network, terminal, native library, certificate-store, and proxy
  behavior all depend on runtime capabilities.
- Runtime/toolchain maturity and component-version migration become WSO2
  platform responsibilities.

**Assessment:** retain as a future restricted-module option, not the initial
format. The RPC contract should remain implementation-neutral enough that a
Wasm adapter could implement it later.

## Recommended concrete design

### Package and installed layout

One release archive per module version and OS/architecture:

```text
api_1.8.0_darwin_arm64/
  module.yaml
  bin/wso2-api
  commands.json
  LICENSE
  NOTICE
  sbom.spdx.json
  provenance.intoto.jsonl
```

`module.yaml` is signed release metadata and contains:

- immutable module ID, assigned namespace, publisher, and version;
- supported OS/architecture and executable path;
- artifact and file digests;
- supported shell range and module-protocol range;
- declared capabilities, product API audiences, and maximum scopes;
- command-schema version and digest;
- release channel, build provenance/SBOM references, and revocation status.

The local store should be versioned, for example
`modules/<module>/<version>/`, with a small active-version pointer and an
installation receipt containing the verified metadata. Exact OS paths are an
implementation choice. The shell reads receipts for `wso2 version` and module
inventory; it must not execute modules merely to discover versions.

### Discovery and namespace ownership

The official catalog assigns one top-level namespace to one WSO2 product
module. Built-in root commands always win. Resolution uses only the managed
store and active receipt—never `PATH`, the working directory, or an arbitrary
executable supplied by an environment variable.

The shell can merge the signed `commands.json` schema into root help and
completion without starting the module. At launch, the module confirms the
schema digest in its handshake, preventing installed metadata and executable
behavior from silently diverging.

### Invocation and protocol

Recommended transport: an inherited anonymous pipe or
Unix-domain-socket/socketpair equivalent, with a Windows named-pipe
implementation. Do not expose a predictable TCP listener. Use length-delimited
protobuf messages; gRPC is optional and should be adopted only if its generated
surface materially simplifies bidirectional streaming.

Suggested launch sequence:

1. Shell parses global flags, selects the active or explicit context, resolves
   the module receipt, and validates compatibility.
2. Shell creates a private IPC channel and launches the verified executable
   with a minimal allowlisted environment.
3. Module sends `Hello`: module ID/version, namespace, schema digest, supported
   protocol versions, SDK version, and requested capabilities.
4. Shell selects the highest mutually supported protocol and replies with
   `Welcome`: invocation ID, selected protocol, shell version, locale,
   interaction policy, terminal features, output mode, deadline, and granted
   capabilities.
5. Shell sends `Invoke`: product argument vector plus a non-secret context
   snapshot. The module emits structured progress/log/result/problem events.
6. Cancellation propagates from shell to module. The shell enforces a graceful
   shutdown deadline and then terminates the child.

Protocol versions should be integers for breaking wire changes. Additive
message fields and separately versioned capabilities handle compatible
evolution. Compatibility is checked both before installation from signed
metadata and again at runtime through the handshake. Unknown required
capabilities fail closed with a stable error and upgrade guidance.

For commands that need a real terminal, the handshake can grant a `tty-v1`
capability and explicitly transfer terminal ownership. Otherwise the shell
owns rendering so table, JSON, YAML, quiet mode, redaction, and error envelopes
stay consistent.

### Context and authentication boundary

The module receives only the selected context's non-secret data: context type,
organization ID, region, product endpoint, and relevant resource identifiers.
It does not receive credential-store references, refresh tokens, personal
access tokens, client secrets, the complete process environment, or unrelated
product sessions.

When a command needs authorization:

1. The module calls `AcquireAccess` over the private broker channel with the
   product endpoint/audience and scopes needed for this operation.
2. The shell verifies that the request is within both the signed module
   declaration and the active user's/session's permissions.
3. The shell refreshes or exchanges the organization-bound credential. Where
   the WSO2 authorization service supports RFC 8693, token exchange can mint a
   narrower token for the requested audience and scopes. Otherwise the broker
   uses the best deployment-supported short-lived token mechanism; RFC 8693
   support must not be assumed.
4. The broker returns a memory-only access token with expiry. It never returns
   the long-lived source credential.

The module may necessarily see the short-lived token to call a product API;
therefore it remains trusted WSO2 code. The value must not be put in argv,
environment variables, files, logs, command results, or crash reports. It
expires with the shortest practical lifetime and cannot be refreshed by the
module. Switching organizations changes the active context/session; no token
bound to organization A may be used for organization B.

### Lifecycle, trust, and rollback

Use a signed official catalog with namespace delegation and TUF-style
timestamp/snapshot/targets roles. Verify before activation:

- catalog freshness and rollback protection;
- publisher authorization for the namespace;
- shell/protocol/platform compatibility;
- archive digest, signature, provenance policy, and expected file layout;
- module conformance test status and declared capabilities.

Installation extracts into a new immutable version directory, writes the
receipt, verifies the installed files, then atomically changes the active
pointer. Updating follows the same path and retains at least the previous
verified version. A failed health check never changes active state.
`wso2 module rollback <name>` atomically reactivates the retained receipt;
explicit rollback is allowed by user policy but is recorded so a network
attacker cannot cause it silently.

Offline bundles should carry the archive plus all signed catalog and
verification material needed under an administratively pinned trust root.
Module pins and lock/export data should identify exact versions and digests for
reproducible CI.

### SDK and product-team contract

The Go SDK should provide:

- module bootstrap, handshake, cancellation, and lifecycle;
- a standard Cobra command construction layer and global-flag integration;
- generated protocol clients and typed context models;
- an auth-broker client that requests named audience/scope sets;
- typed results, progress events, problems, exit categories, and redaction;
- command-schema generation and a check that schema matches the binary;
- test harnesses for protocol compatibility, golden help/output, secret
  leakage, cancellation, and broker-policy denial;
- packaging, manifest generation, SBOM/provenance attachment, signing, and
  conformance tooling.

Product teams own product resources/actions, API clients, validation, and
product-specific scopes. They do not implement root login, credential storage,
module updates, global output policy, or root contexts.

## Phased implementation plan

### Phase 0: prove the seam

- Build a shell dispatcher and one small example module.
- Use managed-directory discovery and a minimal handshake plus `Invoke`.
- Pass only non-secret context data and mock `AcquireAccess`.
- Measure cold startup on macOS, Linux, and Windows.

**Exit criterion:** one module contributes help/schema and runs a structured
command without importing shell internals.

### Phase 1: production protocol and SDK

- Freeze protocol version 1, capability rules, error model, streaming, TTY,
  cancellation, and conformance tests.
- Implement the invocation-scoped auth broker and secure-store adapters.
- Migrate one real product namespace behind the SDK.

**Exit criterion:** a product module can release independently and use
organization-bound authentication without accessing long-lived credentials.

### Phase 2: managed distribution

- Add the official signed catalog, platform archives, receipts, atomic
  activation, pins, update, rollback, revocation, and offline bundles.
- Make `version`, `module list`, and `doctor` receipt-driven and offline-safe.

**Exit criterion:** interrupted or malicious updates cannot replace the active
verified module, and the prior version is recoverable.

### Phase 3: migration and adoption

- Add a deliberately limited adapter for existing executables.
- Migrate product teams to the SDK and remove passthrough exceptions as each
  module becomes conformant.
- Publish module-author documentation, compatibility policy, release templates,
  and support ownership.

### Phase 4: optimize only from evidence

- Consider a warm process pool or optional user daemon only if startup/token
  refresh measurements justify its operational cost.
- Prototype a Wasm component adapter only for use cases that require a stronger
  capability boundary or language-neutral portable package.

## Decisions still requiring product-owner agreement

1. **Wire technology:** plain protobuf framing versus gRPC. The semantic
   contract should be agreed before selecting the library.
2. **Token service support:** which cloud and on-premises identity deployments
   can issue audience/scope-restricted exchanged tokens, and the fallback for
   each product.
3. **Capability policy:** exact audiences/scopes per namespace and which new
   capabilities require user/admin approval.
4. **Trust infrastructure:** TUF repository ownership, signing identities,
   threshold/root-key operations, revocation SLA, and offline trust-root
   distribution.
5. **Compatibility policy:** supported shell range, protocol support window,
   SDK deprecation window, module retention count, and emergency rollback
   rules.
6. **Terminal contract:** which commands truly require direct TTY ownership
   rather than structured shell-rendered interaction.
7. **Platform matrix:** initial OS/architecture targets and whether modules may
   contain native auxiliary binaries.

These decisions do not change the recommended process boundary. They determine
the precise version-1 protocol, security policy, and operational commitments.
