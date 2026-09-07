# Domain Docs

How engineering skills should consume this repository's domain documentation.

## Before exploring, read these

- **`CONTEXT.md`** - the normative sales-invoice domain language and invariants.
- **`CONTEXT-MAP.md`** - relationships and ownership boundaries between the contexts described by the root domain model.
- **`docs/adr/`** - ADRs relevant to the area being changed.

If any of these files are absent, proceed silently. Domain-modeling skills create or extend them only when terms or decisions are resolved.

## File structure

This is a single-context repository:

```text
/
├── CONTEXT.md
├── CONTEXT-MAP.md
└── docs/adr/
```

`CONTEXT-MAP.md` documents relationships within the root domain model; it is not a router to separate package-level context documents.

## Use the glossary's vocabulary

When naming a domain concept in an issue, proposal, hypothesis, or test, use the term defined in `CONTEXT.md`. Avoid drifting to synonyms that obscure the established domain language.

If a required concept is missing, reconsider whether it belongs to the project vocabulary or record the gap for domain modeling.

## Flag ADR conflicts

If proposed work conflicts with an existing ADR, surface that conflict explicitly instead of silently overriding the decision.
