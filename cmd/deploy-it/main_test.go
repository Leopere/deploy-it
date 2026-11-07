package main

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCLIProcess(t *testing.T) {
	if os.Getenv("DEPLOY_IT_RETIREMENT_CHILD") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"deploy-it"}, os.Args[i+1:]...)
			break
		}
	}
	if os.Getenv("DEPLOY_IT_TEST_CLOSE_STDIN") == "1" {
		os.Stdin.Close()
	}
	main()
	os.Exit(0)
}

func TestLegacyCLIHasNoSideEffects(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "repo")
	for _, dir := range []string{bin, home, filepath.Join(repo, ".git")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	marker := filepath.Join(root, "executed")
	for _, name := range []string{"git", "deploy-it", "ship-it", "curl", "wget", "python3", "ssh"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\necho executed >> '"+marker+"'\nexit 93\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	fixtures := map[string]string{
		filepath.Join(repo, "uncommitted.txt"):                      "preserve working file",
		filepath.Join(repo, ".git", "index"):                        "preserve index",
		filepath.Join(repo, ".deploy-it.json"):                      `{"version":1,"after_ship":true,"command":["git","deploy"],"environment":"production"}`,
		filepath.Join(home, "existing-operation", "operation.json"): `{"state":"queued","command":["git","deploy"]}`,
		filepath.Join(home, "existing-operation", "command.log"):    "preserve historical log",
	}
	for path, content := range fixtures {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := func() map[string]string {
		result := map[string]string{}
		if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			result[path] = string(data)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := snapshot()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, stdin string
		args        []string
		closed      bool
	}{
		{name: "EOF"},
		{name: "closed stdin", closed: true},
		{name: "hook JSON", stdin: `{"hook_event_name":"Stop","cwd":"` + repo + `"}`},
		{name: "malformed JSON", stdin: `{broken`},
		{name: "explicit deployment", args: []string{"--commit", strings.Repeat("a", 40), "--branch", "main", "--wait"}},
		{name: "worker", args: []string{"_worker", "existing-operation"}},
		{name: "retry", args: []string{"retry", "existing-operation"}},
		{name: "wait", args: []string{"wait", "existing-operation"}},
		{name: "status", args: []string{"status", "existing-operation"}},
		{name: "check", args: []string{"check"}},
		{name: "install", args: []string{"install", "--skills-only"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			args := append([]string{"-test.run=^TestCLIProcess$", "--"}, tc.args...)
			cmd := exec.CommandContext(ctx, executable, args...)
			cmd.Dir = repo
			cmd.Env = append(os.Environ(), "DEPLOY_IT_RETIREMENT_CHILD=1", "HOME="+home, "PATH="+bin,
				"DEPLOY_IT_STATE_DIR="+home, "DEPLOY_IT_COMMIT="+strings.Repeat("a", 40))
			if tc.closed {
				cmd.Env = append(cmd.Env, "DEPLOY_IT_TEST_CLOSE_STDIN=1")
			}
			cmd.Stdin = strings.NewReader(tc.stdin)
			var output, diagnostics bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &diagnostics
			err := cmd.Run()
			if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
				t.Fatalf("exit=%v stdout=%q stderr=%q", err, output.String(), diagnostics.String())
			}
			if output.Len() != 0 || !strings.Contains(diagnostics.String(), "retired") || !strings.Contains(diagnostics.String(), "Jenkins") {
				t.Fatalf("stdout=%q stderr=%q", output.String(), diagnostics.String())
			}
			if after := snapshot(); !reflect.DeepEqual(before, after) {
				t.Fatal("legacy command executed an external command or changed worktree, index, operation data, or configuration")
			}
		})
	}
}
