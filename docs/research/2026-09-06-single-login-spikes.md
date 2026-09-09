# Single login spikes: API Manager federation and machine tokens

**Status:** Measured, 2026-09-06
**Decides:** section 11 step 0 of
[One login, one session per product](https://github.com/wso2/wso2-cli/issues/167)
**Against:** `cli-thunder3` (ThunderID v1.0.1, http://localhost:8492) and
`cli-apim` (API Manager 4.7.0, https://localhost:9443)

Three questions, three answers:

| Question | Answer | Consequence for the design |
| --- | --- | --- |
| Does a public CLI client on API Manager, federated to ThunderID, answer an authorization code flow with a management-scoped token, with no second credential prompt? | **Yes.** One prompt at the ThunderID sign-in; the API Manager authorization completes through the sign-on cookie in five redirects and no prompt. The token carries `apim:api_view`, the publisher answers 200. A user without the mapped group gets `openid` only and 401. | `federated` is API Manager's default strategy for interactive use. |
| Does ThunderID grant `system` to a client-credentials client? | **Yes.** An m2m application holding a role with the `system` permission on the seeded system resource server receives `scope system`, `aud https://localhost:8090/mcp`, and `GET /users` answers 200. | `iam` administration works from CI with one machine client. |
| Does API Manager accept a ThunderID machine token as a jwt-bearer assertion? | **No.** Every ThunderID access token is typed `at+jwt`, and API Manager's jwt-bearer grant refuses it with "Signature validation failed", whatever its audience. Only tokens typed `JWT` (ThunderID's ID tokens) pass. A machine client has no ID token. | CI reaches API Manager management with a product-level secret on the `apim` record (spec section 8's fallback). `derived` stays an interactive strategy. |

## 1. Federation setup that produced the result

Nothing here changes product code. Since 2026-09-07 (#164) `wso2 iam apps
create --type federation` applies the ThunderID part and `wso2 apim
bootstrap --login-provider` the API Manager part; the key manager stays
with `wso2 apim key-managers add`.

On ThunderID, a confidential application `apim-federation` on the console
flow family (authentication flow `…0068`, registration `…0069`, recovery
`…0070`), redirect `https://localhost:9443/commonauth`, grants
authorization code and refresh, `client_secret_post`, ID token attributes
`email`, `groups`, `name`, scope claims `email` and `groups`. The create
request needs `ouId` and `type`; without `ouId` ThunderID answers a bare
"invalid request format" (APP-1018) and only names the missing type once
`ouId` is present.

On API Manager, the existing identity provider `Thunder3` gained an
OpenID Connect federated authenticator: client `apim-federation`,
authorization endpoint `http://localhost:8492/oauth2/authorize` (the
browser's address), token and userinfo endpoints through
`host.docker.internal`, callback `https://localhost:9443/commonauth`, and
`commonAuthQueryParams` set to `scope=openid email groups&resource=
https://localhost:9443/oauth2/token`. The resource parameter is required:
ThunderID refuses an authorization without one and this deployment has no
default resource server. The existing claim mapping (`groups` to the local
role claim, role mapping `Administrators` to `admin`) is unchanged.
**Just-in-time provisioning must be on** (`PRIMARY`, silent): without it
the federated user's mapped roles do not reach the scope issuer and the
token carries `openid` alone. Updating an identity provider that a service
provider already references requires the default authenticator entry to
carry `enabled=true`, or the update fails with "Error in disabling default
federated authenticator".

A new service provider `wso2-cli-sso`, registered through dynamic client
registration with the four loopback callbacks, then set to public with
mandatory S256 PKCE and JWT tokens (`OAuthAdminService`), and its
authentication set to one federated step through `Thunder3`
(`IdentityApplicationManagementService`), consent skipped.

## 2. The measured run

The browser was played with one cookie jar; ThunderID's login page with
its flow API; a credential prompt is counted when the flow asks for
inputs.

```text
== 1. wso2 login: Thunder authorization for wso2-cli (system resource)
  hop 1: http://localhost:8492/oauth2/authorize
  hop 2: thunder login page
  thunder: credentials prompt #1
  thunder token: openid system ok
== 2. apim command: authorization for DgP2V4Arw9KYeo2ltIm4r8r19vca at API Manager, federated
  hop 1: https://localhost:9443/oauth2/authorize
  hop 2: http://localhost:8492/oauth2/authorize
  hop 3: thunder login page
  thunder: flow answered from the sign-on session, no prompt
  hop 4: https://localhost:9443/commonauth
  hop 5: https://localhost:9443/oauth2/authorize
  apim token scope: apim:api_view openid
  aud: DgP2V4Arw9KYeo2ltIm4r8r19vca sub: 01900000-0000-7000-8000-000000000030 iss: https://localhost:9443/oauth2/token
  publisher apis: HTTP 200
```

As `cliuser`, who is in no ThunderID group: the same five hops, no
prompt, `apim token scope: openid`, publisher 401.

## 3. Machine tokens

`wso2-cli-ci`, an m2m application with client credentials and token
exchange, assigned the role `CLI Machine` (permission `system` on the
system resource server; assignment type is `app`, not `application`):

```text
client_credentials, resource=https://localhost:8090/mcp, scope=system
  scope: system   aud: https://localhost:8090/mcp
  GET /users -> HTTP 200
```

Against API Manager's jwt-bearer grant, presented as the public client
`wso2-cli-public`:

| Assertion | Header `typ` | API Manager's answer |
| --- | --- | --- |
| machine token bound to `https://localhost:9443/oauth2/token` | `at+jwt` | Signature validation failed |
| machine token exchanged to that resource | `at+jwt` | Signature validation failed |
| user access token | `at+jwt` | Signature validation failed |
| ID token for `apim-federation` | `JWT` | audience does not match the alias `wso2-cli` |
| ID token for `wso2-cli` (cliuser) | `JWT` | issued, `scope default` |

Same signing key throughout (`kid QafRmysf…`), so the refusal is the
token type, not the key. Nothing in the API Manager log explains it
further at the default log level.

## 4. Environment as left

- ThunderID `cli-thunder3`: applications `apim-federation` (confidential)
  and `wso2-cli-ci` (m2m), role `CLI Machine`. Secrets are test values in
  the session scratchpad, not in any repository file.
- API Manager `cli-apim`: identity provider `Thunder3` now federates
  through OpenID Connect with just-in-time provisioning; service provider
  `wso2-cli-sso` (client `DgP2V4Arw9KYeo2ltIm4r8r19vca`, public, PKCE,
  federated). A provisioned local user for the ThunderID admin now exists
  in `PRIMARY`.
- The replay script is `~/dev/wso2/cli-exercise-3/spike-0a.sh`; it takes
  the API Manager client id in `CID` and the ThunderID user in
  `THUNDER_USER`/`THUNDER_PASS`.
