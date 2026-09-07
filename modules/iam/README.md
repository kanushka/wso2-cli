# The iam product module (ThunderID)

This is the WSO2 CLI product module for ThunderID. It is a separate program:
the `wso2` shell resolves it from the managed module store and launches it,
and the two speak the module contract over the module's standard input and
output.

It does two jobs. `wso2 iam bootstrap` registers this CLI as a public OAuth
client in a ThunderID deployment, once, using an administrator password the
shell hands it through `WSO2_IAM_ADMIN_PASSWORD`, and prints the
`wso2 iam connect` line to run next; `connect` is the shell's, and writes the
identity, the product and the context from the URL and this module's product
descriptor. Every other command runs under an identity that logged in with
that client and reaches ThunderID's management
APIs through access the shell brokers for the `system` scope: resource
servers and their permission trees, users, applications, and roles.

```sh
# From the repository root.
go build ./modules/iam/...
go test ./modules/iam/...
make install-module NAMESPACE=iam
```

It must not import anything under the shell's `internal` tree, and it must
not print to standard output. Both are asserted by `internal/boundaries`.

## Commands

| Command | What it does |
| --- | --- |
| `wso2 iam status` | This module's version and endpoint, and what to run first. |
| `wso2 iam bootstrap --url <issuer>` | Logs in as the administrator (password in `WSO2_IAM_ADMIN_PASSWORD`) through the seeded console client, registers the public client `wso2-cli` with the shell's loopback callbacks if absent, on the console's authentication flow so that logins for different resource servers share one browser session, and prints the `wso2 iam connect` line. |
| `wso2 iam resource-servers list \| create <name> --identifier <uri> --permission a:b:c...` | An API ThunderID issues tokens for, with its permission tree; parents are reused. |
| `wso2 iam users list \| create <username> --email <address> [--password-variable WSO2_IAM_USER_PASSWORD]` | A person; the password is read from the environment, never a flag. |
| `wso2 iam apps list \| create <client-id> --type m2m\|public` | An OAuth client. An m2m client's generated secret is shown once. |
| `wso2 iam roles list \| create <name> --resource-server <name> --permission ... [--assign-user u] [--assign-app c]` | A role granting permissions to users and apps, resolved by name. |
| `wso2 iam roles assign <name> [--user <username>]... [--app <client-id>]...` | Adds users and apps to a role that exists; what is already assigned is reported, not refused. A session established before the role was granted must log in again to carry it. |

Every `create` is idempotent: run again, it reports `created false` and
changes nothing. Every result ends with the command to run next. Secrets
reach the module only through `WSO2_IAM_*` variables; see the reference on
what a module process can see.

A call that fails because the deployment's TLS certificate is not trusted by
this machine is refused with `iam.certificate_untrusted`, naming the host and
the two commands that export the certificate and point `WSO2_CA_FILE` at it,
the same way the shell refuses an untrusted identity provider.
