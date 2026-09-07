# One login, many sessions: the live matrix

**Status:** Measured, 2026-09-07
**Records:** section 10 of
`docs/superpowers/specs/2026-09-06-one-login-many-sessions-design.md`
**Against:** `cli-thunder3` (ThunderID v1.0.1, http://localhost:8492),
`cli-apim` (API Manager 4.7.0, https://localhost:9443 management,
https://localhost:8243 gateway) and `cli-is` (Identity Server 7.1.0,
https://localhost:9444)
**Branch:** `claude/apim-journey-step1`, the shell and both product modules
built from the working tree

What was counted: credential prompts and browser redirects, separately.
Every `wso2` invocation is its own process, so every row after the first
is already a measurement across a shell restart.

| Row | Result | Prompts | Redirects |
| --- | --- | --- | --- |
| 1. ThunderID login serving `iam` (direct) | **Pass.** `wso2 iam connect http://localhost:8492` records the product and creates the identity, the context and the login-product pin. | 1 | 2 |
| 2. The same login serving API Manager management (federated) | **Pass.** No second password. `apis list` and `apps list` both answer. | 0 | 5 |
| 2a. First-use acquisition | **Pass.** After `wso2 login --no-products`, `wso2 apim apis list` prints the notice, authorizes through the sign-on and answers. | 0 | 5 |
| 2b. The same under `--no-input` | **Pass.** Refused before anything opens, naming the flag that caused it. The refusal a product with no session gets is `auth.session_required`; one whose stored session cannot be renewed now gets `auth.reauthorization_required` (section 10). | 0 | 0 |
| 3. Identity Server as the login provider | **Pass** for API Manager management, by the `derived` (jwt-bearer) strategy, for the administrator and refused for the group-less user. `iam` and the gateway are not reachable under it. | 1 | 4 |
| 4. Asgardeo | **Not run.** No tenant available. |  |  |
| 5. The gateway (sibling) | **Pass** for the leg the CLI owns. `wso2 login` establishes `iam` direct and `apim` sibling from one prompt, and the gateway answers 200. The mock backend behind it still rejects the token, for a reason outside the shell. | 1 | 4 |
| 5a. The gateway recorded by `connect --gateway` (#163) | **Pass.** `wso2 apim connect https://localhost:8243 --gateway --audience http://localhost:18090/hello --scopes hello:read,orders:read` on the same identity, then one `wso2 login`: `iam direct`, `apim federated`, `apim/gateway sibling`, all established. `gateway invoke /hello/1.0.0/hello` answers 200 through the mock backend with no `--context`. `--no-input` with the gateway session missing refuses `auth.session_required` naming `wso2 login --only apim`. Measured 2026-09-07 from `cli-exercise-4`. | 1 | 9 |
| 6. A user without management rights (`cliuser`) | **Pass.** Both products refuse, each naming what an administrator must grant. | 1 | 7 |
| 7. CI: one machine client, no browser | **Pass** for `iam`. `apim` under a machine identity is refused as designed, naming the two credential flags. | 0 | 0 |

## 1. How the runs were made

A home containing nothing but the two installed modules:

```sh
rm -rf matrix-home && mkdir -p matrix-home/cli
WSO2_HOME=.../matrix-home make install-module NAMESPACE=iam
WSO2_HOME=.../matrix-home make install-module NAMESPACE=apim
```

The modules must be installed from this branch: the shell reads the
product descriptor out of the **module receipt**, and a receipt built
before the descriptor existed makes `connect` refuse with
`shell.connect_unsupported`.

`WSO2_NO_BROWSER=1` makes the shell print the authorization URL instead of
opening it. A script followed each printed URL with one cookie jar for the
whole run, which is what makes the prompt and redirect counts mean
anything: one jar is one browser, so a sign-on established by the first
authorization is available to the second. ThunderID's sign-in page was
answered through its flow API.

## 2. Setup: two commands, not two flag lists

The two eight-flag lines the tryout guide used are gone. What replaces
them:

```sh
wso2 iam connect http://localhost:8492
wso2 apim connect https://localhost:9443 --client-id DgP2V4Arw9KYeo2ltIm4r8r19vca
```

The first creates the `thunder` identity, its context, selects it, records
`iam` direct, and pins `loginProduct: iam`. The second attaches `apim` to
the same identity under a federated grant at
`https://localhost:9443/oauth2/token`. Neither touches the secure store.
Only `--client-id` is typed, because API Manager's public CLI client is
registered per deployment; the descriptor cannot name it.

## 3. Rows 1 and 2: one login, two products

`wso2 login`:

```text
Logged in to the "thunder" context.
Subject    01900000-0000-7000-8000-000000000030
Products   apim, iam
iam        direct, established
apim       federated, established
```

One credential prompt, at ThunderID, for the `iam` authorization: two
redirects. The API Manager authorization then completed through the
sign-on with no prompt, in five redirects. `whoami` reports
`apim: federated, present; iam: direct, present`; `doctor`'s session check
passes only because both products hold one.

`iam users list`, `apim apis list` and `apim apps list` all answer. The
last matters: it needs `apim:subscribe` rather than `apim:api_view`, so it
proves the product record — not the command — is what the session is
authorized for.

**Access-token expiry was not measured.** API Manager issues a one-hour
access token; the run did not span one. The path it would exercise —
API Manager refusing to renew a management scope, and the shell
authorizing again through the sign-on — is the same code that row 2a
exercises on first use, but that is an inference, not a measurement.

## 4. Rows 2a and 2b: first use, and no input

After `wso2 logout --keep-browser-session` and `wso2 login --no-products`,
only the login session exists. Then:

```text
The "apim" product needs to be authorized. Opening the browser to authorize it at https://localhost:9443/oauth2/token.
COUNT   APIS
2       MockAPI/1.0.0 /mockapi (PUBLISHED), MockAPI2/1.0.0 /mockapi2 (PUBLISHED)
```

The same command with `--no-input` on the product command line — the flag
the shell now reads as its own — is refused before anything opens:

```text
error: the "apim" product has no session under this identity yet (auth.session_required)
  Run wso2 login --only apim before this command; --no-input asked that no browser open.
```

## 5. Row 6: the user without management rights

`cliuser` is in no ThunderID group, so API Manager maps no role to it.
Signing in as `cliuser` costs one prompt, and both authorizations
complete: `wso2 login` reports `iam direct, established` and
`apim federated, established`. Establishing a session is not the same as
being authorized, and the refusals arrive at the commands:

```text
$ wso2 iam users list
error: the deployment refused to narrow this session to the permissions the "iam" module asked for (auth.narrowing_unavailable)

$ wso2 apim apis list
error: the "apim" product's identity provider signed this user in again but issued none of the permissions the module asked for (apim:admin, apim:api_create, apim:api_publish, apim:api_view, apim:app_manage, apim:subscribe), so the user is not authorized for the product (auth.narrowing_unavailable)
  Ask an administrator of https://localhost:9443/oauth2/token to map this user's group to a role that carries [...], then run wso2 login --only apim.
```

The second is the message the design asks for: it names the issuer, the
permissions and the administrator action, and carries no token.

## 6. Row 7: CI

```sh
export WSO2_CI_CLIENT_SECRET=...
wso2 iam connect http://localhost:8492 --identity thunder-ci \
  --client-id wso2-cli-ci --client-secret-variable WSO2_CI_CLIENT_SECRET
WSO2_NO_INPUT=1 wso2 iam users list --context thunder-ci
```

No login step, no browser, no session: the strategy is `inline` and
`connect`'s own next line says `wso2 iam status`, not `wso2 login`.
`whoami`, `doctor` and `logout` all exit 0 — `doctor` reports the session
check as `not-applicable`, `logout` as `Session none`.

`apim` under the same machine identity is refused at `connect` time, which
is what section 8 of the design asks for:

```text
error: the apim product does not accept the machine client the "thunder-ci" identity holds, so it needs a credential of its own (auth.product_not_configured)
  Register a client for this CLI on the product (wso2 apim bootstrap does) and pass --client-id <id> --client-secret-variable <VAR> naming its credential [...]
```

The credential path itself is unit-tested but still unproven live: no
API Manager client is registered for it on this deployment.

## 7. Row 5: the gateway, as a sibling

The `sibling` strategy is the login provider issuing for a *different*
resource server than the login one. Here that is a mock API published on
API Manager's gateway whose resource server is registered in ThunderID.

`connect` cannot record this product. `modules/apim/module.json` declares
`"grant": "federated"`, and the shell attaches that grant to every `apim`
record it writes, so `connect` can only ever produce the federated
management shape. The row was recorded the long way:

```sh
wso2 iam connect http://localhost:8492 --identity thunder-gw
wso2 identity add-product thunder-gw apim \
  --endpoint https://localhost:8243 \
  --audience http://localhost:18080/mockapi \
  --scopes reference:status:read,orders:read
```

`iam connect` first is what makes the row correct rather than inverted:
it pins `loginProduct: iam`. Recorded with `wso2 identity create`, which
writes no pin, `apim` would sort before `iam` and become the login
product, making `iam` the sibling — the exact drift the pin exists to
stop, visible here as a setup mistake rather than a theory.

`wso2 login --context thunder-gw`:

```text
iam        direct, established
apim       sibling, established
```

One credential prompt, two ThunderID authorizations of two redirects
each; the second answered by the sign-on. Then:

```text
$ wso2 apim gateway invoke /mockapi/1.0.0/health --context thunder-gw --no-input
GET  https://localhost:8243/mockapi/1.0.0/health  200  {"status":"up"}
```

**What that proves and what it does not.** API Manager's gateway accepted
a ThunderID access token: the token reached the gateway, key manager
`Thunder3` validated it, and the subscription and key mapping resolved.
That is the whole of what the shell is responsible for. `/status` on the
same API answers 401 `token_rejected` — `enable_outbound_auth_header` is
on for this deployment, so the gateway forwards the token to the mock
backend, and that backend process is running with
`ISSUER=http://localhost:8490`, an earlier exercise's deployment. Proving
that last leg means restarting the backend for issuer 8492, which would
break the exercise it belongs to; it was deliberately not done. `/health`
is unauthenticated at the backend, which is why it isolates the gateway
leg cleanly.

Two writes to `cli-apim` were needed, both on the JIT-provisioned
federated user's own application. `CliApp`, which the earlier exercise
used, belongs to API Manager's local administrator and is invisible to
this user:

```sh
wso2 apim apps subscribe DefaultApplication MockAPI/1.0.0
wso2 apim apps map-keys DefaultApplication --key-manager Thunder3 \
  --client-id wso2-cli --key-type PRODUCTION
```

ThunderID needed nothing: the `Mock API` resource server
(`http://localhost:18080/mockapi`), its `reference:status:read` and
`orders:read` permissions, the `Mock API Caller` role and admin's
assignment to it were all already in place. `cliuser` holds none of them,
which is what makes the denied sub-row meaningful here too.

## 8. Row 3: Identity Server as the login provider

`cli-is` (Identity Server 7.1.0, https://localhost:9444) serves API Manager
management by the `derived` strategy: the shell authorizes at Identity
Server for the grant's assertion scopes, then presents that session's
identity token as a jwt-bearer assertion at API Manager's own token
endpoint. Everything the 2026-09-06 proof configured is still live and was
re-verified before the run.

**`connect` cannot record any of this.** No module's descriptor names
`identity-server` as a provider, so there is no `connect` that creates an
Identity Server identity; and `apim`'s descriptor hard-codes the federated
grant, so even the product record has to be written the long way:

```sh
wso2 identity create is --issuer https://localhost:9444/oauth2/token \
  --client-id <the Identity Server CLI client> --provider identity-server
wso2 identity add-product is apim \
  --endpoint https://localhost:9443 --audience <the API Manager CLI client> \
  --scopes apim:api_view,apim:api_create,apim:api_publish,apim:subscribe,apim:app_manage,apim:admin \
  --grant jwt-bearer --grant-issuer https://localhost:9443/oauth2/token \
  --grant-client-id <the API Manager CLI client> --grant-scopes openid,groups
```

The identity is recorded with no product of its own, which is legal here:
Identity Server's derivation is scoped refresh, not resource-bound, so a
bare login session is a valid shape and `apim` hangs off it under the
grant.

`wso2 login --context is`:

```text
Session    direct, established
apim       derived, established
```

One credential prompt at Identity Server, three redirects; the second
authorization — the one that mints the assertion session — answered from
the sign-on with no prompt, in one. Then `apis list`, `apps list` and
`apis list` again all answer, in that order. The order matters: `apps
list` needs `apim:subscribe` and `apis list` needs `apim:api_view`, and
the third call proves the session was not consumed by the second. That is
the exact case the earlier proof recorded as **blocked** — Identity Server
permanently narrows a refresh token to the smallest scope set ever
requested with it, so two products with different scope sets could not
share one login. Per-product sessions remove the wall: each product holds
its own session at its own scope set, and nothing narrows anything else.

`cliuser`, who is in no Identity Server group, logs in the same way and is
refused at the command.

### The assertion scopes are load-bearing, and nothing says so

`--grant-scopes openid,groups` is not optional here, and omitting it costs
an hour. Without it the assertion carries no `groups` claim, API Manager's
`ISLocal` identity provider maps the user to no role, and **every** command
is refused — for the administrator exactly as for the denied user, with
the same message:

```text
error: the "apim" module asked for the permissions apim:api_view and the deployment issued default (auth.narrowing_unavailable)
  Check the deployment's API resource registration and the permissions granted to the registered OAuth application, then retry.
```

That recovery points at the deployment, which was correctly configured in
both cases. The actual cause is a flag on the record.

**Fixed** (`fix(auth): say the user is not authorized when a derived
product's issuer grants nothing`). The `derived` path had never gained the
user-focused refusal the `federated` path got, so it reported this state
with the generic registration message. It now says the issuer accepted the
assertion and granted nothing, and — because the two causes are genuinely
indistinguishable at that point — the recovery names both: the role the
user may not hold, and the assertion scopes the record may not request. A
grant of *some* of the permissions asked for keeps the registration
message, which is the true one for it. Verified live against `cli-is` in
both states, the denied user and the administrator with the flag missing.
What is still missing is any default for those scopes; that is in
section 10.

## 9. What the matrix changed

**Every `apim` command failed on the first attempt**, and the cause was
only visible live. `connect` records the descriptor's six management
scopes. The module asked the broker for one scope per command
(`apim:api_view` for `apis list`). The broker serves a stored session's
token only when the two scope sets are *equal*, so it tried to narrow by
refresh; API Manager refuses to narrow at all; the shell re-authorized,
got the same six back, and refused with `auth.narrowing_unavailable`. The
loop is invisible to unit tests because the fake issuer narrows.

The fix is the one section 7 of the design already asks for: the module
stops naming scopes per command, so the request arrives empty and inherits
the product's recorded scopes. Eleven call sites in the `apim` module now
send no scopes. Request, record and session then agree by construction,
and the product record is the only ceiling — which is what ADR 0005's
proof rests on either way.

## 10. Defects the runs found and did not fix

- **Fixed** (`fix(auth): tell a stored session that cannot be renewed
  apart from one that is missing`). Under `--no-input`, a product that
  **had** a session carrying none of its permissions was refused with "has
  no session under this identity yet" — false, since `whoami` showed it
  present in the same state, and its recovery was another login, which is
  what the invocation had just forbidden. The cause was structural: the
  broker's establish hook is nullary, so the shell wrote a message for a
  state only the broker could see, and the two calls into it — nothing
  stored, and stored but unrenewable — were indistinguishable. The shell
  now hands down a marker saying only that no browser may open, and the
  broker writes whichever refusal fits. Verified live in both states.
- `whoami` renders a machine identity's product as `iam: inline, inline`:
  the strategy and the session state are the same word.
- `connect` ends with `Next  Run wso2 login --context thunder.` even when
  that context is the only one and already selected.
- Nothing derives the assertion scopes a jwt-bearer grant needs. The
  record's `--grant-scopes` decides whether the identity token carries the
  claim the product maps roles from, and nothing but the operator can
  supply it. A descriptor could name them, the way it names the grant.
- A product descriptor names one grant, so `connect` can record a product
  in one shape only. API Manager is reachable two ways — federated at its
  own issuer for management, sibling at the login provider for the
  gateway — and `connect` can write the first but not the second. Row 5
  fell back to `wso2 identity add-product`, which is the command the
  descriptor was meant to retire. **Addressed by #163** (2026-09-07): the
  descriptor declares a `gateway` block and `wso2 apim connect
  <gateway-url> --gateway` records the gateway beside the management
  record on the same identity.

## 11. Environment as left

`matrix-home` holds four identities: `thunder` (browser, `iam` direct +
`apim` federated, pinned to `iam`), `thunder-ci` (client credentials,
`iam` only) and `thunder-gw` (browser, `iam` direct + `apim` sibling on
the gateway, pinned to `iam`), and `is` (browser, Identity Server, no
product of its own, `apim` derived).

Neither ThunderID nor Identity Server was written to at all. On
`cli-apim`, two writes were made, both on the federated `admin` user's own
`DefaultApplication`: a
subscription to `MockAPI/1.0.0`, and a key mapping of client `wso2-cli`
on key manager `Thunder3`. Everything else — the clients, roles,
identity provider and key managers — is as
`docs/research/2026-09-06-single-login-spikes.md` and the earlier
exercise left it. The mock backend on port 18080 still runs for issuer
`http://localhost:8490` and was deliberately not restarted.
