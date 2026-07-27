# Issue tracker: GitHub

Issues and PRDs for this repo live as GitHub issues in `wso2/wso2-cli`. Use the `gh` CLI with `--repo wso2/wso2-cli` for all operations.

## Conventions

- **Create an issue**: `gh issue create --repo wso2/wso2-cli --title "..." --body "..."`. Use a heredoc for multi-line bodies.
- **Read an issue**: `gh issue view <number> --repo wso2/wso2-cli --comments`, filtering comments by `jq` and also fetching labels.
- **List issues**: `gh issue list --repo wso2/wso2-cli --state open --json number,title,body,labels,comments --jq '[.[] | {number, title, body, labels: [.labels[].name], comments: [.comments[].body]}]'` with appropriate `--label` and `--state` filters.
- **Comment on an issue**: `gh issue comment <number> --repo wso2/wso2-cli --body "..."`
- **Apply / remove labels**: `gh issue edit <number> --repo wso2/wso2-cli --add-label "..."` / `--remove-label "..."`
- **Close**: `gh issue close <number> --repo wso2/wso2-cli --comment "..."`

## Pull requests as a triage surface

**PRs as a request surface: no.** _(Set to `yes` if this repo treats external PRs as feature requests; `/triage` reads this flag.)_

When set to `yes`, PRs run through the same labels and states as issues, using the `gh pr` equivalents:

- **Read a PR**: `gh pr view <number> --repo wso2/wso2-cli --comments` and `gh pr diff <number> --repo wso2/wso2-cli`.
- **List external PRs for triage**: `gh pr list --repo wso2/wso2-cli --state open --json number,title,body,labels,author,authorAssociation,comments`, then keep only `authorAssociation` values `CONTRIBUTOR`, `FIRST_TIME_CONTRIBUTOR`, or `NONE`.
- **Comment / label / close**: use `gh pr comment`, `gh pr edit`, and `gh pr close` with `--repo wso2/wso2-cli`.

GitHub shares one number space across issues and PRs, so a bare `#42` may be either. Resolve it with `gh pr view 42 --repo wso2/wso2-cli`, then fall back to `gh issue view 42 --repo wso2/wso2-cli`.

## When a skill says "publish to the issue tracker"

Create a GitHub issue in `wso2/wso2-cli`.

## When a skill says "fetch the relevant ticket"

Run `gh issue view <number> --repo wso2/wso2-cli --comments`.

## Wayfinding operations

Used by `/wayfinder`. The **map** is a single issue with **child** issues as tickets.

- **Map**: a single issue labelled `wayfinder:map`, holding the Notes / Decisions-so-far / Fog body.
- **Child ticket**: an issue linked to the map as a GitHub sub-issue. Where sub-issues aren't enabled, add the child to a task list in the map body and put `Part of #<map>` at the top of the child body. Labels use `wayfinder:<type>` (`research`, `prototype`, `grilling`, or `task`).
- **Blocking**: use GitHub's native issue dependencies. Where dependencies aren't available, use a `Blocked by: #<n>, #<n>` line at the top of the child body.
- **Frontier query**: list the map's open children, then drop tickets with an open blocker or assignee; first in map order wins.
- **Claim**: `gh issue edit <n> --repo wso2/wso2-cli --add-assignee @me`.
- **Resolve**: comment with the answer, close the issue, then append a context pointer to the map's Decisions-so-far.
