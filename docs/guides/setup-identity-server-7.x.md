# Set up the WSO2 CLI with Identity Server 7.x

This guide registers the CLI in WSO2 Identity Server 7.x, creates a context for
it, and logs in. The examples use a server at `https://localhost:9443` and the
`reference` product. Tested against 7.3.0.
For every field a context can hold, see the
[context file reference](../reference/context-file.md).
The commands below use `ws`, the default name of a released CLI.

Install the reference module from this checkout with the
[local setup guide](setup-example-module.md). From the repository root, run
`export PATH="$PWD/bin:$PATH"` in the same terminal so the `ws` commands below
use that build. Keep the `WSO2_HOME` value from the local setup guide.

## 1. Run a server

To try it locally, start a container (sign in as `admin` / `admin`):

```sh
docker run -d --name wso2is -p 9443:9443 wso2/wso2is:7.3.0
```

If you use an unpacked distribution, check `offset` in
`repository/conf/deployment.toml`. With `offset = 1` the server listens on
9444, and the issuer changes to match.

## 2. Trust the server's certificate

A new server uses a self-signed certificate. Save it and point the CLI at it:

```sh
openssl s_client -connect localhost:9443 -servername localhost </dev/null 2>/dev/null \
  | openssl x509 -outform pem > is-localhost.pem
export WSO2_CA_FILE=$PWD/is-localhost.pem
```

Set `WSO2_CA_FILE` in the shell that runs `ws`. On macOS the CLI ignores
`SSL_CERT_FILE`. The default Identity Server certificate is a CA whose private
key ships with every download, so don't add it to your system trust store.

## 3. Register the application

In the Console (`https://localhost:9443/console`):

1. Go to **Applications → New Application → Standard-Based Application**.
   Name it `WSO2 CLI`, choose **OpenID Connect**, and create it.
2. On the **Protocol** tab:
   - **Allowed grant types**: **Code** and **Refresh Token** only. Add
     **Device Code** if you will log in from machines with no browser.
   - Select **Public client**.
   - **PKCE**: **Mandatory**, with **Plain** cleared.
   - **Authorized redirect URLs**: add all four:

     ```text
     http://127.0.0.1:10425/callback
     http://127.0.0.1:10426/callback
     http://127.0.0.1:10427/callback
     http://127.0.0.1:10428/callback
     ```

   - Access tokens are JWT by default. Keep them that way.
3. Record the **Client ID**.

## 4. Add the API resource

1. Go to **API Resources → New API Resource**. Set the identifier to
   `reference-status` and add the scope `reference:status:read`.
   **Requires authorization** can't be changed later.
2. On the application's **API Authorization** tab, authorize the resource and
   select its scopes.
3. On the application's **Protocol** tab, add `reference-status` to the
   **Audience** list.

Step 3 matters. Identity Server puts the resource identifier in the access
token's `aud` claim only when it is in that list, so the audience you record
for the product is `reference-status`.

## 5. Create a user

The `admin` account is not a user your application signs in. Create one under
**User Management → Users** and set its password directly.

If the resource requires authorization, create a role with the application as
its audience, attach the resource with every scope the context lists, and
assign the user to it. Console changes apply at the next login.

## 6. Create the context

The `ws context create` wizard lists **WSO2 Identity Server (coming soon)**
and refuses it: no WSO2 product the CLI installs signs in at an Identity
Server, so the wizard offers no path that ends anywhere. The `reference`
product here is reached by its own grant rather than by being an Identity
Server product, which is why the flags still work:

```sh
ws context create is-local \
  --issuer https://localhost:9443/oauth2/token \
  --client-id <client-id> --provider identity-server --use
ws context product add reference --url https://localhost:9443 \
  --audience reference-status --scopes reference:status:read
```

The issuer must match the `issuer` value in
`https://localhost:9443/oauth2/token/.well-known/openid-configuration` exactly.

`ws context show` summarizes what was written. In `contexts.yaml` the
context looks like this:

```yaml
name: is-local
type: onprem
credentialRef: is-local
login:
  kind: oauth-browser
  issuer: https://localhost:9443/oauth2/token
  clientId: REPLACE_WITH_YOUR_CLIENT_ID
  provider: identity-server
  product: reference
products:
  reference:
    url: https://localhost:9443
    audience: reference-status
    scopes:
      - reference:status:read
```

## 7. Log in and check

```sh
ws login
ws whoami
ws reference status
```

`ws login` opens the browser and prints the authorization URL on standard
error, so you can open it by hand if no browser appears. `ws whoami` shows
`Status  logged in` once you're logged in. `ws logout` ends the session.

To log in from a machine with no browser, enable the **Device Code** grant
(step 3) and create the context with `--device`.

## CI

CI uses a client-credentials context and doesn't run `ws login`.

1. Create a second standard-based application with the **Client Credentials**
   grant only, not a public client. Authorize the same resource, add it to the
   **Audience** list, and record the client ID and secret. If the resource
   requires authorization, assign the role to the application.
2. Create the context:

   ```sh
   ws context create is-ci \
     --issuer https://localhost:9443/oauth2/token \
     --client-id <ci-client-id> --client-secret-variable WSO2_IS_CI_SECRET
   ws context product add reference --context is-ci \
     --url https://localhost:9443 --audience reference-status \
     --scopes reference:status:read
   ```

3. In the job, set `WSO2_CONTEXT=is-ci`, `WSO2_NO_INPUT=1`, `WSO2_CA_FILE` if
   needed, and `WSO2_IS_CI_SECRET` from your CI secret store. Then run product
   commands directly. `ws context export is-ci` prints a file the job can
   apply with `ws context apply -f <file> --use is-ci`.

## If login fails

| Error | Fix |
| --- | --- |
| `auth.certificate_untrusted` | Set `WSO2_CA_FILE` (step 2). |
| `auth.discovery_failed` | The issuer doesn't match the discovery document exactly (check the port), or PKCE isn't **Mandatory**. If all four callback ports are busy, free one. |
| `auth.narrowing_unavailable` naming the audience | Add `reference-status` to the application's **Audience** list (step 4), then log in again. |
| `auth.narrowing_unavailable` naming permissions | The user holds no role with the scopes (step 5), or access tokens aren't JWT. |
| `auth.product_not_configured` | The context doesn't record the product or one of its scopes. Run `ws context product add`. |
| `auth.login_required` | The session expired or was revoked. Run `ws login`. |
| `auth.context_not_selected` | Run `ws context use is-local`. |

## Sources

- [Run WSO2 Identity Server with Docker](https://hub.docker.com/r/wso2/wso2is)
- [Register a standard-based app](https://is.docs.wso2.com/en/latest/guides/applications/register-standard-based-app/)
- [Device authorization flow](https://is.docs.wso2.com/en/latest/guides/authentication/oidc/implement-device-flow/)
- [API authorization with RBAC (audience and scopes)](https://is.docs.wso2.com/en/latest/guides/authorization/api-authorization/api-authorization/)
