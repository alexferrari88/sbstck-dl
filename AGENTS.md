# Agent Instructions

## Project worklog

Read the project-level `worklog.md` before changing the project. Update it when the tracked goal, decisions, blockers, or next action changes materially, and leave it accurate at handoff.

## Finishing work

At the end of every task that changes this repository, run appropriate checks, commit the completed changes, and push them to `origin` without asking for confirmation. Include only task-related changes and preserve unrelated work. For branch or worktree work, follow the integration procedure below. Verify that the pushed remote ref matches the completed commit before reporting success. If validation or pushing fails, preserve the work and report the blocker.

## Completing branch or worktree work

When branch or worktree work is complete, finish integration without asking for another confirmation:

1. Inspect the working tree, branches, and worktrees; preserve unrelated work and run the checks appropriate to the completed changes.
2. Commit the completed changes, fetch `origin`, and fast-forward `main` with `git merge --ff-only <completed-branch>`.
3. Push `main` to `origin` and verify that local `main`, `origin/main`, and the remote `refs/heads/main` point to the completed commit.
4. Remove the completed linked worktree if one exists, then delete its local branch with `git branch -d`. Keep the primary checkout on `main`.

If validation, fast-forwarding, pushing, or parity verification fails, preserve the branch/worktree and report the blocker. Remove only the completed worktree and local branch after verified integration.

## Agent skills

### Issue tracker

Issues and specs live in GitHub Issues for `alexferrari88/sbstck-dl`.
See `docs/agents/issue-tracker.md`.

### Triage labels

Use the five default triage labels.
See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: root `GLOSSARY.md` and `docs/adr/`.
See `docs/agents/domain.md`.
