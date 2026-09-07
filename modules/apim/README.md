# The apim product module (API Manager)

This is the WSO2 CLI product module for API Manager. It is a separate
program: the `wso2` shell resolves it from the managed module store and
launches it, and the two speak the module contract over the module's
standard input and output.

`wso2 apim bootstrap` registers this CLI on API Manager's resident key
manager through dynamic client registration, once, with an administrator
password the shell hands it through `WSO2_APIM_ADMIN_PASSWORD`. The key
manager has no public client, so the identity it prints is a
client-credentials one, and the product's audience is the client id: that
is what the key manager stamps into its JWTs. Every other command runs under
that identity and asks the shell for exactly the scopes it needs.

```sh
# From the repository root.
go build ./modules/apim/...
go test ./modules/apim/...
make install-module NAMESPACE=apim
```

## Commands

| Command | Scopes | What it does |
| --- | --- | --- |
| `wso2 apim status` | none | Version, endpoint, what to run first. |
| `wso2 apim bootstrap --url <base>` | none | Registers the client (JWT token type), shows the secret once, and prints the `wso2 apim connect` line to run next. That line names the variable to export, never the secret. |
| `wso2 apim apis list \| import --file <openapi> --name --version --api-context --backend \| deploy <name/version> \| publish <name/version>` | `apim:api_view`, `apim:api_create`, `apim:api_publish` | Create from OpenAPI, deploy a revision and wait until the gateway has it, publish. |
| `wso2 apim apps list \| create <name> \| subscribe <app> <name/version> \| keys <app> \| map-keys <app> --key-manager <km> --client-id <id>` | `apim:subscribe`, `apim:app_manage` | Applications, subscriptions, keys on the resident key manager (verified with one token request), and out-of-band keys from another issuer. |
| `wso2 apim key-managers list \| add <name> --well-known <issuer> [--jwks <url>]` | `apim:admin` | Register an external issuer as a custom key manager whose JWTs the gateway validates. |
| `wso2 apim gateway invoke </context/version/path>` | the identity's own | Call an API through the gateway with a token brokered for the selected identity's `apim` product. Under a ThunderID identity whose `apim` product names the API's resource server and the gateway, that is the user's API called with a ThunderID token. |

Every `create`, `import`, `publish`, `subscribe` and `add` is idempotent.
Every result ends with the command to run next. Secrets reach the module
only through `WSO2_APIM_*` variables.

It must not import anything under the shell's `internal` tree, and it must
not print to standard output. Both are asserted by `internal/boundaries`.
