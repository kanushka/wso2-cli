# The iam product module

This is a WSO2 CLI product module. It is a separate program: the `wso2` shell
resolves it from the managed module store and launches it, and the two speak the
module contract over the module's standard input and output.

It lives in this repository and is released by its own tag, independently of the
shell and of every other module:

```sh
git tag iam/v0.1.0-rc.1
git push origin iam/v0.1.0-rc.1
```

That tag builds the module for every supported platform, publishes the archives,
and regenerates the module catalog a user installs from. A release is refused if
no shell that exists can launch it.

## Working on it

```sh
# From the repository root.
make build-module NAMESPACE=iam
make test-module NAMESPACE=iam
make install-module NAMESPACE=iam   # then ./bin/ws iam status
```

The workspace composes this module with the SDK from source, so a change to the
SDK reaches it without a release. The `go.mod` here requires the published SDK
at the version every other module in this repository requires, and carries no
`replace` directive.

## Commands

| Command | Does |
| --- | --- |
| `wso2 iam status` | Reports this module's own status and what to run first. |
| `wso2 iam user list` | Lists the users the deployment records. |
| `wso2 iam app list` | Lists the applications the deployment records. |
| `wso2 iam resource-server list` | Lists the resource servers the deployment records. |
| `wso2 iam resource-server create <name> --identifier <uri> [--description <text>] [--permission <handle>]... [--ou <id-or-handle>]` | Creates a resource server a product's access is bound to. `--identifier` is the absolute URI its tokens are bound to. `--ou` is required when the deployment records more than one organization unit. |
| `wso2 iam resource-server delete <id-or-identifier> --yes` | Deletes a resource server and its permissions. |

The module manages ThunderID, not Identity Server or Asgardeo. Create a context
that logs in through it with
`wso2 context create <name> --login-product iam --url <url> --use`.

## What to change first

`cmd/wso2-module-iam/main.go` declares the command tree above in `commands()`
and binds each command to its handler. To call your product, a handler
needs access, and access is something the shell brokers rather than something a
module holds: declare an audience and a scope in `module.json` and in
`moduleOptions`, then ask for them with `request.Access.Acquire`. A module never
sees a credential and cannot obtain a second token.

`modules/example` is the worked example: a client on the product's REST
API, two read-only commands, and tests that drive the module through the
contract against a fake deployment. The guide at
`docs/guides/build-module-quickstart.md` walks through it.

## What this module must not do

It must not import anything under the shell's `internal` tree, and it must not
print to standard output: that stream carries protocol frames, and anything
written there directly corrupts them. Diagnostics go to standard error. Both
rules are asserted for every module under `modules/`, including this one, by the
tests in `internal/boundaries`.
