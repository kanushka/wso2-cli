# Design a CLI command

Use this guide when adding or reviewing a shell command or a product module's
command tree. It applies to new commands. Existing command names stay stable
unless a separate migration changes them.

The release executable is `ws`. Most repository documentation writes `wso2`
because the name is selected at build time. Substitute the name of the binary
you are running; the command structure is the same.

## Command shape

```text
ws <product-namespace> [<resource>] <action> [<arguments>] [flags]
```

- The shell owns shared commands such as `context`, `login`, `org`, and
  `product`. Put a command there only when its behavior applies across
  products. A product module owns exactly one top-level namespace, such as
  `apim`, `iam`, `agent`, or `integration`. Do not use a shell command name
  as a product namespace.
- Under a product namespace, group operations on the same kind of resource
  under one noun. Put the action at the end of the command path. Omit the
  resource group when the namespace already names the primary resource, or
  when the action applies to the product as a whole, such as
  `ws <namespace> status`.
- Add another noun level only when it identifies a real child resource. A
  command path should describe the target without repeating the product name
  unnecessarily.

For a product that manages APIs, a consistent tree would look like this:

```text
ws apim api list
ws apim api show <api>
ws apim api create --file <file>
ws apim api deploy <api> --environment <environment>
ws apim api delete <api>
```

These illustrate naming, not commands currently supplied by the `apim` module.
`ws product install apim` is a shell command because it installs the product
module; `ws apim ...` contains that module's product operations.

The `agent` namespace already names its primary resource. Its actions can sit
directly under the namespace, while another resource gets its own group:

```text
ws agent list
ws agent show <agent>
ws agent project list
```

These are naming examples, not commands currently supplied by an `agent` module.

## Names and grammar

- Use a lowercase product namespace assigned in this repository. Use one
  familiar word for each concept across command names, help, errors, and docs.
  The domain glossary in [`CONTEXT.md`](../../CONTEXT.md) supplies project terms.
- Use a singular noun for a resource group: `api`, `user`, or `policy`. The
  group stays singular for every action, including `list`: `api list`,
  `api show <api>`, and `user delete <user>`. This matches the shell's own
  `context`, `product`, and `org` families, so one rule covers the whole
  command tree. Do not repeat the primary resource after its namespace: use
  `agent list`, not `agent agent list`.
- Use an imperative verb for an action. Prefer `list` for a collection,
  `show` for one resource, `create` for a new resource, `update` for a change,
  and `delete` for removal. Use a product verb such as `deploy` when it names a
  distinct operation. Do not introduce `get` beside `show`, or `remove` beside
  `delete`, for the same operation in one command family.
- Use lowercase, hyphen-separated words for multiword commands and flags.
  Use descriptive placeholders in angle brackets for required values and
  square brackets for optional syntax: `<environment>`, `[<name>]`.
- Give a flag the same name and meaning wherever the same choice appears.
  Reserve shell flags such as `--context`, `--output`, `--no-input`, `--help`,
  and `--verbose` for the shell. Keep secrets out of command-line arguments.

## Help behavior

- `ws help` and `ws --help` show shell commands and known product namespaces.
  The root page distinguishes installed products from those available to
  install. It works offline and does not require a context or login.
- `ws help <namespace>` and `ws <namespace> --help` show the installed
  product's command groups and actions. `ws help <namespace> <command>` and
  `ws <namespace> <command> --help` show help for that command. Product help
  comes from the command tree recorded at installation and must work without
  launching the module or contacting the product.
- A group named without an action should show its available actions and exit
  successfully. An unknown command or flag should fail with a usage error,
  name the bad input, and point to the nearest relevant help page. It must not
  silently show a parent page as if the input were valid.
- Give every visible command a short sentence that starts with a verb and
  describes the result. Describe required arguments, accepted flags, and any
  meaningful defaults in the command's documentation. Help must list only
  commands and flags the current installation accepts. Mark a command's
  provider limits when a namespace covers more than one provider.
- Keep examples beside the command in its module documentation. The current
  product help renderer displays a short description, usage, child commands,
  and flags; it does not display Cobra `Long` or `Example` text. Do not rely on
  those fields to teach an operation until the help renderer supports them.

## Review a new command

Before merging, check these points against both the command tree and its docs:

1. The owner is correct: shell for shared policy, product namespace for product
   operations.
2. The path follows namespace, resource, action. Its nouns, verbs, arguments,
   and flags follow the rules above.
3. Root, group, and leaf help lead users to the command without login or a
   network call. Unknown input fails with a useful suggestion.
4. Each documented example can be run with the flags and arguments the command
   declares. The command appears in the module's declared tree, which the
   shell uses to parse and render help.

See the [command reference](../reference/commands.md) for existing shell
commands and the [module guide](build-module-quickstart.md) for declaring a
product command tree.
