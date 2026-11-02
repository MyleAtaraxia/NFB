package main

import (
	"github.com/MyleAtaraxia/NFB/internal/cmd"
	"os"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
