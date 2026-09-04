# The iam product module (ThunderID)

This is the WSO2 CLI product module for ThunderID. It is a separate program:
the `wso2` shell resolves it from the managed module store and launches it,
and the two speak the module contract over the module's standard input and
output.

It does two jobs. `wso2 iam bootstrap` registers this CLI as a public OAuth
client in a ThunderID deployment, once, using an administrator password the
shell hands it through `WSO2_IAM_ADMIN_PASSWORD`, and prints the
`wso2 identity create` line to run next. Every other command runs under an
identity that logged in with that client and reaches ThunderID's management
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
