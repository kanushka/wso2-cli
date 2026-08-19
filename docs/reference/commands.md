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

`wso2 module install <module>` and `wso2 module install <module>@<version>`
are built. The [module catalog](module-catalog.md) reference describes what
they select, what they verify, and how each refusal is reported.

## Commands

| Command | Description |
| --- | --- |
| `wso2 help` | Shows the root command tree and help for a command. |
| `wso2 version` | Shows the shell, protocol, and installed module versions. |
| `wso2 login` | Authenticates the selected context using its configured method. |
| `wso2 logout` | Removes the active session from the shell-owned credential store. |
| `wso2 whoami` | Shows the signed-in user and active organization. |
| `wso2 org list` | Lists organizations available to the signed-in user. |
| `wso2 org use <organization>` | Selects an organization and activates its organization-bound session. |
| `wso2 org current` | Shows the active organization. |
| `wso2 context list` | Lists saved cloud and on-premises contexts. |
| `wso2 context use <context>` | Selects the context used by default for later commands. |
| `wso2 context current` | Shows the active context. |
| `wso2 config list` | Shows non-secret shell preferences. |
| `wso2 config get <key>` | Shows one non-secret shell preference. |
| `wso2 config set <key> <value>` | Changes one non-secret shell preference. |
| `wso2 update` | Applies the approved installation-channel policy for root shell updates. |
| `wso2 module available` | Lists official modules available through verified catalog metadata. |
| `wso2 module install <module>` | Installs the latest compatible stable release of a module. |
| `wso2 module install <module>@<version>` | Installs an exact compatible module version. |
| `wso2 module list` | Lists installed modules, versions, update availability, and verification state. |
| `wso2 module info <module>` | Shows catalog, compatibility, installation, and verification information. |
| `wso2 module update <module>` | Updates one product module. |
| `wso2 module update --all` | Updates all installed product modules. |
| `wso2 module verify <module>` | Verifies an installed module and its receipt. |
| `wso2 module rollback <module>` | Reactivates a retained compatible and non-revoked version. |
| `wso2 module remove <module>` | Removes a module according to the retention policy. |
| `wso2 module install --file <module.wso2module>` | Installs one verified module from an offline file. |
| `wso2 bundle create` | Creates a platform-specific, self-installing offline bundle from verified releases. |
| `wso2 bundle inspect <file>` | Shows bundle contents and verification state without installing it. |
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

### Context

```text
$ wso2 context current
NAME       TYPE    ORGANIZATION   REGION
cloud-us   cloud   acme           us
```

### Module inventory

```text
$ wso2 module list
NAME          INSTALLED   AVAILABLE   COMPATIBLE   VERIFICATION
api           v0.9.0      v0.9.0      yes          verified
agent         v1.2.0      v1.3.0      yes          verified
integration   v0.4.0      v0.4.0      yes          verified
```

Credentials are never shown by these commands. Tokens are stored separately in
the operating system's secure credential store.
