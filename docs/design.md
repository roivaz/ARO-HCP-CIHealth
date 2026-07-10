# ARO-HCP-CIHealth Design

Status: current architecture snapshot  
Last updated: 2026-05-06

## Purpose

ARO-HCP-CIHealth ingests ARO CI data into PostgreSQL, derives failure patterns from stored facts over UTC time windows, and serves operator-facing report, failure-patterns, run-log, and review workflows.

The system has two runtime entrypoints:

- `cihealth run` continuously fetches source data and writes normalized facts into PostgreSQL
- `cihealth app` serves HTML and JSON surfaces from PostgreSQL-backed state and computes failure patterns on demand for the requested window

## Repository Map

The main repository areas are:

- `cmd/main.go`: Cobra CLI bootstrap
- `pkg/cli`: command wiring and PostgreSQL option binding
- `pkg/run`: continuous controller runtime
- `pkg/controllers`: fact-producing controllers and reconciliation helpers
- `pkg/source`: Prow, Sippy, and GitHub source clients plus environment defaults
- `pkg/failurepatterns`: failure-pattern extraction, aggregation, history helpers, and prepared-window logic
- `pkg/frontend`: HTTP handlers, read models, HTML rendering, and product-surface packages
- `pkg/store/contracts`, `pkg/store/postgres`: storage abstraction, PostgreSQL implementation, migrations, and init/bootstrap helpers
- `Dockerfile`: application image build
- `.github/workflows/`: test and image build/push automation
- `Makefile`: local developer workflows, image helpers, and redirect-page publishing commands
- `.cursor/skills/`: project-local review skills

## Runtime Overview

### Controller runtime

`cihealth run` is the ingestion runtime. It talks to:

- Prow for run metadata and artifacts
- Sippy for environment metrics and job summaries
- GitHub for PR metadata used by signal classification

The controller layer normalizes those inputs into PostgreSQL facts and checkpoints so the app can serve windows without reaching back to source systems during page render.

Supporting commands:

- `cihealth run-once` runs one controller reconciliation for one key
- `cihealth sync-once` runs one full controller sync for a controller
- `cihealth migrate import-legacy-data` imports historical data into the current fact store

### App runtime

`cihealth app` is the operator-facing HTTP runtime. It:

- resolves the requested UTC presentation window
- loads fact rows for the requested window and lookback horizon
- prepares failure-pattern data from those facts
- builds report, failure-pattern, run-log, and review read models
- renders HTML pages and internal JSON APIs

The app uses a prepared-window cache so multiple surfaces can reuse the same expensive fact-loading and failure-pattern preparation work.

## Data Flow

The end-to-end flow is:

1. Source clients fetch CI data from Prow, Sippy, and GitHub.
2. Controllers normalize that data into fact tables, metrics tables, and checkpoint/state rows in PostgreSQL.
3. The app resolves a request into a UTC window.
4. The failure-pattern engine loads `RunRecord`, `RawFailureRecord`, and related facts for that window and its history horizon.
5. The extractor derives canonical failure-pattern text and supporting search/query fields from raw failures.
6. Aggregation groups matching failures into environment-scoped failure patterns and computes references, contributing tests, occurrences, runs affected, impact, and cross-environment presence.
7. Read-model builders compute history signals, review signals, and surface-specific summaries before rendering HTML or JSON.

## Storage Model

PostgreSQL is the active runtime store behind `pkg/store/contracts` and `pkg/store/postgres`.

The persisted model includes:

- run facts such as `cfa_runs`
- raw failure facts such as `cfa_raw_failures`
- daily metrics such as `cfa_metrics_daily`
- source checkpoints, sync state, and related metadata

The app does not depend on precomputed report snapshots. Failure patterns, history signals, and review signals are derived from the current fact store for the requested window.

## Failure-Pattern Model

Failure-pattern extraction is fact-backed and window-scoped.

For each raw failure row, the extractor derives:

- a canonical `failure_pattern` string
- a search-oriented query phrase
- provenance/debug context such as `signature_id`
- the failure lane (`provision`, `e2e`, or `other`)

Aggregation groups failures primarily by environment and extracted failure-pattern text. `signature_id` is retained as provenance and debugging context, but it is not the primary identity key for pattern merging.

The resulting read models carry operator-facing fields such as:

- occurrences
- runs affected
- run impact
- contributing tests
- sample references
- also-in environments

History and review logic is built from those same fact-backed patterns. User-facing signal labels are `Regression`, `Flake`, `Noise`, and `Indeterminate`.

## Time And Window Model

All request windows are UTC.

The main window shapes are:

- explicit date windows via `start_date=YYYY-MM-DD&end_date=YYYY-MM-DD`
- Monday-starting week windows via `week=YYYY-MM-DD`
- day windows for run-log views via `date=YYYY-MM-DD`

A week is a seven-day UTC window anchored by a Monday `YYYY-MM-DD` date. The report surface supports both week-based navigation and explicit date-window queries.

The app also uses a history horizon when computing signals and prior presence, so classifications are based on the current window plus prior fact-backed context rather than only the visible rows.

## Product Surfaces

The primary surfaces served by `cihealth app` are:

- `/`: rolling 7-day report landing page
- `/report`: report surface for either a week-shaped or explicit date window
- `/failure-patterns`: detailed failure-pattern window view
- `/run-log`: day-scoped run history view
- `/api/run-log/day`: JSON form of the day-scoped run-log surface
- `/api/review/signals/window`: internal review-signal diagnostics for a date window

The run-log surface is intentionally run-centric. It loads one UTC day of runs and raw failures, then enriches those rows with the contributing failure-pattern matches for that same day.

Tide batch runs are classified from the run URL (`.../pull/batch/...`), not from a stored flag. They are surfaced with a `batch` badge (replacing the `post-good`/`merged PR` badges) and are treated as post-good in the DEV daily metrics, since every PR in a batch has already passed e2e in its own PR check.

## User-Facing Terminology

The UI and docs use these core terms:

- **Failure pattern**: normalized recurring CI failure extracted from raw logs
- **Occurrences**: number of matching failure rows in the selected window
- **Runs affected**: number of distinct CI runs exhibiting the pattern
- **Run impact**: percentage of runs in the environment affected by the pattern
- **Signal**: categorical classification shown to operators (`Regression`, `Flake`, `Noise`, `Indeterminate`)
- **Failed at**: where the failure occurred (`provision`, `e2e`, or `other`)
- **Also in**: other environments where the same pattern was observed in the selected window

## Local Development

Local development defaults to embedded PostgreSQL with initialization and migrations enabled. That is a convenience for running the current architecture locally; the runtime itself is still the same PostgreSQL-backed app + controller system.

Common local workflows are:

- `make run-controllers`
- `make app`
- `make db-dump-remote`
- `make db-restore-local`

Switching to a remote PostgreSQL instance is a runtime configuration choice through `--storage.postgres.*` flags.

## Build And Publishing

This repository contains the application runtime, image build, and developer automation artifacts.

- `Dockerfile` builds the `quay.io/roivaz/cihealth` image
- `.github/workflows/unit-tests.yaml` runs the unit-test suite
- `.github/workflows/build-and-push-image.yaml` builds and, on non-PR events, pushes the container image
- `Makefile` wraps local build/test workflows and the optional storage-account redirect-page publishing flow

Hosted deployment manifests are maintained outside this repository.

## Storage-Account Redirect

The repo still supports publishing a minimal `index.html` / `404.html` redirect page to an Azure Storage static website container. That redirect exists only to hand users off to the hosted app URL. It does not render reports or failure patterns itself.

## Validation

Default repo validation:

- `make check`

Focused validation loops:

- `go test ./pkg/failurepatterns/...`
- `go test ./pkg/frontend/...`
- `go test ./pkg/store/postgres/...`

## Current Limits

The most important current limitations are:

- the run-log surface is useful for day-scoped investigation, but it does not yet carry the full richer build-history metadata available in Prow
- some raw failures can still reference runs that need additional run-record backfill or lookup
- hosted app operation, auth, backups, and runbooks are still being hardened operationally
