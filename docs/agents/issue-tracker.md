# Issue tracker: GitHub

Issues and specs live in GitHub Issues for `alexferrari88/sbstck-dl`.
Use the `gh` CLI from this clone; infer the repository from its remote.

## Conventions

- Create: `gh issue create --title "..." --body-file <path>`.
  Use a temporary Markdown file for multiline bodies.
- Read: `gh issue view <number> --json number,title,body,labels,comments`.
- List: `gh issue list --state open --json number,title,body,labels,comments`.
  Apply appropriate label and state filters.
- Comment: `gh issue comment <number> --body-file <path>`.
- Apply or remove labels: `gh issue edit <number> --add-label "..."`
  or `--remove-label "..."`.
- Close: `gh issue close <number> --comment "..."`.

## Pull requests as a triage surface

**PRs as a request surface: no.**

GitHub issues and PRs share a number space. Resolve ambiguous references
with `gh pr view <number>` and fall back to `gh issue view <number>`.

## Skill operations

When a skill says "publish to the issue tracker", create a GitHub issue.
When it says "fetch the relevant ticket", read the referenced issue.

## Wayfinding operations

The map is one issue labelled `wayfinder:map`, containing Notes,
Decisions-so-far, and Fog. Each child ticket is a separate issue labelled
`wayfinder:<type>` (research, prototype, grilling, or task).

Link children with GitHub sub-issues when supported. Otherwise, put
`Part of #<map>` at the top of each child and add it to the map's task list.

Use native GitHub issue dependencies when supported. Otherwise, put
`Blocked by: #<number>, #<number>` near the top of the child body.
A ticket is unblocked when all its blockers are closed.

The frontier is the map's open, unblocked, unassigned children, in map order.
Claim a ticket before working: `gh issue edit <number> --add-assignee @me`.
Resolve it by commenting with the answer, closing it, and adding a context
pointer to the map's Decisions-so-far.
