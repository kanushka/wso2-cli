# Product module code conventions

What `modules/identity` and `modules/api` do that the scaffold and the SDK reference don't spell out. Read those two modules when a case isn't covered here.

## Layout

```text
modules/<ns>/
├── module.json
├── go.mod                         # sdk + cobra only; no replace directive
├── README.md
├── cmd/wso2-module-<ns>/
│   ├── main.go                    # constants, moduleOptions(), commands(), status
│   ├── access.go                  # clientFor, callFailed, usageProblem, moduleProblem
│   ├── <group>.go                 # one file per command group: schema, types, handlers
│   ├── <group>_test.go
│   └── namespace_test.go          # from the scaffold; leave it as generated
└── internal/<product>/client.go   # HTTP client for the product API
```

Every `.go` file starts with the WSO2 Apache 2.0 header the scaffold writes.

## Command tree

- Build the whole tree in `commands()` in `main.go`, then bind handlers with chained `cobratree.New(root).Handle(cmd, handler)`.
- Declare flags on the `*cobra.Command` with a `<group><verb>Flags` struct. The SDK parses them before the handler runs, so never parse `request` arguments by hand.
- A handler that needs flags is a constructor: `func groupCreate(command *cobra.Command, flags *groupCreateFlags) module.Handler`.
- `Short` is one sentence ending in a full stop, stating what the user gets.

## Results

- One exported `<Noun>Schema = "<ns>.<noun>/v1"` constant per result shape, next to its handler.
- Scalars go through `.With(name, label, value)` and listings through `.WithColumn(...)` and `.WithRow(...)`. Values are strings: format numbers with `strconv`.
- Every result ends with `.With(NextField, "Next", ...)`: a full sentence naming the next `wso2 <ns> ...` command to run. Branch it on the data, for example an empty listing gets a different next step.
- Report the id every other command takes, as well as the human name.

## Access and failures (`access.go`)

- `clientFor(ctx, request)`:
  1. If `request.Context.Endpoint` is empty, return `<ns>.product_not_recorded` with a recovery naming `wso2 context product add <ns> --url ...`.
  2. Call `request.Access.Acquire` with the module's audience constant and scopes.
  3. On error, return the shell's error unchanged: it knows why access was denied.
- `callFailed(err, attempted, endpoint)` maps a product refusal:
  - no HTTP answer → `<ns>.deployment_unreachable`
  - 401/403 → `<ns>.not_authorized`, recovery names the permission to grant
  - 400 → `<ns>.call_failed`, recovery points at the command's input
  - anything else → `<ns>.call_failed`, recovery points at the deployment logs
  
  `attempted` is a verb phrase ("read the users") that makes the message read naturally.
- Bad command-line input → `problem.CategoryUsage` (exit 64). Product refused → `CategoryProductService` (75) or `CategoryAuthPolicy` (77) for access refusals. Any plain `error` becomes `handler_failed`, so return a `problem.Problem`.
- Keep the product's own error words: fold its message, description and field errors into the problem message rather than inventing a new one.
- Message and recovery text never contains a token.

## Product client (`internal/<product>/client.go`)

- `Client{Endpoint, Token string; HTTP *http.Client}` with `Get`/`Post`/… methods decoding JSON into a caller's `out`.
- Bound every call: a request timeout constant and an `io.LimitReader` response limit constant.
- A non-2xx response returns a typed `Failure{Status, Code, Message, ...}` implementing `error`, which `callFailed` reads with `errors.As`.
- The token is only ever used in the `Authorization: Bearer` header: never logged, never in an error.

## Tests

- One `newProductStub(t, routes map[string]string)` per module: an `httptest.Server` that serves canned JSON by path and records the `Authorization` header it received.
- Drive each command with `testkit.Run(ctx, moduleOptions(), commands().Commands(), testkit.Invocation{Command: ..., Arguments: ..., Context: module.Context{Endpoint: stub.URL}, Access: &testkit.Access{Token: "brokered-token"}})`.
- Check `outcome.Err`, then `outcome.Problem`, then `outcome.Result`, in that order.
- Name tests as sentences: `TestUsersListReportsEveryUserTheDeploymentReturns`.
- Cover `Access.Deny`, a missing endpoint, and each status class `callFailed` maps.

## Writing user-facing text

Help, `next` and recovery lines are full sentences naming commands as `wso2 <ns> ...`, always with this module's own namespace: `namespace_test.go` fails otherwise. Use the glossary's words (_context_, _product_), which `CONTEXT.md` defines.
