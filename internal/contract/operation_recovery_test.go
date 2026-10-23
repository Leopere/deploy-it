package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func receiptFixture(t *testing.T) Operation {
	t.Helper()
	base := t.TempDir()
	t.Setenv("DEPLOY_IT_STATE_DIR", base)
	id := digest(t.Name())
	dir := filepath.Join(base, id)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	op := Operation{ID: id, Directory: dir, Root: base, Options: Options{Commit: strings.Repeat("a", 40)}, Environment: "production", State: "passed", Created: time.Now().Add(-time.Minute), Deadline: time.Now().Add(time.Minute), LiveAcceptance: "unknown", Log: filepath.Join(dir, "command.log"), Attempt: 1}
	if err := saveOperation(op); err != nil {
		t.Fatal(err)
	}
	return op
}

func TestOperationTargetLockDeadline(t *testing.T) {
	op := receiptFixture(t)
	path := filepath.Join(op.Directory, "target.lock")
	lock, err := lockFile(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer unlockFile(lock)
	start := time.Now()
	_, err = waitTargetLock(path, start.Add(60*time.Millisecond))
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("unbounded target lock: %v", err)
	}
}

func TestOperationReceiptMustMatchRevision(t *testing.T) {
	op := receiptFixture(t)
	data, _ := json.Marshal(map[string]any{"revision": strings.Repeat("b", 40), "passed": true, "target": "https://example.test"})
	if err := os.WriteFile(filepath.Join(op.Directory, "acceptance.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	status, err := Status(op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status.LiveAcceptance != "unknown" {
		t.Fatalf("accepted wrong revision: %+v", status)
	}
}

func TestOperationRetryRejectsPossibleActivation(t *testing.T) {
	for _, state := range []string{"running", "failed", "queued"} {
		t.Run(state, func(t *testing.T) {
			op := receiptFixture(t)
			op.State = state
			op.CommandStarted = true
			if err := saveOperation(op); err != nil {
				t.Fatal(err)
			}
			if _, err := Retry(op.ID); err == nil {
				t.Fatal("retried possible activation")
			}
			after, err := readOperation(op.Directory)
			if err != nil {
				t.Fatal(err)
			}
			if after.State != state || !after.CommandStarted {
				t.Fatal("retry rewrote original receipt")
			}
		})
	}
}

func TestOperationRetryRejectsAbsentQueuedWorker(t *testing.T) {
	op := receiptFixture(t)
	op.State = "queued"
	op.CommandStarted = false
	if err := saveOperation(op); err != nil {
		t.Fatal(err)
	}
	if _, err := Retry(op.ID); err == nil {
		t.Fatal("retried a delayed queued worker")
	}
}

func TestOperationPreCommandRetryPreservesAttempt(t *testing.T) {
	repo, commit, gate, record := operationFixture(t, false)
	defer closeGate(t, gate)
	setOperationState(t)
	// A missing required variable fails before any repository command can run.
	t.Setenv("DEPLOY_TEST_RECORD", record)
	if err := os.Unsetenv("DEPLOY_TEST_RECORD"); err != nil {
		t.Fatal(err)
	}
	runner, _ := openRunner(t, repo)
	op, err := runner.Enqueue(Options{Commit: commit, Branch: "main", Remote: "origin"})
	if err != nil {
		t.Fatal(err)
	}
	failed := waitOperation(t, op.ID)
	if failed.State != "failed" || failed.CommandStarted {
		t.Fatalf("precommand failure=%+v", failed)
	}
	t.Setenv("DEPLOY_TEST_RECORD", record)
	closeGate(t, gate)
	retried, err := Retry(op.ID)
	if err != nil {
		t.Fatal(err)
	}
	finished := waitOperation(t, retried.ID)
	if finished.State != "passed" || finished.Attempt != 2 {
		t.Fatalf("retry=%+v", finished)
	}
	if _, err := os.Stat(filepath.Join(finished.Directory, "attempt-1.json")); err != nil {
		t.Fatal(err)
	}
}

func TestOperationHandlesSurviveLargeLogs(t *testing.T) {
	op := receiptFixture(t)
	file, err := os.Create(op.Log)
	if err != nil {
		t.Fatal(err)
	}
	writer := &handleWriter{writer: file, dir: op.Directory}
	if _, err = writer.Write([]byte("Queued https://jenkins.example/queue/item/42/\n")); err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Write([]byte(strings.Repeat("x", 1024*1024+100) + "\n")); err != nil {
		t.Fatal(err)
	}
	file.Close()
	observed, err := Status(op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(observed.JenkinsHandles) != 1 || !strings.Contains(observed.JenkinsHandles[0], "/42/") {
		t.Fatalf("lost handles: %+v", observed.JenkinsHandles)
	}
}
