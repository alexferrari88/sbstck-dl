# Domain Docs

## Layout

This is a single-context repo:
- `GLOSSARY.md` at the repository root defines domain vocabulary.
- `docs/adr/` holds architecture decision records.

## Before exploring

Read the root glossary and ADRs relevant to the area being explored.
If these files do not exist, proceed silently. Do not suggest creating
them upfront. The domain-modeling skill creates them when terms or
decisions are resolved.

## Use the glossary's vocabulary

Use defined domain terms in issue titles, proposals, hypotheses, and tests.
Avoid synonyms the glossary explicitly rejects.

If a needed concept is missing, reconsider whether it belongs to the
project or note the gap for domain-modeling.

## Flag ADR conflicts

Explicitly identify any existing ADR that a proposal contradicts and
explain why the decision should be reopened.
