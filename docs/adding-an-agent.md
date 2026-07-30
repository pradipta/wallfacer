# Adding a new agent adapter

wallfacer supports multiple coding agents through a small adapter interface. The
`claude-code`, `cursor-agent` and `kiro-cli` adapters are the reference implementations; this
guide walks through adding another one (OpenCode, Codex, aider, …).

The short version: **one new package implementing 6 methods, plus one registration
line.** Sync, the SQLite index, `list`/`search`, the TUI, resume, and trash are all
agent-agnostic — they pick up a new adapter automatically.

The three shipped adapters bracket most of the design space, so read whichever is closest to
your target:

| | [`claudecode`](../internal/agent/claudecode/claudecode.go) | [`kirocli`](../internal/agent/kirocli/kirocli.go) | [`cursor`](../internal/agent/cursor/cursor.go) |
|---|---|---|---|
| Layout | `~/.claude/projects/<encoded-dir>/<uuid>.jsonl` | flat `~/.kiro/sessions/cli/<uuid>.jsonl` | `~/.cursor/chats/<workspace-hash>/<uuid>/`, transcript filed separately under `~/.cursor/projects/<slug>/agent-transcripts/` |
| Tracked file | the transcript | the transcript | `meta.json`, the chat's metadata record |
| Metadata source | inside the transcript | a `<uuid>.json` sidecar | the tracked `meta.json`, plus the transcript for the first prompt |
| Files per session | one | four plus a directory (see `CompanionFiles` below) | two directories, in two different trees |
| Pre-assigned session ID | yes (`--session-id`) | no | no — `cursor-agent create-chat` mints IDs itself |
| Internal transcripts | `isSidechain` on the first user record | a non-empty `parent_session_id` in the sidecar | none kept locally |

## Before writing code: research the target agent

All the real work is understanding how the agent stores sessions. Answer these four
questions first (poke around with `ls` and `jq` on a machine that has real sessions):

1. **Where do session files live, and what's the format?**
   One file per session on local disk is the assumption the interface is built on.
   Examples: Claude Code uses `~/.claude/projects/<encoded-dir>/<uuid>.jsonl`;
   Kiro CLI uses a flat `~/.kiro/sessions/cli/` (overridable with `$KIRO_HOME`);
   OpenCode stores under `~/.local/share/opencode/`; Codex under `~/.codex/sessions/`.
   An agent may also keep its conversation in a database — Cursor CLI writes a SQLite
   `store.db` per chat — without that forcing a redesign: pick whichever *file* is the
   session's own record, and reach for the database only if nothing else carries the
   metadata you need. Cursor's adapter never opens `store.db`; it only stats it, because
   its existence is what separates a real chat from a created-and-abandoned one.

2. **Can you recover the session's working directory reliably?**
   This is the classic trap. Claude Code encodes the directory into the folder name,
   but the encoding is lossy — the adapter must read `cwd` from *inside* the file
   instead. Kiro CLI encodes nothing at all and records `cwd` in its sidecar. Cursor CLI
   records `cwd` in `meta.json`, but only since a later schema revision, so older chats
   need a fallback: every chat under one `<workspace-hash>` directory ran in the same
   place, so a sibling that does record a `cwd` answers for the whole directory. Expect
   your agent to have its own version of this problem, and prefer whatever the session's
   own files record over anything derived from paths.

   Watch out for files that look per-session and are not. Cursor writes a
   `prompt_history.json` next to each chat's metadata, which reads like that chat's
   prompts but is the whole workspace's history — sibling chats' prompts and interactive
   commands like `/clear` included. Cross-check any such file against a transcript before
   trusting it.

3. **What are the launch and resume commands?**
   Verify against the actually-installed binary, including:
   - Does it support *pre-assigning* a session ID at launch
     (like `claude --session-id <uuid>`)? If yes, wallfacer can track the session
     from birth. If no — Kiro CLI has no such flag, and Cursor CLI mints IDs itself
     via `cursor-agent create-chat` — that's fine too; see `SupportsSessionID` below.
   - How do you resume by ID (like `claude --resume <id>`,
     `cursor-agent --resume <chat-id>` or `kiro-cli chat --resume-id <id>`)? Does it need
     to run in the session's original directory?

4. **Does the agent write internal transcripts you should hide?**
   Claude Code stores subagent ("sidechain") transcripts next to real sessions, and Kiro
   CLI does the same for its subagents. If your agent does something similar, detect it in
   `ParseMetadata` and set `Metadata.Sidechain = true` — wallfacer indexes them hidden so
   sync stays incremental, but never lists them. Watch for near-misses: Kiro CLI's
   `session_created_reason: "subagent"` shows up on ordinary top-level sessions too, so
   the adapter keys off `parent_session_id` instead. Cursor CLI keeps none locally, so its
   adapter never sets the flag; what it does leave behind is *abandoned* chats, and those
   are cheaper to skip in `ListSessionFiles` (one stat for the message store) than to
   index and hide.

## Step 1: implement the `Adapter` interface

Create `internal/agent/<youragent>/<youragent>.go` implementing
[`agent.Adapter`](../internal/agent/agent.go):

```go
type Adapter interface {
    Type() string                                  // stable DB identifier, e.g. "opencode"
    ListSessionFiles() ([]SessionFile, error)      // enumerate sessions, stat only
    ParseMetadata(path string) (*Metadata, error)  // parse ONE session file
    NewSessionCmd(dir, id string) *exec.Cmd        // start a fresh session in dir
    SupportsSessionID() bool                       // can the id be pre-assigned?
    ResumeCmd(dir, id string) *exec.Cmd            // resume session id in dir
}
```

A skeleton:

```go
// Package opencode implements the wallfacer agent adapter for OpenCode.
package opencode

import (
    "os"
    "os/exec"
    "path/filepath"

    "github.com/pradipta/wallfacer/internal/agent"
)

type Adapter struct {
    // SessionsDir is overridable so tests can point at a fixture directory.
    SessionsDir string
    Binary      string
}

func New() *Adapter {
    home, _ := os.UserHomeDir()
    return &Adapter{
        SessionsDir: filepath.Join(home, ".local", "share", "opencode"),
        Binary:      "opencode",
    }
}

func (a *Adapter) Type() string { return "opencode" }

func (a *Adapter) ListSessionFiles() ([]agent.SessionFile, error) {
    // Walk SessionsDir; return one SessionFile{ID, Path, Size, Mtime} per
    // session. Stat only — no file contents. Return (nil, nil) if the
    // directory doesn't exist (agent not installed / never used).
}

func (a *Adapter) ParseMetadata(path string) (*agent.Metadata, error) {
    // Open one session file and extract:
    //   Dir         — working directory (required; prefer what the file records)
    //   FirstPrompt — first human prompt, truncated (used as auto-title)
    //   Summary     — agent-generated summary, if any (preferred over FirstPrompt)
    //   GitBranch   — leave "" if the agent doesn't record it
    //   CreatedAt   — session start time
    //   Sidechain   — true for internal transcripts that shouldn't be listed
    // Read only the head of the file if the format allows it — this runs on
    // every changed file at every sync.
}

func (a *Adapter) SupportsSessionID() bool { return false } // see below

func (a *Adapter) NewSessionCmd(dir, id string) *exec.Cmd {
    cmd := exec.Command(a.Binary)
    cmd.Dir = dir
    return cmd
}

func (a *Adapter) ResumeCmd(dir, id string) *exec.Cmd {
    cmd := exec.Command(a.Binary, "--session", id)
    cmd.Dir = dir
    return cmd
}
```

Notes on the individual methods:

- **`Type()`** is stored in every DB row (`agent_type` column) and used in
  `--agent` filters. Pick it once; changing it later orphans indexed sessions.
- **`ListSessionFiles()` must be cheap.** It runs on every sync, for every adapter.
  wallfacer compares each file's `(Size, Mtime)` against the DB and only calls
  `ParseMetadata` on files that changed — that's what keeps `wallfacer sync`
  fast with hundreds of sessions. Don't open files here.
- **`ParseMetadata` failures should be graceful.** Return what you can; a session
  with only a `Dir` and `CreatedAt` still indexes fine (it just gets a bland title).
- **`SupportsSessionID()`** — if the agent can't take a pre-assigned ID, return
  `false` and ignore the `id` argument in `NewSessionCmd`. The launcher falls back
  to detecting the newborn session automatically: after the agent exits, it syncs
  and looks for a session in that directory created after launch time. Pre-assigned
  IDs are more robust (they also let `--title`/`--tag` overlays apply even if two
  sessions start in the same dir simultaneously), so use them when available.
- **Interactive commands only.** The returned `*exec.Cmd` gets the real terminal
  (inherited stdio); wallfacer ignores SIGINT/SIGTERM while it runs so Ctrl+C
  reaches the agent, not wallfacer.

## Optional: sessions made of several files

wallfacer tracks exactly one file per session — the transcript, whose mtime is the "last
active" signal. Agents that scatter a session across more paths should also implement the
optional `agent.CompanionFiler` interface, or `wallfacer rm` will trash the transcript and
leave the rest behind (and the agent's own session picker will keep offering a session whose
transcript is gone):

```go
type CompanionFiler interface {
    // CompanionFiles returns sibling paths — files or directories — belonging
    // to the same session as the tracked file at path.
    CompanionFiles(path string) []string
}
```

Kiro CLI's implementation returns the metadata sidecar, the prompt history, the lock file and
the per-session scratch directory:

```go
func (a *Adapter) CompanionFiles(path string) []string {
    stem := strings.TrimSuffix(path, ".jsonl")
    return []string{stem + ".json", stem + ".history", stem + ".lock", stem}
}
```

`Trash` moves the whole set into wallfacer's trash, keeping basenames, and `--purge` deletes
it. Return paths freely: ones that don't exist are skipped. Only the tracked file is
load-bearing — a companion that can't be moved is skipped rather than failing the delete.

Two things to know before deriving companions from the tracked file's *directory*, as the
Cursor adapter does — its chat directory holds the message store, prompt history and pasted
buffers, so moving the directory sweeps them in one rename:

- `Trash` and `Purge` pass the session's **current** path, which for an already-trashed
  session is wallfacer's own trash directory. Returning that as a companion would delete
  every other trashed session with it, so guard on the path still being under the agent's
  root. The cost of the guard is that purging a session that was trashed first leaves its
  moved directories in the trash; purging directly removes everything.
- Basenames have to be unique to survive the trash. Cursor's tracked file is called
  `meta.json` for every chat, so trashed chats pile up as `meta.json`, `meta.json.1`, …
  next to their uniquely-named chat directories. Prefer a tracked file whose name carries
  the session ID when the agent gives you the choice.

## Step 2: register the adapter

One line in [`cmd/deps.go`](../cmd/deps.go):

```go
func init() {
    agent.Register(claudecode.New())
    agent.Register(cursor.New())
    agent.Register(kirocli.New())
    agent.Register(opencode.New())   // add this
}
```

That's the entire integration. `Sync()` iterates all registered adapters, so the next
`wallfacer sync` indexes the new agent's sessions alongside the others, and
`wallfacer list --agent opencode`, the TUI (including its `A` filter and the agent picker
on `n`), `resume`, and `rm` all work immediately.

## Step 3: test with a fixture

Mirror [`claudecode_test.go`](../internal/agent/claudecode/claudecode_test.go),
[`kirocli_test.go`](../internal/agent/kirocli/kirocli_test.go) or
[`cursor_test.go`](../internal/agent/cursor/cursor_test.go): construct a temp directory
containing a couple of hand-written session files in the agent's real format, point the
adapter's directory field at it, and assert:

- `ListSessionFiles` finds them with correct IDs, and ignores companions and half-written
  sessions
- `ParseMetadata` extracts the right `Dir`, `FirstPrompt`/`Summary`, `CreatedAt`
- internal/sidechain-style transcripts get `Sidechain: true` (if applicable)
- missing or malformed metadata degrades instead of erroring
- a nonexistent sessions directory returns `(nil, nil)`, not an error

Then verify end-to-end against real data. Point `WALLFACER_DATA_DIR` at a throwaway
directory so experiments can't disturb your real index:

```bash
make build
export WALLFACER_DATA_DIR=/tmp/wf-test
./wallfacer sync && ./wallfacer list --agent <youragent>
./wallfacer new /tmp/wf-scratch --agent <youragent>   # exit the agent, check it indexed
./wallfacer resume <id-prefix>
```

## What you get for free

Once registered, with zero extra code:

- incremental sync and missing-file detection
- titles, tags, projects, search (`internal/store` is agent-agnostic)
- the TUI browser, with the agent type shown on each row, an `A` filter, and a place in the
  new-session picker
- an `AGENT` column in `wallfacer list` and `--agent` filtering
- `resume` by ID prefix or title
- trash / `--purge` semantics for `rm`

## Known limitation

The interface assumes sessions are **files on local disk**. An agent that keeps
sessions in its own database or a remote service would need `ListSessionFiles` /
`ParseMetadata` rethought (e.g. synthesizing session "files" from queries). A local
database next to per-session metadata is not that case: Cursor CLI keeps its messages in
SQLite and still fits, because `meta.json` carries the working directory, title and
timestamps, and the transcript carries the first prompt. Reach for a redesign only when
nothing outside the database identifies a session. None of the current targets (OpenCode,
Codex) need it.
