package app

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Leopere/deploy-it/internal/contract"
	"github.com/Leopere/deploy-it/internal/install"
	"github.com/Leopere/deploy-it/internal/skilldoc"
)

const usage = `deploy-it executes an explicit repository deployment contract.

Usage:
  deploy-it
  deploy-it [--wait]
  deploy-it --commit sha --branch branch [--tag tag] [--remote origin]
  deploy-it retry HANDLE
  deploy-it wait HANDLE
  deploy-it status HANDLE
  deploy-it observe HANDLE
  deploy-it logs HANDLE
  deploy-it check [--commit sha] [--remote origin]
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
		case "_worker":
			if len(args) != 2 {
				return fmt.Errorf("worker requires one operation ID")
			}
			return contract.Work(args[1])
		case "retry":
			if len(args) != 2 {
				return fmt.Errorf("retry requires one operation ID")
			}
			op, err := contract.Retry(args[1])
			if err != nil {
				return err
			}
			return json.NewEncoder(out).Encode(op)
		case "wait":
			if len(args) != 2 {
				return fmt.Errorf("wait requires one operation ID")
			}
			return contract.Wait(args[1])
		case "status", "observe", "logs":
			if len(args) != 2 {
				return fmt.Errorf("%s requires one operation ID", args[0])
			}
			op, err := contract.Status(args[1])
			if err != nil {
				return err
			}
			if args[0] == "logs" {
				file, err := os.Open(filepath.Join(op.Directory, "command.log"))
				if err != nil {
					return err
				}
				defer file.Close()
				_, err = io.Copy(out, file)
				return err
			}
			return json.NewEncoder(out).Encode(op)
		case "check":
			return runContract(args[1:], true, out, errOut)
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
	wait := fs.Bool("wait", false, "observe the detached operation until its command completes")
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
	opts := contract.Options{Commit: *commit, Branch: *branch, Tag: *tag, Remote: *remote, Check: check}
	explicitRemote := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "remote" {
			explicitRemote = true
		}
	})
	if !check && !explicitRemote && *commit == "" && *branch == "" && *tag == "" {
		opts, err = runner.HeadOptions()
		if err != nil {
			return err
		}
	}
	if !check {
		op, err := runner.Enqueue(opts)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Deployment operation %s: %s. Observe: deploy-it status %s\n", op.ID, op.State, op.ID)
		if *wait {
			return contract.Wait(op.ID)
		}
		return nil
	}
	result, err := runner.Run(opts)
	if err != nil {
		return err
	}
	if result.Skipped {
		fmt.Fprintln(out, "No tracked .deploy-it.json contract; deployment skipped.")
	}
	return nil
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
