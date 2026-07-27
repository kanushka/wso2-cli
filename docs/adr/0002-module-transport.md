# ADR 0002: Length-Delimited Protobuf Module Transport

**Status:** Accepted

The shell and each module communicate with length-delimited Protobuf messages
over the module process's inherited standard input and standard output. This
keeps the process-per-command runtime free of listeners and gRPC service
machinery while providing explicit message framing, bidirectional broker
requests, compatibility rules, and a path to non-Go modules. Module standard
output is reserved for protocol frames; standard error carries bounded
diagnostics, and only the shell writes user-facing standard output.

## Considered Options

- JSON-RPC or newline-delimited JSON would simplify initial inspection but
  provides a weaker schema and evolution contract.
- gRPC would provide a mature RPC runtime but adds listener or custom-transport
  complexity that is disproportionate to one short-lived local stream.
