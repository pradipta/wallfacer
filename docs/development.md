# Developing wallfacer

Everything you need to build, test, and release wallfacer locally. For adding
support for a new coding agent, see [adding-an-agent.md](adding-an-agent.md).

## Requirements

- Go 1.22+ (CI uses 1.26)
- No CGO, no C toolchain — SQLite is pure Go (`modernc.org/sqlite`)
- macOS or Linux

## Build & test

```bash
git clone https://github.com/pradipta/wallfacer && cd wallfacer

make build      # ./wallfacer, version stamped from git describe
make test       # unit tests (adapter parsing, store, sync, launcher, trash)
make vet        # go vet
make fmt        # gofmt
make install    # go install into $GOBIN
make release    # cross-compile darwin/linux × amd64/arm64 into dist/
make clean
```

`make build VERSION=v0.1.0-dev` overrides the stamped version (useful before any
tags exist).

## Project layout

```
wallfacer/
├── main.go
├── cmd/                  # Cobra commands; root.go runs the TUI browse loop
├── internal/
│   ├── agent/            # Adapter interface + registry (extensibility seam)
│   │   └── claudecode/   # Claude Code adapter (JSONL parsing, launch/resume)
│   ├── store/            # SQLite index, incremental sync, trash
│   ├── launcher/         # exec the agent with inherited stdio, post-exit sync
│   ├── tui/              # Bubble Tea session browser
│   ├── update/           # once-a-day GitHub release check + notice
│   └── format/           # shared display helpers (titles, relative times)
└── docs/
```

Key design points:

- **Claude's files are never modified.** wallfacer reads
  `~/.claude/projects/**/*.jsonl` and keeps its own overlay (titles, tags,
  projects) in SQLite at `$XDG_DATA_HOME/wallfacer/wallfacer.db`
  (default `~/.local/share/wallfacer/`). Deleting the DB only loses the
  overlay; the index rebuilds from disk on the next sync.
- **Sync is incremental** — files are re-parsed only when their (size, mtime)
  changed. `ListSessionFiles` must therefore stay stat-only.
- **The TUI never runs the agent.** It returns an Action; `browseLoop` in
  `cmd/root.go` execs the agent with the real terminal, then reopens the
  browser.

## Testing tips

- Adapter tests use fixture files in a temp dir — no real `~/.claude` needed.
- For end-to-end testing against real data, `make build` and run `./wallfacer`
  directly; it only ever writes to its own data dir (and `trash/` within it).
- `wallfacer rm` moves files to `<data-dir>/trash/`; only `--purge` deletes.

## Testing the update notice

`internal/update` is covered by unit tests driving an `httptest` server (cache
hits and expiry, rate limits, malformed JSON, pre-release payloads, unreachable
hosts, the grace period, semver comparison); `cmd` covers the stderr/TTY gating
and `internal/tui` the footer. For a manual smoke test, build a binary that
claims to be an old release — the check is skipped for dev builds, so a plain
`make build` never shows a notice:

```bash
go build -ldflags "-X $(go list -m)/cmd.Version=v0.0.1" -o /tmp/wallfacer-old .
WALLFACER_DATA_DIR=/tmp/wallfacer-old-data WALLFACER_UPDATE_INTERVAL=0 \
  /tmp/wallfacer-old sync    # the CLI form, on stderr
WALLFACER_DATA_DIR=/tmp/wallfacer-old-data WALLFACER_UPDATE_INTERVAL=0 \
  /tmp/wallfacer-old         # the footer form, in the browser
```

The scratch `WALLFACER_DATA_DIR` keeps your real index and cache out of it, and
`WALLFACER_UPDATE_INTERVAL=0` defeats the 24h cache so every run re-checks. The
notice is suppressed when stderr is not a terminal, so piping the run through
`grep` or `tee` hides it — run it directly.

| Variable | Effect |
|----------|--------|
| `WALLFACER_NO_UPDATE_CHECK=1` | Turn the check off entirely |
| `WALLFACER_UPDATE_INTERVAL=0` | Ignore the 24h cache and fetch every run |
| `WALLFACER_UPDATE_API=http://…` | Point at something other than `api.github.com` |

That last one is for working offline: anything answering
`GET /repos/pradipta/wallfacer/releases/latest` with
`{"tag_name": "v9.9.9", "html_url": "…"}` will do, including
`python3 -m http.server` over a matching directory tree.

## Releasing

Releases are fully automated by [`.github/workflows/release.yml`](../.github/workflows/release.yml):
push a tag, get a release.

```bash
# Stable release — becomes "latest"
git tag v0.1.0 && git push origin v0.1.0

# Release candidate — marked pre-release, never becomes "latest"
git tag v0.1.0-rc.1 && git push origin v0.1.0-rc.1
```

The workflow runs vet + tests (a failure aborts the release), builds the four
platform binaries via `make release`, and publishes them with auto-generated
notes. Tags created in GitHub's web UI (Releases → *Draft a new release*)
trigger it the same way.

Rules of thumb:

- Any tag containing `-` (`v0.1.0-rc.1`, `v0.1.0-beta.2`) is treated as a
  pre-release. `go install …@latest` skips those too — testers opt in with
  `@v0.1.0-rc.1`.
- Never reuse or move a tag: the Go module proxy caches versions forever.

## Contributing

PRs welcome — especially new agent adapters
([guide](adding-an-agent.md)). Please run `make test vet` before submitting.
