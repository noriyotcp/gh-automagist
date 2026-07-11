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
| `gh automagist dashboard` | Open the interactive TUI dashboard to manage files, start/stop the monitor, and view status. |
| `gh automagist add [path]` | Register a new local file to be monitored. Creates a new Gist or links to an existing one. |
| `gh automagist remove [path]` | Stop monitoring a specific file. |
| `gh automagist list` | View tracked files, open them in `$EDITOR`, or view the Gist online. |
| `gh automagist monitor` | Start the monitor in the foreground. Use `--daemon` to run it silently in the background, or `--debounce=<dur>` to tune the quiet-window before Gist syncs (see [Configuration](#configuration)). |
| `gh automagist status` | View the status of the background daemon (RUNNING/STOPPED) and the list of currently tracked files. |
| `gh automagist stop` | Gracefully terminate the background daemon. |

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
