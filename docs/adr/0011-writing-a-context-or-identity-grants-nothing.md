# ADR 0011: Writing a Context or an Identity Grants Nothing

**Status:** Accepted

A context or an identity is a target-and-metadata artifact: organization,
project, issuer, client identifier, and an opaque reference into the shell's
own credential store. It carries no credential material and no capability, so
creating, importing, or exporting one grants the holder nothing beyond what
they already held. This is the invariant that replaces the architecture
proof's read-only guarantee that no shell command can write a context, so no
shell command can grant itself access: once `wso2 context create` exists,
something can write a context, and the invariant that survives is a property
of what gets written, not of the fact that nothing is written yet.

The property is checkable because the schema enforces it directly: no context
or identity field can hold a token, secret, or password, and `credentialRef`
is an opaque key — a bare word matching `^[a-z][a-z0-9-]{0,63}$`, not a URI or
a connection string — that names an entry in the shell's own OS secure store
or CI variable set. A value the shell cannot resolve to a session fails as an
authentication problem, never as a malformed document, and reading the field
back never yields the credential itself. No writing command performs a network
call: `wso2 context create` and `wso2 identity add-product` validate shape
only, so a mistyped issuer or client ID is accepted at write time and surfaces
at `wso2 login`, when the shell first tries to use what was written, not
before.

## Considered Options

- **A writer-based rule** — no command other than `wso2 login` may write
  authentication configuration — states the same intuition but does not
  survive `wso2 login` itself, which both writes an identity and authenticates
  it. A rule keyed to the writer needs an explicit exception for every command
  that does two things, and the exception list only grows as
  `wso2 identity add-product` and later commands add their own writes.
  Rejected.
- **Validating target fields against the deployment at write time**, such as
  resolving `--project` or checking the issuer reachable, would catch some
  typos earlier, but it requires a network call before a session exists to
  make one with, and it makes `wso2 context create` slower and less usable
  than the hand-authored document it replaces. Rejected.
- **A capability-shaped `credentialRef`**, such as a `keychain://` URI naming a
  store and a location, would make the reference self-describing, but it
  invites parsing and construction outside the shell, which is exactly the
  surface an opaque key exists to close. Store selection stays shell
  configuration, never a per-identity field. Rejected.

## Consequences

**Every context-writing command is checked against one property, not one per
command.** A reviewer of `wso2 context create`, `wso2 identity add-product`,
or any later writer asks only whether the artifact it produces can hold a
credential or a capability; whether the command also authenticates, imports,
or renames is a separate question with its own review, not an exception to
this one.

**An issuer or client ID typo is a `wso2 login` failure, not a
`wso2 context create` failure.** A user who mistypes `--url` sees a working
write followed by an authentication refusal, not a refusal at write time; the
document itself is never the thing that is wrong.

**The documented `credentialRef` examples must match the schema.** They once
showed a reference like `keychain://wso2/acme-cloud`, not the bare opaque word
the validator accepts; [#115](https://github.com/wso2/wso2-cli/issues/115)
corrected the published examples to bare words and added a test that decodes
them, so the two cannot drift apart again.
