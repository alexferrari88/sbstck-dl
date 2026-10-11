# Triage Labels

Map each canonical triage role to its label in GitHub Issues.

| Role | Tracker label | Meaning |
| --- | --- | --- |
| needs-triage | needs-triage | Maintainer needs to evaluate this issue |
| needs-info | needs-info | Waiting on reporter for more information |
| ready-for-agent | ready-for-agent | Fully specified, ready for an agent |
| ready-for-human | ready-for-human | Requires human implementation |
| wontfix | wontfix | Will not be actioned |

When a skill mentions a role, use the corresponding tracker label.
Edit the Tracker label column to change the vocabulary.

## Priority labels

Priority is independent of category and triage state. Use one priority label per triaged issue:

| Label | Meaning |
| --- | --- |
| priority:P1 | Core download completeness or archive integrity; address first |
| priority:P2 | Offline completeness and workflow improvements; next |
| priority:P3 | Deferred export or media enhancements |

Record ordering within a priority and verification evidence in the issue's triage notes or agent brief. A high priority does not imply an issue is ready for implementation.
