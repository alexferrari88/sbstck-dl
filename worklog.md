# Current work

Goal: implement issue #32, an opening table of contents page in exported EPUB books.

Spec: https://github.com/alexferrari88/sbstck-dl/issues/32#issuecomment-6471224386. The reporter confirmed the reader contents panel works on Windows; the request is for a visible contents page before the oldest chapter. Preserve chapter ordering, existing reader navigation, single-post exports, offline portability, metadata, and atomic publication.

Status: implementation in isolated branch `codex/issue-32-opening-contents`. CLI regression checks first failed on the baseline because the reading order contained only chapters. The existing navigation document now appears first in that order and has an explicit Table of contents heading. README documents the default behavior and re-exporting older books.

Validation: in progress. Use the accepted CLI seam to inspect actual exported package reading order and contents destinations. Run EPUBCheck 5.4.0, Calibre 9.16 and KOReader desktop checks with a moved book and isolated networking, relevant race checks, vet, and the full Go suite. Independent standards/spec reviews follow against starting commit 0860c89.

Next action: complete validation and reviews, integrate and push main with verified remote parity, then close #32. Release publication is outside this implementation task.
