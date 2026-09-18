# Troubleshoot a product module

A module can build and pass its tests and still be refused by the shell,
because the shell checks most things without running the module. Install it
locally before you tag a release, so those checks run while you can still fix
them:

```sh
export WSO2_HOME=$(mktemp -d)
make install-module NAMESPACE=<namespace>
```

The shell prints an error code in parentheses. Find it below. For field
details, see the [module manifest](../reference/module-manifest.md) and
[module SDK](../reference/module-sdk.md) references.

## Install and launch

| Error | Cause | Fix |
| --- | --- | --- |
| `modules.incompatible_shell` | The shell's version is outside `compatibility.shell`. A plain `go build` of the shell reports `0.0.0-dev`, which `>=0.1.0` excludes. Install succeeds; the first command fails. | Use the shell `make build-shell` or `make install-module` builds. With a released shell, use a version inside the declared range. |
| `modules.incompatible_protocol` | The module and shell share no protocol version. At install time, no published version speaks this shell's protocol. | Rebuild against an SDK whose protocol the shell speaks. Don't edit `protocolVersions` by hand. Run `make gate-module` before tagging. |
| `modules.incompatible_platform` | The installed binary was built for another OS or architecture. | Install on the machine that runs it. |
| `modules.executable_digest_mismatch` | The binary changed after install, usually because a build was copied over it. | `wso2 product remove <namespace>`, then install again. |
| `modules.receipt_malformed` naming a product descriptor | `capabilities.product` holds a value the shell doesn't read. | Fix it using the [manifest reference](../reference/module-manifest.md#capabilitiesproduct), then release and reinstall. |
| `shell.module_not_installed` | Nothing with that namespace is installed. | Check the name with `wso2 product list`. |
| `catalog.unknown_module` | The catalog has no such module. Usually it isn't released yet, or only as a prerelease. | `wso2 product install <namespace> --channel prerelease`, or install locally with `make install-module`. |
| Release refused before publishing | The release gate found no released shell that speaks the module's protocol, or `compatibility.shell` doesn't parse. | Wait for a shell release, or rebuild against an SDK the released shell speaks. Nothing was published. |

## Running commands

| Error | Cause | Fix |
| --- | --- | --- |
| Garbled output or a protocol failure | Something wrote to stdout, which carries the protocol. | Write diagnostics to stderr. Return user output as result fields. |
| `<namespace>.handler_failed` | A handler returned an error that isn't a `problem.Problem`. | Return a typed problem so you choose the category, code, and recovery. |
| `<namespace>.handler_panicked` | A handler panicked. | Fix the panic. |
| `<namespace>.invalid_result` | The result has no schema, no fields, an unnamed field, or a duplicate field name. | Fix the result. |
| Commands never reach the module | The namespace is a shell command, and the shell handles it first. | Choose another namespace. `make new-module` refuses these. |
| `shell.command_moved` | The user typed a removed command, such as `wso2 <namespace> connect`. | Use the replacement the message prints. |

## Access

| Error | Cause | Fix |
| --- | --- | --- |
| `auth.audience_not_declared` / `auth.scope_not_declared` | A handler asked for an audience or scope not in the installed `module.json`. An empty `AccessRequest.Audience` counts too. `testkit` doesn't check this. | Declare it in both `module.json` `capabilities` and `module.Options`, then release and reinstall. A request that names no scopes gets the context's scopes and never fails this way. |
| `auth.context_not_selected` | No context is selected. This is setup, not a module bug. | `wso2 context use <name>`, or create one (see [Set up with Asgardeo](setup-asgardeo.md)). |
| `auth.product_not_configured` | The context doesn't record the product, or lacks its audience or a scope. | `wso2 context product add <namespace> --url <url>`. For a module with no product descriptor, also pass `--audience` and `--scopes`. |

## A module with no product descriptor

`wso2 context product add` and `wso2 context apply` fill in a product record
from `capabilities.product`. Without a descriptor, the record holds only what
the user passes, so the module's `status` command should tell users which
audience and scopes to pass. Adding a descriptor takes effect only after the
module is released and reinstalled and the context is written again.
`wso2 doctor` lists records that differ from what the installed product would
write now.
