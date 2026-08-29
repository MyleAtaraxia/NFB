package main

import (
	"NFB/internal/cmd"
	"log/slog"
	"os"
)

func main() {
	if err := cmd.Execute(); err != nil {
		slog.Error(os.Stderr.Name(), err)
		os.Exit(1)
	}
}
