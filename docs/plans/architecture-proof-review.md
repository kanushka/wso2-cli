# Architecture Proof Review

**Status:** Accepted review of a completed architecture proof

**Date:** 2026-07-28

**Outcome:** The proof passes its acceptance gate

**Related:** [First CLI vertical slice](first-cli-vertical-slice.md),
[product requirements](../product-requirements.md),
[architecture](../architecture.md), and
[ADR 0002](../adr/0002-module-transport.md) and
[ADR 0003](../adr/0003-shell-owned-output.md)

## 1. Purpose

The [first CLI vertical slice](first-cli-vertical-slice.md) plan owns what the
architecture proof had to build. This document is the review that closes it: it
records that the acceptance gate passes, names the seams a production
implementation replaces, and states what the proof does not establish.

It changes no decision. Where it differs from the product requirements or the
architecture, those documents control.

## 2. Reproducing the gate

```shell
./scripts/acceptance.sh
```

The script builds the three independently versioned modules from the checkout
it is run in and runs every test layer, ending with the black-box runs that
drive the built shell and the built module through the same external seam a
user does. Continuous integration runs this same script, so the proof a pull
request must pass is one a contributor can reproduce exactly. A pull request
must also pass the separate static checks — formatting, `go vet`, module
hygiene, and linting — which the gate deliberately leaves to tooling.

The gate needs a Go toolchain and a warm or reachable module cache. It needs no
Protobuf toolchain, no network catalog, no WSO2 credentials, and no real
product service. It clears every ambient `WSO2_` variable and roots each run in
a temporary state directory, so it neither reads nor writes real WSO2 user
state.

## 3. Production replacement seams

The proof exists to find out whether these four boundaries are in the right
places. Each one is a fixture behind an interface the rest of the system was
written against, so replacing it is a change to one package rather than a
change to the shape of the system.

For each seam, what does not change is as important as what does: it is the
part the proof claims to have established.

### 3.1 Module trust

**Today:** `internal/modules` resolves a namespace to one installed version
through a receipt in the shell-owned managed store, checks the receipt's
compatibility ranges and platform, rejects an executable path that escapes its
immutable version directory, and recomputes the executable's SHA-256 digest
before every launch (`Store.Resolve`, `Receipt.Validate`, `FileDigest`). The
receipt is written by `internal/modules/fixture`, a test-only installer.

**A production shell replaces:** the origin of the receipt and the strength of
the claim it carries. Installation becomes a real command reading signed
catalog metadata; the digest check becomes publisher verification, provenance,
and revocation. `internal/modules/fixture` is deleted rather than hardened.

**What does not change:** resolution happens only through receipts in the
managed store, never `PATH` or the working directory; every compatibility and
containment check happens before launch; and the digest is recomputed per
launch rather than trusted from install time.

### 3.2 Credential storage

**Today:** there is none, deliberately. `internal/contexts` records where a
credential comes from — an authentication method and the name of an
environment variable — and never a credential value. The shell reads that
variable into memory for the length of one invocation
(`auth.Broker.credential`).

**A production shell replaces:** the credential source. `contexts.Auth` gains
methods beyond `development-credential`, and the value behind a context comes
from an OS keychain or secure store populated by a real login, rather than from
an environment variable named in a context.

**What does not change:** a context names a source and holds no secret; the
credential exists only in shell memory during one invocation; it is never
written to state, never passed to a module, and never included in a problem,
diagnostic, or rendered result.

### 3.3 Token issuance

**Today:** `internal/auth` is the broker and `internal/auth/devtoken` is the
issuer. The broker intersects a module's runtime request with the module
receipt, the selected context, the organization, and the invocation, then mints
a token bound to all of them with a two-minute life. The issuer signs with the
source credential and holds no key of its own; every token it mints opens with
`wso2-development-token.` so one that escapes into a log is recognizable as
development material.

**A production shell replaces:** `devtoken` with a real token exchange against
an identity provider. `auth.Broker.Acquire` keeps its signature and its policy;
only the `devtoken.Mint` call inside it becomes an exchange. The reserved
`auth.ProofNamespace` guard, which refuses to broker for any namespace but the
proof, is removed when there is something real to broker for.

**What does not change:** the broker is shell policy applied to facts a module
cannot influence; a module receives only a token and its expiry; access is
granted once per invocation and cannot be renewed by the module; and every
refusal is a typed problem in the authentication exit class with recovery
guidance that names no credential.

### 3.4 Product-service access

**Today:** `internal/statusservice` is a local read-only service that accepts a
request only when the presented token is the one this invocation was granted,
for the expected audience, scope, and organization, and is still within its
life. It is never linked into the shell binary. The reference module reaches it
at the endpoint its context names, carrying only the token the broker gave it.

**A production shell replaces:** nothing in the shell. The service is deleted
and a real product API takes its place, enforcing the same claims through its
own identity provider.

**What does not change:** the module learns where to call from the non-secret
context and learns whether it may call from the broker; those are separate
answers, and holding the endpoint grants nothing. A service failure and a
broker denial remain distinct exit classes, so automation can tell "it is
broken" from "you may not" without reading text.

## 4. Acceptance criteria and the runs that prove them

Every criterion is proved by an automated run in the gate of section 2. The
named tests are the ones written for that criterion; several criteria are also
asserted incidentally elsewhere.

| Criterion | Proof |
| --- | --- |
| A clean checkout builds the three independently versioned modules | `scripts/acceptance.sh` stages 1 to 3; `TestEveryModuleBuildsInTheLocalWorkspace`, `TestTheSDKBuildsAndTestsWithWorkspaceCompositionDisabled` |
| Every run uses an isolated store and context and never touches real WSO2 state | `isolatedStateRoot` in every acceptance run; the `WSO2_` clearing in `scripts/acceptance.sh`; `internal/modules/fixture` and `internal/contexts/fixture` refuse to write to `~/.wso2` |
| `wso2 version` reports receipt-backed inventory without launching the module | `TestVersionReportsIndependentlyInjectedShellAndModuleVersions`, `TestVersionDoesNotLaunchTheInstalledModule` |
| `wso2 reference status` succeeds against the local service in both modes | `TestBrokeredReferenceStatusReportsTheServicesOwnAnswer`, `TestBrokeredReferenceStatusRendersTheServicesAnswerAsJSON` |
| Table and JSON represent the same semantic result | `TestTableAndJSONReportTheSameFields` |
| JSON stays parseable while diagnostics are present | `TestJSONOutputStaysValidWhileTheModuleWritesDiagnostics`, `TestModuleDiagnosticsAreBoundedAndCannotContaminateJSONOutput` |
| Tampering, escaping paths, `PATH` shadowing, incompatible receipts, identity and protocol mismatch | `TestACopiedAndModifiedExecutableIsRejectedBeforeLaunch`, `TestAReceiptPathThatEscapesItsVersionDirectoryIsRejectedBeforeLaunch`, `TestASymbolicLinkThatLeavesTheVersionDirectoryIsRejectedBeforeLaunch`, `TestASameNamedExecutableOnPathOrInTheWorkingDirectoryIsIgnored`, `TestIncompatibleReceiptMetadataIsRejectedBeforeLaunch`, `TestARuntimeIdentityThatContradictsTheReceiptIsRejectedBeforeInvocation` |
| Broker denial, claim mismatch, token expiry, and local-service failure | `TestAMissingCredentialIsDeniedWithSafeRecoveryGuidance`, `TestAnUndeclaredAudienceIsDenied`, `TestAnExcessiveScopeIsDenied`, `TestAServiceThatRejectsTheAccessClaimsIsReported`, `TestExpiredAccessIsRefusedByTheService`, `TestAFailingServiceAndADeniedRequestEndInDifferentExitClasses` |
| Malformed protocol data, premature exit, panic, non-zero exit, and timeout | `TestDamagedFramesBecomeStableProtocolProblems`, `TestAnUnknownEnvelopeMessageKindFailsClosed`, `TestAModuleThatCrashesBeforeAnsweringFailsWithAStableProblem`, `TestAModuleThatPanicsFailsWithAStableProblemWithoutCrashingTheShell`, `TestAModuleThatAnswersThenExitsUncleanlyFailsWithAStableProblem`, `TestAModuleThatNeverAnswersFailsWithAStableProblem`, `TestAHangingModuleIsGivenAGracePeriodToExitBeforeItIsKilled` |
| The canary credential is absent from every disclosure surface | `TestAModuleThatDisclosesAllItCanReachStillDisclosesNoCredential`, `TestNoFileTheRunLeavesBehindHoldsTheCredential`, `TestNoTypedProblemDisclosesTheCredential`, `TestNoCrashDiagnosticDisclosesTheCredential`, `TestTheModuleEnvironmentCarriesNoAmbientCredential` |
| The development credential and issuer are visibly non-production and out of production reach | `TestEveryDevelopmentFixtureNamesItselfNonProduction`, `TestNoDevelopmentFixtureIsReachableFromTheShellBinary`, `TestNoDevelopmentFixtureIsReachableFromTheReferenceModule`, `TestAModuleOutsideTheProofNamespaceIsNeverBrokeredAccess` |
| Built artifacts carry no fixed development credential or signing key | `TestTheBuiltArtifactsCarryNoDevelopmentCredential`, `TestTheShellHasNoCredentialToFallBackOn`, `TestTheDevelopmentIssuerHoldsNoFixedSecret` |
| Every test layer runs in the repository's normal CI environment | The `Architecture Proof` job in `.github/workflows/pr-checks.yml` |
| The reference module can move to another repository unchanged | `TestTheReferenceModuleWorksFromAnotherRepository`, `TestTheReferenceModuleDependsOnThePublicSDKOnly`, `TestTheSDKAndReferenceModuleCannotImportShellInternals` |

### 4.1 Why the canary scan is trusted

A scan that finds nothing is worth only as much as the confidence that it was
looking somewhere. Two things establish that here.

The first is the adversary. `test/acceptance/testdata/canarymodule` is a
conforming module written to disclose everything it can reach — its process
arguments, its whole environment, the invocation, the non-secret context, and
the access it was granted — through both channels a module has. The scan reads
what a hostile module could publish, not what a careful one happens to.

The second is that each scan proves its own subject first. The disclosure run
fails unless the module actually held a fixture token; the state-file walk
fails unless it read the files a run installs; the crash scan fails on empty
diagnostics; the typed-problem scan fails unless the run failed the way it was
meant to. A scan of an empty stream cannot pass for the wrong reason.

## 5. What this proof does not establish

The exclusions in [section 11 of the
plan](first-cli-vertical-slice.md#11-explicitly-deferred) stand unchanged. The
ones most likely to be misread as proved are:

- **No product has migrated.** The `reference` namespace is reserved for the
  proof and is not a candidate public product namespace.
- **No publisher verification.** The digest check proves an executable is the
  one its receipt describes, and says nothing about who published it or
  whether that release is still fit to run.
- **No authentication product.** There is no login, logout, refresh, or
  credential persistence, and the fixture-token format is not a public
  contract.
- **No platform matrix.** The gate passes on the primary development
  environment and the repository's CI runner. A full operating-system and
  architecture matrix is a production hardening gate.
- **No compatibility promise.** The maximum frame size, exit codes, and problem
  codes are asserted by tests so they cannot drift unnoticed. That makes them
  stable, not public.
