# Shell: `identity create`, namespace-scoped module environment, next-step line

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the shell the three things the `iam` and `apim` modules hand
off to: a command that writes an identity and context without JSON, a
module environment that carries `WSO2_<NAMESPACE>_*` variables, and a
rendering rule that prints a result's `next` field as a trailing line.

**Architecture:** Three independent changes in `internal/app`,
`internal/modules` and `internal/output`, each behind an existing seam:
`contexts.Update` for the document, `SanitizedEnvironment` for the process
environment, `resultTable` for rendering. No protocol or SDK change.

**Tech Stack:** Go 1.25, cobra, the repo's `problem`/`result`/`contexts`
packages, `go test`.

**Spec:** `docs/superpowers/specs/2026-09-04-iam-apim-modules-design.md` §3.

## Global Constraints

- Every Go file begins with the Apache-2.0 header the boundaries test
  checks (copy it from `internal/app/trust.go`).
- One-line conventional commits, no attribution trailers, `--no-gpg-sign`
  when no terminal is attached.
- Problem codes reuse the closed list: `shell.missing_required_flag`,
  `shell.invalid_argument`, `shell.conflicting_arguments`,
  `contexts.unknown_identity`; new codes only where the spec names one.
- Markdown hard-wrapped at 80 columns except tables and code blocks.
- Nothing written to the document holds a credential (ADR 0012).

---

### Task 1: `SanitizedEnvironment(namespace)` passes `WSO2_<NAMESPACE>_*`

**Files:**
- Modify: `internal/modules/environment.go`
- Modify: `internal/rpc/launch.go:73` (`command.Env = modules.SanitizedEnvironment()`)
- Modify: `internal/install/declare.go:83`
- Test: `internal/modules/environment_test.go`

**Interfaces:**
- Produces: `func SanitizedEnvironment(namespace string) []string`. Passes
  `WSO2_CA_FILE` and every variable whose name starts with
  `"WSO2_" + strings.ToUpper(namespace) + "_"`, non-empty values only.

- [ ] **Step 1: Extend the test**

Append to `internal/modules/environment_test.go`:

```go
func TestAModuleSeesItsOwnNamespaceVariablesOnly(t *testing.T) {
	t.Setenv("WSO2_IAM_ADMIN_PASSWORD", "secret-for-iam")
	t.Setenv("WSO2_APIM_ADMIN_PASSWORD", "secret-for-apim")
	t.Setenv("WSO2_IAM_EMPTY", "")
	t.Setenv("WSO2_HOME", "/nowhere")

	environment := SanitizedEnvironment("iam")
	if !slices.Contains(environment, "WSO2_IAM_ADMIN_PASSWORD=secret-for-iam") {
		t.Errorf("the module's own variable was withheld: %q", environment)
	}
	for _, entry := range environment {
		if strings.HasPrefix(entry, "WSO2_APIM_") || strings.HasPrefix(entry, "WSO2_HOME") ||
			entry == "WSO2_IAM_EMPTY=" {
			t.Errorf("a module was handed %q", entry)
		}
	}
}
```

Change the existing call in `TestAModuleSeesOnlyTheCertificateFile` to
`SanitizedEnvironment("reference")`.

- [ ] **Step 2: Run, expect a compile failure** — `go test ./internal/modules/`

- [ ] **Step 3: Implement**

```go
func SanitizedEnvironment(namespace string) []string {
	names := []string{CAFileEnvVar}
	if runtime.GOOS == "windows" {
		names = append(names, "SYSTEMROOT", "SYSTEMDRIVE", "WINDIR")
	}
	environment := []string{}
	for _, name := range names {
		if value, present := os.LookupEnv(name); present && value != "" {
			environment = append(environment, name+"="+value)
		}
	}
	prefix := NamespaceEnvPrefix(namespace)
	for _, entry := range os.Environ() {
		name, value, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, prefix) && value != "" {
			environment = append(environment, entry)
		}
	}
	return environment
}

// NamespaceEnvPrefix is the prefix of the variables a module of this
// namespace is handed: WSO2_IAM_ for iam. A user who exports a secret for a
// module names the module in the variable, and no other module sees it.
func NamespaceEnvPrefix(namespace string) string {
	return "WSO2_" + strings.ToUpper(namespace) + "_"
}
```

Update the doc comment above the function to say what is added back and
why. Pass the namespace at both call sites: in `launch.go` it is
`l.Resolved.Receipt.Namespace` (check the field name on `modules.Receipt`
with `grep -n Namespace internal/modules/receipt.go`); in `declare.go` it
is the manifest's namespace already in scope in that function.

- [ ] **Step 4: Run** `go test ./internal/modules/ ./internal/rpc/ ./internal/install/ ./internal/app/` — PASS

- [ ] **Step 5: Document** — in `docs/reference/commands.md`, after the
  "Trusting a deployment's certificate" section, add:

```markdown
## What a module process can see

A product module runs with an environment built from nothing. Two things
are added back: `WSO2_CA_FILE`, and every variable named
`WSO2_<NAMESPACE>_*` for that module's namespace, upper-cased: `WSO2_IAM_`
for `iam`. That is how a secret a command needs, such as an administrator
password for a one-time bootstrap, reaches the module: the user exports it
under the module's prefix and names it on the command line. A module never
sees another module's variables, and nothing else a CI runner exported.
```

Add the same rule to `docs/guides/building-product-modules.md` in the
section on access (search for "Acquire").

- [ ] **Step 6: Commit** — `feat(modules): pass WSO2_<NAMESPACE>_ variables to a module of that namespace`

### Task 2: `next` renders as a trailing line

**Files:**
- Modify: `internal/output/result.go:69-79`
- Test: `internal/output/result_test.go` (create if absent; check `ls internal/output/*_test.go`)

**Interfaces:**
- Produces: table rendering that omits the field named `next` from the
  table and prints `\nNext  <value>\n` after it. JSON unchanged.

- [ ] **Step 1: Failing test**

```go
func TestANextFieldRendersAsATrailingLine(t *testing.T) {
	produced := result.New("x.y/v1").
		With("count", "Count", "1").
		With("next", "Next", "Run wso2 apim apis deploy MockAPI/1.0.0.")
	var out bytes.Buffer
	if err := Result(&out, ModeTable, produced); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if strings.Contains(strings.SplitN(text, "\n", 2)[0], "NEXT") {
		t.Errorf("next was rendered as a column:\n%s", text)
	}
	if !strings.HasSuffix(text, "\nNext  Run wso2 apim apis deploy MockAPI/1.0.0.\n") {
		t.Errorf("next line missing:\n%s", text)
	}
	out.Reset()
	if err := Result(&out, ModeJSON, produced); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"next": "Run wso2 apim apis deploy MockAPI/1.0.0."`) {
		t.Errorf("json lost next:\n%s", out.String())
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/output/ -run Next` — FAIL

- [ ] **Step 3: Implement** in `resultTable`:

```go
const nextField = "next"

func resultTable(w io.Writer, produced result.Result) error {
	headers := make([]string, 0, len(produced.Fields))
	values := make([]string, 0, len(produced.Fields))
	next := ""
	for _, field := range produced.Fields {
		if field.Name == nextField {
			next = field.Value
			continue
		}
		headers = append(headers, field.DisplayLabel())
		values = append(values, field.Value)
	}
	if len(headers) > 0 {
		table := NewTable(headers...)
		table.Append(values...)
		if err := table.Render(w); err != nil {
			return err
		}
	}
	if next == "" {
		return nil
	}
	_, err := fmt.Fprintf(w, "\nNext  %s\n", next)
	return err
}
```

Add a comment: a module says what a user most likely runs next in a field
named `next`; it is a line, not a column, so a full command does not
stretch the table. `output.Report` (used by shell commands) gets the same
rule: skip `next` in the pairs and print the line after `Fields`.

- [ ] **Step 4: Run** `go test ./internal/output/ ./internal/app/` — PASS
- [ ] **Step 5: Commit** — `feat(output): render a result's next field as a trailing line`

### Task 3: `wso2 identity create`

**Files:**
- Create: `internal/app/identity_create.go`
- Modify: `internal/app/identity.go:79` (add the subcommand)
- Test: `internal/app/identity_create_test.go` (package `app_test`, use
  `newShell(t)` from `login_helpers_test.go` and the context fixture
  helpers in `internal/contexts/fixture`)

**Interfaces:**
- Consumes: `contexts.Update`, `contexts.Identity`, `contexts.Product`,
  `contexts.Providers()`, `contexts.ValidName`, `declaresIdentity`,
  `declaresContext`, `output.Report`.
- Produces: the command
  `identity create <name> --issuer --client-id [--client-secret-variable] [--provider] [--product --endpoint [--audience] [--scope]...]`
  and the JSON shape `{"identity","context","kind","issuer","clientId","provider","product","endpoint","audience","scopes","selected","next"}`.

- [ ] **Step 1: Failing tests** (four cases)

```go
func TestIdentityCreateWritesABrowserIdentityWithOneProduct(t *testing.T) {
	shell, out, errOut := newShell(t) // fresh WSO2_HOME
	code := shell.Run([]string{"identity", "create", "thunder-admin",
		"--issuer", "http://localhost:8490", "--client-id", "wso2-cli", "--provider", "thunder",
		"--product", "iam", "--endpoint", "http://localhost:8490",
		"--audience", "https://localhost:8090/mcp", "--scope", "system"})
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	document := loadDocument(t, shell) // helper: contexts.Load(stateRoot)
	identity := document.Identities[0]
	if identity.Auth.Kind != contexts.KindOAuthBrowser || identity.Auth.Provider != "thunder" ||
		identity.Auth.CredentialRef != "thunder-admin" || identity.Auth.ClientSecretVariable != "" {
		t.Errorf("auth = %+v", identity.Auth)
	}
	if got := identity.Products["iam"]; got.Endpoint != "http://localhost:8490" ||
		got.Audience != "https://localhost:8090/mcp" || !slices.Equal(got.Scopes, []string{"system"}) {
		t.Errorf("product = %+v", got)
	}
	if document.DefaultContext != "thunder-admin" || document.Contexts[0].Identity != "thunder-admin" {
		t.Errorf("context not written or selected: %+v", document)
	}
	if !strings.Contains(out.String(), "Next  Run wso2 login --context thunder-admin") {
		t.Errorf("no next line:\n%s", out)
	}
}

func TestIdentityCreateWithASecretVariableIsClientCredentials(t *testing.T) {
	// --client-secret-variable WSO2_APIM_CLIENT_SECRET → Kind client-credentials,
	// ClientSecretVariable set, CredentialRef empty, next line names the product command:
	// "Next  Run wso2 apim status --context apim-admin"
}

func TestIdentityCreateRefusesAThunderProductWithoutAnAudience(t *testing.T) {
	// --provider thunder --product iam --endpoint … and no --audience →
	// exit 64, stderr contains "shell.missing_required_flag" and "--audience"
}

func TestIdentityCreateRefusesADuplicateAndAHalfProduct(t *testing.T) {
	// second create with the same name → exit 64, code contexts.identity_exists
	// --endpoint without --product → exit 64, shell.conflicting_arguments
}
```

- [ ] **Step 2: Run** `go test ./internal/app/ -run IdentityCreate` — FAIL (unknown command)

- [ ] **Step 3: Implement** `identity_create.go`:

```go
const identityCreateUsage = "Run wso2 identity create <name> --issuer <url> --client-id <id> " +
	"[--client-secret-variable <VAR>] [--provider <name>] " +
	"[--product <namespace> --endpoint <url> [--audience <uri>] [--scope <scope>]...]."

type identityCreateFlags struct {
	issuer, clientID, secretVariable, provider string
	product, endpoint, audience             string
	scopes                                   []string
}

func (s Shell) identityCreateCommand() *cobra.Command {
	var flags identityCreateFlags
	command := &cobra.Command{
		Use:   "create <name> --issuer <url> --client-id <id>",
		Short: "Declare an identity and a same-named context without a login.",
		Args:  exactlyOneArgument("an identity name", identityCreateUsage),
		RunE: func(command *cobra.Command, args []string) error {
			return s.identityCreate(command, args[0], flags)
		},
	}
	f := command.Flags()
	f.StringVar(&flags.issuer, "issuer", "", "The token issuer this identity authenticates against.")
	f.StringVar(&flags.clientID, "client-id", "", "The OAuth application the shell presents.")
	f.StringVar(&flags.secretVariable, "client-secret-variable", "",
		"Name of the environment variable holding the client secret; makes this a client-credentials identity.")
	f.StringVar(&flags.provider, "provider", "",
		"The identity provider: "+strings.Join(contexts.Providers(), ", ")+".")
	f.StringVar(&flags.product, "product", "", "A product namespace this identity reaches.")
	f.StringVar(&flags.endpoint, "endpoint", "", "The product service's base URL.")
	f.StringVar(&flags.audience, "audience", "", "The token audience the product's services accept.")
	f.StringArrayVar(&flags.scopes, "scope", nil, "A permission the shell may request for the product; repeatable.")
	return command
}
```

`identityCreate` validates in this order and returns the named code:
name (`shell.invalid_argument`), issuer via `refuseNonIssuerURL`,
`--client-id` present (`shell.missing_required_flag`), provider in
`contexts.Providers()` (`shell.invalid_argument`), product flags all-or-
nothing (`shell.conflicting_arguments`: "--endpoint, --audience and --scope
belong to --product"), audience required when
`contexts.IdentityAuth{Provider: flags.provider}.Derivation() == contexts.DerivationTokenResource`
(`shell.missing_required_flag`). Then `contexts.Writable(root)` and
`contexts.Update` appending the identity (Type `identityType`, Kind by
secret variable, `CredentialRef` = name only for the browser kind) and the
context, selecting it when `document.DefaultContext == ""`; a duplicate
name returns `problem.New(problem.CategoryUsage, "contexts.identity_exists", …)`
with recovery "Run wso2 identity list, or pick another name."
Output through `output.Report` with the JSON fields above; `next` is
`"Run wso2 login --context <name>."` for the browser kind and
`"Run wso2 <product> status --context <name>."` (or
`"Run wso2 identity add-product <name> <namespace> --endpoint <url>."`
when no product was given) for client credentials. Register it in
`identityCommand` beside `add-product` and `list`.

- [ ] **Step 4: Run** `go test ./internal/app/` — PASS
- [ ] **Step 5: Document** — add the row to the command table in
  `docs/reference/commands.md` and replace the "hand-write the context
  document" paragraphs in `docs/guides/login-thunder.md` (§9, first login)
  with the `identity create` line followed by `wso2 login`.
- [ ] **Step 6: Commit** — `feat(identity): add wso2 identity create`

### Task 4: Prove it against the live Thunder

- [ ] Run, with `WSO2_HOME` pointed at an empty directory and the
  `wso2-cli` client already registered in `cli-thunder`:

```bash
./bin/wso2 identity create thunder-admin --issuer http://localhost:8490 --client-id wso2-cli --provider thunder --product iam --endpoint http://localhost:8490 --audience https://localhost:8090/mcp --scope system
WSO2_NO_BROWSER=1 ./bin/wso2 login --context thunder-admin   # complete with thunder-token.sh's sequence
./bin/wso2 whoami
```

Expected: identity written, login succeeds without editing JSON, `whoami`
shows a session. Record the verbatim output in the exercise findings file.
