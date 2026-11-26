# The apim product module

This is a WSO2 CLI product module. It is a separate program: the `wso2` shell
resolves it from the managed module store and launches it, and the two speak the
module contract over the module's standard input and output.

It lives in this repository and is released by its own tag, independently of the
shell and of every other module:

```sh
git tag apim/v0.1.0-rc.1
git push origin apim/v0.1.0-rc.1
```

That tag builds the module for every supported platform, publishes the archives,
and regenerates the module catalog a user installs from. A release is refused if
no shell that exists can launch it.

## Working on it

```sh
# From the repository root.
make build-module NAMESPACE=apim
make test-module NAMESPACE=apim
make install-module NAMESPACE=apim   # then ./bin/ws apim status
```

The workspace composes this module with the SDK from source, so a change to the
SDK reaches it without a release. The `go.mod` here requires the published SDK
at the version every other module in this repository requires, and carries no
`replace` directive.

## Commands

| Command | Does |
| --- | --- |
| `ws apim status` | Reports this module's own status and what to run first: a reachable control plane reports the number of `Projects`, naming `ws apim project create <name>` when there are none and `ws apim api list --project <id>` for the first one otherwise. When the product cannot be reached (no recorded endpoint, access denied, or no answer) it fails with that failure's own code and recovery, so the exit code tells a working setup from a broken one. |
| `ws apim project list` | Lists the projects the control plane records. |
| `ws apim project create <name> [--description <text>]` | Creates a project, sending only `displayName` (and `description`, when given) — the handle is left for platform-api to auto-generate from the name, per `CreateRESTAPIRequest`'s own schema, rather than this command reimplementing its slug rules. A 409 (a project by this name/handle already exists) is refused naming the project and pointing at `ws apim project list`. |
| `ws apim project delete <project-id> [--yes]` | Deletes a project, asking first as for `api delete`. platform-api refuses a project that still holds APIs and an organization's last project, and the refusal carries its words. |
| `ws apim api list [--project <id>]` | Lists the APIs of one project. With no `--project` it reads the projects first and lists every project's APIs with a Project column, because the control plane refuses `/rest-apis` without a `projectId`. |
| `ws apim api create -f <file> --project <id>` | Creates an API in a project from a gateway `RestApi` document (the same CR the gateway itself reads, such as `gateway/hello-api.yaml`). Takes no positional arguments. Refuses a document whose `apiVersion`/`kind` do not match. Refuses, by path (e.g. `spec.upstream.main.hostRewrite`, `spec.operations[1].foo`, `metadata.namespace`, or a top-level key such as `status`), any member of the document, `spec`, `spec.upstream`, a policy, or an operation that it cannot map onto platform-api's `CreateRESTAPIRequest`, rather than dropping it silently — `metadata.name` must be a string when present. Requires `spec.displayName`, `spec.context`, `spec.version` (as a quoted string — an unquoted `1.0` decodes as a number, which is refused, naming the fix), and `spec.upstream.main` (a map with `url` or `ref` — `null` is refused with that exact shape named). A 400 the deployment answers with per-field validation failures is reported with each `field: message`, and its recovery points at the file. |
| `ws apim api deploy <api-id> --gateway-id <gateway-id>` | Deploys an API to a registered gateway. Posts the deployment alone: platform-api's own deploy operation associates the API with the gateway itself when it is not already associated. |
| `ws apim api undeploy <api-id> [--gateway-id <gateway-id>]` | Undeploys every active deployment of the API, on one gateway when named. It reads the deployments itself, so no deployment id is needed. |
| `ws apim api delete <api-id> [--yes]` | Asks `Delete the API <api-id> and every deployment of it? This cannot be undone.` through the shell, then undeploys the API from every gateway and deletes it. `--yes` deletes without asking. A no is `shell.cancelled`; under `--no-input`, `WSO2_NO_INPUT` or a standard input that is not a terminal the shell refuses with `shell.non_interactive`, naming `--yes`, and nothing is called. A 404 is refused `apim.not_found`. |
| `ws apim api invoke <api-id> [path] [-X <method>] [-H <name: value>]... [-d <body>] [--gateway-id <id>] [--no-token]` | Calls a deployed API through its gateway the way a consumer would, and reports the status, the URL called, the duration, the content type and the body (indented when it is JSON). Everything is resolved from the API id: the path is appended to the API's `context`, the gateway is the one the API is deployed on, `DEPLOYED` or still `DEPLOYING` (more than one needs `--gateway-id`; none is refused `apim.not_deployed`, naming `api deploy`), and the token is minted for the audience the API's `jwt-auth` policy declares, through the shell's `api` record (see `docs/reference/module-sdk.md`). An API with no `jwt-auth` policy is called without a token, as is any API with `--no-token`, which is how to see what the gateway answers an anonymous caller. `-H Authorization` is refused: the token is the shell's to present. The API's own answer is the result whatever its status; a 401 from the gateway is what the person came to see, not a failure of this command, so the exit status is 0. The body is capped at 1 MiB. |
| `ws apim api get-token <api-id>` | Mints a token bound to the API's audience and returns it, with the audience and its expiry, for calling the API by hand (`-o json \| jq -r .token` feeds curl). The token is exchanged from the login session and stored nowhere; a note on standard error says so. It lives as long as the identity provider says (an hour on Thunder) and cannot be revoked from here. Refuses `apim.no_audience` for an API whose `jwt-auth` policy declares none. |
| `ws apim gateway list` | Lists the gateways registered with the control plane: name, id, type and endpoints. The id is what `--gateway-id` and `ws apim gateway token create` take. Needs `ap:gateway:read`. |
| `ws apim gateway register <handle> --display-name <name> --endpoint <url> [--endpoint <url> ...] [--type regular\|ai\|event]` | Registers a gateway with the control plane and mints its registration token in the same run. The token is shown exactly once, in the command's own result — it is never written to a file, config, or log, and a note on standard error says so. If the gateway registers but the token mint fails (or the deployment answers a 201 with no token), the gateway is left registered and the command points at `ws apim gateway token create <handle>` to finish the job, naming the underlying failure's own reason (reported as `apim.not_authorized` when it was a 403, so an administrator knows to fix permissions rather than retry blindly). If the 201 from registration carries no id, the handle given on the command line is used instead. |
| `ws apim gateway token create <gateway-id>` | Mints a new registration token for a gateway that is already registered — the way to recover from a `register` whose token mint failed, or to get a fresh token later. Same one-time-display rule as `register`. Notes platform-api's limit of 2 active tokens per gateway in its recovery text when a mint is refused. |
| `ws apim gateway api list` | Lists the APIs the gateway is actually serving. |

Every command above reaches the control plane's own session, except `gateway api list`, which reads the gateway's own management API through its gateway session, and `api invoke` and `api get-token`, which hold the control plane's session to find the API and then the API's own access to call it — three records on the same product (see `cmd/wso2-module-apim/access.go` and `invoke.go`).

## What to change first

`cmd/wso2-module-apim/main.go` declares the command tree above in `commands()`
and binds each command to its handler. To call your product, a handler
needs access, and access is something the shell brokers rather than something a
module holds: declare an audience and a scope in `module.json` and in
`moduleOptions`, then ask for them with `request.Access.Acquire`. A module never
sees a credential and cannot obtain a second token.

`modules/example` is the worked example: a client on the product's REST
API, two read-only commands, and tests that drive the module through the
contract against a fake deployment. The guide at
`docs/guides/build-module-quickstart.md` walks through it.

## What this module must not do

It must not import anything under the shell's `internal` tree, and it must not
print to standard output: that stream carries protocol frames, and anything
written there directly corrupts them. Diagnostics go to standard error. Both
rules are asserted for every module under `modules/`, including this one, by the
tests in `internal/boundaries`.
