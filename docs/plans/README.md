# Plans

One document per delivered slice, written as a record of what shipped rather
than as a set of instructions for shipping it.

**Task breakdowns are not committed here.** The step-by-step of building
something is scaffolding: it goes stale the moment the work merges, and the
code is a better answer to "how" than a document describing code that has since
changed. Keep the breakdown wherever you are working and let it go when the
branch does; the history holds it if anyone needs it.

Three artifacts divide the work between them:

| Artifact | Answers | Expires |
| --- | --- | --- |
| The GitHub issue | What should be true, and why | At delivery |
| A record here | What shipped, and what the plan got wrong | When the slice is forgotten |
| [`docs/adr/`](../adr/) | Which way we decided, and what we rejected | Never |

The spec lives in the issue and is linked, not copied. Decisions live in an
ADR, because the reason a thing is built one way outlives every plan to build
it.

## Shape

    # Title
    **Status:**   delivered, and through which PRs
    **Date:**
    **Outcome:**  one line
    **Related:**  the spec issue, architecture, and the ADRs involved

    ## Goal
    ## Architecture
    ## What shipped
    ## What outran the plan
    ## Verification

**What outran the plan** is the section worth writing carefully. A plan that
turned out to be right in every particular teaches nobody anything; the claims
it got wrong, and how they were caught, are what a reader a year from now
actually needs. Both records here carry one.
