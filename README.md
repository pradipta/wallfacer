# wallfacer

A terminal session manager for [Claude Code](https://claude.com/claude-code) — see every AI coding
session you've ever started, then **name, tag, group, search, resume, or delete** them from one place.

![Build Status](https://github.com/pradipta/wallfacer/actions/workflows/build.yml/badge.svg)
![Release](https://github.com/pradipta/wallfacer/actions/workflows/release.yml/badge.svg)
![GitHub Release](https://img.shields.io/github/v/release/pradipta/wallfacer)
![License](https://img.shields.io/badge/license-MIT-blue.svg)

![wallfacer session browser screenshot](img_1.png)

## Why

Claude Code stores every conversation as an untitled JSONL file under `~/.claude/projects/`,
keyed by whatever directory you were in. After a few weeks you have dozens of transcripts you
can't tell apart and no way to find the one you need.

wallfacer indexes them all — **read-only, it never touches Claude's files** — and keeps your
titles, tags, and projects in its own local SQLite database.

## Features

- **One view of everything** — every session, from every directory, sorted by recency
- **Organize** — rename sessions, tag them, group them into projects
- **Search** — across titles, first prompts, directories, projects, and tags
- **Launch & resume** — start new sessions or jump back into old ones, from anywhere
- **Safe deletes** — `rm` moves to trash; only `--purge` is permanent
- **TUI and CLI** — a full-screen browser for humans, subcommands + `--json` for scripts
- **Extensible** — agents are pluggable adapters; Codex, opencode, Cursor CLI, and kiro-cli are on the roadmap

## Install

```bash
go install github.com/pradipta/wallfacer@latest
```

Requires Go 1.22+. Pure Go, no CGO — macOS and Linux. Pre-built binaries are on the
[releases page](https://github.com/pradipta/wallfacer/releases); building from source is
covered in the [development guide](docs/development.md).

## Quick start

```bash
wallfacer                                  # open the session browser
wallfacer new ~/work/api --title "Fix flaky auth tests"
wallfacer resume "fix flaky auth tests"    # by title or ID prefix
wallfacer search auth
```

## The browser

Bare `wallfacer` opens a full-screen session browser:

| Key | Action |
|-----|--------|
| `↑/↓` `j/k` | move |
| `/` | fuzzy filter |
| `enter` | resume — the terminal is handed to the agent; the browser returns when you exit |
| `n` | new session (prompts for a directory) |
| `r` / `t` / `p` | rename / edit tags / set project |
| `d` | delete → trash, with confirmation |
| `?` / `q` | help / quit |

## Commands

| Command | What it does |
|---------|--------------|
| `wallfacer` | Open the interactive browser |
| `wallfacer new [dir] [--title T] [--project P] [--tag t]` | Start a new session in a directory |
| `wallfacer resume <ref>` | Reopen a session in its original directory |
| `wallfacer list [--project P] [--tag T] [--json]` | List sessions, newest first |
| `wallfacer search <query>` | Search titles, prompts, dirs, projects, tags |
| `wallfacer show <ref>` | Full details of one session |
| `wallfacer rename <ref> <title>` | Rename a session |
| `wallfacer tag add\|rm <ref> <tag>…` | Add or remove tags |
| `wallfacer project set\|clear <ref>` | Group sessions into a project |
| `wallfacer rm <ref> [--purge] [-f]` | Trash a session (`--purge` deletes permanently) |
| `wallfacer sync` | Rescan disk (runs automatically before every command) |

`<ref>` is an ID prefix (`resume 5f2`) or an exact title (`resume "smoke test"`) — ambiguous
references list the candidates instead of guessing. Sessions started outside wallfacer are
picked up automatically; there's no import step.

## How it works

wallfacer scans `~/.claude/projects/`, reading just the head of each session file for its
working directory, timestamps, and first prompt (the automatic title). Your metadata lives in
SQLite at `~/.local/share/wallfacer/` — delete it and you lose only the overlay, never a
conversation. Sync is incremental, so it stays fast with hundreds of sessions.

Other agents plug in through a small adapter interface — see
[docs/adding-an-agent.md](docs/adding-an-agent.md).

## Roadmap

- [ ] Adapters for more agents: [Codex](https://github.com/openai/codex),
      [opencode](https://github.com/sst/opencode), Cursor CLI, kiro-cli
- [ ] Full-text search across session *content* (SQLite FTS5)
- [ ] `wallfacer restore` (un-trash from the CLI)
- [ ] Export a session transcript to Markdown
- [ ] Stats (sessions per project/week, disk usage)
- [ ] Desktop app — sessions in a sidebar, embedded terminal, multiple tabs
      (Wails + xterm.js on top of the same Go internals)

## Contributing

See the [development guide](docs/development.md) for building, testing, and releasing.
Contributions welcome — especially new agent adapters.

## License

[MIT](LICENSE)

---

*The name is borrowed from the Wallfacers of Liu Cixin's* The Dark Forest *— people entrusted
with plans too sprawling for anyone else to follow.*
