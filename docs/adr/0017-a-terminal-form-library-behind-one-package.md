# ADR 0017: A Terminal Form Library Behind One Package

**Status:** Accepted

The shell draws its interactive questions with `charmbracelet/huh`
(`charm.land/huh/v2`), and only `internal/wizard` may import it. Every question
the shell asks goes through that package's `Prompter`: a select, a text input
checked as it is typed, or a yes/no confirmation. On a terminal these are drawn
as forms. Everywhere else, the same questions are printed as numbered prompts.
A boundaries test (`TestOnlyTheWizardImportsTheTerminalUILibrary`) enforces the
import rule.

The setup wizard (#189) is what needs this. `wso2 context create`, `wso2 context
product add` and a creating `wso2 login` ask up to a dozen questions, several
of them choices from lists the installed products supply. Arrow-key selection
and inline validation make that a short sequence a person can follow, rather
than a wall of numbered menus.

Three properties matter here.

**Whether to ask stays outside the library.** `mayPrompt` in `internal/app`
still decides, from `--no-input`, `WSO2_NO_INPUT` and whether standard input is
a terminal, as before. The wizard package decides only how a question is
drawn. A form is drawn only when the shell reads the process's own standard
input, both it and standard error are terminals, standard error reports a
non-zero size (a form drawn in an unsized pseudo-terminal shows nothing), and
`WSO2_ACCESSIBLE` is not set.

**The line prompts are the shell's own, not huh's accessible mode.** huh's
accessible mode builds a new buffered scanner for each question, which reads
ahead and swallows the answers to the questions that follow. It also cannot
report end of input: its numbered select indexes out of range when input ends.
The line renderer reads a byte at a time, as the shell's prompts always have,
and reports end of input to the caller, which turns it into a refusal. Scripted
input, CI logs and screen readers therefore get the same questions in a form
that behaves predictably.

**Prompts never reach standard output** (ADR 0003). Both renderers write only
to the writer the shell hands them, which is standard error.

Two defects in huh 2.0.3 are contained in the wizard package. Before its first
frame, a text field with a placeholder fails to draw unless a width is set, so
the package passes the terminal's width (80 when that cannot be read). When a
form's program ends before it is answered, huh dereferences a nil model, so
the package recovers and reports that as no answer. Tests in the package drive
the forms with key sequences through an open pipe.

## Considered Options

- **Keeping only the numbered prompts** in `internal/app/prompt.go`. This adds
  no dependencies, and the existing prompts were sound. It was the
  recommendation, and the maintainers chose forms for the setup experience
  instead. The numbered prompts survive as the non-terminal renderer, so this
  choice costs nothing where a terminal is absent.
- **bubbletea directly.** It is the same dependency graph minus one module, and
  every field, validation display and key binding would have to be written by
  hand.
- **huh's accessible mode for non-terminals.** Rejected for the read-ahead and
  end-of-input defects above.

## Consequences

The shell's module graph grows by the charm libraries and their terminal
dependencies (about twenty-five modules), and `golang.org/x/sys` moves forward with
them. huh 2.0.3 needs Go 1.25.8, so the shell's module and the workspace now
declare that version; the SDK and the product modules stay at 1.25.0. None of them are reachable from the SDK or from product modules. The
existing prompts (login target, context name, client ID, `context edit`'s
retry, product removal and update confirmations) now go through the same
package, and the confirmations print `[y/N]` hints the package supplies.
