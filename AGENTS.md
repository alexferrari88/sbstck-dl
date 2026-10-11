# Agent Instructions

## Completing branch or worktree work

When Alex says the work is completed, finish integration without asking for another confirmation:

1. Inspect the working tree, branches, and worktrees; preserve unrelated work and run the checks appropriate to the completed changes.
2. Commit the completed changes, fetch `origin`, and fast-forward `main` with `git merge --ff-only <completed-branch>`.
3. Push `main` to `origin` and verify that local `main`, `origin/main`, and the remote `refs/heads/main` point to the completed commit.
4. Remove the completed linked worktree if one exists, then delete its local branch with `git branch -d`. Keep the primary checkout on `main`.

If validation, fast-forwarding, pushing, or parity verification fails, preserve the branch/worktree and report the blocker. Remove only the completed worktree and local branch after verified integration.
