# gh-automagist

`gh-automagist` is a GitHub CLI extension that continuously monitors your local files and automatically synchronizes them to GitHub Gists.
It's designed to run silently in the background, treating your local environment as the source of truth ("Last Write Wins").

## Features

- **Daemonized File Watcher**: Runs silently in the background. Tracks `.config/gh-automagist/state.json`.
- **Instant Synchronization**: Detects file saves instantly via `fsnotify` and pushes changes directly to your Gists.
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
| `gh automagist monitor` | Start the monitor in the foreground. Use `--daemon` to run it silently in the background. |
| `gh automagist status` | View the status of the background daemon (RUNNING/STOPPED) and the list of currently tracked files. |
| `gh automagist stop` | Gracefully terminate the background daemon. |

## Development (Build from source)

If you wish to compile the extension yourself:

```bash
git clone https://github.com/noriyotcp/gh-automagist.git
cd gh-automagist
go build -o gh-automagist
```

## License

MIT License
