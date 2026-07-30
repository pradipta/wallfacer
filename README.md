```
██╗    ██╗█████╗  ██╗     ██╗     ███████╗█████╗   ██████╗███████╗██████╗
██║    ██║██╔══██╗██║     ██║     ██╔════╝██╔══██╗██╔════╝██╔════╝██╔══██╗
██║ █╗ ██║███████║██║     ██║     █████╗  ███████║██║     █████╗  ██████╔╝
██║███╗██║██╔══██║██║     ██║     ██╔══╝  ██╔══██║██║     ██╔══╝  ██╔══██╗
╚███╔███╔╝██║  ██║███████╗███████╗██║     ██║  ██║╚██████╗███████╗██║  ██║
 ╚══╝╚══╝ ╚═╝  ╚═╝╚══════╝╚══════╝╚═╝     ╚═╝  ╚═╝ ╚═════╝╚══════╝╚═╝  ╚═╝
```

# wallfacer

A terminal session manager for [Claude Code](https://claude.com/claude-code),
[Cursor CLI](https://docs.cursor.com/en/cli/overview), [Kiro CLI](https://kiro.dev/docs/cli/) and
[Codex](https://github.com/openai/codex) —
see every AI coding session you've ever started, then
**name, tag, group, search, resume, or delete** them, from a full-screen browser or straight
from the command line.

![Build Status](https://github.com/pradipta/wallfacer/actions/workflows/build.yml/badge.svg)
![Release](https://github.com/pradipta/wallfacer/actions/workflows/release.yml/badge.svg)
![GitHub Release](https://img.shields.io/github/v/release/pradipta/wallfacer)
![License](https://img.shields.io/badge/license-MIT-blue.svg)

![demo](demo.gif)

## Why

Claude Code stores every conversation as an untitled JSONL file under `~/.claude/projects/`,
keyed by whatever directory you were in. Kiro CLI does much the same in a single flat
`~/.kiro/sessions/cli/` folder, Cursor CLI buries each chat in a hash-named directory
under `~/.cursor/chats/`, and Codex files its rollouts by date under `~/.codex/sessions/`.
After a few weeks you have dozens of transcripts, across four
agents, that you can't tell apart and no way to find the one you need.

wallfacer indexes them all — **read-only, it never touches the agents' files** — and keeps
your titles, tags, and projects in its own local SQLite database.

## Features

- **One view of everything** — every session, from every directory and every agent, sorted by recency
- **Organize** — rename sessions, tag them, group them into projects
- **Search** — across titles, first prompts, directories, projects, and tags
- **Launch & resume** — start new sessions or jump back into old ones, from anywhere
- **Multi-agent** — Claude Code, Cursor CLI, Kiro CLI and Codex side by side; pick the agent when
  you start a session, filter by it afterwards
- **Safe deletes** — `rm` moves to trash; only `--purge` is permanent
- **TUI and CLI** — a full-screen browser for humans, subcommands + `--json` for scripts
- **Extensible** — agents are pluggable adapters; opencode is on the roadmap

## Install

```bash
go install github.com/pradipta/wallfacer@latest
```

Requires Go 1.22+. Pure Go, no CGO — macOS and Linux. Pre-built binaries are on the
[releases page](https://github.com/pradipta/wallfacer/releases); building from source is
covered in the [development guide](docs/development.md).

## Two ways to use it

wallfacer is one binary with two front ends, and which one you get depends on whether you
pass a subcommand:

| You type | You get |
|----------|---------|
| `wallfacer` | **The TUI** — a full-screen, interactive session browser. Start here. |
| `wallfacer <command>` | **The CLI** — one-shot subcommands for scripts and muscle memory. |

They are not separate tools and there is nothing to switch between: both read and write the
same SQLite index, so a session you tag in the browser is immediately findable by
`wallfacer list --tag`, and vice versa. Anything the TUI can do, a subcommand can do too.

(If stdout isn't a terminal — `wallfacer | less`, or inside a script — the bare command
prints help instead of opening the browser.)

## Quick start

Open the browser and work from there:

```bash
wallfacer
```

Or drive it from the shell:

```bash
wallfacer new ~/work/api --title "Fix flaky auth tests"
wallfacer resume "fix flaky auth tests"    # by title or ID prefix
wallfacer search auth
wallfacer list --project api --json        # for scripts
```

## The TUI — `wallfacer`

Bare `wallfacer` opens a full-screen session browser: a list on the left, and a detail
pane on the right showing everything `wallfacer show` prints for whatever is highlighted.

![wallfacer session browser screenshot](img.png)

Every row shows its project, tags, directory, age and agent — the same metadata
`wallfacer list` prints. Below ~100 columns the detail pane steps aside and the list
takes the full width.

| Key | Action |
|-----|--------|
| `↑/↓` `j/k` | move |
| `/` | fuzzy filter across titles, projects, dirs and tags |
| `P` / `T` / `A` | cycle the project / tag / agent filter (wraps back to unfiltered) |
| `x` | clear the project, tag and agent filters |
| `tab` | show or hide the detail pane |
| `enter` | resume — the terminal is handed to the agent; the browser returns when you exit |
| `n` | new session (asks for the agent, then directory, title, project, tags) |
| `r` / `t` / `p` | rename / edit tags / set project |
| `d` | delete → trash, with confirmation |
| `?` / `q` | help / quit |

The agent step comes first and is a one-line picker: `←/→` or a digit to choose, `enter` to
go on, `esc` to back out. Claude Code is preselected, and the step is skipped entirely if only
one adapter is registered.

## The CLI — `wallfacer <command>`

Every subcommand is one-shot: it runs, prints, and exits. Same index as the browser.

| Command | What it does |
|---------|--------------|
| `wallfacer` | *(no subcommand)* Open the interactive browser |
| `wallfacer new [dir] [--agent A] [--title T] [--project P] [--tag t]` | Start a new session in a directory |
| `wallfacer resume <ref>` | Reopen a session in its original directory |
| `wallfacer list [--project P] [--tag T] [--agent A] [--json]` | List sessions, newest first |
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

wallfacer scans `~/.claude/projects/`, `~/.cursor/chats/`, `~/.kiro/sessions/cli/` and
`~/.codex/sessions/`, reading
just the head of each session file for its working directory, timestamps, and first prompt (the
automatic title). Every agent's listing carries the agent type, so `wallfacer list` shows an
`AGENT` column and `--agent` narrows to one. Your metadata lives in SQLite at
`~/.local/share/wallfacer/` — delete it and you lose only the overlay, never a conversation.
Sync is incremental, so it stays fast with hundreds of sessions.

`rm` moves a session to wallfacer's trash. For agents that spread one session over several
files — Kiro CLI writes a transcript plus a metadata sidecar, prompt history and a scratch
directory, and Cursor CLI splits a chat between its own directory and a transcript filed under
the project — the whole set travels together, so a deleted session doesn't linger in the
agent's own session picker.

Once a day the browser asks GitHub whether a newer release exists and, if so, mentions it
once on its footer; subcommands repeat that cached answer on stderr, so it never lands in
`--json` output or a pipe. Nothing ever waits for it: the lookup belongs to the browser
because it is the front end that outlives a network round trip, and a subcommand only reads
the cached answer — a file read, no network — so an update that lands late simply shows up
the next time you run wallfacer. The cache lives in the data dir, and
`WALLFACER_NO_UPDATE_CHECK=1` turns the whole thing off.

Other agents plug in through a small adapter interface — see
[docs/adding-an-agent.md](docs/adding-an-agent.md).

## Roadmap

- [x] Adapters for Claude Code, [Cursor CLI](https://docs.cursor.com/en/cli/overview),
      [Kiro CLI](https://kiro.dev/docs/cli/) and [Codex](https://github.com/openai/codex)
- [ ] Adapter for [opencode](https://github.com/sst/opencode)
- [ ] A preferred-agent setting, so the picker's default is yours to choose
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
