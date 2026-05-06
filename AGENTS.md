# ARO-HCP-CIHealth Agent Notes

## Start Here

- Read `README.md` for the operator/developer workflow.
- Read `docs/design.md` for the current architecture and storage/runtime invariants.
- Treat the PostgreSQL-backed app+DB runtime as the current architecture, not a future target.
- Treat embedded PostgreSQL as a local-development convenience, not a separate architecture.

## Repo Map

- `cmd/main.go`: CLI bootstrap
- `pkg/cli`: command wiring and shared option binding
- `pkg/run`, `pkg/controllers`, `pkg/source`: continuous ingestion runtime and source clients
- `pkg/failurepatterns/...`: extraction, range loading, history helpers, and inline failure-pattern aggregation
- `pkg/frontend/...`: HTTP server, readmodel helpers, shared UI, and the report/failure-patterns/run-log surface packages
- `pkg/store/contracts`, `pkg/store/postgres`: store abstraction, PostgreSQL runtime, migrations, init/bootstrap
- `Dockerfile`: container image build
- `.github/workflows/`: unit-test and image build/push automation
- `Makefile`: local developer workflows, image helpers, and redirect-page publishing commands
- `.cursor/skills/`: project-local skills for review/failure-pattern workflows

## Invariants

- All request windows are UTC.
- When a surface uses `week=YYYY-MM-DD`, that value is a Monday-starting seven-day UTC window.
- Semantic identity is driven by extracted failure-pattern text; `signature_id` is provenance/debug context, not the primary merge key.
- The review queue is diagnostic-only runtime state; the app exposes it via `/api/review/signals/window`.
- History/window views are computed from current facts through the inline engine and use the current failure-pattern schema.
- User-facing docs say "failure patterns" and "run log", but some internal files and symbols still use older phase-oriented `global` names.

## Validation

- Default repo validation: `make check`
- Failure-pattern extraction/window changes: `go test ./pkg/failurepatterns/...`
- App/report changes: `go test ./pkg/frontend/...`
- Store or migration changes: `go test ./pkg/store/postgres/...`
- Useful smoke commands: `make app`, `make run-controllers`

## Current Ops State

- `Dockerfile` and `.github/workflows/` support the current image build/publish flow.
- Azure Storage redirect-page publishing remains supported as a compatibility path.
- Hosted app operation, auth, backups, and full runbooks are still evolving.
