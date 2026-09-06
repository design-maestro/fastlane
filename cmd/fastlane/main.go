package main

import (
	"os"

	"github.com/design-maestro/fastlane/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
