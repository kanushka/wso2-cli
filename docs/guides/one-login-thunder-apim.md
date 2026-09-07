# One login for ThunderID and API Manager

**Status:** Working draft
**Last reviewed:** 2026-09-07
**Related:** [Logging in](login.md),
[Registering in ThunderID](login-thunder.md),
[ADR 0014: one login, one session per product](../adr/0014-one-login-one-session-per-product.md),
[the live matrix](../research/2026-09-07-one-login-live-matrix.md),
[the federation spikes](../research/2026-09-06-single-login-spikes.md)

This is the developer walkthrough for the shell's per-product session
model: one `wso2 login` at ThunderID that serves both ThunderID's own
management API and API Manager's, then a mock API registered, published,
subscribed to and called through the gateway. Every command and every
output below was run on 2026-09-07 against ThunderID 1.0.1 at
`http://localhost:8492` and API Manager 4.7.0 at `https://localhost:9443`
(gateway `https://localhost:8243`), with the `iam` and `apim` modules built
from this repository. Where a step is inferred rather than measured, it
says so.

Two things to know before starting. First, the products are recorded with
`wso2 <namespace> connect <url>`, not with `wso2 identity create`: `connect`
reads the module's product descriptor and writes the issuer, audience,
scopes and grant itself, so the only values you type are a URL, plus one
client id for API Manager and, for its gateway, the API's own resource
identifier and permissions. Second, API Manager has to be told to trust
ThunderID before any of this works; section 3 does that with one command on
each product, and nothing in it opens a console.

---

## 1. What one login means here

An identity is one login provider and one person, and holds one session
per product it reaches. `wso2 login` runs one authorization per recorded
product, each answered by the provider's sign-on cookie, so the person
enters credentials once. How each product's session is obtained is its
**strategy**:

| Product | Strategy | What happens |
| --- | --- | --- |
| `iam` | `direct` | ThunderID issues for its own management API. One credential prompt. |
| `apim` (management) | `federated` | API Manager's own issuer, through a public client federated to ThunderID. No prompt; five redirects. |
| `apim/gateway` | `sibling` | ThunderID issues for the API's own resource server, recorded beside the product by `connect --gateway`. No prompt. See section 7. |

That is what ADR 0014 accepts, and it replaces the rule that one identity
was one session narrowed per command. Under that rule an identity naming
Thunder could declare only one product, because ThunderID binds an
authorization to one resource server; the
[ThunderID walkthrough](login-thunder.md#1-what-is-different-about-thunder)
still explains the binding, and that binding is now the reason every
product gets its own session rather than a limit on how many an identity
may declare.

---

## 2. Record ThunderID: bootstrap, then connect

Trust the two self-signed certificates the way
[the ThunderID walkthrough](login-thunder.md#3-trust-the-deployments-certificate)
does; API Manager's is taken from port 9443 the same way. The examples
below ran with ThunderID over plain HTTP and `WSO2_CA_FILE` naming the
API Manager certificate.

`wso2 iam bootstrap` registers this CLI as a public client in ThunderID,
once, with the administrator password read from the environment:

```console
$ WSO2_IAM_ADMIN_PASSWORD=Admin@123 wso2 iam bootstrap --url http://localhost:8492
Issuer        http://localhost:8492
Client ID     wso2-cli
Application   …
Created       true

Next  Run wso2 iam connect http://localhost:8492, then wso2 login.
```

Then record the product. `connect` writes nothing to the secure store and
makes no network call:

```console
$ wso2 iam connect http://localhost:8492

Recorded the "iam" product on the "thunder" identity.

Product            iam
Identity           thunder
Identity created   true
Endpoint           http://localhost:8492
Issuer             http://localhost:8492
Client ID          wso2-cli
Audience           https://localhost:8090/mcp
Scopes             system
Strategy           direct
Replaced           false

Next  Run wso2 login.
```

One line created the `thunder` identity, a same-named context, selected it
because nothing else was, and pinned `iam` as the identity's login product.
The pin is what keeps the login where it is when a second product is
recorded, whatever its namespace sorts as. Do not log in yet: the next two
sections add API Manager, and one login then serves both.

---

## 3. Register the public federated client on API Manager

API Manager needs an identity provider that points at ThunderID, and a
**public** OAuth client for this CLI whose authentication step is that
identity provider. Two commands write them, one per product, because each
product's secrets stay inside its own module: `wso2 iam apps create` makes
the confidential client API Manager federates through on ThunderID, and
`wso2 apim bootstrap --login-provider` makes the identity provider and the
public client on API Manager. The one value that crosses is the federation
client's secret, in an environment variable. The
[federation spike](../research/2026-09-06-single-login-spikes.md#1-federation-setup-that-produced-the-result)
recorded the configuration these commands apply.

First the federation client, on ThunderID, under the `iam` session section
2 established. `--for` names the product that signs in through it, so its
callback is `https://localhost:9443/commonauth`; the client sits on the
console's authentication flow, so its sign-on is the one `wso2 login`
establishes, and its ID token carries `email`, `groups` and `name`:

```console
$ wso2 iam apps create apim-federation --type federation --for https://localhost:9443
ID              …
Client ID       apim-federation
Type            federation
Created         true
Client secret   <shown once>

Next  export WSO2_APIM_FEDERATION_CLIENT_SECRET=<the client secret above, shown once>; then wso2 apim bootstrap --url https://localhost:9443 --login-provider http://localhost:8492 --federation-client-id apim-federation
```

Then API Manager, with its administrator password and that secret in the
environment. `--login-provider` is the address the **browser** reaches
ThunderID on; `--login-provider-internal-url` is the address the **API
Manager container** reaches it on, for the token and userinfo endpoints
(`host.docker.internal` when both run in Docker on one machine), and
defaults to the login provider:

```console
$ export WSO2_APIM_FEDERATION_CLIENT_SECRET=<the secret above>
$ WSO2_APIM_ADMIN_PASSWORD=<admin password> wso2 apim bootstrap --url https://localhost:9443 \
    --login-provider http://localhost:8492 \
    --login-provider-internal-url http://host.docker.internal:8492 \
    --federation-client-id apim-federation
Issuer              https://localhost:9443/oauth2/token
Client ID           fg4dU3xLQ4Nd0hKmQ0bYq6R1ASca
Client secret       <shown once>
Token type          JWT
Identity provider   wso2-cli-localhost-8492 (created)
Public client       DgP2V4Arw9KYeo2ltIm4r8r19vca (created)

Next  Run wso2 apim connect https://localhost:9443 --client-id DgP2V4Arw9KYeo2ltIm4r8r19vca. For a pipeline: export WSO2_APIM_CLIENT_SECRET=<the client secret above, shown once>; then wso2 apim connect https://localhost:9443 --client-id fg4dU3xLQ4Nd0hKmQ0bYq6R1ASca --client-secret-variable WSO2_APIM_CLIENT_SECRET, on a client-credentials identity.
```

The first four rows are the confidential pipeline client bootstrap has
always registered; section 8 uses it. The two new rows are what the
browser login needs:

- the identity provider, named `wso2-cli-` and the login provider's host
  and port unless `--identity-provider <name>` says otherwise, with an
  OpenID Connect federated authenticator for `apim-federation`, callback
  `https://localhost:9443/commonauth`, the additional query parameters
  `scope=openid email groups` and `resource=https://localhost:9443/oauth2/token`
  (ThunderID refuses an authorization that names no resource), `groups`
  mapped to the local role claim and `email` to the email claim, the
  ThunderID group `Administrators` mapped to the API Manager role `admin`
  that carries the `apim:*` scopes (`--map-group <group>=<role>`,
  repeatable, changes that), and just-in-time provisioning on, silent, to
  the `PRIMARY` user store, without which the mapped roles never reach the
  scope issuer and the token carries `openid` alone;
- the public client `wso2-cli-sso` (`--public-client-name` changes that),
  registered with the shell's four loopback callbacks, public, PKCE S256
  mandatory, JWT tokens, its authentication one federated step through the
  identity provider with consent skipped.

The identity provider and the public client are read first and written
only when the read differs, so running the command again reports both as
`(present)` and changes neither; the pipeline client is registered by name
on every run, which API Manager answers with the client it already holds. An identity provider of that name registered by hand is updated
in place, with its default authenticator kept enabled, which is what API
Manager requires once a service provider references it. Without
`--federation-client-id`, or without `WSO2_APIM_FEDERATION_CLIENT_SECRET`
exported, the command is refused with `apim.missing_flag` naming the
`wso2 iam apps create` line above. The public client's id is the one
value `wso2 apim connect` needs; it is per deployment, which is why the
module's descriptor cannot supply it, and the next line carries it.

---

## 4. Record API Manager, then log in once

```console
$ wso2 apim connect https://localhost:9443 --client-id DgP2V4Arw9KYeo2ltIm4r8r19vca

Recorded the "apim" product on the "thunder" identity.

Product            apim
Identity           thunder
Identity created   false
Endpoint           https://localhost:9443
Issuer             https://localhost:9443/oauth2/token
Client ID          DgP2V4Arw9KYeo2ltIm4r8r19vca
Audience           DgP2V4Arw9KYeo2ltIm4r8r19vca
Scopes             apim:api_view,apim:api_create,apim:api_publish,apim:subscribe,apim:app_manage,apim:admin
Strategy           federated
Replaced           false

Next  Run wso2 login.
```

The product attached to the selected `thunder` identity under a federated
grant at API Manager's own issuer; the login product stayed `iam`. Now the
one login:

```console
$ wso2 login
Logged in to the "thunder" context.
Subject    01900000-0000-7000-8000-000000000030
Products   apim, iam
iam        direct, established
apim       federated, established
```

One credential prompt, at ThunderID's sign-in page, for the `iam`
authorization. The API Manager authorization then completed through the
sign-on with no prompt. `wso2 whoami` reports every session and its
strategy, and `wso2 doctor` passes its session check only when every
record holds one. The gateway record is not among them yet; section 7
adds it.

If you would rather see the second authorization when it is needed,
`wso2 login --no-products` establishes the login session alone, and the
first `wso2 apim` command prints a notice, authorizes through the sign-on
and answers. Under `--no-input` it is refused before anything opens, with
`auth.session_required` naming `wso2 login --only apim`.

---

## 5. The mock API on ThunderID: resource server, user, role

Everything on ThunderID from here runs under the `system` session `iam`
holds. The API being protected is a mock backend at
`http://localhost:18080/mockapi`; its identifier is what a gateway token's
`aud` will carry, so it must be an absolute URI.

```sh
wso2 iam resource-servers create "Mock API" --identifier http://localhost:18080/mockapi \
  --permission reference:status:read --permission orders:read
WSO2_IAM_USER_PASSWORD='Cli@12345' wso2 iam users create cliuser --email cliuser@example.com
wso2 iam roles create "Mock API Caller" --resource-server "Mock API" \
  --permission reference:status:read --permission orders:read --assign-user cliuser
```

Every `create` is idempotent: run again, it reports `created false` and
changes nothing. `roles create` on a role that already exists adds the
assignments named, so it is also how a user is added to a role later.

**A role change does not reach a session that already exists.** A session
was authorized for what the user held when it was established, and
ThunderID's sign-on answers a repeat authorization from the same session.
After changing what a user holds, the session that carries the role has to
be established again. For the gateway session that is one narrowed login,
which the narrowing refusal names:

```sh
wso2 login --only apim
```

---

## 6. The mock API on API Manager: import, publish, key manager, subscription

Everything on API Manager runs under the `apim` session, as the federated
user. Because that user was provisioned into API Manager by the identity
provider, applications it creates are its own and visible to it; an
application the local administrator created is not. The backend address is
the one the API Manager container reaches the mock backend on.

```sh
wso2 apim apis import --file mockapi-openapi.yaml --name MockAPI --version 1.0.0 \
  --api-context /mockapi --backend http://host.docker.internal:18080
wso2 apim apis deploy MockAPI/1.0.0
wso2 apim apis publish MockAPI/1.0.0
wso2 apim key-managers add Thunder3 --well-known http://localhost:8492 \
  --jwks http://host.docker.internal:8492/oauth2/jwks
wso2 apim apps create CliApp
wso2 apim apps subscribe CliApp MockAPI/1.0.0
wso2 apim apps map-keys CliApp --key-manager Thunder3 --client-id wso2-cli --key-type PRODUCTION
```

`key-managers add` registers ThunderID as an issuer whose tokens the
gateway validates. `map-keys` tells API Manager that the `wso2-cli` client
on that key manager is this application's production key, which is what
lets a ThunderID token, minted for the `wso2-cli` client, resolve to the
subscription. No `--context` appears on any line: `thunder` is the selected
context.

`apis list` needs `apim:api_view` and `apps list` needs `apim:subscribe`,
and both answer under the same session, which is what shows the session is
authorized for the product's recorded scopes rather than for one command's.

---

## 7. Call the API through the gateway

This is the built path (#163). The gateway is reached the other way round
from management: ThunderID issues for the API's own resource server, and
the token goes to `https://localhost:8243`. That is the `sibling`
strategy, and it is a second record of the same `apim` product on the same
identity, written by a second `connect`:

```console
$ wso2 apim connect https://localhost:8243 --gateway \
    --audience http://localhost:18080/mockapi --scopes reference:status:read,orders:read

Recorded the "apim" gateway on the "thunder" identity.

Product            apim
Record             gateway
Identity           thunder
Identity created   false
Endpoint           https://localhost:8243
Issuer             http://localhost:8492
Client ID          wso2-cli
Audience           http://localhost:18080/mockapi
Scopes             reference:status:read,orders:read
Strategy           sibling
Replaced           false

Next  Run wso2 login --only apim.
```

The audience is the API's resource identifier as ThunderID registered it
(section 5), and the scopes are its permissions; both are typed once, here.
`--gateway` needs the management record to exist first and is refused with
`shell.product_required` otherwise, and a second `--gateway` is refused
with `contexts.product_exists` unless `--replace`. Then log in: `wso2
login` establishes all three sessions from one prompt when nothing is
stored yet, and `wso2 login --only apim` establishes the product's two
records beside a login session that already stands, which is what the
`Next` line says:

```console
$ wso2 login --only apim
Logged in to the "thunder" context.
Subject        01900000-0000-7000-8000-000000000030
Products       apim, iam
apim           federated, established
apim/gateway   sibling, established
$ wso2 whoami
…
Products   apim: federated, present; apim/gateway: sibling, present; iam: direct, present
$ wso2 apim gateway invoke /mockapi/1.0.0/status
```

```text
GET  https://localhost:8243/mockapi/1.0.0/status  200  {"status":"ok", …}
```

No second identity and no `--context`: `gateway invoke` asks the shell for
the gateway record by name and calls the gateway endpoint it holds. Row 5a
of the matrix measured this path on 2026-09-07: one credential prompt, nine
redirects, three sessions from one `wso2 login`, and a 200 from the mock
backend. The outputs above are that run's, with the API names of this guide.

What the 200 proves: the gateway accepted a ThunderID access token, key
manager `Thunder3` validated it, and the subscription and key mapping
resolved.

To call the API as `cliuser`, whom section 5 gave the role, sign in as
that user. A user who holds no role on the resource server signs in
successfully and is refused at the command with
`auth.narrowing_unavailable`; so is one whose role was granted after the
gateway session was established, until the `wso2 login --only apim` in
section 5.

---

## 8. The same from a pipeline

There is no browser and no sign-on in a job, so nothing called a session
is shared; one machine client is minted per product. For ThunderID that is
a client-credentials application holding a role with the `system`
permission (the ThunderID walkthrough's
[section 7](login-thunder.md#7-a-confidential-client-for-ci-if-you-need-one)),
recorded by `connect` with the two credential flags:

```sh
export WSO2_CI_CLIENT_SECRET=…
wso2 iam connect http://localhost:8492 --identity thunder-ci \
  --client-id wso2-cli-ci --client-secret-variable WSO2_CI_CLIENT_SECRET
WSO2_NO_INPUT=1 wso2 iam users list --context thunder-ci
```

No login step: `connect`'s own next line says `wso2 iam status`, and
`whoami`, `doctor` and `logout` all exit 0 on an identity that never
logged in.

API Manager does not accept a ThunderID machine token, so on a machine
identity the `apim` product carries its own credential. This is what
`wso2 apim bootstrap` is for: it registers a confidential client and
prints the line to run, `wso2 apim connect https://localhost:9443
--client-id <id> --client-secret-variable WSO2_APIM_CLIENT_SECRET`, on the
client-credentials identity. Without those flags the product is refused at
`connect` with `auth.product_not_configured`. That refusal was measured;
the credential path itself is unit-tested and was not run live on this
deployment, because no such client was registered on it.

---

## 9. What each refusal means

| Refusal | Cause | What to do |
| --- | --- | --- |
| `shell.missing_required_flag` at `apim connect`, naming a public client | No `--client-id`. | Pass the public client id from the `Public client` row of `apim bootstrap --login-provider`, not the confidential `Client ID` row. |
| `shell.login_provider_required` at `apim connect` | No identity exists to attach the product to. | Run `wso2 iam connect <url>` first. |
| `contexts.product_exists` at `connect` | The product, or with `--gateway` its gateway, is already recorded on the identity. | Add `--replace` to record it again. |
| `shell.product_required` at `apim connect --gateway` | No identity records the `apim` product yet; a gateway is a second record of a product. | Run `wso2 apim connect https://localhost:9443 --client-id <public client>` first (section 4). |
| `apim.no_gateway` at `apim gateway invoke` | The identity records the product without its gateway. | Run the `connect --gateway` line in section 7, then `wso2 login --only apim`. |
| `auth.credential_unavailable` at `wso2 login --only apim`, after API Manager's own login form appeared | The client id is the confidential one from `apim bootstrap`, which does not federate. | `wso2 apim connect … --client-id <public client> --replace`, then `wso2 login --only apim`. |
| `auth.narrowing_unavailable` at an `apim` command, naming the six `apim:*` permissions and an administrator | API Manager signed the user in and mapped no role, so it issued none of them. | Map the user's ThunderID group to a role carrying them with `apim bootstrap --login-provider … --map-group <group>=<role>` (section 3), then `wso2 login --only apim`. |
| `auth.narrowing_unavailable` at an `iam` command | The user holds no role granting `system`. | Assign one, then log the identity out and in. |
| `auth.session_required` under `--no-input` | The product has no session yet and no browser may open. | `wso2 login --only <product>` where a browser can. |
| `auth.reauthorization_required` under `--no-input` | The product's stored session can no longer be renewed. | The same. |

**Access-token expiry was not measured.** API Manager issues a one-hour
access token and the run did not span one. The path it would exercise is
the same first-use acquisition section 4 describes, but that is an
inference.
