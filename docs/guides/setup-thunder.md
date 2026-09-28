# Set up the WSO2 CLI with ThunderID

This guide registers the CLI in ThunderID `1.0.0-beta`, creates a context, and
logs in. The examples use `https://localhost:8090` and the `example` product.
Context fields are described in the
[context file reference](../reference/context-file.md).

Install the example module from this checkout with the
[local setup guide](setup-example-module.md). From the repository root, run
`export PATH="$PWD/bin:$PATH"` in the same terminal so the `ws` commands below
use that build. Keep the `WSO2_HOME` value from the local setup guide.

ThunderID differs from Asgardeo and Identity Server in three ways:

- The issuer is the bare origin, `https://localhost:8090`.
- A product's audience is a resource server identifier, which must be an
  absolute URI.
- The context must set `"provider": "thunder"`. There is no device-code login.

## 1. Run a server

```sh
docker run -d --name thunderid -p 8090:8090 \
  ghcr.io/thunder-id/thunderid:1.0.0-beta \
  bash -c './setup.sh --admin-username admin --admin-password "Admin@123" && ./start.sh'
```

Keep the host port at 8090: the issuer the server advertises must match the
URL you reach it on.

This walkthrough targets `1.0.0-beta`. For a newer release, use the
[current ThunderID quick start](https://thunderid.dev/docs/next/getting-started/get-thunderid/)
and check its Console steps before continuing.

## 2. Trust the server's certificate

```sh
openssl s_client -connect localhost:8090 -servername localhost </dev/null 2>/dev/null \
  | openssl x509 -outform pem > thunder-localhost.pem
export WSO2_CA_FILE=$PWD/thunder-localhost.pem
```

Set `WSO2_CA_FILE` in the shell that runs `ws`. On macOS the CLI ignores
`SSL_CERT_FILE`.

## 3. Register the resource server

In the Console (`https://localhost:8090/console`):

1. Go to **Resource Servers → Add resource server**. Set the name to
   `Example Status` and the identifier to
   `https://localhost:8090/example-status`.
2. On the **Resources** tab, build the permission `example:status:read` as a
   hierarchy of handles `example` → `status` → `read`. Handles can't contain `:`.
3. Don't use **Set as default**.

## 4. Register the application

1. Go to **Applications → Add Application → Custom**. Name it `WSO2 CLI` and
   select **Finish**.
2. On the **General** tab, under **Authorized redirect URIs**, add all four:

   ```text
   http://127.0.0.1:10425/callback
   http://127.0.0.1:10426/callback
   http://127.0.0.1:10427/callback
   http://127.0.0.1:10428/callback
   ```

3. On **Advanced Settings → OAuth2 Configuration**, set **Grant Types** to
   `authorization_code` and `refresh_token`, **Response Types** to `code`, and
   turn **Public Client** on. Leave **Default Audience** empty.
4. Record the **Client ID**.

## 5. Create a user and role

Under **Users**, add a user with a password. Under **Roles**, add a role with
the `Example Status` permissions from step 3 and assign the user to it.
Without the role, login works but every token is refused.

## 6. Create the context

Save this as `thunder-local.json`, then apply it with the command below:

```json
{
  "contexts": [
    {
      "name": "thunder-local",
      "type": "onprem",
      "login": {
        "kind": "oauth-browser",
        "provider": "thunder",
        "issuer": "https://localhost:8090",
        "clientId": "REPLACE_WITH_YOUR_CLIENT_ID",
        "product": "example"
      },
      "products": {
        "example": {
          "url": "https://localhost:8090",
          "audience": "https://localhost:8090/example-status",
          "scopes": ["example:status:read"]
        }
      }
    }
  ]
}
```

```sh
ws context apply -f thunder-local.json --no-install --use thunder-local
```

The CLI stores it with `"credentialRef": "thunder-local"`; `ws context show`
prints it.

`ws context create --issuer` and `ws login --url` can't create a ThunderID
context. Use the context file above so the login names its product.

## 7. Log in and check

```sh
ws login
ws whoami
ws example status
```

`ws whoami` shows `Session  present` once you're logged in.
`ws logout` ends the session.

## CI

1. Add a second **Custom** application named `WSO2 CLI CI`. Set
   **Grant Types** to `client_credentials`, turn **Public Client** off, set
   **Client Authentication Method** to `client_secret_basic`, and record the
   client ID and secret.
2. Save and apply this context file. Keep `"provider": "thunder"`, because
   ThunderID refuses a client-credentials grant that names no resource server.

   ```json
   {
     "contexts": [
       {
         "name": "thunder-ci",
         "type": "onprem",
         "login": {
           "kind": "client-credentials",
           "provider": "thunder",
           "issuer": "https://localhost:8090",
           "clientId": "REPLACE_WITH_YOUR_CI_CLIENT_ID",
           "clientSecretVariable": "WSO2_THUNDER_CI_SECRET"
         },
         "products": {
           "example": {
             "url": "https://localhost:8090",
             "audience": "https://localhost:8090/example-status",
             "scopes": ["example:status:read"]
           }
         }
       }
     ]
   }
   ```

3. In the job, set `WSO2_NO_INPUT=1`, `WSO2_CA_FILE`, and
   `WSO2_THUNDER_CI_SECRET`, run
   `ws context apply -f thunder-ci.json --use thunder-ci`, then run product
   commands. Don't run `ws login`.

## If login fails

| Error | Fix |
| --- | --- |
| `auth.certificate_untrusted` | Set `WSO2_CA_FILE` (step 2). |
| `auth.discovery_failed` | The issuer must be the bare origin, and the port must match what the server advertises. |
| `auth.narrowing_unavailable` about a protected resource | Add `"provider": "thunder"` to the context's `login` block (`ws context edit`). |
| `auth.narrowing_unavailable` about permissions | The user has no role with the permissions (step 5). |
| `auth.product_not_configured` with `invalid_target` | The audience isn't a registered resource server identifier (step 3). |
| `shell.invalid_argument` or `contexts.document_malformed` about the audience | The audience must be an absolute URI. |
| A product reports a permission the context doesn't carry | Add it to the product's `scopes`. |

## Sources

- [Get ThunderID (quick-start)](https://thunderid.dev/docs/next/getting-started/get-thunderid/)
- [Manage Applications](https://thunderid.dev/docs/next/guides/applications/manage-applications/)
- [Manage Resource Servers (permissions, audience claim)](https://thunderid.dev/docs/next/guides/resource-servers/)
