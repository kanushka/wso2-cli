# The experience today and with one login

**Status:** Target user experience, alongside two measured baselines
**Date:** 2026-09-06
**Authoritative:** [One login, one session per product](../superpowers/specs/2026-09-06-one-login-many-sessions-design.md)
**Evidence:** [CI pipelines without the shell](../research/2026-09-06-ci-pipelines-without-wso2-cli.md) ·
the exercise records `iam-apim-journey-findings.md` and
`HANDOFF-auth-derived-grant.md` ·
[Product setup for single login](../research/2026-09-06-single-login-product-setup.md)

One task, told three times: a developer with a ThunderID, an API Manager and
an API of their own registers everything, publishes the API, and calls it
through the gateway. Then the same task from a CI pipeline.

- **Today, product tools**: what a user types with no `wso2` at all. Every
  command shape is quoted from the product documentation cited in the
  research.
- **Today, the shell**: what the exercise branch does, verbatim from the
  measured journey of 2026-09-04 and 2026-09-06.
- **Target**: what the approved design delivers. Not built; not measured.

Where a count matters it is in a table at the end.

---

## 1. A developer on their own machine

### 1.1 Today, product tools

ThunderID has no CLI. Registering the CLI client, the resource server, a
user and a role is either clicking through the console or scripting REST
calls with a `system` token. The token itself comes from replaying the
console's login flow with the administrator password:

```sh
# system token: authorize -> /flow/execute x2 -> callback -> /oauth2/token
TOKEN=$(./thunder-token.sh)          # ~40 lines of curl and jq
curl -H "Authorization: Bearer $TOKEN" http://localhost:8490/resource-servers \
  -d '{"name":"Mock API","identifier":"http://localhost:18080/mockapi", ...}'
curl -H "Authorization: Bearer $TOKEN" http://localhost:8490/users -d '{...}'
curl -H "Authorization: Bearer $TOKEN" http://localhost:8490/roles -d '{...}'
```

API Manager has `apictl`. It logs in with the administrator's username and
password, registers a password-grant client against the resident key
manager, and stores both base64-encoded in `~/.wso2apictl/keys.json`:

```sh
apictl add env dev --apim https://localhost:9443
apictl login dev -u admin -p admin -k
apictl init MockAPI --oas mockapi-openapi.yaml
apictl import api -f MockAPI -e dev -k
```

Wiring ThunderID in as a key manager, creating the application, subscribing
and mapping keys are console clicks or admin REST calls with the same
password-grant token. Calling the API needs a third token, minted by hand
from ThunderID with the resource indicator, then a `curl` to the gateway.

Three credential stores: a script that holds the ThunderID admin password,
`keys.json`, and whatever the developer pasted the gateway token into.

### 1.2 Today, the shell

Measured on the exercise branch. Every product has its own identity, every
identity its own login, and every command names its context.

```text
$ WSO2_IAM_ADMIN_PASSWORD=Admin@123 wso2 iam bootstrap --url http://localhost:8491
Next  Run wso2 identity create thunder-admin --issuer http://localhost:8491 --client-id wso2-cli --provider thunder --product iam --endpoint http://localhost:8491 --audience https://localhost:8090/mcp --scope system, then wso2 login --context thunder-admin.

$ wso2 identity create thunder-admin --issuer http://localhost:8491 --client-id wso2-cli \
    --provider thunder --product iam --endpoint http://localhost:8491 \
    --audience https://localhost:8090/mcp --scope system
$ wso2 login --context thunder-admin                       # browser, credentials
$ wso2 iam resource-servers create "Mock API" --identifier http://localhost:18080/mockapi \
    --permission reference:status:read --permission orders:read
$ WSO2_IAM_USER_PASSWORD='Cli@12345' wso2 iam users create cliuser --email cliuser@example.com
$ wso2 iam roles create "Mock API Caller" --resource-server "Mock API" \
    --permission reference:status:read --permission orders:read --assign-user cliuser

$ export WSO2_CA_FILE=$PWD/apim-9443.pem
$ WSO2_APIM_ADMIN_PASSWORD=admin wso2 apim bootstrap --url https://localhost:9443
Next  export WSO2_APIM_CLIENT_SECRET=… (shown once); then wso2 identity create apim-admin --issuer https://localhost:9443/oauth2/token --client-id fg4d…ASca --client-secret-variable WSO2_APIM_CLIENT_SECRET --product apim --endpoint https://localhost:9443 --audience fg4d…ASca --scope apim:api_view --scope apim:api_create --scope apim:api_publish --scope apim:subscribe --scope apim:app_manage --scope apim:admin

$ export WSO2_APIM_CLIENT_SECRET=…
$ wso2 identity create apim-admin … (the ten-flag line above)
$ wso2 apim apis import --file mockapi-openapi.yaml --name MockAPI --version 1.0.0 \
    --api-context /mockapi --backend http://host.docker.internal:18080 --context apim-admin
$ wso2 apim apis deploy MockAPI/1.0.0 --context apim-admin
$ wso2 apim apis publish MockAPI/1.0.0 --context apim-admin
$ wso2 apim key-managers add Thunder --well-known http://localhost:8491 \
    --jwks http://host.docker.internal:8491/oauth2/jwks --context apim-admin
$ wso2 apim apps create CliApp --context apim-admin
$ wso2 apim apps subscribe CliApp MockAPI/1.0.0 --context apim-admin
$ wso2 apim apps map-keys CliApp --key-manager Thunder --client-id wso2-cli --context apim-admin

$ wso2 identity create thunder-caller --issuer http://localhost:8491 --client-id wso2-cli \
    --provider thunder --product apim --endpoint https://localhost:8243 \
    --audience http://localhost:18080/mockapi --scope reference:status:read --scope orders:read
$ wso2 login --context thunder-caller                      # browser again, credentials again
$ wso2 apim gateway invoke /mockapi/1.0.0/status --scope reference:status:read \
    --scope orders:read --context thunder-caller
GET   https://localhost:8243/mockapi/1.0.0/status   200   {"status":"ok", …}
```

What the user had to know: issuer, client ID, provider, audience, scope,
and grant kind, for each of three identities. The API Manager identity is a
confidential client whose secret sits in the developer's shell environment.
`--context` appears on every product command. Two browser logins, both
asking for credentials, because the CLI client was on a flow without
sign-on nodes until the bootstrap fix landed on 2026-09-05.

### 1.3 Target

```text
$ WSO2_IAM_ADMIN_PASSWORD=Admin@123 wso2 iam bootstrap --url http://localhost:8491
Registered the CLI on http://localhost:8491 and recorded iam on the "thunder" identity.
Next  Run wso2 login.

$ wso2 login
Open this URL to log in: http://localhost:8491/oauth2/authorize?…
Logged in to the "thunder" identity.
  iam    ready   direct
$ wso2 iam resource-servers create "Mock API" --identifier http://localhost:18080/mockapi \
    --permission reference:status:read --permission orders:read
$ WSO2_IAM_USER_PASSWORD='Cli@12345' wso2 iam users create cliuser --email cliuser@example.com
$ wso2 iam roles create "Mock API Caller" --resource-server "Mock API" \
    --permission reference:status:read --permission orders:read --assign-user cliuser

$ export WSO2_CA_FILE=$PWD/apim-9443.pem
$ WSO2_APIM_ADMIN_PASSWORD=admin wso2 apim bootstrap --url https://localhost:9443
Registered the CLI on https://localhost:9443, trusting http://localhost:8491 for sign-on,
and recorded apim on the "thunder" identity.
Next  Run wso2 apim apis import --file <openapi>.

$ wso2 apim apis import --file mockapi-openapi.yaml --name MockAPI --version 1.0.0 \
    --api-context /mockapi --backend http://host.docker.internal:18080
Authorizing apim at https://localhost:9443 through your http://localhost:8491 sign-on…
ID          NAME      VERSION   CONTEXT    STATE     CREATED
e982cccb-…  MockAPI   1.0.0     /mockapi   CREATED   true
$ wso2 apim apis deploy MockAPI/1.0.0
$ wso2 apim apis publish MockAPI/1.0.0
$ wso2 apim key-managers add Thunder --well-known http://localhost:8491
$ wso2 apim apps create CliApp
$ wso2 apim apps subscribe CliApp MockAPI/1.0.0
$ wso2 apim apps map-keys CliApp --key-manager Thunder --client-id wso2-cli

$ wso2 apim connect https://localhost:8243 --gateway \
    --audience http://localhost:18080/mockapi --scopes reference:status:read,orders:read
Recorded the "apim" gateway on the "thunder" identity.
Next  Run wso2 login --only apim.
$ wso2 apim gateway invoke /mockapi/1.0.0/status
Authorizing the gateway for http://localhost:18080/mockapi through your sign-on…
GET   https://localhost:8243/mockapi/1.0.0/status   200   {"status":"ok", …}

$ wso2 whoami
Identity   thunder (http://localhost:8491, admin)
  iam           ready   direct
  apim          ready   federated   https://localhost:9443
  apim/gateway  ready   sibling     http://localhost:18080/mockapi
```

One browser sign-in with credentials. Two more tabs that open, are answered
by the provider's session, and close. No identity flags, no `--context`, no
client secret. The one audience and scope list the user types are the API's
own, once, on the `--gateway` connect; every other value comes from a
module's descriptor. `whoami` says how each record is reached.

The two "Authorizing…" lines are the design's first-use acquisition. A
user who prefers everything up front runs `wso2 login` after both
bootstraps, and it acquires all three in one go.

---

## 2. A CI pipeline

The job: publish a new revision of the API and add a user to a role. No
browser, no person.

### 2.1 Today, product tools

From the published API Manager samples and the ThunderID repository's own
actions:

```yaml
# API Manager: the published Jenkins/Actions samples, password inline
- run: apictl add env dev --apim https://${APIM_DEV_HOST}:9443 -k
- run: apictl login dev -u ${{ secrets.APIM_USERNAME }} -p ${{ secrets.APIM_PASSWORD }} -k
- run: apictl import api -f MockAPI -e dev -k --update

# ThunderID: the project's own composite action replays the console login
- uses: ./.github/actions/obtain-admin-token
  with: { server-url: …, admin-username: admin, admin-password: ${{ secrets.THUNDER_ADMIN_PASSWORD }} }
- run: curl -H "Authorization: Bearer $TOKEN" $SERVER_URL/roles/$ROLE/assignments -d '{…}'
```

Two administrator passwords in the secret store, one password grant, one
scripted user login. Identity Server would add `iamctl` with a third
credential in `serverConfig.json`; Agent Manager would add `amctl login
--client-id --client-secret` with the secret on the command line and then
in `~/.amctl/config.yaml`.

### 2.2 Today, the shell

Measured in exercise 1. A client-credentials identity per product, hand
written or created with the ten-flag command, each with its own secret.

```yaml
env:
  WSO2_NO_INPUT: "1"
  WSO2_THUNDER_CI_SECRET: ${{ secrets.THUNDER_CI_SECRET }}
  WSO2_APIM_CLIENT_SECRET: ${{ secrets.APIM_CLIENT_SECRET }}
steps:
  - run: wso2 identity create thunder-ci --issuer http://thunder:8490 --client-id wso2-cli-ci
           --client-secret-variable WSO2_THUNDER_CI_SECRET --provider thunder
           --product iam --endpoint http://thunder:8490 --audience https://localhost:8090/mcp --scope system
  - run: wso2 identity create apim-ci --issuer https://apim:9443/oauth2/token --client-id fg4d…
           --client-secret-variable WSO2_APIM_CLIENT_SECRET --product apim --endpoint https://apim:9443
           --audience fg4d… --scope apim:api_create --scope apim:api_publish
  - run: wso2 iam roles create "Mock API Caller" --resource-server "Mock API"
           --permission reference:status:read --permission orders:read --assign-user newuser --context thunder-ci
  - run: wso2 apim apis import --file mockapi-openapi.yaml --name MockAPI --version 1.1.0 --context apim-ci
  - run: wso2 apim apis publish MockAPI/1.1.0 --context apim-ci
```

There is no `wso2 iam roles assign`. The built command is `roles create`,
which on a role that already exists adds the assignments named and changes
nothing else, so a job re-states the role's resource server and permissions
to add one user; an `assign` subcommand that takes the role and the user
alone is the target shape, and the target below writes it. Better than the
product tools: no administrator password, secrets only in environment
variables, nothing written to disk. Still two secrets, two
identities, and `--context` everywhere. `wso2 whoami` and `wso2 doctor`
report these identities as needing a login they can never do, and `wso2
logout` fails the job.

### 2.3 Target

One machine client at the login provider, given roles on each product. One
secret. A context document with no secret in it, checked into the
repository or generated by two `connect` commands.

```yaml
env:
  WSO2_NO_INPUT: "1"
  WSO2_CI_CLIENT_SECRET: ${{ secrets.WSO2_CI_CLIENT_SECRET }}
steps:
  - run: wso2 iam connect http://thunder:8490 --client-id wso2-cli-ci --client-secret-variable WSO2_CI_CLIENT_SECRET
  - run: wso2 apim connect https://apim:9443
  - run: wso2 iam roles assign "Mock API Caller" --user newuser   # target; see the note below
  - run: wso2 apim apis import --file mockapi-openapi.yaml --name MockAPI --version 1.1.0
  - run: wso2 apim apis publish MockAPI/1.1.0
  - run: wso2 doctor
```

The shell mints one token per product from the one secret: for `iam` with
ThunderID's resource indicator, for `apim` by presenting that token to API
Manager under the trust the bootstrap configured. Nothing is stored between
steps. `doctor` passes on a machine that has never logged in.

Two things this depends on that are not yet measured, recorded in the CI
research: whether API Manager maps a machine client to the roles that
carry `apim:*` scopes, and whether ThunderID grants `system` to a
client-credentials client. If the first fails, the `apim connect` line
takes its own `--client-secret-variable`, and the pipeline holds two
secrets instead of one. If the second fails, `iam` administration from CI
still needs a user login, as ThunderID's own pipelines do today.

---

## 3. What changes, counted

| | Today, product tools | Today, the shell | Target |
| --- | --- | --- | --- |
| Tools a developer installs | 2 to 4 (`apictl`, `iamctl`, `amctl`, `ap`) plus scripts | 1 | 1 |
| Credential prompts, interactive journey | 3 (admin password twice, gateway token by hand) | 2 browser logins | 1 |
| Identities the user declares | n/a | 3, ten flags each | 0; bootstrap and `connect` record them, the gateway's audience and scopes typed once |
| Terms the user must know | tool-specific | issuer, client ID, provider, audience, scope, grant | product URL |
| Client secrets on the developer machine | 2 (admin password in a script, `keys.json`) | 1 (environment) | 0 |
| Secrets in the CI store | 2 administrator passwords | 2 client secrets | 1 client secret, 2 if the API Manager machine-role gap holds |
| `--context` on product commands | n/a | every command | none with one identity |
| Where "how am I reaching this product" is visible | nowhere | `identity list`, partly | `whoami`, per product, with the strategy |

Interactive counts are for the section 1 journey; CI counts for section 2.
The target column is the design's claim, to be replaced by measured values
when the live matrix in the design's section 9 runs.
