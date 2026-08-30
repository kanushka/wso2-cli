# Logging in with the WSO2 CLI: ThunderID

This is the registration walkthrough for **ThunderID**, one of the three
deployments `wso2 login` supports. Asgardeo and WSO2 Identity Server have
[their](login-asgardeo.md) [own](login-identity-server.md) walkthroughs, and
everything after registration is the same document for all three products:
writing the context document, logging in, CI, troubleshooting. Read this one for
the registration, then return to
[section 2 of the login guide](login.md#2-the-context-document).

**Written against ThunderID `v1.0.0-beta`.** Console layouts move in an alpha
and beta product; if a control named here is not where this says, the version
is the first thing to check. The container recipe below pins that exact version
so the two cannot drift apart.

---

## 1. What is different about Thunder

Everything in this section has a consequence further down, so it is worth
reading before registering anything.

**Thunder decides an access token's audience per request.** Asgardeo and
Identity Server decide it from the application's registration; Thunder reads an
[RFC 8707](https://www.rfc-editor.org/rfc/rfc8707) *resource indicator* on the
authorization request and mints the token for exactly that resource server. The
shell sends the indicator when the context document names Thunder as the
identity provider, and not otherwise.

Three things follow:

- **The audience is a URI.** A resource server's identifier must be an absolute
  URI, so `products.<namespace>.audience` is a URI here. The bare API resource
  identifier that works on Identity Server is refused.
- **One login reaches one product.** Thunder accepts a single resource indicator
  per authorization: *"Only a single resource parameter is supported"*. A
  session is therefore bound to one resource server, and the context document
  refuses an identity that names Thunder and declares more than one product.
  Lifting that is [tracked separately](https://github.com/wso2/wso2-cli/issues/43).
- **The audience check means what it says.** On Asgardeo an access token's `aud`
  is the client ID and cannot distinguish one product from another. On Thunder it
  is the resource server identifier and nothing else, which is the strongest
  audience guarantee of the three products.

**Thunder has no device authorization grant.** Its discovery document advertises
no `device_authorization_endpoint` and its grant handlers register none.
`wso2 login --device-code` cannot work against a Thunder-backed deployment;
browser login is the interactive path.

---

## 2. Run a deployment

```sh
docker run -d --name thunderid -p 8090:8090 \
  ghcr.io/thunder-id/thunderid:1.0.0-beta \
  bash -c './setup.sh --admin-username admin --admin-password "Admin@123" && ./start.sh'
```

`setup.sh` generates the deployment's keys and certificates and seeds the
default resources; `start.sh` serves. It answers in well under a minute. Nothing
is persisted outside the container, so `docker rm -f thunderid` returns the
machine to where it started. That is the reason to prefer it while you are
learning the console, where a half-registered application from a previous
attempt is hard to tell from a correct one.

Thunder runs standalone on embedded storage. It needs no database and no cache
beside it, whatever a compose file you may have seen alongside it suggests.

**Pin the version.** `latest` has carried an older alpha than the newest release
during this walkthrough's lifetime, so a recipe that uses it describes whatever
was pushed last rather than what is written here.

**If port 8090 is taken**, publishing on a different host port is not enough on
its own. Thunder advertises its own public URL in its discovery document, and
the shell checks that document against the issuer it was fetched from, so the
advertised URL and the URL you reach it on have to agree. Change the advertised
one to match:

```sh
docker run -d --name thunderid -p 8490:8090 \
  ghcr.io/thunder-id/thunderid:1.0.0-beta \
  bash -c 'sed -i "s|public_url: \"https://localhost:8090\"|public_url: \"https://localhost:8490\"|" deployment.yaml \
    && ./setup.sh --admin-username admin --admin-password "Admin@123" && ./start.sh'
```

**Everything below this point writes `8090`.** If you took the offset recipe,
substitute the host port you chose, in the discovery URL, the console URL, the
certificate you trust, the resource server's identifier, and the issuer and
audience you write into the context document. The port is part of the issuer's
identity here, not a detail of how you reach it, so a value that is close but
not exact fails at discovery.

Confirm what it advertises rather than assuming it:

```sh
curl -sk https://localhost:8090/.well-known/openid-configuration
```

Note the issuer is the **bare origin**, `https://localhost:8090`, and not a
path under it. Identity Server's issuer is `https://localhost:9443/oauth2/token`; the
shape is not the same and using one product's shape against the other fails at
discovery.

The console is at `https://localhost:8090/console`, with the administrator
credentials the recipe set.

---

## 3. Trust the deployment's certificate

Thunder serves TLS with a minimum version of 1.3 and, on a fresh deployment, a
self-signed certificate. The shell uses the process's ordinary HTTP client and
has no flag anywhere for a custom certificate authority, so until that
certificate is trusted, login cannot reach discovery at all:

```text
tls: failed to verify certificate: x509: certificate signed by unknown authority
```

On macOS, note that Go **ignores `SSL_CERT_FILE`**, since `crypto/x509` honors
it on every Unix except Darwin, so the keychain is the only way in. Take the
certificate from the port:

```sh
openssl s_client -connect localhost:8090 -servername localhost </dev/null 2>/dev/null \
  | openssl x509 -outform pem > thunder-localhost.pem

security add-trusted-cert -r trustRoot -p ssl \
  -k ~/Library/Keychains/login.keychain-db thunder-localhost.pem
```

**This trade is narrower than the equivalent one for Identity Server**, and the
difference is worth knowing. Identity Server ships a `CA:TRUE` certificate whose
private key is inside every download and every copy of the public container
image, behind a published password; trusting it means trusting a signing key
anyone can obtain, for any hostname. Thunder's is generated by `setup.sh` on
the deployment that serves it, so trusting it trusts that deployment and
nothing else. `-p ssl` confines it to TLS and the login keychain confines it to
your user.

Remove it when you are done:

```sh
security delete-certificate -c localhost ~/Library/Keychains/login.keychain-db
```

---

## 4. Register the API as a resource server

This is the step with no equivalent name on the other two products. Asgardeo and
Identity Server call it an API resource; Thunder calls it a **resource server**,
and its identifier is what lands in an access token's `aud`.

1. **Resource Servers → Add resource server**.
2. **Name**: `Reference Status`.
3. **Identifier**: `https://localhost:8090/reference-status`.

   It must be an **absolute URI**. Thunder refuses anything else with
   `invalid_target: Invalid resource parameter: must be an absolute URI`, and
   the refusal arrives at login rather than at registration, so a bare name here
   costs a browser sign-in to discover.

Then add the permissions the reference module asks for. On the resource
server's **Resources** tab there is a **Resource Hierarchy** panel:

4. Use the **+** on the panel header to add a top-level resource. Give it a
   **Name** and a **Handle**; the **Permission** shown beside it is what a
   token's `scope` will carry, and the handle is immutable once created.
5. Use the **+** on a resource's own row to add a child beneath it.

**Handles cannot contain the resource server's delimiter**, which is `:` by
default. A handle of `reference:status:read` is refused with
`Delimiter conflict in handle`. A permission of that shape is built as a
hierarchy instead: `reference`, with a `status` child, with `read` and `write`
children under that. Thunder joins the handles with the delimiter to produce
the permission.

For a first run, two flat resources are enough and simpler to verify: handles
`read` and `write`, giving permissions `read` and `write`.

### Do not set a default resource server

The resource server page carries a **Set as default** action, whose confirmation
says *"Requests without a resource parameter will fall back to it."* It does
exactly that, and it is worth understanding rather than using.

With a default configured, Thunder issues tokens for requests that name no
resource, and binds every one of them to the default, whichever product asked.
That is precisely the weakness the shell's audience check exists to avoid, and
it is the one thing that would make Thunder's audience guarantee no better than
Asgardeo's. Leave it unset; the shell names the resource on every request and
does not need the fallback.

---

## 5. Register the CLI as a public client

1. **Applications → Add Application → Custom**.

   Custom is the type that exposes the whole OAuth configuration. The
   technology-named types (React, Node.js, and so on) preset choices that do not
   describe a command-line tool.

2. **Name** it `WSO2 CLI`, then **Finish**. The OAuth settings are configured
   after creation, on the application's own page.

3. On the **General** tab, under **Authorized redirect URIs**, add all four
   loopback callbacks with **Add URI**:

   ```text
   http://127.0.0.1:10425/callback
   http://127.0.0.1:10426/callback
   http://127.0.0.1:10427/callback
   http://127.0.0.1:10428/callback
   ```

   These are the ports the shell binds, in order, taking the first that is free.
   Registering all four is what lets a login succeed when something else on the
   machine already holds one of them.

4. On the **Advanced Settings** tab, under **OAuth2 Configuration**:

   - **Grant Types**: `authorization_code` and `refresh_token`, and nothing else.
     The refresh grant is what every later per-module acquisition narrows from;
     without it a login succeeds and no module can be granted anything.
   - **Response Types**: `code`.
   - **Public Client**: on. **Client Authentication Method** then locks itself to
     `none`, which is correct: the shell is a public client and holds no
     secret.
   - **PKCE Required**: locked on, labelled *"Always required for public
     clients."* Nothing to do; Thunder does not let a public client skip it.

5. Leave **Default Audience** empty. It applies only to tokens that target no
   resource server, and the shell always targets one.

---

## 6. Create a user, and grant it the permissions

1. **Users → add a user**, with a username and password you will sign in with.
2. **Roles → add a role**, for example `Reference Status Caller`.
3. On the role, add **permissions**, choosing the `Reference Status` resource
   server and the permissions you created under it.
4. Assign the role to the user.

A user with no such role signs in successfully and receives a token stating no
permissions, which the shell then refuses. See the troubleshooting note below.

---

## 7. A confidential client for CI, if you need one

For the non-interactive path, register a second application:

1. **Applications → Add Application → Custom**, named `WSO2 CLI CI`.
2. **Advanced Settings → OAuth2 Configuration**: **Grant Types**
   `client_credentials`; **Public Client** off; **Client Authentication Method**
   `client_secret_basic`.
3. Record the client ID and secret.

The client-credentials grant has no earlier authorization to inherit a resource
binding from, so the shell sends the resource indicator on that request too. A
Thunder deployment refuses the grant outright without it.

**That is why a Thunder CI identity carries `provider` exactly as a browser one
does.** `provider` is what selects the resource-bound derivation, and the shell
sends no indicator without it, so the
[CI context in the login guide](login.md#51-write-the-ci-context), which is
written for Asgardeo, cannot be used here as it stands. This is the Thunder
form:

```json
{
  "name": "thunder-ci",
  "type": "onprem",
  "auth": {
    "kind": "client-credentials",
    "provider": "thunder",
    "issuer": "https://localhost:8090",
    "clientId": "REPLACE_WITH_YOUR_CI_CLIENT_ID",
    "clientSecretVariable": "WSO2_THUNDER_CI_SECRET"
  },
  "products": {
    "reference": {
      "endpoint": "https://localhost:8090",
      "audience": "https://localhost:8090/reference-status",
      "scopes": ["read", "write"]
    }
  }
}
```

The same two rules that bind a Thunder browser identity bind this one, and the
shell refuses the document rather than the grant if either is broken: **exactly
one product**, and an **audience that is an absolute URI**. Carrying the login
guide's `"audience": "reference-status"` over is refused at parse as not a URI,
which is the cheap failure; omitting `provider` is the expensive one, because
the document parses and the deployment refuses every grant.

[Section 5.2 of the login guide](login.md#52-wire-the-job) has the job wiring,
which is the same for all three products.

---

## 8. Record what you need

- **Client ID** of the `WSO2 CLI` application.
- **Issuer**, the bare origin, taken verbatim from the `issuer` value in
  `https://localhost:8090/.well-known/openid-configuration`.
- **Audience**, which is the resource server's **identifier**: the absolute URI
  from section 4, not its name.
- **Scopes**, the permissions from section 4.
- **Whether this machine trusts the deployment's certificate** (section 3).

---

## 9. Log in, and check what it wrote

With the issuer and client ID from the section above, one command creates
the identity and the context and signs you in:

```console
$ wso2 login --url https://thunder.example.com \
    --client-id <client-id> --context thunder-local
```

It reports the names it assigned, and `wso2 context list` shows them.
Nothing below has to be typed by hand; it is what the document now holds,
and reading it is how you check the login recorded what you expected. Everything from here is [the main login guide](login.md), from section 2. Two
members are Thunder-specific:

```json
{
  "name": "thunder-local",
  "type": "onprem",
  "auth": {
    "kind": "oauth-browser",
    "provider": "thunder",
    "issuer": "https://localhost:8090",
    "clientId": "wso2-cli",
    "credentialRef": "thunder-local-login"
  },
  "products": {
    "reference": {
      "endpoint": "https://localhost:8090",
      "audience": "https://localhost:8090/reference-status",
      "scopes": ["read", "write"]
    }
  }
}
```

`provider` is what makes the shell send the resource indicator. Without it the
login is refused with `invalid_target` and no session is established.

You may also write `narrowing` explicitly, as `scoped-refresh` or
`token-resource`, for a deployment that does not behave the way its product
ordinarily does. An explicit `narrowing` wins over what `provider` implies. A
Thunder deployment with a default resource server configured is the case this
exists for.

---

## 10. Troubleshooting

### `auth.narrowing_unavailable`, mentioning a protected resource

> the deployment will not issue access for the "reference" module without being
> told which protected resource it is for

The identity does not name Thunder as its provider, so the shell asked in a
shape this deployment does not accept. Add `"provider": "thunder"` to the
identity's `auth` block.

### `contexts.document_malformed`, about one login and one product

The identity derives access by resource and declares more than one product.
Thunder accepts one resource indicator per authorization, so one session cannot
reach two products. Split them across two identities, each with its own
`credentialRef`.

### `contexts.document_malformed`, about a product without an audience

A resource-bound derivation has to name the resource it binds to, and it takes
that name from the product's `audience`. Add it.

### `auth.narrowing_unavailable`, about permissions the deployment did not state

Thunder issues a token stating no permissions when the client or user holds
none, rather than refusing the request. The shell cannot prove such a token
carries what was asked for, so it refuses. Check that the signed-in user holds a
role granting the permissions on the right resource server (section 6).

### `auth.discovery_failed` at login

Either the certificate is not trusted (section 3), or the issuer in the context
document is not what the deployment advertises. Compare it against the `issuer`
value in the discovery document, and remember it is the bare origin on Thunder.

### The login page asks for credentials every time

Expected on a default deployment, and measured: a second authorization request
minutes after a completed sign-in presented the sign-in form again. Whether a
single sign-on session can be configured is not established here.

---

## 11. Proving it against this deployment

The live runs in `test/smoke/` work against Thunder exactly as they do against
the other two products. `test/smoke/env.example` carries a Thunder block; copy
it, fill in what section 8 told you to record, and see
[`test/smoke/RUNNING.md`](../../test/smoke/RUNNING.md).

The measured behaviour behind everything above is recorded in
[`docs/research/asgardeo-redirect-uri-and-scope-narrowing.md`](../research/asgardeo-redirect-uri-and-scope-narrowing.md)
§3.2, with the date and the deployment each verdict came from.
