# ADR 0004: Shell-Brokered Authentication

**Status:** Accepted

The shell alone authenticates against identity backends and holds credential
material. The CLI is a public OAuth client: interactive identities log in
through the browser with Authorization Code + PKCE, and the resulting session
lives only in the OS secure store — a machine without a keyring gets a typed
refusal, never a plaintext fallback. Product modules never see credentials or
long-lived tokens; they ask the shell's broker for an audience and scopes
through the module contract and receive short-lived, issuer-minted access
material. The broker resolves how access is obtained per identity kind behind
an internal source seam (scoped refresh for browser sessions, inline client
credentials for automation, the development fixture for the architecture
proof), and it verifies what the deployment actually issued: the effective
scopes must equal the request and the audience must cover the product's, so a
backend that cannot narrow produces a typed refusal rather than silently
granting broader access. Every authentication failure carries a stable problem
code and the auth-policy exit class, and token material appears on no output
surface.

## Considered Options

- Per-module authentication would let each product ship its own flow, but the
  surveyed WSO2 CLIs show where that leads: five of them persist secrets in
  plaintext, and policy could never evolve without releasing every module.
- A plaintext session fallback for keyring-less machines would keep login
  working everywhere at the cost of silently downgrading the credential
  guarantee the user was promised.
- Trusting the token request instead of verifying the issued token would be
  simpler, but a deployment that ignores scope narrowing would then hand
  modules broader access than they asked for, unnoticed.
