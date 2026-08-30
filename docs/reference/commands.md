# WSO2 CLI shell commands

**Status:** Proposed reference
**Related:** [Product requirements](../product-requirements.md),
[architecture](../architecture.md)

This reference describes the proposed shared `wso2` shell commands. It does not
represent an available production interface. Product operations belong to
product modules, such as `wso2 api`, `wso2 identity`, `wso2 integration`, and
`wso2 agent`.

The distinction between catalog refresh and module binary update remains an
open decision. The lifecycle command names below are therefore provisional.

These are built: `wso2 context create <name>`, `wso2 context use <context>`,
`wso2 context list`, `wso2 context current`, `wso2 login`,
`wso2 login --url <issuer> --client-id <id>`, `wso2 logout`,
`wso2 module available`, `wso2 module list`,
`wso2 module install <module>`, `wso2 module install <module>@<version>`,
`wso2 module install <module> --channel <channel>`,
`wso2 module update <module>`, `wso2 module update --all`, and
`wso2 module remove <module>`. The
[module catalog](module-catalog.md) reference describes what they select, what
they verify, how a channel and a pin are recorded per module, and how each
refusal is reported.

## Commands

| Command | Description |
| --- | --- |
| `wso2 help` | Shows the root command tree and help for a command. |
| `wso2 version` | Shows the shell, protocol, and installed module versions. |
| `wso2 login` | Authenticates the selected context using its configured method. |
| `wso2 login --url <issuer> --client-id <id>` | Logs in against a named issuer and creates the identity and the context it authenticated, reporting both names. `--context <name>` names them; without it the identity name is derived from the issuer host, and an issuer whose host cannot make a legal name is refused with `contexts.identity_name_underivable`. An identity of that name whose issuer and client ID both match is reused; one that differs in either is refused with `contexts.identity_exists` and never replaced. The first context created becomes the selected one. Nothing is written unless the login succeeded. Omitting `--client-id` prompts in an interactive terminal and is refused with `shell.missing_required_flag` under `--no-input`. |
| `wso2 logout` | Ends the session of the identity the selected context names: asks the identity provider to revoke its refresh token, and removes the shell-owned session that every context sharing that credential reference reaches. |
| `wso2 whoami` | Shows the signed-in user and active organization. |
| `wso2 org list` | Lists organizations available to the signed-in user. |
| `wso2 org use <organization>` | Selects an organization and activates its organization-bound session. |
| `wso2 org current` | Shows the active organization. |
| `wso2 context create <name>` | Creates a context naming an identity with `--identity`, and optionally an organization and project with `--organization` and `--project`. It writes no credential and makes no network call, so an unreachable issuer or a misspelled organization is reported by the command that needs it rather than here. Creating a context whose name is taken is refused. The first context created becomes the selected one. |
| `wso2 context list` | Lists saved cloud and on-premises contexts, marking the selected one. |
| `wso2 context use <context>` | Selects the context used by default for later commands. |
| `wso2 context current` | Shows the active context. |
| `wso2 config list` | Shows non-secret shell preferences. |
| `wso2 config get <key>` | Shows one non-secret shell preference. |
| `wso2 config set <key> <value>` | Changes one non-secret shell preference. |
| `wso2 update` | Applies the approved installation-channel policy for root shell updates. |
| `wso2 module available` | Lists official modules the module catalog publishes. |
| `wso2 module install <module>` | Installs the latest compatible stable release of a module. |
| `wso2 module install <module>@<version>` | Installs an exact compatible module version. |
| `wso2 module list` | Lists installed modules, versions, and update availability. |
| `wso2 module info <module>` | Shows catalog, compatibility, and installation information. |
| `wso2 module update <module>` | Updates one product module. |
| `wso2 module update --all` | Updates all installed product modules. |
| `wso2 module verify <module>` | Verifies an installed module and its receipt. |
| `wso2 module rollback <module>` | Reactivates a retained compatible version. |
| `wso2 module remove <module>` | Removes one installed module, leaving configuration and credentials alone. |
| `wso2 module install --file <module.wso2module>` | Installs one module from an offline file. |
| `wso2 bundle create` | Creates a platform-specific, self-installing offline bundle from catalog releases. |
| `wso2 bundle inspect <file>` | Shows bundle contents without installing it. |
| `wso2 bundle install <file>` | Imports a bundle when the WSO2 CLI is already installed. |
| `wso2 doctor` | Checks shell, context, secure-store, catalog, and module health. |

`project` commands are intentionally not included yet. Product-specific
projects, deployment, and runtime operations remain within their product
modules.

On a fresh air-gapped machine, the user runs the transferred platform-specific
offline bundle directly; no `wso2` command exists yet. `wso2 bundle install`
is only for a machine where the shell is already installed. The administrator
must establish trust in the bootstrap before execution through platform signing
on Windows or macOS, or detached-signature verification on Linux.

## Exit classes

The shell alone decides the process exit status: a module returns a typed
problem and the shell maps its category to one of these classes. The mapping is
a stable contract for automation, so a script may branch on the status without
parsing any output.

| Status | Class | What produces it |
| --- | --- | --- |
| `0` | Success | The command completed. |
| `64` | Usage | Invalid arguments, flags, or configuration, including a malformed context document. |
| `69` | Module trust | A module integrity, signature, platform, or compatibility failure. |
| `70` | Module process | A protocol violation, or a module process that failed to launch or crashed. |
| `75` | Product service | A failure the product service itself reported. |
| `77` | Authentication policy | An authentication or broker policy failure, including no context selected, and a missing or expired session. |

An unrecognized problem category is reported as a module process failure, `70`,
rather than as success.

## Non-interactive use

`--no-input` declares that nothing may prompt, open a browser, or wait for a
human. `WSO2_NO_INPUT`, set to any non-empty value, does the same for every
invocation in the environment. A job that sets either wants to fail fast on a
misconfigured identity rather than hang until its own timeout.

A browser or device login under `--no-input` is refused with
`auth.non_interactive` and exit class `77`. Automation authenticates with a
client-credentials identity instead, which acquires access inline with no login
step.

## Sample output

### Version

```text
$ wso2 version
WSO2 CLI          v0.1.0
Protocol          v2, v1
Platform          darwin/arm64

Installed modules
NAME          VERSION
api           v0.9.0
agent         v1.2.0
integration   v0.4.0
```

### Active identity

```text
$ wso2 whoami
User           jane@example.com
Organization   acme
Context        cloud-us
```

### First login against a self-hosted issuer

```text
$ wso2 login --url https://idp.customer.example --client-id wso2-cli \
    --context customer

Logged in to the "customer" context.
Subject    ops
Email      ops@customer.example
Products   none configured

Created identity "customer" and context "customer".
It is the first context, so it is now the selected one.

No products are configured for this identity. A self-hosted deployment is not
discoverable, so each product's endpoint has to be recorded:

  wso2 identity add-product customer <namespace> \
      --endpoint <url> --audience <resource-id> --scopes <list>
```

The authorization URL is written to the diagnostic stream, not to this one: it
is an instruction to act on rather than the command's result, so a caller
redirecting standard output still sees it.

### Ending a session

```text
$ wso2 logout

Ended the session for the "cloud-us" context.
Context      cloud-us
Identity     acme-cloud
Session      ended
Revocation   confirmed
Shared with  cloud-us

The identity provider accepted the request to revoke this session's refresh token.

A browser single-sign-on session at the identity provider is unaffected by this
command, so a later login may not prompt for credentials.
```

`Revocation` is `confirmed` when the identity provider accepted the request,
`not-attempted` when it publishes no revocation endpoint and so was never asked,
and `failed` when it was asked and did not accept, or could not be reached. The
shell-owned session is removed under all three, and the command succeeds under
all three; what changes is only what it claims. `--output json` renders the same
fields as a JSON object, which is the only way a script can read which of the
three happened. See
[ADR 0010](../adr/0010-best-effort-revocation-on-session-end.md).

### Context

```text
$ wso2 context create cloud-us --identity acme-cloud --organization acme

Created the "cloud-us" context.
Context        cloud-us
Identity       acme-cloud
Organization   acme
Project
Selected       yes

It is the first context, so it is now the selected one. Run wso2 context use
<name> to select another.

$ wso2 context list
CURRENT   CONTEXT    IDENTITY     ORGANIZATION   PROJECT
*         cloud-us   acme-cloud   acme

$ wso2 context current
Context        cloud-us
Identity       acme-cloud
Organization   acme
Project
```

An identity is created by `wso2 login`, not by a command of its own, so
`--identity` names one that already exists and a name that does not is refused
with `contexts.unknown_identity`.

### Module inventory

```text
$ wso2 module list
MODULE        INSTALLED   CHANNEL   UPDATE
api           v0.9.0      stable    current
agent         v1.2.0      stable    v1.3.0 available
integration   v0.4.0      stable    pinned to v0.4.0

1 module(s) have an update available. Run wso2 module update --all to take them.
```

Credentials are never shown by these commands. Tokens are stored separately in
the operating system's secure credential store.
