# Reference Module OAuth Boundary

**Status:** Delivered. Tasks 1-7 merged through PR #48.

**Date:** 2026-08-09

**Outcome:** The reference audience verifies issuer-minted access

**Related:** [Issue #47](https://github.com/wso2/wso2-cli/issues/47) is the
spec, with [architecture](../architecture.md),
[ADR 0004](../adr/0004-shell-brokered-authentication.md) and
[ADR 0005](../adr/0005-audience-side-verification.md), which this work
established.

## Goal

Give the reference status service a second way to establish that a token is
genuine — verifying an OpenID issuer's RS256 signature against the keys it
publishes — so the acceptance suite's boundary assertions hold for a real OAuth
access token and not only for the development fixture credential.

The half that already worked is worth stating, because it is easy to misread
this slice as delivering it: the reference module had received a genuine
issuer-minted token since the login slice, and the scope-narrowing and
audience-binding assertions on it already existed. What did not exist was an
audience that checked one. The service receiving that token answered 200 to any
bearer, so every assertion about a refusal described a fixture recomputing a
shared secret.

## Architecture

`internal/statusservice` keeps one `authorize` and gains a `verifier` seam
beneath it. `devtokenVerifier` is the shared-secret path; `jwksVerifier`
verifies an issuer-signed JWT through `github.com/coreos/go-oidc/v3`. Both
answer in one normalized `access` shape, so `authorize` never learns which
format it was handed, and a service is configured with a source credential or
an issuer, never both and never neither.

The acceptance harness grows a second arm: a schema version 2
client-credentials identity against `internal/auth/fakeissuer`, which needs no
keyring and therefore runs the shell as a real subprocess — which the
in-process login tests structurally cannot.

## What shipped

    internal/statusservice/verifier.go   CREATE  access shape, verifier seam, devtoken path
    internal/statusservice/jwks.go       CREATE  issuer-signed verification via JWKS
    internal/statusservice/statusservice.go MODIFY one policy over both verifiers
    internal/auth/fakeissuer/fakeissuer.go  MODIFY optional org_id claim
    test/acceptance/broker_test.go       MODIFY  second deployment arm, both-kind tests
    docs/adr/0005-audience-side-verification.md CREATE

Five boundary tests run under both credential kinds: the happy path, its JSON
rendering, expired access, the split between a service failure and a broker
denial, and the sweep proving the module environment carries no ambient
credential. Four stay single-kind and say why.

## What outran the plan

Two claims the plan asserted were wrong, and both were caught by mutation
checks rather than by review. They are recorded here because in each case the
test that was supposed to guard the property could not fail.

**The signing-algorithm guard does not do what the plan said.** The plan
claimed `SupportedSigningAlgs` is what makes `alg: none` unreachable. go-oidc
filters `none` and the HMAC algorithms out of an issuer's advertised set before
a caller's configuration is read, so the HS256 subtest passed with the setting
deleted. What the setting does is narrow the library's ten-algorithm asymmetric
allowlist to one; an ES256 subtest now holds it, verified to return 200 with the
guard removed and 401 with it restored.

**The organization rule weakened the fixture path.** The plan had `authorize`
skip the organization check when a token names none, which is correct for the
issuer-minted format. But `devtoken.Mint` requires only an audience and an
invocation, so that allowance also admitted a fixture token naming no
organization to a service configured for any organization. The strictness now
lives in `devtokenVerifier`, leaving the allowance to the format that needs it.

A third correction is smaller but the same shape: the acceptance test pointing
a service at one issuer while the shell authenticates against another proves
the refusal but cannot isolate the `iss` comparison, because two fake issuers
hold different signing keys and the token dies at signature verification first.
The isolating test lives in `jwks_test.go`, and the acceptance test now says so.

## Verification

`make test vet lint acceptance` and `make smoke-build` all green. `make test`
runs with `-race`; lint reports 0 issues on both invocations. `smoke-build`
matters here because `statusservice.Options` changed shape and the smoke
package is invisible to the default gate.

The task-by-task breakdown this document originally carried is in the history
at `git show 1d39296:docs/plans/reference-oauth-boundary.md`.
