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
make release    # cross-compile darwin/linux × amd64/arm64 into dist/ as tarballs
make brew-formula  # rewrite HomebrewFormula/ from the archives in dist/
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
├── HomebrewFormula/      # the Homebrew tap; generated, see Releasing
├── scripts/              # release-time helpers
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

`internal/update` never lets anything wait on the network, and the split that
achieves it is worth knowing before you change it:

- `Start` resolves the check from a cached JSON file in the data dir. It is a
  local file read (~10µs), never an HTTP call, and it is all a one-shot
  subcommand ever does.
- `Refresh` is the only thing that talks to GitHub, on a goroutine, and only
  `browseLoop` calls it — the browser is the one front end that outlives a
  ~500ms round trip. It shows a late answer on its footer via `noticeMsg`, and
  leaves it in the cache for subcommands to print.

So a subcommand shows an update that a *previous* browser session discovered.
That is deliberate: a command that finishes in 10ms cannot complete a 500ms
lookup, and the notice is never worth a millisecond of anyone's time.

Unit tests cover the rest — cache hits and expiry, rate limits, malformed JSON,
pre-release payloads, unreachable hosts, semver comparison, and timing guards
asserting that `Start`, `Result` and `Await` never block. `cmd` covers the
stderr/TTY gating; `internal/tui` covers the footer.

For a manual smoke test, build a binary that claims to be an old release — the
check is skipped for dev builds, so a plain `make build` never shows a notice:

```bash
go build -ldflags "-X $(go list -m)/cmd.Version=v0.0.1" -o /tmp/wallfacer-old .
export WALLFACER_DATA_DIR=/tmp/wallfacer-old-data WALLFACER_UPDATE_INTERVAL=0
/tmp/wallfacer-old            # browser: footer hint, and warms the cache
/tmp/wallfacer-old sync       # subcommand: the same hint on stderr, from cache
```

Run the browser first — with a cold cache a subcommand has nothing to print. The
scratch `WALLFACER_DATA_DIR` keeps your real index and cache out of it, and
`WALLFACER_UPDATE_INTERVAL=0` defeats the 24h cache so the browser re-checks
every time. The CLI notice is suppressed when stderr is not a terminal, so
piping the run through `grep` or `tee` hides it — run it directly.

| Variable | Effect |
|----------|--------|
| `WALLFACER_NO_UPDATE_CHECK=1` | Turn the check off entirely |
| `WALLFACER_UPDATE_INTERVAL=0` | Ignore the 24h cache and re-check every run |
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
platform tarballs plus `checksums.txt` via `make release`, publishes them with
auto-generated notes, and then — for stable tags only — regenerates
`HomebrewFormula/wallfacer.rb` and commits it back to `main`. Tags created in
GitHub's web UI (Releases → *Draft a new release*) trigger it the same way.

Rules of thumb:

- Any tag containing `-` (`v0.1.0-rc.1`, `v0.1.0-beta.2`) is treated as a
  pre-release. `go install …@latest` skips those too — testers opt in with
  `@v0.1.0-rc.1`. So does Homebrew: the formula is only rewritten for stable
  tags, so `brew install` never hands anyone an RC.
- Never reuse or move a tag: the Go module proxy caches versions forever.

## The Homebrew tap

There is no `homebrew-wallfacer` repository. `HomebrewFormula/` in this repo *is*
the tap — Homebrew looks for formulae in `Formula/`, `HomebrewFormula/` or the
repository root, and the two-argument `brew tap <user>/<name> <url>` form drops
the usual `homebrew-` naming requirement:

```bash
brew tap pradipta/wallfacer https://github.com/pradipta/wallfacer
brew install pradipta/wallfacer/wallfacer
```

The formula is generated, never edited by hand, because Homebrew pins every
download by sha256 and those checksums don't exist until the release binaries
are built. That is also why the release workflow has to commit back to `main`
after publishing: at tag time there is nothing to hash yet.

It can also be regenerated from a release that already exists, which is how to
repair a formula without cutting a new version — the checksums come from the
`checksums.txt` attached to that release, so they match the published archives
by construction:

```bash
scripts/brew-formula.sh v1.2.1 --from-release > HomebrewFormula/wallfacer.rb
```

### Why the release job pushes with a deploy key

`main` carries a ruleset requiring a pull request, one approving review and two
passing `build` checks. `GITHUB_TOKEN` cannot bypass it, and cannot be granted a
bypass either: naming the Actions app as a bypass actor on a user-owned
repository is rejected with *"Actor GitHub Actions integration must be part of
the ruleset source or owner organization"*. Adding the **Write** repository role
to the bypass list does not help — the Actions bot holds no repository role, and
the push is still rejected.

So the ruleset's bypass list includes **Deploy keys**, and the release job pushes
over SSH with a read-write deploy key whose private half is the
`RELEASE_SSH_KEY` secret. GitHub's own published host keys are pinned from
`api.github.com/meta` rather than trusted on first use.

The tradeoff is explicit: that key can push to `main` without review. It is
scoped to this repository alone — unlike a personal access token, it grants
nothing anywhere else — and it is revocable at any time:

```bash
gh repo deploy-key list
gh repo deploy-key delete <id>
gh secret delete RELEASE_SSH_KEY
```

To rotate it, generate a new keypair, `gh repo deploy-key add key.pub
--allow-write --title …`, `gh secret set RELEASE_SSH_KEY < key`, then delete the
old key.

If you would rather have no bypass at all, delete the workflow step and run the
`--from-release` command above in a pull request after each release. That costs
one PR per release and no stored credential.

### Testing the tap without cutting a release

Two halves, both worth rehearsing after any change to `make release`, the
generator, or the release workflow. Neither one touches GitHub.

**Install the formula for real.** Serve a local build over HTTP and generate a
formula pointing at it, in a scratch git repo standing in for the tap. Build and
generate with the same `VERSION`, or `brew test` will fail comparing the
formula's version against what the binary prints:

```bash
export HOMEBREW_NO_AUTOREMOVE=1        # see the note below — this one matters
make release VERSION=v9.9.9
python3 -m http.server 8611 --directory dist --bind 127.0.0.1 &

TAP=/tmp/wf-tap; rm -rf $TAP; mkdir -p $TAP/HomebrewFormula; git init -q $TAP
BREW_BASE_URL=http://127.0.0.1:8611 scripts/brew-formula.sh v9.9.9 \
  > $TAP/HomebrewFormula/wallfacer.rb
git -C $TAP add -A
git -C $TAP -c user.name=lab -c user.email=lab@lab commit -qm formula

brew tap you/wallfacer-lab $TAP     # the two-argument form: any git URL, any name
brew style   you/wallfacer-lab/wallfacer
brew install you/wallfacer-lab/wallfacer
brew test    you/wallfacer-lab/wallfacer
wallfacer --version

brew uninstall you/wallfacer-lab/wallfacer && brew untap you/wallfacer-lab
kill %1; brew developer off         # brew test switches developer mode on
```

**Rehearse the release job's commit back to main**, against a bare repo standing
in for GitHub, running the workflow step verbatim rather than a paraphrase of it:

```bash
LAB=/tmp/wf-lab; rm -rf $LAB; mkdir -p $LAB
git init -q --bare $LAB/origin.git
git clone -q . $LAB/work
cd $LAB/work && git remote set-url origin $LAB/origin.git
git push -q origin HEAD:main
git tag v9.9.9 && git checkout -q v9.9.9      # CI runs detached at the tag
make release

ruby -ryaml -e 'puts YAML.load_file(".github/workflows/release.yml")["jobs"]["release"]["steps"].last["run"]' \
  | sed 's/${{ github.ref_name }}/v9.9.9/g' | bash

git -C $LAB/origin.git show main:HomebrewFormula/wallfacer.rb | head
```

Run that step twice: the second run must print `formula already current` and
leave `origin/main` alone. Then tag a second version and confirm the formula's
version, URLs and checksums all move. The first release is the interesting case —
the formula is untracked then, which is why the step stages before it checks for
changes.

Two things that will surprise you:

- **`brew uninstall` autoremoves.** Homebrew 6 removed an unrelated formula it
  considered unneeded when the test formula was uninstalled. Export
  `HOMEBREW_NO_AUTOREMOVE=1` before any of this.
- **"Tapped 10 commands and 1 formula."** Homebrew reads a `cmd/` directory at a
  tap's root as external commands, and this repo has one — its Go entrypoints.
  Cosmetic: none of them are invocable as `brew <something>`, and tap trust means
  a tap's commands are not loaded unless you trust the whole tap. It is the price
  of the tap living in the source repo.

## Contributing

PRs welcome — especially new agent adapters
([guide](adding-an-agent.md)). Please run `make test vet` before submitting.
