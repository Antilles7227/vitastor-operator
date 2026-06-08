package main

import (
	"os"

	"github.com/antilles7227/vitastor-operator/plugin/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
