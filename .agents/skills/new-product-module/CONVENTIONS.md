# Product module code conventions

Repository conventions beyond the [quickstart](../../../docs/guides/build-module-quickstart.md). Use `modules/iam` and `modules/apim` as prior art when a case is uncovered.

## Layout

```text
modules/<ns>/
├── module.json
├── go.mod
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

Follow [command design](../../../docs/guides/command-design.md) for names, flags and help, and [Add a command](../../../docs/guides/build-module-quickstart.md#2-add-a-command) for tree registration.

- Keep `commands()` in `main.go`.
- Declare flags on the `*cobra.Command` with a `<group><verb>Flags` struct. The SDK parses them before the handler runs, so never parse `request` arguments by hand.
- A handler that needs flags is a constructor: `func groupCreate(command *cobra.Command, flags *groupCreateFlags) module.Handler`.
- End each `Short` sentence with a full stop.

## Results

Read the SDK's [result reference](../../../docs/reference/module-sdk.md#returning-a-result) before constructing results or listings.

- One exported `<Noun>Schema = "<ns>.<noun>/v1"` constant per result shape, next to its handler.
- Every result ends with `.With(NextField, "Next", ...)`: a full sentence naming the next `wso2 <ns> ...` command to run. Branch it on the data, for example an empty listing gets a different next step.
- Report the id every other command takes, as well as the human name.

## Access and failures (`access.go`)

Follow [Call your product](../../../docs/guides/build-module-quickstart.md#3-call-your-product) for brokered access, declarations and token handling. Read the SDK's [failure reference](../../../docs/reference/module-sdk.md#returning-a-failure) for problem construction and categories.

- `clientFor(ctx, request)`:
  1. If `request.Context.Endpoint` is empty, return `<ns>.product_not_recorded` with a recovery naming `wso2 context product add <ns> --url ...`.
  2. Call `request.Access.Acquire` with the module's audience constant; follow the quickstart's guidance for request scopes.
  3. On error, return the shell's error unchanged: it knows why access was denied.
- `callFailed(err, attempted, endpoint)` maps a product refusal:
  - no HTTP answer → `<ns>.deployment_unreachable`
  - 401/403 → `<ns>.not_authorized`, recovery names the permission to grant
  - 400 → `<ns>.call_failed`, recovery points at the command's input
  - anything else → `<ns>.call_failed`, recovery points at the deployment logs
  
  `attempted` is a verb phrase ("read the users") that makes the message read naturally.
- Map bad input to `problem.CategoryUsage`, product failures to `CategoryProductService`, and access refusals to `CategoryAuthPolicy`.
- Keep the product's own error words: fold its message, description and field errors into the problem message rather than inventing a new one.

## Product client (`internal/<product>/client.go`)

- `Client{Endpoint, Token string; HTTP *http.Client}` with `Get`/`Post`/… methods decoding JSON into a caller's `out`.
- Bound every call: a request timeout constant and an `io.LimitReader` response limit constant.
- A non-2xx response returns a typed `Failure{Status, Code, Message, ...}` implementing `error`, which `callFailed` reads with `errors.As`.
- Send the brokered token in the `Authorization: Bearer` header.

## Tests

Use the [SDK testkit example](../../../docs/reference/module-sdk.md#testing-a-handler) with the existing module options and command tree.

- One `newProductStub(t, routes map[string]string)` per module: an `httptest.Server` that serves canned JSON by path and records the `Authorization` header it received.
- Set the invocation endpoint to the stub URL and supply `testkit.Access` with a test token for protected calls.
- Check `outcome.Err`, then `outcome.Problem`, then `outcome.Result`, in that order.
- Name tests as sentences: `TestUsersListReportsEveryUserTheDeploymentReturns`.
- Cover `Access.Deny`, a missing endpoint, and each status class `callFailed` maps.

## Writing user-facing text

Write `next` and recovery lines as full sentences using `wso2 <ns> ...` with this module's own namespace; the generated namespace test checks these. Follow [help behavior](../../../docs/guides/command-design.md#help-behavior) when writing help and module documentation.
