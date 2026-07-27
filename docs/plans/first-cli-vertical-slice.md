# First CLI Vertical Slice

**Status:** Approved implementation plan

**Date:** 2026-07-27

**Outcome:** Architecture proof

**Related:** [Product requirements](../product-requirements.md),
[architecture](../architecture.md), and
[ADR 0002](../adr/0002-module-transport.md) and
[ADR 0003](../adr/0003-shell-owned-output.md)

## 1. Purpose

This slice proves the riskiest runtime boundaries of the WSO2 CLI with one
non-production command:

```shell
wso2 reference status
```

It is an architecture proof, not a pilot release, minimum viable product, or
claim that a real WSO2 product has migrated. The `reference` namespace is
reserved for the proof and is not a candidate public product namespace.

The product requirements remain authoritative for product intent, and the
architecture remains authoritative for the target system. This document owns
only the implementation order, scope, and acceptance gate for this slice.

## 2. Decisions

The slice uses these agreed constraints:

- the shell, public Go SDK, and reference module are independently buildable Go
  modules composed locally with a Go workspace;
- the shell discovers modules only through receipts in its managed store and
  never searches `PATH`;
- the proof checks receipt compatibility and the executable's SHA-256 digest
  but does not claim publisher or release verification;
- the shell and module exchange length-delimited Protobuf messages over the
  module's inherited standard input and standard output;
- module standard output contains protocol frames only, module standard error
  contains bounded diagnostics, and only the shell writes user output;
- a shell-owned development credential source and token issuer prove the
  authentication-broker boundary without implementing login or persistent
  credential storage;
- the reference module receives only a short-lived, organization- and
  invocation-bound fixture token, never the source credential; and
- the runtime contract is limited to handshake, invocation, broker access,
  typed result, and typed problem messages.

## 3. End-to-end scenario

```text
acceptance harness
  -> isolated state and managed module store
  -> integrity-checked reference module and receipt
  -> non-secret reference context
  -> shell-only source credential

user
  -> wso2 reference status
  -> receipt resolution and executable digest check
  -> reference module subprocess
  -> protocol handshake and invocation
  -> module AcquireAccess request
  -> shell policy and development token issuer
  -> short-lived fixture token
  -> local read-only status service
  -> typed Result or Problem
  -> shell-rendered table or JSON
```

The acceptance harness supplies a canary source credential through a
shell-readable development credential source. The shell removes that source
from the child environment. The local status service accepts only a fixture
token with the expected audience, scope, organization, invocation, and expiry.
The fixture-token format and development issuer are test infrastructure, not
public protocol or authentication contracts.

## 4. Repository shape

The initial layout is:

```text
.
├── go.mod
├── go.work
├── cmd/
│   └── wso2/
├── internal/
│   ├── app/
│   ├── auth/
│   ├── boundaries/
│   ├── context/
│   ├── exit/
│   ├── modules/
│   │   └── fixture/
│   ├── output/
│   ├── rpc/
│   ├── semver/
│   ├── state/
│   └── version/
├── sdk/
│   ├── go.mod
│   ├── module/
│   ├── problem/
│   ├── protocol/
│   ├── result/
│   └── testkit/
├── test/
│   └── acceptance/
└── examples/
    └── reference-module/
        ├── go.mod
        └── cmd/
            └── wso2-module-reference/
```

`internal/boundaries` holds the build-boundary tests, `internal/exit` owns the
exit classes of section 8, `internal/modules/fixture` is the test-only fixture
installer of section 5, `internal/semver` implements receipt compatibility
ranges, `internal/state` locates the shell-owned state root, and
`test/acceptance` holds the black-box runs of increment 5.

Dependency rules:

- the SDK imports no shell `internal` package;
- the reference module imports only the public SDK and ordinary third-party
  dependencies, never shell internals;
- no committed `replace` directive points to a local checkout;
- `go.work` composes the unpublished SDK and reference module for this slice;
- the SDK builds and tests with `GOWORK=off`; and
- the reference module's `GOWORK=off` release check begins after an SDK version
  is published and is not a gate for this proof.

## 5. Local module discovery

The acceptance harness installs the reference executable under an isolated
managed store and writes a module receipt containing:

- namespace;
- module version;
- executable path relative to its immutable version directory;
- supported protocol range;
- supported shell range;
- operating system and architecture;
- declared authentication audience and scopes; and
- executable SHA-256 digest.

An active-version pointer selects one installed version. Resolution must:

1. give built-in shell commands precedence;
2. reject namespaces other than the requested namespace;
3. reject paths that escape the managed version directory;
4. reject incompatible shell, protocol, platform, or architecture values;
5. recompute and compare the executable digest before every launch; and
6. ignore same-named executables on `PATH` or in the working directory.

The test fixture writes the receipt directly through an internal test helper.
The slice does not add a public unverified local-install command.

`wso2 version` is the only supporting built-in required by the slice. It reads
the receipt and reports the shell, protocol, platform, and reference module
versions without launching the module.

## 6. Runtime contract

Each frame is an unsigned-varint length followed by one Protobuf envelope. The
decoder rejects truncated, malformed, and oversized frames. Unknown Protobuf
fields are ignored for additive compatibility; unknown envelope message kinds
and unknown required capabilities fail closed. The initial maximum frame size
is an internal constant covered by tests; it is not yet a public compatibility
promise.

The slice implements only these semantic messages:

| Message | Direction | Purpose |
| --- | --- | --- |
| `Hello` | module to shell | Runtime identity, supported protocols, and capabilities |
| `Welcome` | shell to module | Selected protocol, invocation identity, and non-secret context |
| `Invoke` | shell to module | Product arguments, output mode, and invocation policy |
| `AcquireAccess` | module to shell | Requested audience and scopes |
| `AccessGranted` | shell to module | Short-lived fixture token and expiry |
| `AccessDenied` | shell to module | Typed broker denial |
| `Result` | module to shell | Typed semantic command result |
| `Problem` | either direction | Stable failure category, code, safe message, and recovery |

Every post-handshake envelope carries the invocation ID. Request and response
messages that may be interleaved also carry a correlation ID.

The sequence is:

1. The shell launches the integrity-checked module with a sanitized
   environment.
2. The module sends `Hello`.
3. The shell compares runtime identity with the receipt, selects one mutually
   supported protocol, and sends `Welcome`.
4. The shell sends `Invoke` for `reference status`.
5. The module sends `AcquireAccess` for the declared reference-status audience
   and read scope.
6. The shell intersects the request with the receipt, selected context, and
   invocation policy, then sends `AccessGranted` or `AccessDenied`.
7. The module calls the local status service and sends one terminal `Result` or
   `Problem`.
8. The module exits. A missing terminal message, extra protocol output,
   non-zero exit, panic, or hang becomes a shell-owned process problem.

The slice handles invocation timeout by closing the protocol input and
terminating the child after a short grace period. A protocol-level cancellation
message, streaming, prompts, and direct terminal ownership are deferred.

## 7. Context and broker fixture

The isolated reference context contains only:

- context name;
- organization ID;
- local status endpoint;
- authentication method identifier; and
- the name of the environment variable holding the source credential.

It contains no credential value. The shell reads the named variable into
memory, applies receipt and context policy, and asks a development issuer for a
short-lived token. The token must be bound to:

- the reference-status audience;
- the read-only status scope;
- the selected organization;
- the current invocation ID; and
- a near-term expiry.

The reference module receives the fixture token only inside
`AccessGranted`. It receives neither the source credential nor its source
reference. The token cannot be refreshed by the module.

## 8. Result and problem behavior

The reference status result has one semantic shape used by both renderers:

```text
organization
service
status
checkedAt
```

The shell renders it as a human-readable table by default or as a JSON object
with `--output json`. JSON standard output must remain valid when the module
writes diagnostics to standard error.

The slice defines named exit classes for:

- usage or configuration;
- authentication or broker policy;
- module integrity or compatibility;
- protocol or module process failure; and
- product-service failure.

Exact codes are owned centrally by the shell and asserted in golden tests.
Problem details and diagnostics must be scanned for the canary source
credential.

## 9. Implementation sequence

### Increment 1 — Establish build boundaries

Create the three Go modules, local workspace, shell entry point, SDK bootstrap,
and reference executable.

Gate:

- all three modules build in the workspace;
- the SDK tests with `GOWORK=off`;
- dependency checks reject shell-internal imports and local `replace`
  directives; and
- shell and module versions can be injected independently.

### Increment 2 — Resolve an integrity-checked module

Implement the receipt, isolated store, active-version resolver, digest check,
fixture installer, and `wso2 version`.

Gate:

- version reporting does not launch the module;
- valid receipts resolve deterministically;
- tampered binaries and escaping paths are rejected; and
- `PATH` and working-directory shadowing have no effect.

### Increment 3 — Complete one structured invocation

Add Protobuf schemas and generated types, framing, handshake negotiation,
process launch, `Invoke`, `Result`, `Problem`, table rendering, and JSON
rendering.

Gate:

- `wso2 reference status` reaches a static handler and returns equivalent table
  and JSON results;
- receipt and runtime identity must match;
- malformed, oversized, incompatible, and partial messages fail closed;
- module standard output cannot contaminate user output; and
- crash, non-zero exit, and timeout produce stable shell problems.

### Increment 4 — Prove the authentication broker

Add the isolated context, development credential source, broker policy,
development token issuer, SDK broker client, and local status service.

Gate:

- the complete command succeeds with the expected audience, scope,
  organization, invocation, and expiry;
- undeclared audience or scope is denied;
- a token for another organization or invocation is rejected;
- missing source credentials produce safe recovery guidance; and
- the source credential never reaches module arguments, environment, receipt,
  context, protocol result, output, diagnostics, or crash text.

### Increment 5 — Black-box acceptance

Run the built shell and module from an isolated state directory against the
local service.

Gate:

1. `wso2 version` reports the installed reference module without executing it.
2. `wso2 reference status` succeeds in table and JSON modes.
3. A copied and modified executable is rejected before launch.
4. A protocol mismatch fails before invocation.
5. Broker denial and local-service failure map to distinct typed problems.
6. A crashing or hanging module cannot crash or indefinitely block the shell.
7. The JSON result remains valid while diagnostics are present.
8. A canary scan finds no source-credential disclosure.

## 10. Test layers

| Layer | Required coverage |
| --- | --- |
| Unit | Receipt validation, path containment, digest comparison, frame codec, negotiation, broker policy, token claims, rendering, redaction, and exit mapping |
| Contract | SDK handshake, invocation, broker request, result/problem semantics, incompatible versions, malformed frames, and bounded diagnostics |
| Integration | Managed-store resolution, sanitized subprocess launch, context selection, credential source, token issuer, local status service, crash, and timeout |
| End to end | Build three modules, install isolated fixture, version, successful status, tamper, mismatch, denial, service failure, crash, timeout, and canary scan |

The slice must pass on the primary development environment and the repository's
normal CI runner. A complete operating-system and architecture matrix is a
production hardening gate, not a condition of this architecture proof.

## 11. Explicitly deferred

- real WSO2 product namespaces, APIs, and module migrations;
- browser, device-code, on-premises, and workload-identity authentication;
- login, logout, session refresh, secure-store persistence, and OS keychains;
- signed catalog metadata, publisher verification, provenance, SBOMs,
  revocation, installation, update, rollback, and garbage collection;
- public local-file installation or arbitrary binary overrides;
- command description, merged help, completion metadata, and health checks;
- YAML output, streaming, progress, prompts, direct TTY ownership, and a
  daemon;
- production archives, installers, self-update, offline bundles, and the
  supported-platform matrix; and
- compatibility guarantees beyond the fixtures required to exercise protocol
  negotiation.

## 12. Definition of done

The slice is complete only when:

- a clean checkout builds the shell, SDK, and reference module without touching
  the user's real WSO2 state;
- the end-to-end scenario passes from built binaries;
- every gate in increments 1 through 5 is automated;
- the reference module can be moved to another repository without changing its
  imports or runtime contract;
- all development credential and token-issuer implementations are visibly
  non-production and inaccessible through production constructors;
- no production artifact contains a fixed development credential or signing
  key; and
- the implementation review lists the production replacement seams for module
  trust, credential storage, token issuance, and the product service.

The next slice should add persistent shell-owned authentication and login/logout
or migrate one real product command. That choice requires a separate plan based
on product-owner and authentication research.
