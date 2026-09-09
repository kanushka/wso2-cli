# Single login: product support and initial setup

Checked 2026-09-06. This follow-up uses official documentation, released source, and read-only inspection of the running exercise containers. It does not use earlier exercise research as evidence. Configuration fragments below are setup templates, not changes already applied or an end-to-end tested deployment.

## Decision

Implement one shell login with target-specific access acquisition. The products provide the necessary building blocks for a configured single-login environment. A universal, stock, secretless RFC 8693 exchange path across every product is **not confirmed** and must not be advertised as working out of the box.

There are two supported architectural routes to preserve one credential entry:

1. A resource server accepts a central provider's JWT directly. Thunder and Platform Gateway document this for their management APIs.
2. A target issues its own token after federated authorization-code login. The shell coordinates that flow against the shared identity provider and stores each renewable grant. IS, Asgardeo and APIM have the required federation/OAuth mechanisms. Browser redirects or consent may occur during initial acquisition; future interaction depends on session, grant lifetime and policy.

Exchange is an additional acquisition strategy, not the prerequisite for single login. If the requirement is absolutely no browser interaction after the first login completes, acquire the configured target grants during initial login or validate a suitable exchange route for each target.

## Verified scope

| Product | Confirmed capability | Qualification |
| --- | --- | --- |
| Thunder v1.0.1 | Native/public PKCE; direct external-JWT validation on its own APIs; management permissions extracted from verified token | Configure trusted issuer, exact audience and scopes. OU-sensitive operations also need a matching `ouId`. Some operations require root `system`; do not grant it universally. |
| IS | Public authorization code + PKCE and refresh; API-resource authorization and application roles; federated login; token exchange | Test selected management endpoints and the exact installed release. Public exchange policy is a separate gate from application type. |
| Asgardeo / WSO2 Identity Platform | Public PKCE; management API authorization and roles; external connections; trusted-token-issuer exchange | Tenant/organization scope matters. Hosted exchange policy cannot be changed using a local deployment.toml. Public exchange and organization-switch flows remain deployment checks. |
| APIM 4.7.0 | Public authorization code/refresh; external OIDC federation with role mapping; management-scope processing for exchange and JWT bearer | Installed exchange grant is not public-client enabled. Public JWT bearer is enabled. Dedicated CLI client and upstream trust must still be provisioned. |
| Platform Gateway 1.2.0 docs | External JWT/JWKS authentication and role mapping on Gateway Controller management API | Different API from APIM Publisher. Roles must be explicitly configured. Documented fragment does not establish audience enforcement; verify before production. |

Local observations: `cli-thunder3` reports `v1.0.1` in `/opt/thunderid/version.txt`; `cli-apim` contains `wso2am-4.7.0` and OAuth component `6.14.14`. `cli-thunder` and `cli-thunder2` use `1.0.0-beta` image tags. Do not apply v1.0.1 assumptions to those beta instances. No IS or Platform Gateway container appeared in the running-container inventory. No cloud tenant was tested. No product configuration was changed.

## Initial setup: common inputs

An existing authorized product administrator performs setup once per environment. Routine CLI users should not need these bootstrap credentials.

Record for each target: product/version, API base URL, tenant or OU, trusted login issuer, discovered authorization/token/JWKS endpoints, CLI client ID, redirect URI, expected access-token audience, permitted scopes, and group/role mappings. Use stable reachable hostnames and trusted TLS; container-local `localhost` is not the host machine or another container.

Choose one primary login authority for the environment: Thunder, IS, or Asgardeo. Register a dedicated public OIDC CLI application using authorization code, S256 PKCE and refresh where supported. Use an explicitly registered loopback IP callback, for example `http://127.0.0.1:49173/callback`; the port is an illustrative fixed choice, not an existing CLI setting. The shell must implement state validation, a one-use callback and secure token storage. Do not reuse a portal application's client or distribute a shared secret.

If other products issue their own tokens, create their dedicated public CLI applications too and configure their login flows to use the same upstream identity. A server-side federation client secret belongs on the product server; it is distinct from a secret embedded in the public CLI.

## Thunder as a management target

Thunder's released guide explicitly describes external tokens calling Thunder's own APIs without reissuing tokens or duplicating the user directory. Configure `deployment.yaml` and restart Thunder:

```yaml
server:
  security:
    trusted_issuer:
      issuer: "https://identity.example.com/EXACT-ISSUER"
      jwks_url: "https://identity.example.com/ACTUAL-JWKS-ENDPOINT"
      audience: "https://thunder.example.com"
      required_claims:
        - claim: "ouId"
          value: "TARGET-THUNDER-OU-ID"
```

Replace every placeholder. The upstream issuer must actually issue the configured audience and claims. The `required_claims` check is an admission condition, not an OU mapper. Use a verified, administrator-controlled attribute for the local OU value; do not allow users to choose it.

At the upstream provider, authorize an API resource representing Thunder. Start with `system:user:view` for user listing and include the correct `ouId`. The released API permission map assigns `GET /users` that permission. With a configured system-permission prefix, use the corresponding prefixed value instead. Broad `system` grants access to all system operations and bypasses ordinary OU policy checks; reserve it for intentional full administrators.

Thunder's non-root permission-grant checks may consult local entity permissions, so direct external JWT validation is not proof that every role/membership mutation works without local identity provisioning. Test those commands separately. Do not solve a failed granular permission check by automatically assigning `system`.

When Thunder is the issuer, use its resource-server API to define a URI identifier for the target and authorize users through roles. Request the target via `resource`. Thunder supports one target resource per token. When IS or Asgardeo is the issuer, configure the actual access-token audience through a supported mechanism and verify the result; the OIDC settings' **ID Token Audience** field is not evidence of access-token audience configuration.

Changing Thunder Console login is optional for CLI use. If desired, its separate `trusted_issuer` runtime configuration must also be set. The server trust block alone does not change Console login.

Sources: [trusted issuer guide](https://thunderid.dev/docs/v1.0.x/guides/trusted-issuer/), [released JWT authenticator](https://github.com/thunder-id/thunderid/blob/v1.0.1/backend/internal/system/security/jwt_authenticator.go), [permission map](https://github.com/thunder-id/thunderid/blob/v1.0.1/backend/internal/system/security/permissions.go), [system authorization](https://github.com/thunder-id/thunderid/blob/v1.0.1/backend/internal/system/sysauthz/service.go), [grant checks](https://github.com/thunder-id/thunderid/blob/v1.0.1/backend/internal/system/sysauthz/grant.go), [resource setup](https://github.com/thunder-id/thunderid/blob/v1.0.1/docs/versioned_docs/version-v1.0.x/guides/resource-servers.mdx), [PKCE](https://thunderid.dev/docs/v1.0.x/guides/protocols/oauth-oidc/pkce/).

## IS and Asgardeo as management targets

For each target tenant:

1. Register the dedicated CLI OIDC application as public; enable Code, Refresh Token and mandatory S256 PKCE; register its loopback callback.
2. Under **API Authorization**, authorize the exact management API resources/scopes used by the module. For example, application listing uses `internal_application_mgt_view`. API definitions determine scope names; root `internal_*` and child-organization `internal_org_*` APIs are not interchangeable.
3. Create a CLI application role carrying those selected permissions. Assign the intended local user/group or mapped external IdP group. Both the application and the user's role must authorize the requested scope.
4. If another provider is the primary login authority, add it as an OIDC/SAML Connection, configure claim/group mappings and JIT provisioning where required, and select that Connection in the CLI application's login flow. Link identities securely; do not match an administrator by an unverified email.
5. Have the shell perform authorization code + PKCE at this target's tenant endpoint. The upstream session supplies SSO; the target issues its own management token. Store the returned refresh grant in the shell.

Console Administrator roles and REST API application roles are separate. Console admin SSO alone does not authorize the CLI. Likewise, an M2M management token represents application authority and is not evidence that the signed-in user's permissions were enforced.

Asgardeo endpoints are tenant-specific, e.g. `https://api.asgardeo.io/t/<organization>/oauth2/authorize` and `/oauth2/token`. Use discovery and the API definitions for actual URLs. For child-organization APIs, share the app and assign organization roles as documented. Parent-managed invited users may require Organization Switch; do not claim its secretless support from the confidential-client example.

For exchange, register a **Trusted Token Issuer**, configure issuer/JWKS and intended assertion audience, enable Token Exchange on the requesting app, and require linked local-account authorization where management rights depend on local roles. Current examples use client authentication; mobile-app availability alone does not establish public exchange. A grant handler's `isConfidentialClient()` default also does not by itself disprove public support: the public-client authenticator can mark a request authenticated when its grant policy permits it. Inspect the deployed grant policy.

Sources: [public PKCE flow](https://is.docs.wso2.com/en/latest/guides/authentication/oidc/implement-auth-code-with-pkce/), [API authorization and roles](https://wso2.com/identity-platform/docs/guides/authorization/api-authorization/api-authorization/), [role assignment](https://wso2.com/identity-platform/docs/guides/users/manage-roles/), [Console roles](https://wso2.com/identity-platform/docs/references/user-management/user-roles/), [OIDC client settings](https://wso2.com/identity-platform/docs/references/app-settings/oidc-settings-for-app/), [IS exchange](https://is.docs.wso2.com/en/7.2.0/guides/authentication/configure-token-exchange/), [Asgardeo exchange](https://wso2.com/identity-platform/docs/guides/authentication/configure-token-exchange/), [organization API setup](https://wso2.com/identity-platform/docs/apis/organization-apis/authentication/).

## APIM as a management target

Baseline setup:

1. On the upstream provider, register APIM's server-side federation application with callback `https://<apim-host>/commonauth`. Keep its credentials on APIM.
2. On APIM, register the upstream OIDC IdP, configure its discovered endpoints and TLS trust, map its groups to local roles, and enable the required provisioning. The official example maps `publisher` to `Internal/publisher` and `groups` to `http://wso2.org/claims/role`; choose mappings matching the actual commands.
3. Register a **separate** CLI service provider in APIM, with the CLI loopback callback, authorization code/refresh grants, mandatory PKCE, and **Allow authentication without the client secret**. Select the upstream federated IdP for this CLI service provider. The portal federation guide provides the pattern; this dedicated-CLI combination still needs a live management call.
4. The shell acquires an APIM-issued token using the shared SSO session and calls APIM's management API with the relevant `apim:*` scopes.

### Browserless alternatives and the exact public-client gate

Read-only inspection of installed `repository/conf/identity/identity.xml` found:

| Grant | PublicClientAllowed |
| --- | --- |
| authorization_code | true |
| refresh_token | true |
| urn:ietf:params:oauth:grant-type:jwt-bearer | true |
| urn:ietf:params:oauth:grant-type:token-exchange | absent |

The installed OAuth `6.14.14` source constructs its public grant list from `PublicClientAllowed` and the public authenticator rejects grants outside that list. Thus the current stock exchange block does not provide the public CLI path, even though the exchange handler exists. This is more precise than claiming APIM cannot support public clients.

Two candidates can be evaluated:

- **JWT bearer (RFC 7523):** Already marked public-client allowed in this deployment. Configure trusted assertion issuer/signing keys, an appropriate audience/alias and role mappings; allow the JWT bearer grant on the dedicated public CLI app. Submit the eligible upstream JWT as `assertion`, plus `client_id` and the requested management scopes. This is distinct from RFC 8693 `subject_token`. Product support is confirmed; the complete mapping/client combination is not live tested.
- **RFC 8693 configured for public clients:** The installed custom-grant template supports arbitrary grant properties, including `PublicClientAllowed`. A candidate configuration is below. This changes server policy and requires integration/security validation; it is not an instruction to enable public client credentials or all grants. Disable the duplicate built-in block and register the same handler once.

```toml
# Candidate for APIM 4.7.0; not applied or live tested.
[oauth.grant_type.token_exchange]
enable = false

[[oauth.custom_grant_type]]
name = "urn:ietf:params:oauth:grant-type:token-exchange"
grant_handler = "org.wso2.carbon.identity.oauth2.grant.token.exchange.TokenExchangeGrantHandler"
grant_validator = "org.wso2.carbon.identity.oauth2.grant.token.exchange.TokenExchangeGrantValidator"

[oauth.custom_grant_type.properties]
PublicClientAllowed = true
IsRefreshTokenAllowed = true
IATValidityPeriod = "1h"
```

Check the rendered grant block and restart in an isolated test deployment. Do not rely solely on the legacy `oauth.public_client_support.grant_type_names` template: inspection of the pinned configuration parser did not establish that it populates the effective public grant list. Verify actual token-endpoint behavior.

Sources: [APIM federation](https://apim.docs.wso2.com/en/latest/install-and-setup/setup/sso/configuring-identity-server-as-external-idp-using-oidc/), [PKCE/public configuration](https://apim.docs.wso2.com/en/latest/install-and-setup/setup/security/securing-api-m-web-portals/#bypass-client-credentials-by-making-pkce-mandatory), [JWT bearer](https://apim.docs.wso2.com/en/latest/api-security/key-management/authentication/grant-types/jwt-grant/), [public authenticator](https://github.com/wso2-extensions/identity-inbound-auth-oauth/blob/v6.14.14/components/org.wso2.carbon.identity.oauth/src/main/java/org/wso2/carbon/identity/oauth2/client/authentication/PublicClientAuthenticator.java), [grant policy parser](https://github.com/wso2-extensions/identity-inbound-auth-oauth/blob/v6.14.14/components/org.wso2.carbon.identity.oauth/src/main/java/org/wso2/carbon/identity/oauth/config/OAuthServerConfiguration.java), [management scope issuer](https://github.com/wso2/carbon-apimgt/blob/v9.33.122/components/apimgt/org.wso2.carbon.apimgt.impl/src/main/java/org/wso2/carbon/apimgt/impl/issuers/SystemScopesIssuer.java), [exchange extension](https://github.com/wso2-extensions/identity-oauth2-grant-token-exchange/blob/v1.3.0/README.md).

## Platform Gateway as a management target

For the documented 1.2.0 configuration, the controller can validate an upstream JWT directly. This is specifically its management API, not a policy on a proxied business API:

```yaml
controller:
  auth:
    basic:
      enabled: false
    idp:
      enabled: true
      issuer: "https://identity.example.com/EXACT-ISSUER"
      jwks_url: "https://identity.example.com/ACTUAL-JWKS-ENDPOINT"
      roles_claim: "groups"
      role_mapping:
        admin: ["gateway-admins"]
        developer: ["api-developers"]
```

Configure the issuer to include those controlled group values in the **access token**. Do not add wildcard administrator mapping. The docs say omitting `roles_claim` bypasses role authorization, while disabling both authentication methods opens the controller. The documented configuration lacks an audience option; absence from docs is not proof that the implementation lacks validation. Audience rejection must be verified in the chosen binary before calling this a production-ready setup. Use an enforcing boundary or a verified product fix if the controller does not reject wrong-audience tokens.

Source: [Gateway 1.2.0 management authentication](https://wso2.com/api-platform/docs/api-gateway/1.2.0/gateway-controller-management-api/authentication/).

## Repeatable bootstrap deliverable

The initial setup should be packaged as an administrator-run provisioning workflow, separate from ordinary `wso2 login`. This is a proposal, not an existing command:

1. **Inspect:** Read installed versions, endpoints, current clients, trust and roles. Abort on incompatible versions or ambiguous tenant mappings.
2. **Prepare:** Generate named client/connection/role configurations and server fragments from non-secret environment inputs. Keep bootstrap credentials in the credential store or a protected input channel.
3. **Apply:** Reconcile only the integration's named resources; do not reset existing product configuration. Preserve unrelated grants and roles. Save a rollback record of changed non-secret settings.
4. **Verify:** Confirm granted scopes and execute real read-only management calls, plus denied cases. A token-endpoint success is insufficient.
5. **Export:** Write a non-secret CLI environment profile containing the target/client/acquisition metadata. Remove temporary bootstrap credentials from runtime CLI configuration.

For a hosted Asgardeo tenant, use its Console or authorized management APIs. Local server configuration fragments cannot modify the hosted token service.

## Acceptance test before declaring the environment ready

Use one authorized user and one user without management rights. Count credential prompts and browser redirects separately. Starting without cached tokens, run `wso2 login`, then one real management read on every configured target. Repeat after shell restart and access-token expiry. Verify refresh and logout behavior; offline JWT validation does not imply instant upstream revocation propagation.

Reject wrong issuer, wrong audience, missing role/scope, wrong tenant/OU, expired token, and a modified signature. Check that a user cannot obtain administrator permissions by merely requesting a scope. Test role/membership mutations separately where authorization performs local identity lookups. Do not replace user-delegated tokens with M2M admin credentials to make the test pass.

The evidence currently confirms the product mechanisms and identifies the APIM configuration gap. It does not establish that the existing CLI or all deployed tenants already satisfy this acceptance test.
