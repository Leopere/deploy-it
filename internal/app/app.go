package app

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Leopere/deploy-it/internal/contract"
	"github.com/Leopere/deploy-it/internal/install"
	"github.com/Leopere/deploy-it/internal/skilldoc"
)

const usage = `deploy-it executes an explicit repository deployment contract.

Usage:
  deploy-it --commit sha --branch branch [--tag tag] [--remote origin]
  deploy-it check [--commit sha] [--remote origin]
  deploy-it trust [--remote origin]
  deploy-it install [--skills-only]
  deploy-it version
  deploy-it skill
`

func Run(args []string, version string, out, errOut io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "help", "-h", "--help":
			fmt.Fprint(out, usage)
			return nil
		case "version", "--version":
			fmt.Fprintln(out, version)
			return nil
		case "skill":
			fmt.Fprint(out, skilldoc.SkillMD)
			return nil
		case "install":
			return runInstall(args[1:], out)
		case "check":
			return runContract(args[1:], true, out, errOut)
		case "trust":
			return runTrust(args[1:], out, errOut)
		}
	}
	return runContract(args, false, out, errOut)
}

func runContract(args []string, check bool, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("deploy", flag.ContinueOnError)
	fs.SetOutput(errOut)
	commit := fs.String("commit", "", "immutable shipped commit")
	branch := fs.String("branch", "", "shipped branch")
	tag := fs.String("tag", "", "shipped tag")
	remote := fs.String("remote", "origin", "Git remote containing the shipped revision")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	runner, err := contract.Open(cwd, out, errOut)
	if err != nil {
		return err
	}
	result, err := runner.Run(contract.Options{Commit: *commit, Branch: *branch, Tag: *tag, Remote: *remote, Check: check})
	if err != nil {
		return err
	}
	if result.Skipped {
		fmt.Fprintln(out, "No tracked .deploy-it.json contract; deployment skipped.")
	}
	return nil
}

func runTrust(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("trust", flag.ContinueOnError)
	fs.SetOutput(errOut)
	remote := fs.String("remote", "origin", "Git remote identifying the repository")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	runner, err := contract.Open(cwd, out, errOut)
	if err != nil {
		return err
	}
	return runner.Trust(*remote)
}

func runInstall(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(out)
	skillsOnly := fs.Bool("skills-only", false, "only install skills")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	return install.Local(!*skillsOnly, out)
}
