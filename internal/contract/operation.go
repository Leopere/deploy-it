package contract

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Operation is a durable handoff receipt, not a claim that a deployment is live.
// Options, target and revision are frozen before starting the detached observer.
type Operation struct {
	ID             string          `json:"id"`
	Directory      string          `json:"directory"`
	Root           string          `json:"repository"`
	Options        Options         `json:"options"`
	Environment    string          `json:"environment"`
	State          string          `json:"state"`
	Created        time.Time       `json:"created_at"`
	Started        *time.Time      `json:"started_at,omitempty"`
	Finished       *time.Time      `json:"finished_at,omitempty"`
	Deadline       time.Time       `json:"deadline"`
	PID            int             `json:"pid,omitempty"`
	Error          string          `json:"error,omitempty"`
	Recovery       string          `json:"recovery,omitempty"`
	LiveAcceptance string          `json:"live_acceptance"`
	Evidence       json.RawMessage `json:"evidence,omitempty"`
	Log            string          `json:"log"`
	ElapsedSeconds float64         `json:"elapsed_seconds"`
	JenkinsHandles []string        `json:"jenkins_handles,omitempty"`
	CommandStarted bool            `json:"command_started"`
	Attempt        int             `json:"attempt"`
}

func stateDirectory() (string, error) {
	if dir := os.Getenv("DEPLOY_IT_STATE_DIR"); dir != "" {
		return filepath.Abs(dir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "deploy-it", "operations"), nil
}

func operationDirectory(id string) (string, error) {
	raw, err := hex.DecodeString(id)
	if err != nil || len(raw) != 32 {
		return "", errors.New("invalid operation ID")
	}
	base, err := stateDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, id), nil
}

func digest(value string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(value))) }

func readOperation(dir string) (Operation, error) {
	var op Operation
	data, err := os.ReadFile(filepath.Join(dir, "operation.json"))
	if err != nil {
		return op, err
	}
	err = json.Unmarshal(data, &op)
	if err == nil && (op.Directory != dir || filepath.Base(dir) != op.ID) {
		return Operation{}, errors.New("operation receipt identity mismatch")
	}
	return op, err
}

func saveOperation(op Operation) error {
	data, err := json.MarshalIndent(op, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(op.Directory, ".receipt-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(append(data, '\n')); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, filepath.Join(op.Directory, "operation.json")); err != nil {
		return err
	}
	d, err := os.Open(op.Directory)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// Enqueue validates local immutable inputs, persists an operation, and starts an
// independent process. An existing operation is returned regardless of outcome:
// an unknown or failed activation must never be blindly submitted again.
func (r *Runner) Enqueue(opts Options) (Operation, error) {
	ctx, cancel := r.budget.context()
	defer cancel()
	commit, err := r.resolveCommit(ctx, opts.Commit, true)
	if err != nil {
		return Operation{}, err
	}
	opts.Commit = commit
	opts.Check = false
	result, err := r.handoffContract(ctx, commit)
	if err != nil {
		return Operation{}, err
	}
	base, err := stateDirectory()
	if err != nil {
		return Operation{}, err
	}
	if err = os.MkdirAll(base, 0700); err != nil {
		return Operation{}, err
	}
	root, err := filepath.EvalSymlinks(r.Root)
	if err != nil {
		return Operation{}, err
	}
	id := digest(root + "\x00" + result.Environment + "\x00" + commit)
	dir := filepath.Join(base, id)
	// Serialize record creation and spawning, including concurrent same-revision hooks.
	lock, err := lockFile(filepath.Join(base, id+".submit.lock"), false)
	if err != nil {
		return Operation{}, err
	}
	defer unlockFile(lock)
	if _, err = os.Stat(dir); err == nil {
		return Status(id)
	} else if !os.IsNotExist(err) {
		return Operation{}, err
	}
	if err = os.Mkdir(dir, 0700); err != nil {
		return Operation{}, err
	}
	now := time.Now().UTC()
	op := Operation{Attempt: 1, ID: id, Directory: dir, Root: root, Options: opts, Environment: result.Environment, State: "queued", Created: now, Deadline: now.Add(maximumDeploymentDuration), LiveAcceptance: "unknown", Log: filepath.Join(dir, "command.log")}
	if err = saveOperation(op); err != nil {
		return Operation{}, err
	}
	return launchOperation(op)
}

func launchOperation(op Operation) (Operation, error) {
	exe, err := os.Executable()
	if err == nil {
		var log *os.File
		log, err = os.OpenFile(op.Log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err == nil {
			cmd := exec.Command(exe, "_worker", op.ID)
			cmd.Dir = op.Root
			cmd.Stdin = nil
			cmd.Stdout = log
			cmd.Stderr = log
			cmd.Env = append(withoutEnv(os.Environ(), "SHIP_IT_DEADLINE_UNIX_MS"), "DEPLOY_IT_STATE_DIR="+filepath.Dir(op.Directory))
			detachWorker(cmd)
			err = cmd.Start()
			log.Close()
			if err == nil {
				_ = cmd.Process.Release()
			}
		}
	}
	if err != nil {
		finished := time.Now().UTC()
		op.State = "failed"
		op.Finished = &finished
		op.Error = "start background observer: " + err.Error()
		op.Recovery = "No command started. Repair the executable or state directory; reconcile before resubmitting."
		if saveErr := saveOperation(op); saveErr != nil {
			return op, saveErr
		}
		return op, err
	}
	return op, nil
}

func withoutEnv(env []string, key string) []string {
	result := make([]string, 0, len(env))
	for _, v := range env {
		if !strings.HasPrefix(v, key+"=") {
			result = append(result, v)
		}
	}
	return result
}

// Status only observes local durable evidence. It never starts an activation.
func Status(id string) (Operation, error) {
	dir, err := operationDirectory(id)
	if err != nil {
		return Operation{}, err
	}
	op, err := readOperation(dir)
	if err != nil {
		return op, err
	}
	if op.State == "queued" || op.State == "running" {
		lock, lockErr := lockFile(filepath.Join(dir, "worker.lock"), true)
		if lockErr == nil {
			defer unlockFile(lock)
			op, err = readOperation(dir)
			if err != nil {
				return op, err
			}
			// A new child has a short startup grace. Do not rewrite its state here:
			// observation must not race a worker that is about to acquire its lock.
			if (op.State == "queued" || op.State == "running") && time.Since(op.Created) > 10*time.Second {
				op.State = "failed"
				op.Error = "background observer is absent; deployment outcome is unknown"
				op.Recovery = "Inspect command.log and the existing Jenkins queue/build and live target. Do not resubmit an unknown activation."
			}
		} else if !isLockBusy(lockErr) {
			return op, lockErr
		}
	}
	end := time.Now()
	if op.Finished != nil {
		end = *op.Finished
	}
	op.ElapsedSeconds = end.Sub(op.Created).Seconds()
	op.JenkinsHandles = retainedJenkinsHandles(op.Log)
	if data, err := os.ReadFile(filepath.Join(dir, "jenkins-handles.json")); err == nil {
		var saved []string
		if json.Unmarshal(data, &saved) == nil {
			seen := map[string]bool{}
			for _, h := range op.JenkinsHandles {
				seen[h] = true
			}
			for _, h := range saved {
				if !seen[h] {
					op.JenkinsHandles = append(op.JenkinsHandles, h)
					seen[h] = true
				}
			}
		}
	}
	evidence, err := os.ReadFile(filepath.Join(dir, "acceptance.json"))
	if err == nil {
		var receipt struct {
			Revision string `json:"revision"`
			Passed   bool   `json:"passed"`
			Target   string `json:"target"`
		}
		if json.Unmarshal(evidence, &receipt) == nil && receipt.Revision == op.Options.Commit && receipt.Passed && receipt.Target != "" && op.State == "passed" {
			op.LiveAcceptance = "passed"
			op.Evidence = json.RawMessage(evidence)
		}
	}
	return op, nil
}

// Work is internal. It holds both an operation lock and a per-environment lock.
// Jenkins remains the authority for global target locks and live acceptance.
func Work(id string) error {
	dir, err := operationDirectory(id)
	if err != nil {
		return err
	}
	lock, err := waitTargetLock(filepath.Join(dir, "worker.lock"), time.Now().Add(10*time.Second))
	if err != nil {
		return err
	}
	defer unlockFile(lock)
	op, err := readOperation(dir)
	if err != nil {
		return err
	}
	if op.State != "queued" {
		return fmt.Errorf("operation is already %s; refusing another activation", op.State)
	}
	// Persist running only after target serialization. Queued workers remain observable.
	base := filepath.Dir(dir)
	target, err := waitTargetLock(filepath.Join(base, digest(op.Root+"\x00"+op.Environment)+".target.lock"), op.Deadline)
	if err != nil {
		return finishOperation(op, err)
	}
	defer unlockFile(target)
	now := time.Now().UTC()
	op.State = "running"
	op.Started = &now
	op.PID = os.Getpid()
	if err = saveOperation(op); err != nil {
		return err
	}
	os.Setenv("SHIP_IT_DEADLINE_UNIX_MS", fmt.Sprint(op.Deadline.UnixMilli()))
	os.Setenv("DEPLOY_IT_OPERATION_ID", op.ID)
	os.Setenv("DEPLOY_IT_RECEIPT_FILE", filepath.Join(dir, "acceptance.json"))
	output := &handleWriter{writer: os.Stdout, dir: dir}
	runner, runErr := Open(op.Root, output, output)
	if runErr == nil {
		runner.beforeCommand = func() error { op.CommandStarted = true; return saveOperation(op) }
		_, runErr = runner.Run(op.Options)
	}
	return finishOperation(op, runErr)
}

func finishOperation(op Operation, runErr error) error {
	finished := time.Now().UTC()
	op.Finished = &finished
	op.State = "passed"
	if runErr != nil {
		op.State = "failed"
		op.Error = runErr.Error()
		op.Recovery = "Inspect command.log and retained Jenkins queue/build handles. Reconcile the existing operation and live target before any retry."
		fmt.Fprintln(os.Stderr, "deploy-it background operation failed:", runErr)
	}
	if err := saveOperation(op); err != nil {
		return err
	}
	return runErr
}

func waitTargetLock(path string, deadline time.Time) (*os.File, error) {
	for {
		if !time.Now().Before(deadline) {
			return nil, errors.New("deployment deadline expired while queued for target; no command started")
		}
		lock, err := lockFile(path, true)
		if err == nil {
			return lock, nil
		}
		if !isLockBusy(err) {
			return nil, err
		}
		time.Sleep(min(50*time.Millisecond, time.Until(deadline)))
	}
}

// Read only the immutable manifest and executable tree metadata during handoff.
// Full archive validation remains in Run in the detached worker.
func (r *Runner) handoffContract(ctx context.Context, commit string) (Result, error) {
	data, err := gitOutput(ctx, r.Root, "show", commit+":"+ManifestName)
	if err != nil {
		return Result{}, err
	}
	manifest, err := decodeManifest([]byte(data))
	if err != nil {
		return Result{}, err
	}
	command := manifest.Command[0]
	if !strings.HasPrefix(command, "./") {
		return Result{}, errors.New("deployment command must start with ./")
	}
	relative := filepath.Clean(strings.TrimPrefix(command, "./"))
	if relative == "." || relative == ".." || strings.HasPrefix(relative, "../") || filepath.IsAbs(relative) || relative == "ship.sh" || relative == "ship-it" {
		return Result{}, errors.New("invalid deployment executable")
	}
	for _, path := range []string{ManifestName, filepath.ToSlash(relative)} {
		metadata, err := gitOutput(ctx, r.Root, "ls-tree", commit, "--", path)
		if err != nil {
			return Result{}, err
		}
		expected := "100755"
		if path == ManifestName {
			expected = "100644"
		}
		fields := strings.Fields(metadata)
		if len(fields) < 3 || (path == ManifestName && fields[0] != "100644" && fields[0] != "100755") || (path != ManifestName && fields[0] != expected) {
			return Result{}, fmt.Errorf("invalid tracked deployment file %s", path)
		}
	}
	return Result{Environment: manifest.Environment}, nil
}

var jenkinsHandleRE = regexp.MustCompile(`https?://[^\s"'<>]+/(?:queue/item/[0-9]+|job/[^\s"'<>]+/[0-9]+)/?`)

func retainedJenkinsHandles(path string) []string {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	// Status remains bounded even when a repository command produces a huge log.
	info, err := file.Stat()
	if err == nil && info.Size() > 1024*1024 {
		_, _ = file.Seek(-1024*1024, 2)
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	handles := []string{}
	seen := map[string]bool{}
	for scanner.Scan() {
		for _, handle := range jenkinsHandleRE.FindAllString(scanner.Text(), -1) {
			if !seen[handle] {
				seen[handle] = true
				handles = append(handles, handle)
			}
		}
	}
	return handles
}

// Wait observes the same durable operation without owning its process lifetime.
// Killing this waiter cannot kill Jenkins or restart the detached command.
func Wait(id string) error {
	for {
		op, err := Status(id)
		if err != nil {
			return err
		}
		switch op.State {
		case "passed":
			return nil
		case "failed":
			return fmt.Errorf("operation %s failed: %s; %s", id, op.Error, op.Recovery)
		}
		if time.Now().After(op.Deadline.Add(time.Second)) {
			return fmt.Errorf("operation %s observation deadline reached; inspect deploy-it status %s; do not resubmit", id, id)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// Retry is safe only when a durable pre-command receipt proves that no command
// could have run. Once execution starts, only Jenkins/target reconciliation can
// decide recovery; deploy-it cannot safely repeat an opaque repository command.
func Retry(id string) (Operation, error) {
	dir, err := operationDirectory(id)
	if err != nil {
		return Operation{}, err
	}
	submit, err := lockFile(filepath.Join(filepath.Dir(dir), id+".submit.lock"), false)
	if err != nil {
		return Operation{}, err
	}
	defer unlockFile(submit)
	worker, err := lockFile(filepath.Join(dir, "worker.lock"), true)
	if err != nil {
		return Operation{}, fmt.Errorf("operation observer is active: %w", err)
	}
	op, err := readOperation(dir)
	if err != nil {
		unlockFile(worker)
		return op, err
	}
	if op.CommandStarted || op.State == "passed" {
		unlockFile(worker)
		return op, errors.New("command may have run; reconcile existing Jenkins queue/build and target; refusing duplicate activation")
	}
	if op.State != "failed" {
		unlockFile(worker)
		return op, errors.New("only a recorded pre-command failure can be retried; queued or interrupted operations require reconciliation")
	}
	archive := filepath.Join(dir, fmt.Sprintf("attempt-%d.json", op.Attempt))
	data, err := os.ReadFile(filepath.Join(dir, "operation.json"))
	if err == nil {
		err = os.WriteFile(archive, data, 0600)
	}
	if err != nil {
		unlockFile(worker)
		return op, err
	}
	now := time.Now().UTC()
	op.State = "queued"
	op.Created = now
	op.Deadline = now.Add(maximumDeploymentDuration)
	op.Started = nil
	op.Finished = nil
	op.PID = 0
	op.Error = ""
	op.Recovery = ""
	op.Attempt++
	err = saveOperation(op)
	unlockFile(worker)
	if err != nil {
		return op, err
	}
	return launchOperation(op)
}

// Capture queue/build handles as soon as the adapter emits them, independently
// of later log size or command completion. No credentials are persisted here.
type handleWriter struct {
	mu      sync.Mutex
	writer  io.Writer
	dir     string
	pending string
	handles []string
}

func (w *handleWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.writer.Write(data)
	if err != nil {
		return n, err
	}
	w.pending += string(data)
	for {
		end := strings.IndexByte(w.pending, '\n')
		if end < 0 {
			break
		}
		line := w.pending[:end]
		w.pending = w.pending[end+1:]
		changed := false
		for _, h := range jenkinsHandleRE.FindAllString(line, -1) {
			if !contains(w.handles, h) {
				w.handles = append(w.handles, h)
				changed = true
			}
		}
		if changed {
			encoded, _ := json.Marshal(w.handles)
			file, saveErr := os.CreateTemp(w.dir, ".jenkins-handles-")
			if saveErr != nil {
				return n, saveErr
			}
			name := file.Name()
			_, saveErr = file.Write(encoded)
			if saveErr == nil {
				saveErr = file.Sync()
			}
			closeErr := file.Close()
			if saveErr == nil {
				saveErr = closeErr
			}
			if saveErr == nil {
				saveErr = os.Rename(name, filepath.Join(w.dir, "jenkins-handles.json"))
			}
			_ = os.Remove(name)
			if saveErr != nil {
				return n, saveErr
			}
		}
	}
	if len(w.pending) > 1024*1024 {
		w.pending = w.pending[len(w.pending)-4096:]
	}
	return n, nil
}
