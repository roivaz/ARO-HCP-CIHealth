# ARO-HCP-CIHealth Design

Status: current architecture snapshot  
Last updated: 2026-04-19

## Purpose

ARO-HCP-CIHealth ingests CI run/failure data, derives failure patterns inline from stored facts over arbitrary UTC windows, and serves operator-facing report, failure-patterns, run-log, and review workflows.

The important architectural point is that the Go app + PostgreSQL runtime is the current architecture, not a future target state.

## System Overview

The runtime has three main planes:

1. **Controllers**
   - Ingest and derive facts into PostgreSQL.
   - Main entrypoint: `cihealth run`
   - Supporting debug helpers: `cihealth run-once`, `cihealth sync-once`

2. **Inline failure-pattern engine**
   - Loads fact rows from PostgreSQL for the requested UTC time range.
   - Extracts row-level failure patterns and aggregates them deterministically in memory.
   - Computes history and review signals from fact-backed lookback windows instead of stored semantic-week snapshots.
   - Legacy Phase3 link tables may still exist, but the runtime no longer reapplies them in read models.

3. **Product surfaces**
   - `cihealth app` serves report/failure-patterns/run-log views from PostgreSQL plus internal diagnostics APIs.
   - Azure Storage can still host a tiny redirect page that points users at the hosted app URL.

## Codebase Map

The architecture maps to the repository roughly like this:

- CLI bootstrap and command surface:
  - `cmd/main.go`
  - `pkg/cli`
- Controllers and source ingestion:
  - `pkg/run`
  - `pkg/controllers`
  - `pkg/source`
- Failure-pattern extraction and range loading:
  - `pkg/failurepatterns`
  - `pkg/failurepatterns/extractor`
  - `pkg/failurepatterns/window`
- Product-facing HTTP, read-model, and HTML surfaces:
  - `pkg/frontend`
  - `pkg/frontend/readmodel`
  - `pkg/frontend/ui`
  - `pkg/frontend/report`
  - `pkg/frontend/failurepatterns`
  - `pkg/frontend/runlog`
- Runtime storage contract and PostgreSQL implementation:
  - `pkg/store/contracts`
  - `pkg/store/postgres`
- Deployment and publishing artifacts:
  - `deploy/`
  - `Dockerfile`
  - `infra/azure/`

## Week Compatibility Contract

The public week contract is now a compatibility layer, not a persisted storage model:

- a week is still keyed by a Monday-starting `YYYY-MM-DD`
- week-shaped routes and CLI flags expand to concrete UTC `[start_date, end_date]` windows
- history and navigation still reason in calendar-week units where that improves presentation
- no stored semantic-week snapshot is required to serve report, failure-patterns, run-log, or review APIs

## Inline Failure-Pattern Model

The active workflow is:

1. **Fact loading**
   - Read `RunRecord`, `RawFailureRecord`, and metric rows from PostgreSQL for the requested UTC range.
   - Expand week-shaped requests into date windows before data loading.

2. **Row-level extraction and deterministic aggregation**
   - Extract `failure_pattern` text and a search-query phrase per raw failure row.
   - Aggregate rows into environment-scoped failure patterns in memory.
   - Derive references, contributing tests, impact, seen-in, and sampling data from the loaded facts.

3. **History and review diagnostics**
   - Compute prior-week presence and lookback signals from fact-backed history windows.
   - Emit review items as diagnostic output for extraction/merge quality, not as persisted semantic truth.
   - Improvements happen by changing extraction or merge logic and rerunning against the same facts.

For the detailed historical background and terminology drift, see `docs/semantic-materialization.md`.

## Failure-Pattern Identity V2

The current inline engine implements the `v2` failure-pattern identity rules.

Key rules:

- failure-pattern identity is driven by extracted `failure_pattern` text, not by the raw `signature_id`
- `signature_id` is still retained as provenance/debug context and as one possible search pivot
- durable raw-to-failure-pattern joins use row/run anchors, especially `(environment, run_url, row_id)`

This matters because failure-pattern quality problems now tend to fall into three classes:

- extraction is too generic, so distinct failures overmerge
- extraction is too noisy or instance-specific, so identical failures undermerge
- extraction misses the most useful nested detail, so the resulting `failure_pattern` is low quality even if clustering is technically correct

## Terminology And Search Notes

User-facing docs and UI now use "failure patterns" and "run log" for the operator surfaces layered on top of fact-backed windows.

Some internal files, symbols, or helper names may still retain phase-oriented terminology such as `global`, `signature`, or `semantic` from the removed weekly materialization pipeline. That does not usually indicate a different product surface. When searching the repo, check both the user-facing and legacy terms unless you are specifically working on failure-pattern merge semantics.

## UI Terminology

The following user-facing terms are used consistently across all report and failure-pattern views. When adding or editing UI labels, tooltips, or copy, use these terms, not the internal names from the storage model or semantic pipeline.

### Core Concepts

| User-facing term | Replaces | Definition |
|---|---|---|
| **Failure pattern** | signature, failure signature, semantic cluster | A recurring, normalized form of a CI failure extracted from raw logs. |
| **Occurrences** | support count, matched failure support, total signature support | The number of individual CI failures matching a given pattern within the selected window. |
| **Runs affected** | jobs affected | The number of distinct job runs exhibiting a given failure pattern. |
| **Run impact** | impact | Percentage of all job runs in the environment affected by this pattern (`runs affected / total runs`). |
| **Signal** | flake score, flake signal, bad PR | Categorical classification of each failure pattern: **Regression** (likely caused by a specific PR), **Flake** (recurring intermittent failure), **Noise** (low-quality extraction), or **Indeterminate** (insufficient data). Displayed as a color-coded label with reasons in the hover tooltip. |
| **After last push of merged PR** | after last push, PostGoodCount | Runs that occurred after the last push of a PR that eventually merged. Used as an input to regression detection; no dedicated main-table column. |
| **Failed at** | lane | Where in the job run the failure occurred: `provision`, `e2e`, or `other`. |
| **Also in** | seen in | Other environments where the same failure pattern was also detected during the selected window. |

### "Failed at" Values

| Value | Meaning |
|---|---|
| `provision` | Failure during environment setup. DEV only — each DEV presubmit deploys its own ephemeral Azure environment before running tests. |
| `e2e` | Failure during the test suite execution step. Applies to all environments. |
| `other` | Failure due to CI infrastructure issues, image build problems, etc. CFA does not extract failure patterns for these. Replaces `unknown`. |

### View Names (Navigation)

| Label | Route | Notes |
|---|---|---|
| Last 7 Days | `/report` (rolling) | Rolling 7-day report window |
| Weekly Report | `/report` (week-anchored) | Per-week semantic report |
| Failure Patterns | `/failure-patterns` | Windowed failure-pattern view |
| Run Log | `/run-log` | Day-scoped run history |

### What Not to Expose in User-Facing Views

- Numeric scores for signals — use the categorical label (Regression / Flake / Noise / Indeterminate); classification reasons appear in the column hover tooltip only.
- Quality flag badges (`context type stub leaked`, `source deserialization/no-output error`, `struct/object fragment`, etc.) — internal diagnostics only.
- `Failure-pattern threshold` summary card — remove entirely.
- Internal names such as `signature`, `support`, `lane`, `matched failure support` — use the table above.

## Storage Model

PostgreSQL is the active runtime store behind `pkg/store/contracts` and `pkg/store/postgres`.

The current persisted model is:

- facts/state tables such as `cfa_runs`, `cfa_raw_failures`, `cfa_metrics_daily`, and related checkpoints/metadata
- legacy semantic tables that may still exist in PostgreSQL but are no longer used by the active runtime:
  - `cfa_sem_global_clusters`
  - `cfa_sem_review_queue`
- older schemas created legacy Phase3 tables that current migrations now drop:
  - `cfa_phase3_issues`
  - `cfa_phase3_links`
  - `cfa_phase3_events`

Important persistence rule:

- the active runtime persists facts, checkpoints, and test metadata only
- read models derive failure patterns and review signals inline from the fact tables
- legacy semantic/Phase3 rows are ignored by the runtime

NDJSON is no longer part of the runtime architecture. It remains only as a legacy import format for `cihealth migrate import-legacy-data`.

## Presentation Windows

User-facing report surfaces resolve a presentation window independently from the underlying fact storage:

- `/report` accepts either `week=YYYY-MM-DD` or `start_date=YYYY-MM-DD&end_date=YYYY-MM-DD`
- `/` redirects to a rolling 7-day `/report` window
- `/failure-patterns` and `/run-log` use the same shared window resolver for navigation and range normalization

The important invariant is that this does **not** change the underlying fact store:

- the app expands week-shaped requests into explicit UTC date windows
- the app loads fact rows directly for the requested window and lookback horizon
- cross-week operator views are composed in memory at query/render time

Identity across windows follows explicit rules:

- use the environment plus extracted failure-pattern text/search query from the inline aggregated rows
- never treat `signature_id` alone as semantic identity

The field contract also stays explicit:

- window-local fields are recomputed across the requested range, for example runs affected, impact, seen-in, references, and samples
- signal inputs (scoring references, trend, after-last-push, prior-weeks-present) are derived from a signal horizon — the union of the presentation window and at least 3 prior weeks — ensuring stable classification regardless of the user's selected date range

## Local Runtime Model

Local operation defaults to embedded PostgreSQL with initialization and migrations enabled. That is a development convenience, not the architecture itself.

In practice:

- `cihealth run` and `cihealth app` operate against PostgreSQL
- embedded Postgres is the default local transport
- switching to remote PostgreSQL is a configuration detail via `--storage.postgres.*`, not a different runtime model

## Product Surfaces

### Unified app

`cihealth app` is the primary operator surface:

- report view (`/report`) with classic week-shaped and arbitrary-window modes
- failure-patterns view
- day-scoped run history view (`/run-log`, `/api/run-log/day`)
- internal review-signals endpoint (`/api/review/signals/window`)
- cross-week history lookups based on fact-backed lookback windows

### Day run history view and current fact gap

The run-history surface is intentionally day-scoped and run-centric:

- it loads the requested UTC day of `RunRecord` + `RawFailureRecord` facts
- resolves the requested day through the shared presentation-window rules used by the other report surfaces
- enriches each raw failure row by matching it against the contributing inline failure patterns for that day

This gives a Prow-like operator view, but it is not yet a full Prow-history data model.

Current gap to keep explicit:

- `RunRecord` currently stores `run_url`, `job_name`, PR metadata, `failed`, and `occurred_at`
- it does not yet include richer run-history fields such as duration/build metadata or more detailed terminal run state
- some raw failures can still reference runs whose `RunRecord` needs backfill/lookup, so the first version of the run page is informative but not a complete fact model

### Storage-account redirect

The old static export flow has been removed.

The remaining Azure Storage publishing step is intentionally narrow:

- generate a tiny `index.html`/`404.html` redirect page
- upload that redirect page to the storage account's static website container
- hand users off to the hosted app URL

It is not part of failure-pattern generation or report rendering; it only preserves an existing access path while hosted operation is being hardened.

## Current Command Surface

Primary commands:

- `cihealth run`
- `cihealth app`

Secondary maintenance/debug commands:

- `cihealth run-once`
- `cihealth sync-once`
- `cihealth migrate import-legacy-data`

## Key Design Decisions

1. **App + DB is the primary runtime**
   - Report, failure-patterns, run-log, and diagnostics all read from PostgreSQL-backed state.

2. **Week semantics are compatibility-only**
   - The UI and APIs may still present Monday-starting week labels, but the runtime loads concrete date windows from fact tables.

3. **Failure-pattern identity is text-first**
   - Extracted failure-pattern text drives merge semantics; `signature_id` remains provenance only.

4. **Derived outputs are computed inline**
   - Failure patterns, history signals, and review rows are recomputed from facts for the requested range instead of being persisted as weekly snapshots.

5. **Review queue drives improvement loops**
   - Failure-pattern quality is improved by inspecting review signals and refining extraction/merge behavior against the same fact inputs.

6. **Storage-account publishing is redirect-only compatibility**
   - It preserves an existing entrypoint without duplicating the report surface outside the live app.

## Developer Orientation

For a new coding session, the default validation loop is:

- `make check` for broad repo validation
- `go test ./pkg/failurepatterns/...` for extraction, merge, or history-window work
- `go test ./pkg/frontend/...` for app/report work
- `go test ./pkg/store/postgres/...` for store or migration work

For manual smoke testing, use the main runtime commands described in `README.md`. Agent-oriented repo guidance lives in `AGENTS.md`.

## Next Milestone

The major remaining phase is hosted operation.

That work includes:

- deploying the Go app in a hosted environment
- running against managed PostgreSQL
- scheduling controllers and other fact-producing maintenance flows
- adding auth, deployment automation, backups, and runbooks
- operating the storage-account redirect and hosted app path together until the redirect is no longer needed

The architecture refactor is largely complete; the next work is operationalization and continuous failure-pattern quality improvement.
