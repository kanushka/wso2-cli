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

Describing it once in a file beats re-exporting it into every shell. Copy
[`env.example`](env.example) and fill it in:

```sh
cp test/smoke/env.example test/smoke/.env
make smoke-login
```

Both live targets source `test/smoke/.env` when it exists and print which file
they read. Keep one per deployment and name the one you want:

```sh
make smoke-login SMOKE_ENV=test/smoke/asgardeo.env
```

Nothing parses these files. Go has no dotenv convention and this module has no
dependency that would add one — the file is an ordinary shell fragment, so
sourcing it yourself does exactly what `make` does, which is what to do when
running `go test` directly:

```sh
. test/smoke/asgardeo.env
go test -tags smoke -count=1 -v -timeout 30m ./test/smoke/ -run TestLoginSmoke
```

Sourcing overwrites what the calling shell already exported, so the file you
name always wins and switching deployments does not need a fresh terminal. That
matters most in the case that would otherwise be baffling: a leftover export
from the last deployment quietly outranking the file you just edited.

`*.env` is ignored by git. Nothing secret belongs in one anyway — a public
client has no secret — and a client secret in a file this casual is a mistake
worth avoiding on purpose.

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
cp test/smoke/env.example test/smoke/.env   # then fill it in
make smoke-login
```

A browser opens; sign in. The run then proves four things in order: `wso2 login`
exits zero, the refresh token is readable back out of the operating system's
secure store, the broker derives an access token from that session, and it
derives a second one carrying strictly less than the session holds:

```
LOGIN SMOKE: granted  — asked for everything the session carries, received access of
                        1261 characters bound to "reference-status" carrying
                        [reference:status:read reference:status:write]
LOGIN SMOKE: narrowed — asked for one permission out of the 2 the session holds,
                        received access of 1230 characters bound to "reference-status"
                        carrying [reference:status:read]
```

The second line is the one that measures anything about narrowing. When the
request is every permission the session already carries, the shell compares the
issued scopes against an identical request, so that check holds however the
deployment behaved — a deployment that disregarded the request entirely would
still be reported as granted. Only a strict subset can fail, and a strict subset
is what a module actually asks for.

The two acquisitions run as two separate invocations because the shell allows a
module one acquisition per command and refuses a second with
`auth.already_granted`. Against a deployment that rotates refresh tokens, the
second acquisition also proves the first persisted its replacement.

Run it once per deployment. Two things change between them, and only one of them
is obvious:

```sh
export WSO2_SMOKE_ISSUER='https://localhost:9443/oauth2/token'
export WSO2_SMOKE_IDENTITY_TYPE=onprem
```

The other is the audience. On Asgardeo it has to be the **client ID**, because
that is the only value Asgardeo ever puts in an access token's `aud`. On
Identity Server it is the **API resource identifier**, which reaches `aud` once
the identifier is in the application's audience list. Sections 2.5 and 3.5 of
[the walkthrough](../../docs/guides/login.md) cover both. This is the main
reason to keep a file per deployment rather than editing one in place.

A local Identity Server also has to be trusted by the operating system before
any of this can reach it — see section 3.6 of the walkthrough.

### The one refusal that is not a failure

If the deployment will not prove a grant is exactly what was asked for, the
acquisition step reports:

```
LOGIN SMOKE: refused auth.narrowing_unavailable — asked for one permission out of
the 2 the session holds, and the shell would not hand the module a grant it could
not prove was exactly what it asked for. Login and session persistence passed;
this refusal is the designed outcome, not a failure.
```

and the run passes. The shell does not hand a module more authority than it
asked for, so refusing is the correct behavior, not a fallback. The walkthrough's
troubleshooting section explains what to change in the registration if you want
a grant instead.

Read which acquisition refused. On the **narrowed** one it is a statement about
the deployment: it would not issue a token carrying strictly less than the
session. On the **broad** one it is almost always the registration instead —
most often an audience the deployment never binds — and the run stops there
rather than repeating one finding twice.

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
  **Corroborate this one before recording it.** `invalid_scope` is also exactly
  what the token endpoint answers when the application's API resource
  authorization carries an authorization policy (RBAC) that the signing-in user
  does not satisfy — a registration gap, not a protocol finding about the
  deployment. The first live Asgardeo run hit it twice before producing a real
  verdict, and from the verdict line alone it is indistinguishable from a
  genuine "this deployment refuses to narrow" result. Before recording `rejected`, go to the application's
  Authorization tab and confirm the resource's policy reads `No Authorization
  Policy`, or, if it reads `Role Based Access Control (RBAC)`, that the
  signing-in user holds a role granting every scope the resource lists. Only
  once that is confirmed does `invalid_scope` say something about the
  deployment rather than about who was signed in when the experiment ran.
  Recording it without checking puts a false claim about Asgardeo into a
  research document whose whole purpose is being trustworthy about exactly
  that.
- `inconclusive (opaque access token)` — the deployment issues opaque access
  tokens, so nothing can be proven about what they carry. Configure the
  application to issue JWT access tokens and run it again; until then this
  question has no answer on this deployment.
- `inconclusive (audience not bound)` — a token came back that is not bound to
  the configured audience. On Asgardeo this is not a registration defect to
  fix: Asgardeo binds a JWT access token's `aud` claim to the **client ID**,
  never to the API resource identifier whose scopes the token carries, and this
  is not configurable — the application's Protocol tab exposes an Audience
  field only under **ID Token**, and the Access Token section has no audience
  control at all. See section 2.5 of
  [the walkthrough](../../docs/guides/login.md), and
  [the research document](../../docs/research/asgardeo-redirect-uri-and-scope-narrowing.md)
  this file already links above. The remedy is to set `WSO2_SMOKE_AUDIENCE`
  here, and `products.<namespace>.audience` in a real context document, to the
  **client ID** — that is the only value Asgardeo ever puts in `aud`. On a
  deployment that does bind tokens to API resources, the resource identifier is
  the correct value to configure, and an unbound token there means the resource
  is not authorized on the application instead. Whether Identity Server 7.x
  behaves like Asgardeo here is not yet measured.

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
