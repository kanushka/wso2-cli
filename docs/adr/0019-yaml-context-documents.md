# ADR 0019: YAML Context Documents

**Status:** Accepted

The context document is written as YAML, at `cli/contexts.yaml`, and the input
file `wso2 context apply -f` reads may be YAML or JSON. `wso2 context export`
prints YAML, or JSON with `--output json`. Both files are written and read by
people: a platform team commits the input file, and a developer opens the
local document with `wso2 context edit`. YAML is the format those people
already use for the same job, in a kubeconfig and in CI definitions, and it
reads more easily than JSON with its braces and quotes.

YAML is read only as a spelling of JSON. `internal/yamldoc` converts a YAML
document to JSON before anything decodes it, so every reader keeps its strict
`encoding/json` decode, its member names and its validation, and a JSON
document is read exactly as before. This is the approach `kubectl` takes. The
converter accepts only the subset of YAML that has a JSON meaning. It refuses:

- anchors, aliases and merge keys, which let a small file expand into a large
  one and let one value silently stand for another;
- custom and binary tags;
- keys that are not strings, and a key repeated in one mapping;
- a second document in the file, and a file larger than 1 MiB.

Writing goes the other way: the document is encoded as JSON, converted to
block-style YAML, and decoded back before it is written, as every write
already was. A string that YAML would read as another type, such as `true`,
`1.0` or `null`, is quoted.

This amends ADR 0008, which kept every YAML parser out of the shell. That rule
came with Cobra: its documentation generator pulls a YAML parser into the
module graph, and the shell did not need one to route commands. The concern
behind it stands: every package the shell links runs with the shell's trust.
So the shell links exactly one YAML parser, `go.yaml.in/yaml/v3`, which the
`apim` module already required, and only `internal/yamldoc` may import it. The
boundaries tests enforce both, and still refuse the other YAML parsers and
Cobra's documentation generator.

There is no migration from `contexts.json`, the name the document had while
it was JSON: the CLI is pre-release, and its users can recreate their
contexts or apply a context file again. This shell reads and writes only
`contexts.yaml`. The schema version does not change: the members and their
meaning are the same, only the spelling on disk differs.

## Considered Options

- **Keeping JSON.** This costs nothing in dependencies, but people keep
  reading and writing files in the format they find hardest to read.
- **YAML for the input file only.** That is where most of the readability
  benefit is, and it leaves the local document where it was. It still links
  the parser, so the amendment to ADR 0008 is the same, and it leaves a shell
  whose two context files are in different formats.
- **Decoding YAML directly into the structs** with `yaml:` tags beside the
  `json:` tags. This duplicates every member name, loses the strict decode
  (unknown members, trailing documents), and lets two tag sets drift apart.
- **`sigs.k8s.io/yaml`**, which converts in the same way. It links a second
  YAML parser, a fork of `yaml.v2`, and it expands anchors and aliases rather
  than refusing them.

## Consequences

- This shell ignores `contexts.json`, and a shell older than this one ignores
  `contexts.yaml`. On a machine that runs both, the two do not share contexts
  or the document lock: each reads and writes its own file.
- YAML's implicit typing is limited by the JSON decode: a value of the wrong
  type (for example `schemaVersion: "4"`) is refused, not coerced. Under the
  YAML 1.2 rules the parser applies, `no` and `yes` are strings.
- Comments a person writes in `contexts.yaml` are not kept: the shell rewrites
  the whole document on every write. Comments in an input file are not
  affected, because the shell does not write that file.
