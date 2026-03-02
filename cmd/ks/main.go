package main

import (
	"os"

	"github.com/mad01/kitty-session/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
