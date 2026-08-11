package contract

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const manifest = `{"version":1,"after_ship":true,"environment":"production","command":["./deploy.sh","literal argument"],"env":["DEPLOY_TEST_RECORD"],"timeout_seconds":30}`

func TestMissingManifestSkips(t *testing.T) {
	t.Parallel()
	repo := newRepo(t, false)
	runner, _ := Open(repo, &bytes.Buffer{}, &bytes.Buffer{})
	result, err := runner.Run(Options{Check: true})
	if err != nil || !result.Skipped {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestExecutesTrustedRemoteRevisionFromPrivateSnapshot(t *testing.T) {
	repo := newRepo(t, true)
	record := filepath.Join(t.TempDir(), "record")
	t.Setenv("DEPLOY_TEST_RECORD", record)
	t.Setenv("UNDECLARED_SECRET", "must-not-leak")
	write(t, filepath.Join(repo, ManifestName), manifest, 0o644)
	script := "#!/bin/sh\nset -eu\nif env | grep -q '^UNDECLARED_SECRET='; then exit 19; fi\nprintf '%s|%s|%s|%s|%s|%s' \"$1\" \"$DEPLOY_IT_COMMIT\" \"$DEPLOY_IT_BRANCH\" \"$DEPLOY_IT_TAG\" \"$DEPLOY_IT_ENVIRONMENT\" \"$PWD\" > \"$DEPLOY_TEST_RECORD\"\n"
	write(t, filepath.Join(repo, "deploy.sh"), script, 0o755)
	commitAndPush(t, repo, "contract", "v2026.08.10.1")
	commit := output(t, repo, "rev-parse", "HEAD")
	runner, out := trustedRunner(t, repo)
	result, err := runner.Run(Options{Commit: commit, Branch: "main", Tag: "v2026.08.10.1", Remote: "origin"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(data), "|")
	if len(parts) != 6 || parts[0] != "literal argument" || parts[1] != commit || parts[2] != "main" || parts[3] != "v2026.08.10.1" || parts[4] != "production" || parts[5] == repo {
		t.Fatalf("handoff=%q", data)
	}
	if result.Environment != "production" || !strings.Contains(out.String(), "Deployed production") {
		t.Fatalf("result=%#v out=%q", result, out.String())
	}
}

func TestRejectsUnpushedCommit(t *testing.T) {
	repo := newRepo(t, true)
	record := filepath.Join(t.TempDir(), "record")
	t.Setenv("DEPLOY_TEST_RECORD", record)
	write(t, filepath.Join(repo, ManifestName), manifest, 0o644)
	write(t, filepath.Join(repo, "deploy.sh"), "#!/bin/sh\nexit 0\n", 0o755)
	git(t, repo, "add", ManifestName, "deploy.sh")
	git(t, repo, "commit", "-m", "unpushed contract")
	commit := output(t, repo, "rev-parse", "HEAD")
	runner, _ := trustedRunner(t, repo)
	_, err := runner.Run(Options{Commit: commit, Branch: "main", Remote: "origin"})
	if err == nil || !strings.Contains(err.Error(), "pushed remote branch") {
		t.Fatalf("error=%v", err)
	}
}

func TestUntrustedContractDoesNotExecute(t *testing.T) {
	repo := newRepo(t, true)
	record := filepath.Join(t.TempDir(), "record")
	t.Setenv("DEPLOY_TEST_RECORD", record)
	write(t, filepath.Join(repo, ManifestName), manifest, 0o644)
	write(t, filepath.Join(repo, "deploy.sh"), "#!/bin/sh\ntouch \"$DEPLOY_TEST_RECORD\"\n", 0o755)
	commitAndPush(t, repo, "contract", "")
	commit := output(t, repo, "rev-parse", "HEAD")
	t.Setenv("DEPLOY_IT_TRUST_FILE", filepath.Join(t.TempDir(), "trust.json"))
	runner, _ := Open(repo, &bytes.Buffer{}, &bytes.Buffer{})
	_, err := runner.Run(Options{Commit: commit, Branch: "main", Remote: "origin"})
	if err == nil || !strings.Contains(err.Error(), "not locally trusted") {
		t.Fatalf("error=%v", err)
	}
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Fatal("untrusted deployment executed")
	}
}

func TestSnapshotIgnoresDeletedOrModifiedWorktreeContract(t *testing.T) {
	repo := newRepo(t, true)
	record := filepath.Join(t.TempDir(), "record")
	t.Setenv("DEPLOY_TEST_RECORD", record)
	write(t, filepath.Join(repo, ManifestName), manifest, 0o644)
	write(t, filepath.Join(repo, "deploy.sh"), "#!/bin/sh\nprintf shipped > \"$DEPLOY_TEST_RECORD\"\n", 0o755)
	commitAndPush(t, repo, "contract", "")
	commit := output(t, repo, "rev-parse", "HEAD")
	runner, _ := trustedRunner(t, repo)
	if err := os.Remove(filepath.Join(repo, ManifestName)); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(repo, "deploy.sh"), "#!/bin/sh\nexit 9\n", 0o755)
	if _, err := runner.Run(Options{Commit: commit, Branch: "main", Remote: "origin"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(record)
	if string(data) != "shipped" {
		t.Fatalf("snapshot output=%q", data)
	}
}

func TestCheckDoesNotExecuteAndCommandFailurePropagates(t *testing.T) {
	repo := newRepo(t, true)
	record := filepath.Join(t.TempDir(), "record")
	t.Setenv("DEPLOY_TEST_RECORD", record)
	write(t, filepath.Join(repo, ManifestName), manifest, 0o644)
	write(t, filepath.Join(repo, "deploy.sh"), "#!/bin/sh\ntouch \"$DEPLOY_TEST_RECORD\"\nexit 7\n", 0o755)
	commitAndPush(t, repo, "contract", "")
	commit := output(t, repo, "rev-parse", "HEAD")
	runner, _ := trustedRunner(t, repo)
	if _, err := runner.Run(Options{Check: true, Remote: "origin"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Fatal("check executed deployment")
	}
	if _, err := runner.Run(Options{Commit: commit, Branch: "main", Remote: "origin"}); err == nil || !strings.Contains(err.Error(), "exit status 7") {
		t.Fatalf("failure=%v", err)
	}
}

func TestManifestStrictnessAndRecursionProtection(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"version":1,"after_ship":true,"environment":"production","command":["./deploy.sh"],"timeout_seconds":30,"surprise":true}`,
		`{"version":1,"after_ship":false,"environment":"production","command":["./deploy.sh"],"timeout_seconds":30}`,
		`{"version":1,"after_ship":true,"environment":"production","command":["./deploy.sh"],"timeout_seconds":0}`,
		`{"version":1,"after_ship":true,"environment":"production","command":["./deploy.sh"],"env":["DEPLOY_IT_COMMIT"],"timeout_seconds":30}`,
		`{"version":1,"after_ship":true,"environment":"production","command":["./deploy.sh"],"timeout_seconds":30} {}`,
	} {
		if _, err := decodeManifest([]byte(body)); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	if _, err := validateExecutable(t.TempDir(), "./ship.sh"); err == nil || !strings.Contains(err.Error(), "must not invoke ship-it") {
		t.Fatalf("recursion error=%v", err)
	}
}

func TestTrustChangesWhenExecutableChanges(t *testing.T) {
	repo := newRepo(t, false)
	t.Setenv("DEPLOY_TEST_RECORD", filepath.Join(t.TempDir(), "record"))
	write(t, filepath.Join(repo, ManifestName), manifest, 0o644)
	write(t, filepath.Join(repo, "deploy.sh"), "#!/bin/sh\nexit 0\n", 0o755)
	runner, _ := trustedRunner(t, repo)
	write(t, filepath.Join(repo, "deploy.sh"), "#!/bin/sh\nexit 8\n", 0o755)
	data, _, executable, err := loadContract(repo)
	if err != nil {
		t.Fatal(err)
	}
	identity, _ := runner.repositoryIdentity("origin")
	if err := verifyTrust(identity, data, executable); err == nil {
		t.Fatal("changed executable retained trust")
	}
}

func trustedRunner(t *testing.T, repo string) (*Runner, *bytes.Buffer) {
	t.Helper()
	t.Setenv("DEPLOY_IT_TRUST_FILE", filepath.Join(t.TempDir(), "trust.json"))
	var out bytes.Buffer
	runner, err := Open(repo, &out, &out)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Trust("origin"); err != nil {
		t.Fatal(err)
	}
	return runner, &out
}

func newRepo(t *testing.T, pushSeed bool) string {
	t.Helper()
	dir := t.TempDir()
	remote := t.TempDir()
	git(t, remote, "init", "--bare", "-b", "main")
	git(t, dir, "init", "-b", "main")
	git(t, dir, "config", "user.name", "Test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "remote", "add", "origin", remote)
	write(t, filepath.Join(dir, "seed"), "seed\n", 0o644)
	git(t, dir, "add", "seed")
	git(t, dir, "commit", "-m", "seed")
	if pushSeed {
		git(t, dir, "push", "-u", "origin", "main")
	}
	return dir
}

func commitAndPush(t *testing.T, repo, message, tag string) {
	t.Helper()
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-m", message)
	if tag != "" {
		git(t, repo, "tag", tag)
	}
	git(t, repo, "push", "origin", "main", "--tags")
}

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
}

func output(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}
