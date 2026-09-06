# WSO2 CLI single-login feasibility

Date: 2026-09-05. Fresh official documentation and released-source review; existing exercise research was not used as evidence. No end-to-end product test was performed for this investigation.

Follow-up: [2026-09-06 product setup and compatibility](2026-09-06-single-login-product-setup.md) adds released Thunder external-JWT management support, Platform Gateway management configuration, and read-only confirmation that the installed APIM exchange block is not public-client enabled. Use that follow-up for setup and the more precise public-client assessment. Neither document constitutes an end-to-end tested deployment.

## Conclusion

One interactive login followed by commands against multiple configured products is a feasible architecture. Separate product access tokens do not require separate credential entry. Use token exchange where supported for the registered client; otherwise use federated authorization-code flows with a shared browser session. Arbitrary independent installations do not acquire mutual trust merely because they are WSO2 products.

## Primary evidence

- **IS 7.2:** Documents federated and locally issued token exchange. Issuer trust, client configuration and local-account authorization remain prerequisites. Examples authenticate the requesting client with a secret; these examples alone do not establish secretless native-CLI exchange. [Official guide](https://is.docs.wso2.com/en/7.2.0/guides/authentication/configure-token-exchange/).
- **Asgardeo / WSO2 Identity Platform:** Documents trusted token issuers, exchange grants and linked local accounts for RBAC. This supports exchange feasibility, not automatic access to every management API. [Official guide](https://wso2.com/identity-platform/docs/guides/authentication/configure-token-exchange/).
- **Thunder v1.0.1:** Exchange intersects requested scopes with the subject token's scopes and filters permission scopes for the target resource. A Thunder exchange cannot create missing APIM permissions. This does not constrain an exchange performed at APIM's own authorization server. [Released handler](https://github.com/thunder-id/thunderid/blob/v1.0.1/backend/internal/oauth/oauth2/granthandlers/token_exchange.go). Resource indicators target one resource. [Resource guide](https://github.com/thunder-id/thunderid/blob/v1.0.1/docs/versioned_docs/version-v1.0.x/guides/protocols/oauth-oidc/resource-indicators.mdx).
- **Thunder SSO:** Apps using the same authentication flow can reuse its browser session; distinct flows do not share sessions. [Versioned SSO guide](https://github.com/thunder-id/thunderid/blob/v1.0.1/docs/versioned_docs/version-v1.0.x/guides/flows/single-sign-on.mdx).
- **APIM public-client correction:** Official documentation explicitly allows authorization code with mandatory PKCE and authentication without a client secret. A blanket claim that APIM cannot support public clients is incorrect. A dedicated CLI application still needs configuration and validation. [Official PKCE configuration](https://apim.docs.wso2.com/en/latest/install-and-setup/setup/security/securing-api-m-web-portals/#bypass-client-credentials-by-making-pkce-mandatory).
- **APIM SSO:** Documents IS as an OIDC identity provider, JIT provisioning and remote-to-local role mappings. Applying this pattern to a dedicated CLI application is a proposed integration, not a live-tested result. [Official federation guide](https://apim.docs.wso2.com/en/latest/install-and-setup/setup/sso/configuring-identity-server-as-external-idp-using-oidc/).
- **APIM management token exchange:** APIM v4.7.0 pins carbon-apimgt 9.33.122 and token-exchange extension 1.3.0. Its SystemScopesIssuer explicitly handles TOKEN_EXCHANGE, maps external roles, and authorizes requested REST API scopes. This is source evidence of a management-token exchange path, beyond gateway key-manager integration. [Release dependencies](https://github.com/wso2/product-apim/blob/v4.7.0/all-in-one-apim/pom.xml), [SystemScopesIssuer](https://github.com/wso2/carbon-apimgt/blob/v9.33.122/components/apimgt/org.wso2.carbon.apimgt.impl/src/main/java/org/wso2/carbon/apimgt/impl/issuers/SystemScopesIssuer.java).

## Proposed CLI behavior

The shell owns login and token acquisition. Modules request a token for a specific configured product, tenant, resource and scope set. The shell uses a valid cached token, refreshes an existing grant, exchanges an eligible token, or runs authorization code with PKCE against a federated target. The last option may open a browser without requiring credential entry again. Consent, expired sessions and stronger authentication policy can still require interaction.

Configure issuer trust, audiences, client registrations and authorization mappings once per environment. Start with IS and APIM because their federation path is documented; then validate Asgardeo and Thunder separately rather than assuming symmetric interoperability.

## Security requirements and unresolved checks

- Bind tokens and caches to issuer, tenant, client, resource and granted scopes. Validate actual returned scopes before executing privileged commands.
- Keep refresh credentials in the OS credential store and expose only the appropriate short-lived token to each module. In-process modules are not a security isolation boundary.
- Do not ship a shared client secret in the CLI. Public native applications cannot keep such a secret confidential. [RFC 8252 section 8.5](https://www.rfc-editor.org/rfc/rfc8252.html#section-8.5).
- Prefer audience-restricted product tokens. A broad shared bearer token increases the consequences of token theft. [RFC 9700 section 2.3](https://www.rfc-editor.org/rfc/rfc9700.html#section-2.3).
- Verify public-client RFC 8693 support independently of PKCE support. PKCE secures the authorization-code flow; it does not establish exchange client-authentication policy.
- Verify management authorization, account linking, role mapping, tenant boundaries, refresh, revocation and logout on real installations. Successful token issuance alone is insufficient.

## Next proof

With a fresh IS 7.2 and APIM 4.7 environment, register dedicated CLI applications and configure federation. Count user credential prompts while running one IAM management command and one APIM Publisher management command. Repeat after restarting the CLI and after access-token expiry. Separately test APIM token exchange with a trusted upstream token, checking issuer, audience, role mapping and client authentication. Repeat the matrix with Asgardeo and Thunder. Include denied-role and wrong-tenant cases.
