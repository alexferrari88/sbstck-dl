# Current work

Goal: merge the verified media metadata PR #29, then implement PR #26's focused offline archive rebuild brief and ship the completed change to main.

Status: implementation and validation complete. #29 merged as 098bef063dccf967bfcd5f5913fef4bc6149052a. #26 adds the standalone offline update-archive-page command, documentation and actual CLI regressions over the existing archive workflow.

Scope: add update-archive-page for one selected HTML, Markdown or text archive using existing restoration and publication. Retain metadata and local covers, discover legacy files, reflect additions/deletions including the final post, and preserve post bodies. Reject invalid input and restoration/publication failures without replacing the previous valid index. No network, media download or duplicate scanner; existing incremental/multiple-format download behavior stays covered.

Decisions: output defaults to the current directory; format defaults to HTML and accepts html/md/txt. Reuse the current archive workflow with no downloader and bypass the root download networking setup. Preserve existing legacy filename and unknown-date behavior. Metadata saves before the index using existing failure-safe publication; an index publication failure can leave updated metadata available for retry while preserving the prior index. No library or download-pipeline changes were needed.

Validation: full repository tests, full race tests, build, vet, gofmt and diff checks pass. Actual CLI regressions cover HTML/md/txt additions/deletions/final deletion, legacy/unknown-date discovery, retained metadata and local covers after folder movement, unchanged bodies, zero captured network requests, independent formats, invalid arguments/manifests, unreadable paths and failed publication. All non-root POSIX permission cases ran on Linux. Independent correctness review and existing incremental/archive regressions pass. Windows amd64 and macOS arm64 binaries and command test binaries compile with CGO disabled; runtime checks ran on Linux. #29 also passed exact-head/integrated tests and six localhost metadata fixtures before merge.

Next action: none for this implementation. Future downloader work in #30 must preserve the public metadata API accepted in #29; this change does not publish a release.
