# Handoff: one product, no Thunder

Status: open, raised 2026-09-17 while testing the setup wizard (#189). Nothing
is decided here. It builds on `docs/plans/context-v4-handoff.md` and ADR 0016.

## Problem

A user whose deployment is a single product, say the API Platform, wants to
use that product through the CLI. Today every interactive context signs in at
an identity provider, and the local setup assumes it is Thunder:

```
ws context create
  Sign in with: Thunder          → installs the identity module
  Add a product: api
```

That works only because the API Platform quickstart bundles Thunder on
`:8501`. The user ends up with a context whose `login.product` is `identity`,
a product they never asked for. The `identity` module is installed only
because its descriptor carries Thunder's defaults (the client, audience and
scopes).

A deployment without Thunder is not covered, and neither is one without any
identity provider.

## What the API Platform accepts

From `docs/research/wso2-authentication-landscape.md` §6, the control plane
(`platform-api`) has three authentication modes:

| Mode | What it is | CLI today |
|---|---|---|
| IdP | Validates JWTs from a configured issuer and JWKS, then enforces scopes and maps the IdP's organization claim | Works when the issuer is Thunder. With IS or Asgardeo it is untested (see below). |
| `file` | Local users and a login endpoint that issues locally signed JWTs. Documented as not for production. | Not supported |
| `internal_token` | Tokens signed inside the platform | Not user-facing |

The `ap` CLI performs no OAuth flow. It stores basic credentials, a bearer
token or an API key in plaintext YAML.

## Cases

### A. The product trusts Identity Server or Asgardeo

```
ws context create             # Sign in with: WSO2 Identity Server / Asgardeo
ws context product add api    # grant exchange, from the api descriptor
```

The context records no login product. `api` is reached by RFC 8693 token
exchange at the context's issuer, which is what the `api` descriptor declares
(`"grant": "exchange"`).

Unverified:

- Whether IS 7.x and Asgardeo accept the exchange request the shell sends,
  including the `resource` parameter and the audience binding. The shell's
  exchange has only been exercised against Thunder.
- Whether a deployment would rather have the login session cover `api`
  directly (a `direct` strategy, with the audience on the login token) than
  exchange it. That would need the descriptor or the context to say so.
- Which audience and scopes the platform expects from those issuers. The
  descriptor names none, so the user would need `--audience` and `--scopes`,
  and the wizard does not ask for them yet.

### B. The product is its own login (`file` mode)

There is no OAuth issuer. The product has a login endpoint that takes a
username and password and returns its own JWT. Supporting this needs:

- **A new login kind**, for example `product-password`, whose session is the
  product's token (with a refresh, if the product offers one), stored in the
  OS secure store like any session.
- **A descriptor member** naming the product's login endpoint and token
  shape, so the kind is declared by the module and never guessed.
- **A decision against ADR 0004** (shell-brokered authentication) and ADR
  0005 (audience-side verification). The shell cannot narrow or verify a
  token the product mints for itself the way it does for an IdP token.
- A password prompt. The wizard package can ask one (`huh` has a password
  input). Echo and storage rules need writing down first.

The platform documents `file` mode as not for production, so this may not be
worth building. It is listed so the decision gets made rather than skipped.

### C. Bring your own token

`login.kind` already names `pat`, which is not implemented. A user pastes a
token they obtained elsewhere, and the shell stores it and presents it
unchanged. This covers any mode above at the cost of refresh and
verification. It is also what `ap` does today.

### D. Thunder without the `identity` module

This is not a missing case but a cost of the current one. The Thunder
defaults could live somewhere other than the `identity` module (in the shell,
or in each product's descriptor as the provider it expects), so that an API
Platform user installs one product and not two. That changes what a
descriptor declares and what `login.product` means (ADR 0016), and was
deliberately not done in #189.

## Questions to answer first

1. Which deployments actually ship without Thunder: IS-backed, Asgardeo-backed,
   `file` mode, or all three?
2. Should A be verified against a real IS 7.x and Asgardeo tenant before
   anything else is built? (`make smoke-login` already targets both.)
3. Is B wanted at all, given `file` mode is not for production? If not, C
   may be the only non-IdP path worth having.
4. For D, should a product's descriptor name the provider it trusts, so the
   wizard can ask for the product first and derive the login from it?

## Where the work would be

- `internal/contexts`: a new kind for B or C, and validation for it
- `internal/modules/product.go`: descriptor members for B and D
- `internal/auth`: the session source for B or C
- `internal/app/context_wizard.go`: the questions for each case (audience and
  scopes for A, a password or a token for B and C)
- `modules/api/module.json`: whatever the descriptor gains
- ADRs 0004, 0005 and 0016, and `CONTEXT.md` (**Login product**,
  **Acquisition strategy**)
