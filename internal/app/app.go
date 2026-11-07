package app

import (
	"errors"
	"fmt"
	"io"
)

const guidance = "deploy-it is retired. Commit and normal-push changes with Git, then invoke the repository's tracked Jenkins submission command for that exact pushed revision and observe live acceptance."

const usage = `deploy-it is retired.

Usage:
  deploy-it help
  deploy-it version

Commit and normal-push changes with Git, then invoke the repository's tracked
Jenkins submission command for that exact pushed revision and observe live
acceptance. This command does not deploy, install, inspect manifests, resume
operations, or change local files.
`

var ErrRetired = errors.New(guidance)

func Run(args []string, version string, out, _ io.Writer) error {
	if len(args) == 1 {
		switch args[0] {
		case "help", "-h", "--help":
			_, err := fmt.Fprint(out, usage)
			return err
		case "version", "--version":
			_, err := fmt.Fprintln(out, version)
			return err
		}
	}
	return ErrRetired
}
