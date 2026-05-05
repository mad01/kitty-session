package main

import (
	"fmt"
	"os"

	"github.com/mad01/kitty-session/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "ks:", err)
		os.Exit(1)
	}
}
