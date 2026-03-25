package main

import (
	"os"

	"github.com/1broseidon/ketch/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
