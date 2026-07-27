# ADR 0003: Shell-Owned User Output

**Status:** Accepted

Product modules return typed results and problems through the module contract,
and the shell alone renders user-facing standard output, diagnostics, and exit
codes. The public SDK supplies result and problem types plus authoring helpers,
but it does not let a module write user output directly. Centralizing the final
presentation keeps table and machine output consistent, prevents module output
from corrupting structured output, and allows shell policy to evolve without
releasing every module.

## Considered Options

- SDK-side rendering would keep formatting close to product handlers but would
  make formatting behavior depend on each module's SDK version.
- Raw subprocess passthrough would simplify dispatch but could not enforce
  structured output, diagnostics separation, or stable problem behavior.
