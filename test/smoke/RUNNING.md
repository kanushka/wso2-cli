# Running the live smoke and the empirical experiments

**Status:** Working draft
**Related:** [Login walkthrough](../../docs/guides/login.md),
[Asgardeo redirect URIs and scope narrowing](../../docs/research/asgardeo-redirect-uri-and-scope-narrowing.md)

Everything in this directory except `config.go` is behind the `smoke` build tag.
The default gate never builds it, so nothing here can open a browser, contact a
deployment, or touch the operating system's secure store during `go test ./...`
or `scripts/acceptance.sh`.

These runs need a human. They open a real browser and wait for a real sign-in.

## What to export

Register the application first — [the walkthrough](../../docs/guides/login.md)
covers Asgardeo and Identity Server 7.x — then describe it with these variables.

| Variable | Required | Meaning |
| --- | --- | --- |
| `WSO2_SMOKE_ISSUER` | yes | The issuer exactly as its discovery document states it. Asgardeo: `https://api.asgardeo.io/t/<org>/oauth2/token`. Identity Server 7.x: `https://localhost:9443/oauth2/token`. |
| `WSO2_SMOKE_CLIENT_ID` | yes | The registered public client. |
| `WSO2_SMOKE_AUDIENCE` | yes | The API resource identifier brokered access must be bound to. |
| `WSO2_SMOKE_SCOPE` | yes | Permissions, separated by spaces or commas. The narrowing experiment needs at least two. |
| `WSO2_SMOKE_TENANT` | no | The identity's home organization. Left unset, the smoke context names no organization. |
| `WSO2_SMOKE_ENDPOINT` | no | The product endpoint recorded on the identity. Defaults to the issuer's origin. |
| `WSO2_SMOKE_IDENTITY_TYPE` | no | `cloud` (default) or `onprem`. |
| `WSO2_SMOKE_UNREGISTERED_PORT` | no | The loopback port the any-port experiment binds. Defaults to `16000`. Must be outside 10425-10428. |
| `WSO2_SMOKE_DEADLINE` | no | How long an **experiment** waits at the browser. Defaults to `3m`. It does not reach `make smoke-login`, which signs in through `wso2 login` and carries the shell's own five-minute deadline. |
| `WSO2_EMPIRICAL` | experiments only | Set to `1` to opt into the experiments. `make empirical-asgardeo` sets it for you. |

With none of them set, both targets skip and say which variables they wanted.
That is the expected result on a machine with no deployment:

```
--- SKIP: TestLoginSmoke (0.00s)
    no live deployment is configured: set WSO2_SMOKE_ISSUER, WSO2_SMOKE_CLIENT_ID,
    WSO2_SMOKE_AUDIENCE, WSO2_SMOKE_SCOPE (see test/smoke/RUNNING.md)
```

A variable that is set but unreadable — a malformed issuer, a port that is not a
number — fails instead of skipping. A run that skipped over a deployment someone
believed they had configured would be worse than one that stopped.

## The smoke run

```sh
export WSO2_SMOKE_ISSUER='https://api.asgardeo.io/t/<org>/oauth2/token'
export WSO2_SMOKE_CLIENT_ID='<client id>'
export WSO2_SMOKE_AUDIENCE='<api resource identifier>'
export WSO2_SMOKE_SCOPE='reference:status:read reference:status:write'

make smoke-login
```

A browser opens; sign in. The run then proves three things in order: `wso2 login`
exits zero, the refresh token is readable back out of the operating system's
secure store, and the broker derives one access token from that session.

Run it once per deployment. Against a local Identity Server, the only change is
the issuer:

```sh
export WSO2_SMOKE_ISSUER='https://localhost:9443/oauth2/token'
export WSO2_SMOKE_IDENTITY_TYPE=onprem
make smoke-login
```

### The one refusal that is not a failure

If the deployment will not prove a narrowed grant, the acquisition step reports:

```
LOGIN SMOKE: refused auth.narrowing_unavailable — the deployment would not prove
a narrowed grant. Login and session persistence passed; this refusal is the
designed outcome, not a failure.
```

and the run passes. The shell does not hand a module more authority than it
asked for, so refusing is the correct behavior, not a fallback. The walkthrough's
troubleshooting section explains what to change in the registration if you want
a grant instead.

## The experiments

Run once per deployment, ever. They answer the two questions the research
document could not settle from public sources.

```sh
make empirical-asgardeo
```

Two browser sign-ins, one per experiment. Each prints a single verdict line to
standard output:

```
ASGARDEO ANY-PORT LOOPBACK: rejected
  deployment: https://api.asgardeo.io/t/<org>/oauth2/token

ASGARDEO REFRESH NARROWING: honored
  deployment: https://api.asgardeo.io/t/<org>/oauth2/token
```

The `ASGARDEO` prefix is the question's name, not a claim about where the answer
came from — the `deployment:` line under each verdict is what says that. Record
only verdicts whose deployment line names the deployment you mean to record.

### Reading the any-port verdict

- `supported` — the deployment waived the port when matching the loopback
  redirect URI, as RFC 8252 section 7.3 asks and as Identity Server documents
  from 6.0.0 onwards.
- `rejected` — exact-match only. The flow never returned to the listener.
  **Corroborate this one before recording it.** `rejected` is the catch-all
  branch: it is what the experiment says for *any* login that did not complete
  and was not a discovery failure. A closed browser, a denied consent, an
  unfinished sign-in, or a code exchange the deployment refused all land here
  too, and none of them answers the question. Only record `rejected` if the
  browser actually displayed a redirect-URI-mismatch error. If it displayed a
  normal sign-in page, the run measured your attention span, not the deployment.
- `inconclusive (auth.discovery_failed)` — the port was busy or the issuer was
  unreadable. The experiment never reached its question.

### Reading the narrowing verdict

- `honored` — the deployment issued a token carrying exactly the one permission
  asked for. RFC 6749 section 6 behavior.
- `honored (protocol scopes retained)` — the deployment narrowed the product
  permissions to exactly the one asked for, and kept `openid` and
  `offline_access` in its answer. **The narrowing question is answered yes.**
  Record it as honored, with the qualifier. The shell still refuses the grant,
  because a module must not receive permissions it did not ask for — that
  refusal is about the shell's contract with modules, not about whether the
  deployment narrows. The login that establishes the session always requests
  `openid` and `offline_access` alongside the product permissions, so a
  deployment echoing them back is ordinary, not a finding.
- `ignored` — a token came back carrying a materially different permission set:
  a product permission that was not asked for, or one that was asked for and is
  missing. Protocol scopes are already excluded before this verdict is reached,
  so `ignored` means the deployment really did disregard the request. The line
  above the verdict prints both permission sets; copy them into the research
  document with the verdict.
- `rejected` — the token endpoint answered `invalid_scope`.
- `inconclusive (opaque access token)` — the deployment issues opaque access
  tokens, so nothing can be proven about what they carry. Configure the
  application to issue JWT access tokens and run it again; until then this
  question has no answer on this deployment.
- `inconclusive (audience not bound)` — a token came back that is not bound to
  the configured audience. Fix the API resource registration first.

### Recording the verdicts

Both verdicts belong in section 3 of
[`docs/research/asgardeo-redirect-uri-and-scope-narrowing.md`](../../docs/research/asgardeo-redirect-uri-and-scope-narrowing.md),
in the "Empirical verdict" column, replacing the pending cell. Record the date,
the verdict, and the deployment line the run printed. The document's section 3
says the same thing at the point a reader meets the cells.

## What these runs leave behind

Nothing that persists.

- The context document is written into a temporary state root that the test
  removes. Your own `~/.wso2` is never read or written.
- The session is stored under the secure-store reference `wso2-cli-smoke`, which
  no human would choose for a real context, and is deleted before and after
  every run. A real session under any other reference is never touched.
- The access tokens the runs obtain are never written anywhere and never
  printed. Runs report token lengths and expiry times only.

On macOS the first secure-store write may raise a keychain prompt. Allowing it
once is enough.
