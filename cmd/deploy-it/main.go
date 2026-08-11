package main

import (
	"fmt"
	"os"

	"github.com/Leopere/deploy-it/internal/app"
)

var version = "devel"

func main() {
	if err := app.Run(os.Args[1:], version, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "deploy-it:", err)
		os.Exit(1)
	}
}
