# Current work

PR #33 adds opt-in downloads for in-text audio clips. The contributor commit and the accepted agent brief are preserved. [PR #33](https://github.com/alexferrari88/sbstck-dl/pull/33) · [Agent brief](https://github.com/alexferrari88/sbstck-dl/pull/33#issuecomment-6762928053)

The clip resolver authenticates at the publication selected by `--url` and transfers CDN audio without the subscriber cookie. Local players replace real clip markers in place. An older saved HTML post without a marker gets one labelled fallback per missing clip, with its surrounding bytes and permissions preserved. The opt-in command supports both direct post and bulk backfill. An HTML write without clips makes existing clip publication pending before replacing or restoring the post, so the next clip run can repair its players from cached bytes.

Linux validation at `7c3fd65909edf5b4c33b963247643b0ca73d9a07` passed the full Go suite, full race suite, vet, build, and changed-file formatting and diff checks. Seven independent CLI scenarios covered credential isolation, cached retries, preservation of edits, preview upgrades, inert markup, and failed publication. GitHub's Linux, macOS, Windows, and EPUB conformance jobs passed on a merge tree identical to that source. [Run 42788656814](https://github.com/alexferrari88/sbstck-dl/actions/runs/42788656814)

The standards re-review found no documented violations and retained a nonblocking note about duplicated clip and video outcome handling. The spec re-review cleared the backfill, marker, and completion findings. Clip completion repair uses readable media manifests. Plain downloads treat unreadable manifests as optional.
