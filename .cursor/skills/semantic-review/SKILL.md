---
name: semantic-review
description: >-
  Review failure-pattern quality in CI Failure Atlas by querying the local
  failure-pattern and review-signals window APIs, identifying overmerged,
  undermerged, and low-quality failure patterns, and proposing concrete engine
  improvements. Use when the user asks to review failure patterns, check
  extraction quality, audit semantic output, or run a review pass.
---

# Failure Pattern Review

Review the currently extracted failure patterns from the local app, identify
semantic quality problems, and propose concrete improvements to the inline
failure-pattern workflow. Work read-only unless the user explicitly asks for
implementation.

## Review Categories

1. **Overmerged** — distinct failures merged into one pattern (mixed root causes, generic wrapper hiding multiple issues)
2. **Undermerged** — multiple patterns clearly describe the same issue, differing only by noise (resource names, timestamps, IDs)
3. **Low-quality** — stable pattern text that isn't useful (wrapper instead of nested cause, too generic, missing key detail)

## Workflow

### Step 1 — Determine the target window

- Default to the last 7 UTC calendar days unless the user specifies a window.
- If the user asks for a specific week or "go back N weeks," convert that into
  explicit `start_date` / `end_date` bounds (Monday-starting UTC weeks).
- The user may also ask for an explicit date range.

### Step 2 — Fetch failure patterns

```
Base URL: http://127.0.0.1:8082
GET /api/failure-patterns/window?start_date=<start>&end_date=<end>
```

If the response has zero rows across all environments, step back one week at a
time (up to 8 attempts) until a non-empty window is found.

### Step 3 — Fetch review signals

```
GET /api/review/signals/window?start_date=<start>&end_date=<end>
```

Treat signals as a **prioritization input**, not ground truth. Start with
high-severity signals, then work down.

Key signal reasons and what they mean:

| Reason | What it detects |
|--------|-----------------|
| `new_pattern` | Pattern is absent from the configured prior-history window |
| `recurrence` | Pattern existed in prior history but not in the immediately previous equal-length window |
| `weak_canonical_needs_review` | Pattern canonical is too weak or generic to trust without inspection |
| `ambiguous_provider_anchor` | Multiple provider anchors were merged into one pattern |

Signals include a `severity` field (`high` / `medium` / `low`).

### Step 4 — Inspect suspicious patterns

For each suspicious pattern, fetch its detail view including:
`failure_pattern_id`, `failure_pattern`, `runs_affected`, `occurrences`,
`failed_at`, `contributing_tests`, `full_error_samples`, `affected_runs`.

### Step 5 — Corroborate when needed

Use [Search OpenShift CI](https://search.dptools.openshift.org/) to validate
suspected merge boundaries with real log-level evidence.

## Review Heuristics

- Patterns whose samples describe multiple different failures → likely overmerged.
- Generic text + diverse raw samples → likely overmerged.
- Nearly identical text differing only by noise → likely undermerged.
- Phrases that omit the most useful nested error detail → low quality.
- Wrapper phrases, provider-only phrases, placeholder-dominated phrases → suspicious.
- Require evidence from ≥2-3 representative samples before confirming a finding.
- Not every review signal is a confirmed defect — validate before reporting.

## Codebase Focus Areas

When inspecting the engine to understand why a problem occurs:

- `pkg/failurepatterns/extractor` — evidence extraction and canonicalization
- `pkg/failurepatterns/window` — load/extract/aggregate flow and review-item generation
- `pkg/failurepatterns/failurepatterns.go` — pattern presence / history helpers
- `pkg/frontend/readmodel` — review signal computation, API models

## Finding Template

For each finding, produce:

- **Category**: overmerged / undermerged / low-quality
- **Severity**: high / medium / low
- **Environment(s)**
- **Failure pattern ID(s)**
- **Current failure pattern text**
- **Why it looks wrong**
- **Evidence**: relevant API fields, representative `full_error_samples`, CI-search observations
- **Likely pipeline layer**: extraction, merge identity, readmodel, review-signal heuristics
- **Recommended improvement**
- **Suggested regression coverage**

## Output Format

1. Findings ordered by severity (high first).
2. Short **Improvement plan** grouping recommendations by pipeline layer.
3. Short **Validation plan** describing how to verify after the next app run.

## Constraints

- Tie every recommendation to a specific observed pattern or class of patterns.
- Distinguish confirmed problems from weaker suspicions.
- Ignore deprecated Phase3/manual-linking state.
- Do not implement changes unless the user explicitly asks.
- If the local API is unavailable, say so clearly and stop.
