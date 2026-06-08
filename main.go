package main

import (
	"os"

	"github.com/galkasoft/agentmail/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
