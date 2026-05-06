# ARO-HCP-CIHealth

ARO-HCP-CIHealth is a PostgreSQL-backed Go application for ingesting ARO CI data, deriving failure patterns inline from stored facts over arbitrary UTC windows, and serving operator-facing report/failure-patterns/run-log views plus internal diagnostics APIs.

The app+DB runtime is the primary architecture. Dynamic HTML is served directly from PostgreSQL-backed state.

## Current Architecture

- `cihealth run` continuously ingests Sippy, Prow, and GitHub data and derives normalized facts into PostgreSQL.
- `cihealth app` serves the unified report, failure patterns, run log, and internal review-signals API from PostgreSQL, computing failure patterns inline from fact tables.

Local development defaults to embedded PostgreSQL with initialization and migrations enabled. Remote PostgreSQL is supported through the usual `--storage.postgres.*` flags.

## Repository Guide

- `cmd/main.go` bootstraps the Cobra CLI.
- `pkg/cli` defines the command surface and shared PostgreSQL setup.
- `pkg/run`, `pkg/controllers`, and `pkg/source` implement continuous ingestion and source clients.
- `pkg/failurepatterns` owns extraction, range loading, history helpers, and inline failure-pattern aggregation.
- `pkg/frontend` serves the unified report/failure-patterns/run-log app and API surface.
- `pkg/frontend/readmodel` holds shared window/history helpers and surface builders; `pkg/frontend/ui` holds shared chrome/table rendering; `pkg/frontend/report`, `pkg/frontend/failurepatterns`, and `pkg/frontend/runlog` own the active product-surface packages.
- `pkg/store/contracts` defines the store interfaces; `pkg/store/postgres` implements the active runtime store, migrations, and init/bootstrap helpers.
- `Dockerfile` builds the container image for the Go application.
- `.github/workflows/` contains the unit-test and image build/push automation.
- `Makefile` wraps local developer workflows, image helpers, and redirect-page publishing commands.
- `.cursor/skills/` contains project-local skills for failure-pattern/review workflows.

Search note: user-facing docs now say "failure patterns" and "run log", but some internal files and symbols may still use older `global`, `signature`, or `semantic`-era names. When navigating the repo, check both terms unless you are specifically working on failure-pattern merge semantics.

## Prerequisites

- Go 1.25+
- Access to the Sippy, Prow, and GitHub APIs used by the controllers
- Optional Azure CLI access if you want to upload the storage-account redirect page

## Core Workflow

### 1. Ingest facts

```bash
go run cmd/main.go run \
  --source.envs dev,int,stg,prod \
  --history.weeks 4
```

This runs the controller set continuously and keeps facts/state tables up to date in PostgreSQL.

### 2. Run the app

```bash
go run cmd/main.go app \
  --week 2026-03-22 \
  --app.listen 127.0.0.1:8082 \
  --history.weeks 4
```

Open `http://127.0.0.1:8082/` for the rolling 7-day report or `http://127.0.0.1:8082/report` for the report surface. The app derives failure patterns inline from facts for the requested date window; there is no separate materialization step.

Key app routes:

- `/` renders the rolling 7-day report window
- `/report?week=YYYY-MM-DD` renders the classic week-shaped report view
- `/report?start_date=YYYY-MM-DD&end_date=YYYY-MM-DD` renders an arbitrary UTC report window
- `/failure-patterns?start_date=YYYY-MM-DD&end_date=YYYY-MM-DD` renders the failure-patterns window view
- `/api/review/signals/window?start_date=YYYY-MM-DD&end_date=YYYY-MM-DD` returns internal review-signal diagnostics for a UTC date window

The day-scoped run history surface is:

- HTML: `/run-log?date=YYYY-MM-DD&env=dev`
- JSON: `/api/run-log/day?date=YYYY-MM-DD&env=dev`

It renders one row per run for that day and enriches attached raw failures with inline failure-pattern matches derived from the current fact store.

Current limitation: this is intentionally not yet a full Prow-history clone. `RunRecord` currently carries `run_url`, `job_name`, PR metadata, `failed`, and `occurred_at`, but not richer build/duration metadata, and some raw failures can still reference runs that need run-record backfill.

### 4. Refresh local embedded PostgreSQL from a remote dump

If you want to test against fresh production-like data locally, the `Makefile` includes Docker-backed PostgreSQL client helpers. This avoids relying on whatever `pg_dump` or `psql` version is installed on your workstation.

Typical flow:

1. Port-forward the remote PostgreSQL instance to localhost with `kubectl port-forward`.
2. Dump the remote database through that forwarded port.
3. Start the local app in another terminal so embedded PostgreSQL is up.
4. Restore the dump into the local embedded database.

Example:

```bash
# Terminal 1: remote port-forward
kubectl port-forward <pod-or-service> 5432:5432

# Terminal 2: dump the remote database through localhost
make db-dump-remote \
  REMOTE_PGUSER=<remote-user> \
  REMOTE_PGPASSWORD=<remote-password> \
  REMOTE_PGDATABASE=<remote-database> \
  DB_DUMP_FILE=.work/cihealth-prod.sql

# Terminal 3: start the local app (this starts embedded PostgreSQL by default)
make app

# Terminal 4: restore the dump into the local embedded database
make db-restore-local DB_DUMP_FILE=.work/cihealth-prod.sql
```

Notes:

- `db-dump-remote` uses the `postgres:18.3` Docker image and writes plain SQL with `--clean --if-exists --no-owner --no-privileges`.
- `db-restore-local` restores that SQL into the local database with `psql -v ON_ERROR_STOP=1`.
- Remote dump credentials are required explicitly: `REMOTE_PGUSER`, `REMOTE_PGPASSWORD`, and `REMOTE_PGDATABASE`.
- Local restore defaults to `127.0.0.1:5432` and `postgres/postgres`, but `LOCAL_PGHOST`, `LOCAL_PGPORT`, `LOCAL_PGUSER`, `LOCAL_PGPASSWORD`, and `LOCAL_PGDATABASE` can be overridden if needed.
- For safety, `db-restore-local` only allows localhost targets.

### 5. Upload the storage redirect page

```bash
make site-upload \
  AZ_STORAGE_ACCOUNT=<storage-account-name> \
  SITE_ROOT=site
```

This generates a minimal `index.html`/`404.html` redirect page under `SITE_ROOT` and uploads it to the storage account's static website container.

The redirect target defaults to `https://cihealth.tools.hcpsvc.osadev.cloud/` and can be overridden with `SITE_REDIRECT_URL=...`.

## Redirect Page Details

To preview the generated redirect locally before uploading:

```bash
python -m http.server 8080 --directory site
```

## Build And Publishing

- `Dockerfile` is the image build entrypoint for `quay.io/roivaz/cihealth`.
- `.github/workflows/unit-tests.yaml` runs the unit-test suite.
- `.github/workflows/build-and-push-image.yaml` builds and pushes the application image.
- Hosted deployment manifests live outside this repository.

## Window And Week Model

- All report windows are UTC.
- `/report` accepts either `week=YYYY-MM-DD` or `start_date=YYYY-MM-DD&end_date=YYYY-MM-DD`.
- A week is a Monday-starting seven-day UTC window keyed by `YYYY-MM-DD`.
- `/failure-patterns`, `/run-log`, and `/api/review/signals/window` build their read models from fact-backed windows.

## Validation And Developer Loop

Default validation after code changes:

```bash
make check
```

Useful focused loops:

- `go test ./pkg/failurepatterns/...` for extraction, merge, or history-window changes
- `go test ./pkg/frontend/...` for UI, API, and report rendering changes
- `go test ./pkg/store/postgres/...` for schema, migration, or query-layer changes

Useful local smoke commands:

```bash
make run-controllers CONTROLLER_ENVS=dev,int,stg,prod
make app APP_WEEK=2026-03-23
make db-dump-remote REMOTE_PGUSER=<remote-user> REMOTE_PGPASSWORD=<remote-password> REMOTE_PGDATABASE=<remote-database> DB_DUMP_FILE=.work/cihealth-prod.sql
make db-restore-local DB_DUMP_FILE=.work/cihealth-prod.sql
make site-upload AZ_STORAGE_ACCOUNT=<storage-account-name> SITE_ROOT=site
```

## Other Commands

The main runtime commands above are the normal operator surface. There are also targeted maintenance/debug helpers:

- `cihealth run-once`
- `cihealth sync-once`
- `cihealth migrate import-legacy-data`

## Next Milestone

The remaining big phase is hosted operation rather than more architectural refactoring. That work includes:

- running the Go app against managed PostgreSQL instead of local embedded defaults
- scheduling controllers and other fact-producing maintenance flows
- establishing auth, deployment, backups, and operational runbooks
- keeping the storage-account redirect and hosted app deployment paths operational

## Reference

- Architecture notes: `docs/design.md`
- Agent review prompt: `docs/semantic-materialization-review-agent-prompt.md`
- Agent-oriented working notes: `AGENTS.md`
