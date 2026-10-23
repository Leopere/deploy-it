package contract

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == "_worker" {
		if err := Work(os.Args[2]); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestOperationEnqueueDeduplicatesAndRunsDetachedSnapshot(t *testing.T) {
	repo, commit, release, record := operationFixture(t, false)
	defer closeGate(t, release)
	setOperationState(t)

	runner, _ := openRunner(t, repo)
	opts := Options{Commit: commit, Branch: "main", Remote: "origin"}
	const callers = 8
	results := make([]Operation, callers)
	errs := make([]error, callers)
	var group sync.WaitGroup
	for i := range results {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			results[i], errs[i] = runner.Enqueue(opts)
		}(i)
	}
	group.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	id := results[0].ID
	for _, result := range results {
		if result.ID != id {
			t.Fatalf("enqueue returned different IDs: %s and %s", id, result.ID)
		}
	}

	// Change both the worktree and index after enqueue. The detached worker must
	// continue from the frozen pushed commit.
	if err := os.WriteFile(filepath.Join(repo, "deploy.sh"), []byte("#!/bin/sh\nexit 41\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "deploy.sh")
	if err := os.WriteFile(filepath.Join(repo, "deploy.sh"), []byte("#!/bin/sh\nexit 42\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	queued, err := Status(id)
	if err != nil {
		t.Fatal(err)
	}
	if queued.State != "queued" && queued.State != "running" {
		t.Fatalf("enqueue did not return a live operation: %#v", queued)
	}
	if queued.Finished != nil {
		t.Fatalf("enqueue waited for completion: %#v", queued)
	}
	if _, err := os.Stat(release); err == nil {
		t.Fatal("test release gate opened before enqueue returned")
	}
	closeGate(t, release)

	finished := waitOperation(t, id)
	if finished.State != "passed" {
		t.Fatalf("state = %q, want passed; error=%s", finished.State, finished.Error)
	}
	if finished.LiveAcceptance != "unknown" {
		t.Fatalf("acceptance without a receipt = %q, want unknown", finished.LiveAcceptance)
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != commit+"\n" {
		t.Fatalf("command used revision %q, want frozen commit %q", data, commit)
	}
	if len(finished.JenkinsHandles) != 1 || !strings.Contains(finished.JenkinsHandles[0], "/queue/item/91") {
		t.Fatalf("retained Jenkins handles = %#v", finished.JenkinsHandles)
	}

	again, err := runner.Enqueue(opts)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != id {
		t.Fatalf("repeat enqueue changed ID: %s", again.ID)
	}
	if _, err := os.Stat(record); err != nil {
		t.Fatalf("deduplicated operation did not retain original output: %v", err)
	}
}

func TestOperationReceiptSetsLiveAcceptanceAndFailureRetainsHandles(t *testing.T) {
	repo, commit, release, _ := operationFixture(t, true)
	defer closeGate(t, release)
	setOperationState(t)
	runner, _ := openRunner(t, repo)
	opts := Options{Commit: commit, Branch: "main", Remote: "origin"}

	passed, err := runner.Enqueue(opts)
	if err != nil {
		t.Fatal(err)
	}
	closeGate(t, release)
	finished := waitOperation(t, passed.ID)
	if finished.State != "passed" || finished.LiveAcceptance != "passed" {
		t.Fatalf("receipt status = %#v", finished)
	}

	// A separate commit is used for failure to exercise immutable operation IDs
	// and ensure deduplicating a failed result never executes its command again.
	release2 := filepath.Join(t.TempDir(), "release")
	record2 := filepath.Join(t.TempDir(), "record")
	t.Setenv("DEPLOY_TEST_RELEASE", release2)
	t.Setenv("DEPLOY_TEST_RECORD", record2)
	write(t, filepath.Join(repo, "deploy.sh"), "#!/bin/sh\nprintf 'https://jenkins.example/job/publish/92\\n'\nprintf x >> \"$DEPLOY_TEST_RECORD\"\nexit 9\n", 0o755)
	git(t, repo, "add", "deploy.sh")
	git(t, repo, "commit", "-m", "failing operation")
	git(t, repo, "push", "origin", "main")
	failedCommit := output(t, repo, "rev-parse", "HEAD")
	failed, err := runner.Enqueue(Options{Commit: failedCommit, Branch: "main", Remote: "origin"})
	if err != nil {
		t.Fatal(err)
	}
	failed = waitOperation(t, failed.ID)
	if failed.State != "failed" || failed.LiveAcceptance != "unknown" {
		t.Fatalf("failed status = %#v", failed)
	}
	logs, err := os.ReadFile(failed.Log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logs), "/job/publish/92") || !strings.Contains(string(logs), "exit status 9") {
		t.Fatalf("failure log lost Jenkins handle or error: %s", logs)
	}
	failedAgain, err := runner.Enqueue(Options{Commit: failedCommit, Branch: "main", Remote: "origin"})
	if err != nil {
		t.Fatal(err)
	}
	if failedAgain.ID != failed.ID {
		t.Fatalf("failed repeat enqueue ID = %q, want %q", failedAgain.ID, failed.ID)
	}
	data, err := os.ReadFile(record2)
	if err != nil || string(data) != "x" {
		t.Fatalf("failed command was not retained exactly once: data=%q err=%v", data, err)
	}
}

func TestOperationWorkerSurvivesEnqueueCallerExit(t *testing.T) {
	repo, commit, release, _ := operationFixture(t, false)
	defer closeGate(t, release)
	setOperationState(t)
	t.Setenv("DEPLOY_IT_TEST_REPO", repo)
	t.Setenv("DEPLOY_IT_TEST_COMMIT", commit)

	cmd := exec.Command(os.Args[0], "-test.run=^TestOperationEnqueueCallerHelper$")
	cmd.Env = append(os.Environ(), "DEPLOY_IT_CALLER_HELPER=1")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("enqueue caller process: %v", err)
	}
	id := strings.TrimSpace(strings.SplitN(string(output), "\n", 2)[0])
	if len(id) != 64 {
		t.Fatalf("caller returned invalid operation ID %q", id)
	}
	queued, err := Status(id)
	if err != nil {
		t.Fatal(err)
	}
	if queued.State != "queued" && queued.State != "running" {
		t.Fatalf("operation did not survive caller exit: %#v", queued)
	}
	if queued.Finished != nil {
		t.Fatalf("gated operation finished before release: %#v", queued)
	}
	closeGate(t, release)
	if finished := waitOperation(t, id); finished.State != "passed" {
		t.Fatalf("detached worker state = %q, error=%s", finished.State, finished.Error)
	}
}

func TestOperationEnqueueCallerHelper(t *testing.T) {
	if os.Getenv("DEPLOY_IT_CALLER_HELPER") != "1" {
		t.Skip("subprocess helper")
	}
	runner, err := Open(os.Getenv("DEPLOY_IT_TEST_REPO"), os.Stdout, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	op, err := runner.Enqueue(Options{Commit: os.Getenv("DEPLOY_IT_TEST_COMMIT"), Branch: "main", Remote: "origin"})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(op.ID)
}

func operationFixture(t *testing.T, receipt bool) (repo, commit, release, record string) {
	t.Helper()
	repo = newRepo(t, true)
	release = filepath.Join(t.TempDir(), "release")
	record = filepath.Join(t.TempDir(), "record")
	t.Setenv("DEPLOY_TEST_RELEASE", release)
	t.Setenv("DEPLOY_TEST_RECORD", record)
	write(t, filepath.Join(repo, ManifestName), `{"version":1,"after_ship":true,"environment":"test","command":["./deploy.sh"],"env":["DEPLOY_TEST_RELEASE","DEPLOY_TEST_RECORD"],"timeout_seconds":30}`, 0o644)
	acceptance := ""
	if receipt {
		acceptance = "printf '{\"revision\":\"%s\",\"passed\":true,\"target\":\"test-target\"}\\n' \"$DEPLOY_IT_COMMIT\" >> \"$DEPLOY_IT_RECEIPT_FILE\"\n"
	}
	script := fmt.Sprintf("#!/bin/sh\nset -eu\nprintf 'https://jenkins.example/queue/item/91\\n'\nprintf '%%s\\n' \"$DEPLOY_IT_COMMIT\" >> \"$DEPLOY_TEST_RECORD\"\nwhile [ ! -f \"$DEPLOY_TEST_RELEASE\" ]; do sleep 0.02; done\n%s", acceptance)
	write(t, filepath.Join(repo, "deploy.sh"), script, 0o755)
	commitAndPush(t, repo, "operation contract", "")
	commit = output(t, repo, "rev-parse", "HEAD")
	return repo, commit, release, record
}

func setOperationState(t *testing.T) {
	t.Helper()
	t.Setenv("DEPLOY_IT_STATE_DIR", filepath.Join(t.TempDir(), "operations"))
}

func closeGate(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("release\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func waitOperation(t *testing.T, id string) Operation {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		op, err := Status(id)
		if err != nil {
			t.Fatal(err)
		}
		if op.State == "passed" || op.State == "failed" {
			return op
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("operation %s did not finish", id)
	return Operation{}
}
