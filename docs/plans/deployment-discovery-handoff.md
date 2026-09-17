# Handoff: deployment discovery

Status: deferred on 2026-09-14. This work depends on the product teams. It
builds on `docs/plans/context-v4-handoff.md`, which has to ship first.

## Goal

The user points at the login product and the shell learns everything else:

```
wso2 context create local --url http://localhost:8501
wso2 login
```

or just `wso2 login --url http://localhost:8501`. On-premises users skip
writing an input file and running `product add`. Context-v4 refuses a bare
`--url` because a URL alone doesn't identify a descriptor. Discovery is what
makes a bare `--url` legal. Cloud login (#186) can use the same
mechanism with a WSO2-hosted document, so both paths share one design.

## Design constraint

Discovery **produces a context-v4 input file** and sends it through
`apply`'s normal steps: validate, plan, `--dry-run`, install, resolve and
freeze defaults, write. It is not a second way to write `contexts.json`, so:

- nothing from the context-v4 work gets thrown away, and defaults are
  frozen locally exactly as they are for a hand-written input file
- `wso2 context show` and `export` still print a file a person can read
- a deployment without discovery refuses a bare `--url` and names the
  explicit `--login-product` or `--issuer` form, as context-v4 does

## Sketch

```
GET <login-product-url>/.well-known/wso2-deployment
{
  "login": { "product": "identity", "clientId": "wso2-cli", "provider": "thunder" },
  "products": {
    "identity": { "url": "http://localhost:8501" },
    "api":      { "url": "http://localhost:9251", "gateway": { "url": "http://localhost:9091" } }
  }
}
```

The body is one context in input form, without `name`. The user supplies the
name. The path, the name, and the versioning are all open. See the questions
below.

## Why this needs other teams

The shell can't discover a product the deployment doesn't announce. One of
these has to be true:

| Option | Who does the work | Notes |
|---|---|---|
| **A. Login product serves it** | Thunder / IS team | Thunder already knows the API platform as a resource server or app. The doc is generated from config Thunder already holds. One team, one endpoint. |
| **B. Each product serves its own** | Every product team | The user still needs every URL, which defeats the purpose. Rejected. |
| **C. A context file at any HTTPS URL** | Nobody | This is just `wso2 context apply -f https://…`. It needs no product-team work and could ship inside the context-v4 work as a cheap step before A. |

Recommendation: ship C with context-v4 and propose A to the Thunder/IS team.
Cloud (#186) uses A's format, hosted by WSO2.

## Security, the part to get right

ADR 0012 says writing a context grants nothing, but a context does decide
**where the shell sends tokens**. If a discovery document is spoofed or
wrong, it can point `api` at a host the attacker controls, and the shell
then presents tokens there. ADR 0005 (audience-side verification) limits
what a stolen token can do, but it doesn't stop the shell sending one.
Minimum bar:

- Before writing anything, show every discovered URL (this is `apply`'s
  plan) and require confirmation. With `--no-input`, refuse unless
  `--accept-discovered` is given.
- A discovered document must not carry `credentialRef` or `defaultContext`.
  This follows the input-file rule in context-v4.
- By default, refuse products on a different host from the login product
  unless the user confirms. Localhost port differences count as the same
  host.
- Never trust discovery over HTTP, except for loopback.
- Discovery runs only at create or apply time, never at login or command
  time. Re-running it is an explicit command (`wso2 context refresh`?), so a
  file never changes without the user seeing it.

## Open questions

1. Where the path lives and what it's called (`/.well-known/wso2-deployment`?),
   and whether to register it or keep it vendor-private.
2. How the document is versioned, and what the shell does with an unknown
   product namespace in it: ignore it, or offer to install it?
3. Whether discovered products get auto-installed the same way `product add`
   does, and whether that needs its own confirmation.
4. Should a cloud organization change (`wso2 org use`) re-run discovery,
   since products may differ per organization?
5. Who in the Thunder/IS team owns option A, and when.

## First steps

1. Draft a one-page format proposal for the Thunder/IS team, built from the
   sketch above and the security section.
2. Ship option C with context-v4 so on-premises teams get most of the
   benefit now.
3. Get cloud (#186) to agree on the same document format.
