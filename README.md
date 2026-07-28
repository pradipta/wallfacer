# wallfacer

> Face the wall of scattered AI coding sessions.

**wallfacer** is a terminal session manager for [Claude Code](https://claude.com/claude-code)
(and, eventually, other coding agents). If you start Claude Code sessions all over your
filesystem and lose track of them, wallfacer finds them all, lets you **name, tag, group,
search, resume, and delete** them — from one place.

```
$ wallfacer

  wallfacer — sessions

  ▌ Fix flaky auth tests  ◆ api-cleanup  #golang #tests
  ▌ ~/work/api · 2h ago · claude-code

    Session manager brainstorm  #cli
    ~/projects/wallfacer · 1d ago · claude-code

    Explain to me this repository…
    ~/projects/NullAway · 4d ago · claude-code

  / filter · enter resume · n new · r rename · t tags · p project · d delete · q quit
```

*(The name is borrowed from the Wallfacers of Liu Cixin's *The Dark Forest* — people entrusted
with plans too sprawling for anyone else to follow.)*

## Why

Claude Code stores every conversation as a JSONL file under `~/.claude/projects/`, keyed by the
directory you happened to be in. There's no way to see them all, no names, no organization —
after a few weeks you're left with dozens of untitled transcripts you can't tell apart.

wallfacer indexes those files (read-only — **it never modifies Claude's data**), overlays your
own titles/tags/projects in a local SQLite database, and wraps launching so every new session
is tracked from birth.

## Install

Requires Go 1.22+. Pure Go (no CGO), works on macOS and Linux.

```bash
go install github.com/pradipta/wallfacer@latest
```

Or from a clone:

```bash
git clone https://github.com/pradipta/wallfacer && cd wallfacer
make install        # or: make build / make release (cross-compiled binaries in dist/)
```

Optional alias for heavy use: `alias wf=wallfacer`.

## Quick start

```bash
wallfacer                # open the interactive browser (syncs first)
wallfacer new ~/work/api --title "Fix flaky auth tests" --project api-cleanup --tag tests
#   → Claude Code opens in ~/work/api; when you exit, the session is saved & named

wallfacer list           # all sessions, newest first
wallfacer resume "fix flaky auth tests"    # by title (case-insensitive) or ID prefix
wallfacer search auth    # matches title, first prompt, directory, project, tags
```

## The browser (TUI)

Running bare `wallfacer` opens a full-screen session browser:

| Key | Action |
|-----|--------|
| `↑/↓` `j/k` | move |
| `/` | fuzzy filter (title, dir, project, tags) |
| `enter` | resume the selected session — the terminal is handed to the agent; the browser returns when you exit |
| `n` | new session (prompts for a directory, `~` works) |
| `r` | rename |
| `t` | edit tags (comma-separated, replaces the set) |
| `p` | set project (empty clears) |
| `d` | delete → trash, with confirmation |
| `?` | full help, `q` quit |

## CLI reference

```
wallfacer new [dir] [--title T] [--project P] [--tag t1 --tag t2] [--agent claude-code]
wallfacer resume <id-prefix | title>
wallfacer list [--project P] [--tag T] [--dir D] [--agent A] [--all] [--json]
wallfacer search <query>
wallfacer show <id-prefix | title>
wallfacer rename <ref> <new-title>
wallfacer tag add|rm <ref> <tag>...
wallfacer project set <ref> <project> | project clear <ref>
wallfacer rm <ref> [--purge] [-f]
wallfacer sync
```

Every command that takes a session accepts an **ID prefix** (`wallfacer resume 5f2`) or an
**exact title** (`wallfacer resume "smoke test"`). Ambiguous references fail with the list of
candidates rather than guessing.

`list --json` emits full records for scripting: `wallfacer list --json | jq '.[].Dir'`.

## How it works

- **Discovery.** Claude Code writes each session to
  `~/.claude/projects/<encoded-dir>/<uuid>.jsonl`. wallfacer scans these, reading only the
  head of each file for the working directory, timestamp, git branch, and your first prompt
  (used as the automatic title, or the session summary when Claude has generated one).
  Subagent transcripts are recognized and hidden. Sync is incremental — unchanged files are
  never re-read — so it stays fast with hundreds of sessions.
- **Your metadata** (titles, tags, projects) lives in SQLite at
  `$XDG_DATA_HOME/wallfacer/wallfacer.db` (default `~/.local/share/wallfacer/`), never inside
  Claude's files. Delete the DB and you lose only the overlay; the index rebuilds from disk.
- **Launching.** `wallfacer new` pre-assigns a session UUID via `claude --session-id`, so the
  session is tracked from the moment it starts; `resume` runs `claude --resume <id>` in the
  session's original directory. The terminal is handed to claude directly (Ctrl+C reaches
  claude, not wallfacer).
- **Deleting.** `rm` moves the JSONL into `~/.local/share/wallfacer/trash/` — restore it by
  moving it back. `rm --purge` is the only operation that permanently deletes anything.

## Extending to other agents

Adapters implement a small interface (`internal/agent.Adapter`): enumerate session files,
parse one file's metadata, and provide launch/resume commands. The `claude-code` adapter is
~200 lines; OpenCode (`~/.local/share/opencode/`) and Codex (`~/.codex/sessions/`) follow the
same session-files-on-disk pattern and are natural next targets. Sessions carry an
`agent_type`, so a mixed index works out of the box.

See **[docs/adding-an-agent.md](docs/adding-an-agent.md)** for a full walkthrough — research
checklist, adapter skeleton, registration, and testing.

## Roadmap

- [ ] Full-text search across session *content* (SQLite FTS5)
- [ ] OpenCode and Codex adapters
- [ ] `wallfacer restore` (un-trash from the CLI)
- [ ] Export a session transcript to Markdown
- [ ] Stats (`sessions per project/week`, disk usage)

## Development

```bash
make test    # unit tests (adapter parsing, store, sync, launcher, trash)
make vet
make release # darwin/linux × amd64/arm64 binaries in dist/
```

Contributions welcome — especially new agent adapters.

## License

[MIT](LICENSE)
