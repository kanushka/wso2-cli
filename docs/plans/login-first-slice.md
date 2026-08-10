# `wso2 login` First Slice

**Status:** Delivered. Tasks 1-12 merged into `feature/login` through
PRs #24-#29, #33, #34 and #35, with the definition of done met.

**Date:** 2026-08-07

**Outcome:** Browser and non-interactive login, and a token-source seam in the
broker

**Related:** [Issue #17](https://github.com/wso2/wso2-cli/issues/17) is the
spec, with [architecture](../architecture.md) and
[ADR 0004](../adr/0004-shell-brokered-authentication.md), which this work
established.

## Goal

Implement `wso2 login` (browser Authorization Code + PKCE) and inline
client-credentials acquisition, on a version-2 identities/contexts schema, with
OS-keychain session storage and a token-source seam in the broker, so the
reference module receives a real issuer-minted access token.

## Architecture

The shell gains schema v2 (`identities` plus contexts referencing them) with a
compatibility read for the v1 architecture-proof documents. A new
`internal/auth/session` package owns keychain persistence; a new
`internal/auth/oauthflow` package owns the browser PKCE flow; `internal/auth`
gains an unexported `source` seam resolved per identity kind (dev fixture,
oauth-browser scoped-refresh, client-credentials). Every failure is a typed
`auth_policy` problem with a stable code.

**Tech Stack:** Go 1.25, `golang.org/x/oauth2`, `github.com/coreos/go-oidc/v3`,
`github.com/zalando/go-keyring`, `github.com/go-jose/go-jose/v4` (test JWT
signing only).

## What shipped

    internal/contexts/identity.go        CREATE  Identity/IdentityAuth/Product + validation
    internal/contexts/legacy.go          CREATE  v1 compatibility read
    internal/contexts/contexts.go        MODIFY  v2 document, load/decode/encode, selection
    internal/auth/session/                CREATE  keychain blob, issuer check, rotation lock
    internal/auth/oauthflow/              CREATE  browser PKCE flow, browser opening
    internal/auth/fakeissuer/             CREATE  test OIDC issuer
    internal/auth/source*.go             CREATE  source seam and the three kinds
    internal/auth/discovery.go           CREATE  token-endpoint discovery
    internal/auth/claims.go              CREATE  unverified JWT claim extraction
    internal/app/login.go                CREATE  wso2 login command
    test/acceptance/login_test.go        CREATE  in-process login and broker chain
    test/smoke/                          CREATE  flag-gated live and empirical runs
    docs/guides/login.md                 CREATE  walkthrough

## Definition of done (from the spec, verbatim checks)

- [x] `wso2 login` completes PKCE against a real Asgardeo trial tenant and a local IS 7.x (`make smoke-login`, run twice with the two issuer configs). — Asgardeo tenant 2026-08-06; `wso2/wso2is:7.3.0` 2026-08-06. The nonce echo is checked by both, since `oauthflow/login.go` refuses on a mismatch and neither login could otherwise have completed.
- [x] The refresh token lands in the OS secure store (smoke run; deterministic equivalent in Task 7). — both smoke runs read it back out and compare its issuer.
- [x] The reference module receives a real short-lived access token through the broker against a backend proven to satisfy the scope/audience policy, and a test introspects that token (Task 11 deterministic; smoke against whichever real backend passes the empirical narrowing test).

  Deterministic: `TestLoginThenTheModuleReceivesIssuerMintedNarrowedAccess` launches the real module subprocess, has the issuer introspect what it presented, and proves refresh rotation across three runs. Live: `login_smoke_test.go` acquires twice, and the second acquisition asks for one permission out of the several the session holds. That second request is what makes the check capable of failing — asking for everything the session carries leaves the shell comparing the issued scopes against an identical request, which holds however the deployment behaved. Measured against Identity Server 7.3.0 on 2026-08-06: 1261 characters carrying both permissions, then 1230 carrying one. The module half needs nothing further, because a module never learns which issuer minted its token and running it live proves nothing the deterministic chain does not.
- [x] If Asgardeo fails the narrowing experiment: login and session persistence still pass; broker acquisition refuses `auth.narrowing_unavailable`; the research doc records the verdict (Task 12). — moot in the favorable direction: both deployments honor narrowing (`make empirical-asgardeo`, 2026-08-06). The refusal path itself is pinned by `TestADeploymentThatCannotNarrowIsRefusedRatherThanGrantedMore`.
- [x] CI path acquires a client-credentials token inline in an acceptance test (Task 11). — `TestAnInlineIdentityAuthenticatesACommandWithNoLoginStep`, plus the missing-secret, cannot-narrow, non-interactive and secret-disclosure cases beside it.
- [x] `docs/research/asgardeo-redirect-uri-and-scope-narrowing.md` empirical cells filled in (Task 12). — Asgardeo §3 filled 2026-08-06; Identity Server 7.3.0 recorded in §3.1.

**One finding outran this plan.** Asgardeo binds an access token's `aud` to the
client ID and exposes no way to change it, while Identity Server 7.3.0 adds the
API resource identifier once it is registered as an audience. So the spec's
"the issued token's audience covers the requested product audience" is
satisfiable on one supported product and structurally not on the other. Nothing
in the broker needs to change — it verifies rather than assumes — but what
`products.<namespace>.audience` can *mean* differs by product, and that is a
decision for issue #17 rather than a task here.

The task-by-task breakdown this document originally carried is in the history
at `git show e8aa8e4:docs/plans/login-first-slice.md`.
