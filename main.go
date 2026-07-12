package main

import "github.com/noriyo_tcp/gh-automagist/cmd"

// Populated at build time via goreleaser's -X ldflags. Defaults let
// `go run` / plain `go build` still work.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cmd.SetVersionInfo(version, commit, date)
	cmd.Execute()
}
