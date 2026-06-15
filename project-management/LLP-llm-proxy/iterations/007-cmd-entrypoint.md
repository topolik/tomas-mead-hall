# LLP Iteration 007 — cmd/llp entrypoint

- **Start:** 2026-06-12
- **End:** 2026-06-12
- **Phase:** Implementation (bug fix)

## What happened

`run.sh` and `watch.sh` both call `go build -o llp ./cmd/llp`, but `cmd/llp/main.go` was never committed — the directory was absent from the merge in iteration 006. The project would not build.

## Changes

- **`cmd/llp/main.go`** — wires all packages into a runnable binary:
  - Parses `--config` flag (default `config.yaml`)
  - Loads + builds the registry (`registry.Load` / `registry.Build`)
  - Opens the SQLite usage store (`usage.Open`), creates its parent dir if needed
  - Creates the in-memory auth store (`auth.NewStore`)
  - Starts the control socket (`control.Serve`)
  - Creates the router (`router.New`)
  - Creates and starts the HTTP server (`server.New` / `httpSrv.Serve`)
  - Handles `SIGINT`/`SIGTERM` for graceful shutdown
  - Logs `data API on <addr>` and `control socket <path>` so `watch.sh` status parsing works

- **`.gitignore`** — changed bare `llp` rule to `/llp` so the rule matches only the compiled root-level binary and no longer suppresses the `cmd/llp/` source directory from git.

## Verification

`go build -o /tmp/llp ./cmd/llp` — success.

## Decisions

### [Decision LLP-014: dirOf helper instead of filepath.Dir]
**Date:** 2026-06-12 · **Phase:** Implementation · **Decided by:** Developer
**Decision:** Used a small inline `dirOf` to avoid importing `path/filepath` solely for one `Dir` call.
**Revisit if:** filepath is imported for any other reason — replace with `filepath.Dir`.
