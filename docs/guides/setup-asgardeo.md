# Set up the WSO2 CLI with Asgardeo

This guide registers the CLI in an Asgardeo organization, creates a context
for it, and logs in. The examples use the organization `acme` and the
`reference` product (`wso2 product install reference`). For every field a
context can hold, see the [context file reference](../reference/context-file.md).

Asgardeo was renamed WSO2 Identity Platform on 2026-06-15; the console paths
below were written on 2026-08-06 and are unchanged by the rename.

## 1. Register the application

In the Asgardeo Console, for your organization:

1. Go to **Applications → New Application → Standard-Based Application**.
   Name it `WSO2 CLI`, choose **OpenID Connect**, and create it.
2. On the **Protocol** tab:
   - **Allowed grant types**: **Code** and **Refresh Token** only. Add
     **Device Code** if you will log in from machines with no browser.
   - Select **Public client**.
   - **PKCE**: **Mandatory**. Leave **Support PKCE 'Plain'** cleared.
   - **Authorized redirect URLs**: add all four:

     ```text
     http://127.0.0.1:10425/callback
     http://127.0.0.1:10426/callback
     http://127.0.0.1:10427/callback
     http://127.0.0.1:10428/callback
     ```

   - **Access Token → Token type**: **JWT**.
3. Record the **Client ID**.

## 2. Add the API resource

1. Go to **API Resources → New API Resource**. Set the identifier to
   `reference-status` and add the scope `reference:status:read`.
2. **Requires authorization** can't be changed later. If you leave it
   checked, users get the scopes only through a role (step 3).
3. In **Applications → WSO2 CLI → Authorization**, authorize the resource and
   select its scopes.

**Asgardeo puts the client ID in the access token's `aud` claim, not the API
resource identifier** (measured against Asgardeo on 2026-08-06). So the
audience you record for a product is the client ID.

## 3. Create a user

The Console administrator account can't sign in to your application. Create a
user under **User Management → Users → Add User** and set its password
directly.

If the resource shows an authorization policy other than
**No Authorization Policy**, create a role under **Applications → WSO2 CLI →
Roles** with **Role Audience** set to **Application**, attach the resource with
every scope the context lists, and assign the user to it.

Console changes apply at the next login.

## 4. Create the context

Run `wso2 context create` with no flags to answer prompts: choose **Asgardeo**,
then enter the organization name and client ID.

Or pass flags:

```sh
wso2 context create acme \
  --issuer https://api.asgardeo.io/t/acme/oauth2/token \
  --client-id <client-id> --provider asgardeo --use
wso2 context product add reference --url https://api.asgardeo.io \
  --audience <client-id> --scopes reference:status:read
```

The issuer must match the `issuer` value in
`https://api.asgardeo.io/t/acme/oauth2/token/.well-known/openid-configuration`
exactly.

`wso2 context show` prints what was written. The stored context looks like
this:

```json
{
  "name": "acme",
  "type": "cloud",
  "credentialRef": "acme",
  "login": {
    "kind": "oauth-browser",
    "issuer": "https://api.asgardeo.io/t/acme/oauth2/token",
    "clientId": "REPLACE_WITH_YOUR_CLIENT_ID",
    "tenant": "acme",
    "provider": "asgardeo",
    "product": "reference"
  },
  "organization": "acme",
  "products": {
    "reference": {
      "url": "https://api.asgardeo.io",
      "audience": "REPLACE_WITH_YOUR_CLIENT_ID",
      "scopes": ["reference:status:read"]
    }
  }
}
```

## 5. Log in and check

```sh
wso2 login
wso2 whoami
wso2 reference status
```

`wso2 login` opens the browser. `wso2 whoami` shows `Session  present` once
you're logged in. `wso2 logout` ends the session.

To log in from a machine with no browser, enable the **Device Code** grant
(step 1) and create the context with `--device`. `wso2 login` then prints a
URL and a code to enter on another device.

## CI

CI uses a client-credentials context and doesn't run `wso2 login`.

1. Create an **M2M Application** with the **Client Credentials** grant only.
   Authorize the same API resource and scopes, set the token type to **JWT**,
   and record the client ID and secret. With an authorization policy, assign
   the role to the application.
2. Create the context. Its audience is the M2M application's client ID:

   ```sh
   wso2 context create acme-ci \
     --issuer https://api.asgardeo.io/t/acme/oauth2/token \
     --client-id <m2m-client-id> --client-secret-variable WSO2_ACME_CI_SECRET
   wso2 context product add reference --context acme-ci \
     --url https://api.asgardeo.io --audience <m2m-client-id> \
     --scopes reference:status:read
   ```

3. In the job, set `WSO2_CONTEXT=acme-ci`, `WSO2_NO_INPUT=1`, and
   `WSO2_ACME_CI_SECRET` from your CI secret store, then run product commands
   directly.

To share the context, `wso2 context export acme-ci > context.json` writes a
file the job applies with `wso2 context apply -f context.json --use acme-ci`.

## If login fails

| Error | Fix |
| --- | --- |
| `auth.discovery_failed` | The issuer doesn't match the discovery document exactly, or PKCE isn't set to **Mandatory**. If all four callback ports are busy, free one. |
| `auth.narrowing_unavailable` naming the audience | Set the product's audience to the client ID: `wso2 context product add reference --url <url> --audience <client-id> --replace`. |
| `auth.narrowing_unavailable` naming permissions | The user holds no role with the scopes (step 3), or the token type isn't **JWT**. |
| `auth.product_not_configured` | The context doesn't record the product or one of its scopes. Run `wso2 context product add`. |
| `auth.login_required` | The session expired or was revoked. Run `wso2 login`. |
| `auth.keyring_unavailable` | No OS secure store. On headless Linux, start a keyring daemon or use a CI context. |
| `auth.context_not_selected` | Run `wso2 context use acme`. |
| `auth.login_not_required` | The context uses client credentials. Run the product command directly. |

## Sources

- [Register a standard-based app](https://wso2.com/asgardeo/docs/guides/applications/register-standard-based-app/)
- [OAuth2 grant types (device authorization grant)](https://wso2.com/asgardeo/docs/references/grant-types/)
- [Access tokens (`aud` claim)](https://wso2.com/asgardeo/docs/references/tokens/access-tokens/)
