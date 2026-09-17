# Set up the WSO2 CLI with ThunderID

This guide registers the CLI in ThunderID `1.0.0-beta`, creates a context, and
logs in. The examples use `https://localhost:8090` and the `reference` product
(`wso2 product install reference`). Context fields are described in the
[context file reference](../reference/context-file.md).

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

ThunderID's docs now lead with a Compose quick-start
(`docker compose -f oci://ghcr.io/thunder-id/thunderid-quick-start:latest up`)
instead of this `docker run` form; the command above was written on
2026-08-06 and still works against `1.0.0-beta`.

## 2. Trust the server's certificate

```sh
openssl s_client -connect localhost:8090 -servername localhost </dev/null 2>/dev/null \
  | openssl x509 -outform pem > thunder-localhost.pem
export WSO2_CA_FILE=$PWD/thunder-localhost.pem
```

Set `WSO2_CA_FILE` in the shell that runs `wso2`. On macOS the CLI ignores
`SSL_CERT_FILE`.

## 3. Register the resource server

In the Console (`https://localhost:8090/console`):

1. Go to **Resource Servers → Add resource server**. Set the name to
   `Reference Status` and the identifier to
   `https://localhost:8090/reference-status`.
2. On the **Resources** tab, build the permission `reference:status:read` as a
   hierarchy of handles `reference` → `status` → `read`. Handles can't contain `:`.
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
the `Reference Status` permissions from step 3 and assign the user to it.
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
        "product": "reference"
      },
      "products": {
        "reference": {
          "url": "https://localhost:8090",
          "audience": "https://localhost:8090/reference-status",
          "scopes": ["reference:status:read"]
        }
      }
    }
  ]
}
```

```sh
wso2 context apply -f thunder-local.json --no-install --use thunder-local
```

The CLI stores it with `"credentialRef": "thunder-local"`; `wso2 context show`
prints it.

If the `identity` product is installed, `wso2 context create` can build the
login from its descriptor instead:

```sh
wso2 context create thunder-local --login-product identity \
  --url https://localhost:8090 --use
wso2 context product add reference --url https://localhost:8090 \
  --audience https://localhost:8090/reference-status \
  --scopes reference:status:read
```

That path records the login only, so add the product you are going to run
before step 7.

`wso2 context create --issuer` and `wso2 login --url` can't create a ThunderID
context.

## 7. Log in and check

```sh
wso2 login
wso2 whoami
wso2 reference status
```

`wso2 whoami` shows `Session  present` once you're logged in.
`wso2 logout` ends the session.

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
           "reference": {
             "url": "https://localhost:8090",
             "audience": "https://localhost:8090/reference-status",
             "scopes": ["reference:status:read"]
           }
         }
       }
     ]
   }
   ```

3. In the job, set `WSO2_NO_INPUT=1`, `WSO2_CA_FILE`, and
   `WSO2_THUNDER_CI_SECRET`, run
   `wso2 context apply -f thunder-ci.json --use thunder-ci`, then run product
   commands. Don't run `wso2 login`.

## If login fails

| Error | Fix |
| --- | --- |
| `auth.certificate_untrusted` | Set `WSO2_CA_FILE` (step 2). |
| `auth.discovery_failed` | The issuer must be the bare origin, and the port must match what the server advertises. |
| `auth.narrowing_unavailable` about a protected resource | Add `"provider": "thunder"` to the context's `login` block (`wso2 context edit`). |
| `auth.narrowing_unavailable` about permissions | The user has no role with the permissions (step 5). |
| `auth.product_not_configured` with `invalid_target` | The audience isn't a registered resource server identifier (step 3). |
| `shell.invalid_argument` or `contexts.document_malformed` about the audience | The audience must be an absolute URI. |
| A product reports a permission the context doesn't carry | Add it to the product's `scopes`. |

## Sources

- [Get ThunderID (quick-start)](https://thunderid.dev/docs/next/getting-started/get-thunderid/)
- [Manage Applications](https://thunderid.dev/docs/next/guides/applications/manage-applications/)
- [Manage Resource Servers (permissions, audience claim)](https://thunderid.dev/docs/next/guides/resource-servers/)
