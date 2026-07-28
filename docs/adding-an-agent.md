# Adding a new agent adapter

wallfacer supports multiple coding agents through a small adapter interface. The
`claude-code` adapter is the reference implementation; this guide walks through adding
another one (OpenCode, Codex, aider, …).

The short version: **one new package implementing 6 methods, plus one registration
line.** Sync, the SQLite index, `list`/`search`, the TUI, resume, and trash are all
agent-agnostic — they pick up a new adapter automatically.

## Before writing code: research the target agent

All the real work is understanding how the agent stores sessions. Answer these four
questions first (poke around with `ls` and `jq` on a machine that has real sessions):

1. **Where do session files live, and what's the format?**
   One file per session on local disk is the assumption the interface is built on.
   Examples: Claude Code uses `~/.claude/projects/<encoded-dir>/<uuid>.jsonl`;
   OpenCode stores under `~/.local/share/opencode/`; Codex under `~/.codex/sessions/`.

2. **Can you recover the session's working directory reliably?**
   This is the classic trap. Claude Code encodes the directory into the folder name,
   but the encoding is lossy — the adapter must read `cwd` from *inside* the file
   instead. Expect your agent to have its own version of this problem, and prefer
   whatever the session file itself records over anything derived from paths.

3. **What are the launch and resume commands?**
   Verify against the actually-installed binary, including:
   - Does it support *pre-assigning* a session ID at launch
     (like `claude --session-id <uuid>`)? If yes, wallfacer can track the session
     from birth. If no, that's fine too — see `SupportsSessionID` below.
   - How do you resume by ID (like `claude --resume <id>`)? Does it need to run in
     the session's original directory?

4. **Does the agent write internal transcripts you should hide?**
   Claude Code stores subagent ("sidechain") transcripts next to real sessions.
   If your agent does something similar, detect it in `ParseMetadata` and set
   `Metadata.Sidechain = true` — wallfacer indexes them hidden so sync stays
   incremental, but never lists them.

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

## Step 2: register the adapter

One line in [`cmd/deps.go`](../cmd/deps.go):

```go
func init() {
    agent.Register(claudecode.New())
    agent.Register(opencode.New())   // add this
}
```

That's the entire integration. `Sync()` iterates all registered adapters, so the next
`wallfacer sync` indexes the new agent's sessions alongside Claude's, and
`wallfacer list --agent opencode`, the TUI, `resume`, and `rm` all work immediately.

## Step 3: test with a fixture

Mirror [`claudecode_test.go`](../internal/agent/claudecode/claudecode_test.go):
construct a temp directory containing a couple of hand-written session files in the
agent's real format, point the adapter's directory field at it, and assert:

- `ListSessionFiles` finds them with correct IDs
- `ParseMetadata` extracts the right `Dir`, `FirstPrompt`/`Summary`, `CreatedAt`
- internal/sidechain-style transcripts get `Sidechain: true` (if applicable)
- a nonexistent sessions directory returns `(nil, nil)`, not an error

Then verify end-to-end against real data:

```bash
make build
./wallfacer sync && ./wallfacer list --agent <youragent>
./wallfacer new /tmp/wf-test --agent <youragent>   # exit the agent, check it indexed
./wallfacer resume <id-prefix>
```

## What you get for free

Once registered, with zero extra code:

- incremental sync and missing-file detection
- titles, tags, projects, search (`internal/store` is agent-agnostic)
- the TUI browser, with the agent type shown on each row
- `resume` by ID prefix or title
- trash / `--purge` semantics for `rm`

## Known limitation

The interface assumes sessions are **files on local disk**. An agent that keeps
sessions in its own database or a remote service would need `ListSessionFiles` /
`ParseMetadata` rethought (e.g. synthesizing session "files" from queries). None of
the current targets (OpenCode, Codex) need this.
