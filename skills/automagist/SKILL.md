---
name: automagist
description: Pick the right gh-automagist subcommand and flags for a tracked file's Gist — which command syncs this path, what a status badge means, how to link a second machine with add --gist-id, and how to recover a Gist that was overwritten. Reads state with status and fetch; never runs a command that writes.
disable-model-invocation: true
argument-hint: "[path]"
allowed-tools: [Read, Bash(gh automagist status:*), Bash(gh automagist fetch:*)]
---

# automagist

`gh-automagist` watches local files and syncs them to GitHub Gists. This skill maps an
intent to a command line. It does not duplicate `--help`; for the full per-command
description see the Commands table at
https://github.com/noriyotcp/gh-automagist#commands , and `gh automagist <cmd> --help`
for exact flags.

State lives in `~/.config/gh-automagist/state.json`, alongside `monitor.pid`,
`monitor.json`, and the daemon's `log/` directory.

## If a path was given

`/automagist <path>` means "tell me what to do about this file". Answer it directly
rather than printing the whole index:

1. Run `gh automagist status`.
2. Find that path under `Registered Files`. If it is absent, the file is not tracked
   and the command is `gh automagist add <path>` — or `add <path> --gist-id <id>` if a
   Gist for it already exists on another machine.
3. If it is present, read its badge and give the one command line the badge table
   below maps to. A `[in sync]` badge means there is nothing to run.

## What you probably want

| Intent | Command |
| :--- | :--- |
| See the daemon and every tracked path with its sync state | `gh automagist status` |
| Check whether a Gist moved without downloading content | `gh automagist fetch [path]` |
| See the actual remote-vs-local diff | `gh automagist fetch [path] --diff` |
| Take the Gist's content onto local disk | `gh automagist pull [path]` |
| Send local content up to the Gist | `gh automagist push [path]` |
| Start tracking a file as a brand-new Gist | `gh automagist add <path>` |
| Point a second machine at a Gist that already exists | `gh automagist add <path> --gist-id <id>` |
| Stop tracking a file | `gh automagist remove <path>` |
| Start, bounce, or stop the daemon | `gh automagist monitor --daemon` / `restart` / `stop` |
| Browse tracked files interactively, open one in `$EDITOR` | `gh automagist list` (human only) |
| Do all of the above in a TUI | `gh automagist dashboard` (human only) |

Omitting `[path]` makes `fetch`, `pull`, and `push` operate on every tracked file.

## Before reaching for push

The most forgettable fact about this tool is that the answer is often "do nothing".
Three questions, all answerable from one `gh automagist status`:

1. **Is the daemon running?** The first line says `RUNNING` or `STOPPED`.
2. **Is this path tracked?** It appears under `Registered Files` if so.
3. **Was the file edited while the daemon was down?** The daemon reconciles every
   tracked file on startup, so a `restart` also catches those edits.

If the daemon is running and the path is tracked, an edit is PATCHed automatically
after the debounce window goes quiet — 5 seconds by default
(`pkg/monitor/monitor.go`, overridable with `--debounce` or
`GH_AUTOMAGIST_DEBOUNCE_INTERVAL`). So the answer is usually: wait out the window and
read `status` again. A manual `push` is for when the daemon is down, or was down when
the edit happened.

## Reading the status badges

Six badges, from `cmd/status.go`:

| Badge | Meaning |
| :--- | :--- |
| `[in sync]` | Local matches the last sync, and the Gist has not moved |
| `[local: unsynced]` | Local content differs from `content_sha` — `push` sends it |
| `[remote: newer ⇧]` | The Gist holds content we have never seen — `fetch --diff`, then `pull` |
| `[diverged: local + remote]` | Both moved. Look at `fetch --diff` before choosing a direction |
| `[error: …]` | The Gist could not be read |
| `[local unreadable: …]` | The local file could not be read (moved or deleted?) |

## Facts `--help` does not tell you

- **`add` and `remove` take effect immediately; no `restart` needed.** The daemon
  watches `state.json` itself and rebuilds its watch set whenever that file is
  written (v1.12.2). `restart` is for picking up a new binary or a new `--debounce`.
- **Remote changes are judged by content, not by timestamps.** A Gist's `updated_at`
  belongs to the whole Gist, so pushing one file moves it for every sibling. Both
  `push` and the `status` / `fetch` side compare per-file digests against
  `content_sha` (v1.12.1); the timestamp is only a fallback for entries that have no
  digest recorded. Note that `fetch --help` still describes the old timestamp
  behavior — the help string is stale, the behavior is content-based.
- **Several files can share one Gist**, keyed by filename within it. That is why
  content-based judgment matters rather than being a nicety.
- **`state.json` keeps three watermarks per file:** `content_sha` (digest at the last
  successful sync, in either direction — this is what makes local dirtiness
  detectable with no network call), `remote_updated_at` (the last observed Gist
  timestamp, still stored but no longer the primary signal), and `updated_at` (when a
  transfer last *succeeded*, not when an edit was noticed).
- **`add --gist-id` refuses to guess a direction.** It reads the Gist first. Identical
  content links with no API write; a filename the Gist does not hold yet is uploaded;
  a genuine difference is reported and nothing is written until you pass
  `--adopt-remote` (take the Gist's content, backing up the local file) or `--force`
  (replace the Gist's content with the local file). The two are mutually exclusive and
  both require `--gist-id`.
- **Recovering a Gist that was overwritten** — Gists keep revisions:

  ```bash
  gh api gists/<id>/commits --jq '.[] | "\(.version[0:8])  \(.committed_at)"'
  gh api gists/<id>/<version> --jq '.files["<filename>"].content'
  ```

## Running these commands as an agent

Read-only and safe to run unasked: `status`, `fetch`, `fetch --diff --no-pager`
(`--diff` pages when stdout is a terminal).

**Never run `list` or `dashboard`.** Both are interactive `huh` forms that take over
the screen and block until a human picks "← Back" or presses Ctrl+C. To answer
"is this path tracked", use `status` or read `~/.config/gh-automagist/state.json`.

Everything else writes — `add`, `remove`, `push`, `pull`, `monitor`, `restart`,
`stop`. Print the command line for the user to run and let them decide. In
particular, print `pull` *without* `--yes`, so its confirmation prompt survives.
