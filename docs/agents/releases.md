# Releases

Use `.github/workflows/build-release.yml` to publish releases. Do not publish a
release through the GitHub UI or `gh release create` directly: those paths bypass
the validation gate.

1. Update `cmd/version.go` to the intended `vX.Y.Z` version, commit, and push it.
2. Optionally prepare a **draft** release with custom notes targeting that exact
   commit. Leave it unpublished. Otherwise the workflow generates release notes.
3. Dispatch the workflow from `main`, specifying the source and matching tag:

   ```sh
   gh workflow run build-release.yml --ref main -f branch=main -f tag=vX.Y.Z
   ```

The source is resolved once to a commit SHA. Each native Linux, macOS, and Windows
job runs the publication-gate tests, application tests, vet, and the CLI version
check, then builds its amd64 binary and decoder-source bundle. The final job runs
only if all three jobs succeed. It requires all six nonempty assets and source
notices, uploads to a draft, checks the uploaded asset names and SHA-256 digests,
and publishes last. Failed tests/builds create no release. Failed uploads or
verification leave an unpublished draft for inspection and retry.

Existing tags must point to the validated commit. Published releases are never
modified. A draft without an existing tag must target the exact commit SHA, not
a moving branch name. Rerun the failed workflow or dispatch again with the same
source commit and tag to retry a failed draft; an unexpected extra draft asset
must be removed before publication. Runs for the same tag are serialized.

For validation without a release, omit the tag:

```sh
gh workflow run build-release.yml --ref main -f branch=main
```

This runs all three native jobs and the complete asset check, retaining binaries
as workflow artifacts without creating a tag, draft, or public release. Do not
dispatch the new workflow against an older source commit that lacks its scripts.
After publication, verify the run succeeded and the release has all six assets.

This workflow controls its own publication path. Repository maintainers with
release-write access can still bypass it by manually publishing; use the command
above for every release.
