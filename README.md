# gh-automagist (v2)

`gh-automagist` is a GitHub CLI extension that continuously monitors your local files and automatically synchronizes them to GitHub Gists.
It's designed to run silently in the background, treating your local environment as the source of truth ("Last Write Wins").

This is the v2 rewrite in Go, replacing the original Ruby version for better performance, zero dependencies, and easier distribution as a single binary.

## Features

- **Daemonized File Watcher**: Runs silently in the background. Tracks `.config/gh-automagist/state.json`.
- **Instant Synchronization**: Detects file saves instantly via `fsnotify` and pushes changes directly to your Gists.
- **Interactive UI**: Includes an intuitive TUI built with Charmbracelet `huh` to manage your tracked files.
- **Zero Dependencies**: Distributed as a single compiled Go binary. No Ruby, no external gems needed.

## Installation

You can install or upgrade the extension natively via the GitHub CLI:

```bash
gh extension install noriyo_tcp/gh-automagist
```

## Commands

| Command | Description |
| :--- | :--- |
| `gh automagist add [path]` | Register a new local file to be monitored. Creates a new Gist or links to an existing one. |
| `gh automagist remove [path]` | Stop monitoring a specific file. |
| `gh automagist list` | Open the interactive TUI to view tracked files, open them in `$EDITOR`, or view the Gist online. |
| `gh automagist monitor` | Start the background daemon to watch your registered files. |
| `gh automagist status` | View the status of the background daemon (RUNNING/STOPPED) and the list of currently tracked files. |
| `gh automagist stop` | Gracefully terminate the background daemon. |

## Development (Build from source)

If you wish to compile the extension yourself:

```bash
git clone https://github.com/noriyo_tcp/gh-automagist.git
cd gh-automagist
go build -o gh-automagist
```

## License

MIT License
