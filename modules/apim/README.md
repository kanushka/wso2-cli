# The apim product module (API Manager)

This is the WSO2 CLI product module for API Manager. It is a separate
program: the `wso2` shell resolves it from the managed module store and
launches it, and the two speak the module contract over the module's
standard input and output.

The product is recorded with the shell's `wso2 apim connect <base>
--client-id <id>`, on an identity that logged in at another provider; the
product's audience is the client id, which is what the key manager stamps
into its JWTs. Which client depends on the identity. A browser identity
needs a **public** client on API Manager federated to its login provider,
registered by hand as
[the one-login guide](../../docs/guides/one-login-thunder-apim.md) section 3
describes. `wso2 apim bootstrap` is the pipeline's route: it registers a
**confidential** client on the resident key manager through dynamic client
registration, once, with an administrator password the shell hands it
through `WSO2_APIM_ADMIN_PASSWORD`, and prints the `connect` line that
records it, with `--client-secret-variable`, on a client-credentials
identity. Every other command runs under the identity's `apim` session,
authorized for the scopes the product record holds; a command names none of
its own.

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
| `wso2 apim bootstrap --url <base>` | none | Registers the pipeline's confidential client (JWT token type), shows the secret once, and prints the `wso2 apim connect` line to run next on a client-credentials identity. That line names the variable to export, never the secret, and says that a browser identity needs the public federated client instead. |
| `wso2 apim apis list \| import --file <openapi> --name --version --api-context --backend \| deploy <name/version> \| publish <name/version>` | `apim:api_view`, `apim:api_create`, `apim:api_publish` | Create from OpenAPI, deploy a revision and wait until the gateway has it, publish. |
| `wso2 apim apps list \| create <name> \| subscribe <app> <name/version> \| keys <app> \| map-keys <app> --key-manager <km> --client-id <id>` | `apim:subscribe`, `apim:app_manage` | Applications, subscriptions, keys on the resident key manager (verified with one token request), and out-of-band keys from another issuer. `map-keys` is idempotent for the application that holds the mapping; a client already mapped onto another application is refused with `apim.client_mapped_elsewhere`, since the gateway checks the subscription on the application that holds the mapping. |
| `wso2 apim key-managers list \| add <name> --well-known <issuer> [--jwks <url>]` | `apim:admin` | Register an external issuer as a custom key manager whose JWTs the gateway validates. |
| `wso2 apim gateway invoke </context/version/path>` | the identity's own | Call an API through the gateway with a token brokered for the selected identity's `apim` product. Under a ThunderID identity whose `apim` product names the API's resource server and the gateway, that is the user's API called with a ThunderID token. A 2xx answer is the result; any other status is an `apim.refused` problem carrying the status and body, so the exit code tells success from refusal. An identity whose `apim` endpoint is the management origin (the one `wso2 apim connect` records) is refused with `apim.not_gateway` before anything else. |

Every `create`, `import`, `publish`, `subscribe` and `add` is idempotent.
Every result ends with the command to run next. Secrets reach the module
only through `WSO2_APIM_*` variables.

It must not import anything under the shell's `internal` tree, and it must
not print to standard output. Both are asserted by `internal/boundaries`.

A call that fails because the deployment's TLS certificate is not trusted by
this machine is refused with `apim.certificate_untrusted`, naming the host and
the two commands that export the certificate and point `WSO2_CA_FILE` at it,
the same way the shell refuses an untrusted identity provider.
