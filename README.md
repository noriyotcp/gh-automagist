# gh-automagist

`gh-automagist` is a GitHub CLI extension that continuously monitors your local files and automatically synchronizes them to GitHub Gists.
It's designed to run silently in the background, treating your local environment as the source of truth ("Last Write Wins").

## Features

- **Daemonized File Watcher**: Runs silently in the background. Tracks `.config/gh-automagist/state.json`.
- **Debounced Synchronization**: Detects file saves via `fsnotify` and, after a configurable quiet-window (default 5 seconds), pushes the latest content to your Gists. Rapid successive edits collapse into a single Gist revision.
- **Interactive UI**: Includes an intuitive TUI built with Charmbracelet `huh` to manage your tracked files.

## Supported OS

Currently, `gh-automagist` officially supports **macOS** and **Linux**. Windows is not supported at this time due to background daemon technicalities.

## Installation

You can install or upgrade the extension natively via the GitHub CLI:

```bash
gh extension install noriyotcp/gh-automagist
```

## Commands

| Command | Description |
| :--- | :--- |
| `gh automagist dashboard` | Open the interactive TUI dashboard to manage files, start/stop the monitor, and view status. Linking a file to an existing Gist prompts for a direction in place when the two sides differ, so the `--adopt-remote` / `--force` decision does not send you back to the shell. |
| `gh automagist add [path]` | Register a new local file to be monitored. Without `--gist-id` it creates a new Gist. With `--gist-id <id>` it reads the Gist first: identical content is linked without an API write, a name the Gist does not hold yet is uploaded, and a genuine difference is reported and blocked — nothing is sent and nothing is tracked until you pick a direction with `--adopt-remote` (take the Gist's content, backing up the local file) or `--force` (replace the Gist's content with the local file). |
| `gh automagist remove [path]` | Stop monitoring a specific file. |
| `gh automagist list` | View tracked files, open them in `$EDITOR`, or view the Gist online. |
| `gh automagist monitor` | Start the monitor in the foreground. Use `--daemon` to run it silently in the background, or `--debounce=<dur>` to tune the quiet-window before Gist syncs (see [Configuration](#configuration)). On startup it reconciles every tracked file with the same rules as `push`, so edits made while the daemon was down are caught instead of waiting for the next write. As a daemon it writes one log per run to `~/.config/gh-automagist/log/monitor_<timestamp>.log` and prints that path on startup; in the foreground the same output goes to the terminal. |
| `gh automagist status` | View the status of the background daemon (RUNNING/STOPPED, with daemon version when known) and the list of currently tracked files. Warns when the running daemon's version differs from the installed binary — a hint to run `restart`. |
| `gh automagist --version` | Print the installed binary's version, commit, and build date. |
| `gh automagist fetch [path]` | Check tracked Gists for remote changes without applying them. Pass `--diff` to see the actual unified diff (local vs remote) — for all newer files without a path, or one specific file with a path. Add `--no-pager` to skip the pager. |
| `gh automagist pull [path]` | Fetch tracked files from their Gists back to local disk with backup and safety checks. Supports `--force`, `--yes`, `--dry-run`, `--no-backup`. |
| `gh automagist push [path]` | Send local changes up to their Gists. Only pushes files whose Gist still holds the content of the last sync; anything the remote changed on its own is blocked with a reason. Supports `--force`, `--dry-run`. |
| `gh automagist stop` | Gracefully terminate the background daemon. |

## Linking a second machine

`add --gist-id` is how a Gist created on one machine gets picked up on another,
and the two copies are rarely identical when that happens. The command reads the
Gist before writing anything, so a stale local copy cannot silently overwrite the
newer content already there:

```bash
gh automagist add ~/.zshrc --gist-id <id>
#   Local:  1820 bytes
#   Remote: 2044 bytes, updated 2026-09-01T12:04:31Z
#   Diff:   +9 lines, -1 lines (remote relative to local)
#
#   Nothing was written. Re-run with one of:
#     --adopt-remote  take the Gist's content, backing up the local file
#     --force         replace the Gist's content with the local file
```

`--adopt-remote` keeps a timestamped `.bak.<timestamp>` copy of the local file
beside it before writing, and suppresses the daemon's echo PATCH the same way
`pull` does.

`gh automagist dashboard` reaches the same decision through its own prompt:
"Add File" → "Link to an existing Gist" shows the comparison above and then
asks which side wins, so the two directions are available without leaving the
TUI.

If a `--gist-id` link did overwrite something before you noticed, the Gist's own
revision history still has it:

```bash
gh api gists/<id>/commits --jq '.[] | "\(.version[0:8])  \(.committed_at)"'
gh api gists/<id>/<version> --jq '.files["<filename>"].content'
```

## Configuration

### Debounce interval

Every write to a tracked file arms a per-file quiet-window; only after that window elapses without another write does the sync run. Rapid edits therefore collapse into a single Gist revision. The default (**5 seconds**) is tuned around the observed cadence of AI-agent Edit tools such as Claude Code, which emit edits roughly 2–10 seconds apart.

Override in order of precedence:

1. **CLI flag** on `monitor` / `restart`:
    ```bash
    gh automagist monitor --debounce=10s --daemon
    gh automagist restart --debounce=500ms
    ```
2. **Environment variable** `GH_AUTOMAGIST_DEBOUNCE_INTERVAL` (evaluated at daemon start):
    ```bash
    export GH_AUTOMAGIST_DEBOUNCE_INTERVAL=10s
    gh automagist monitor --daemon
    ```
3. **Compiled-in default**: 5 seconds.

Values are Go `time.Duration` strings (`500ms`, `5s`, `2m`, ...). A value of `0` or negative disables debouncing (every write triggers a sync).

## Development (Build from source)

If you wish to compile the extension yourself:

```bash
git clone https://github.com/noriyotcp/gh-automagist.git
cd gh-automagist
go build -o gh-automagist
```

## License

MIT License
