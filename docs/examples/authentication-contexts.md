# Authentication context examples

**Status:** Proposed examples
**Related:** [Architecture](../architecture.md)

These examples illustrate how WSO2 CLI contexts describe authentication without
containing credentials. The field names are a proposed shape until the context
schema is finalized.

## Security rules

- Contexts contain target metadata, an authentication method, and non-secret
  references only.
- A `credentialRef` is an opaque reference to an interactive credential in the
  OS secure store; it is not the credential itself.
- Fields ending in `Variable` contain environment-variable names, not their
  values.
- Access tokens, refresh tokens, personal access tokens, passwords, client
  secrets, and private keys never appear in context files.
- Interactive long-lived credentials are stored in the OS secure store.
- CI injects secrets from its secret store. The CLI reads them into job memory
  and does not persist them.

## 1. Cloud browser OAuth/OIDC with PKCE

This is the default interactive developer login for WSO2 Cloud.

```yaml
apiVersion: cli.wso2.com/v1
kind: WSO2Config
defaultContext: cloud-dev

contexts:
  - name: cloud-dev
    type: cloud
    cloud:
      region: us
      org: acme
      project: retail-dev
    auth:
      method: browser-pkce
      credentialRef: keychain://wso2/cloud-dev
```

Usage:

```shell
wso2 login --context cloud-dev
```

The CLI opens a browser and uses the OAuth 2.0/OIDC Authorization Code flow
with PKCE. Any resulting long-lived interactive credential is stored at the
secure-store reference, not in this file.

## 2. Cloud device authorization

Use device authorization for an interactive developer working on a headless or
remote machine.

```yaml
apiVersion: cli.wso2.com/v1
kind: WSO2Config
defaultContext: cloud-remote

contexts:
  - name: cloud-remote
    type: cloud
    cloud:
      region: eu
      org: acme
      project: retail-dev
    auth:
      method: device-code
      credentialRef: keychain://wso2/cloud-remote
```

Usage:

```shell
wso2 login --context cloud-remote --device-code
```

The terminal displays verification instructions. The user approves the request
in a browser on another device. Device authorization remains interactive and
must not be used by CI.

## 3. On-premises browser OAuth/OIDC

Use browser login only when the selected on-premises deployment exposes its own
configured OAuth/OIDC identity provider.

```yaml
apiVersion: cli.wso2.com/v1
kind: WSO2Config
defaultContext: customer-api-dev

contexts:
  - name: customer-api-dev
    type: onprem
    products:
      api:
        endpoint: https://api.dev.customer.example
        auth:
          method: browser-oidc
          issuer: https://login.customer.example
          clientId: wso2-cli
          scopes:
            - openid
            - profile
            - offline_access
          credentialRef: keychain://wso2/customer-api-dev/api
```

Usage:

```shell
wso2 login --context customer-api-dev
```

The issuer, public client identifier, and scopes are non-secret deployment
metadata. The CLI must not assume that an on-premises deployment uses WSO2
Cloud SSO or WSO2 Identity Server.

## 4. On-premises Personal Access Token

This example names an environment variable supplied by a local secret manager
or CI secret store. It does not contain the token.

```yaml
apiVersion: cli.wso2.com/v1
kind: WSO2Config
defaultContext: customer-api-pat

contexts:
  - name: customer-api-pat
    type: onprem
    products:
      api:
        endpoint: https://api.customer.example
        auth:
          method: personal-access-token
          tokenVariable: WSO2_API_PAT
```

Usage:

```shell
# WSO2_API_PAT is injected by the secret store.
wso2 login --context customer-api-pat --no-input
```

For an interactive local context, the implementation may use a
`credentialRef` to an OS-secure-store entry instead of `tokenVariable`. The
context still never stores the token value.

## 5. On-premises client credentials

Client credentials are intended for non-interactive automation when supported
by the selected deployment.

```yaml
apiVersion: cli.wso2.com/v1
kind: WSO2Config
defaultContext: customer-api-ci

contexts:
  - name: customer-api-ci
    type: onprem
    products:
      api:
        endpoint: https://api.customer.example
        auth:
          method: client-credentials
          tokenEndpoint: https://login.customer.example/oauth2/token
          clientIdVariable: WSO2_API_CLIENT_ID
          clientSecretVariable: WSO2_API_CLIENT_SECRET
          scopes:
            - api:read
            - api:write
```

Usage:

```shell
# Both variables are injected by the CI secret store.
wso2 login --context customer-api-ci --no-input
```

The variable names, token endpoint, and scopes are non-secret metadata. The
client secret remains in the CI job's memory and is never written to the
context, filesystem, OS secure store, or module environment.

## CI guidance

CI is non-interactive and must use:

- a Personal Access Token; or
- client credentials.

A future workload-identity method may be added separately. CI must never start
browser Authorization Code or Device Authorization flows.
