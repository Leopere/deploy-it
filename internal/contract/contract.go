package contract

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const ManifestName = ".deploy-it.json"

var (
	environmentRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	envNameRE     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	remoteRE      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
)

type Manifest struct {
	Version        int      `json:"version"`
	AfterShip      bool     `json:"after_ship"`
	Environment    string   `json:"environment"`
	Command        []string `json:"command"`
	Env            []string `json:"env,omitempty"`
	OptionalEnv    []string `json:"optional_env,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

type Options struct {
	Commit string
	Branch string
	Tag    string
	Remote string
	Check  bool
}

type Result struct {
	Skipped     bool
	Environment string
	Elapsed     time.Duration
}

type Runner struct {
	Root   string
	Out    io.Writer
	Err    io.Writer
	budget deploymentBudget
}

func Open(cwd string, out, errOut io.Writer) (*Runner, error) {
	budget, err := currentDeploymentBudget()
	if err != nil {
		return nil, err
	}
	ctx, cancel := budget.context()
	defer cancel()
	root, err := gitOutput(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		if errors.Is(err, errDeploymentDeadline) {
			return nil, err
		}
		return nil, errors.New("not inside a Git repository")
	}
	return &Runner{Root: strings.TrimSpace(root), Out: out, Err: errOut, budget: budget}, nil
}

func (r *Runner) Run(opts Options) (Result, error) {
	ctx, cancel := r.budget.context()
	defer cancel()
	commit, err := r.resolveCommit(ctx, opts.Commit, !opts.Check)
	if err != nil {
		return Result{}, err
	}
	present, err := r.pathAtCommit(ctx, commit, ManifestName)
	if err != nil {
		return Result{}, err
	}
	if !present {
		return Result{}, fmt.Errorf("deployment requires %s in shipped commit", ManifestName)
	}

	snapshot, cleanup, err := materialize(ctx, r.Root, commit, r.budget.hardMS)
	if err != nil {
		return Result{}, err
	}
	defer func() {
		// Do not start potentially slow filesystem cleanup after the hard
		// command budget has expired. The private temporary directory contains
		// only the immutable archive and is left for the OS cleaner in that case.
		if ctx.Err() == nil {
			cleanup()
		}
	}()
	_, manifest, executable, err := loadContract(snapshot)
	if err != nil {
		return Result{}, err
	}
	if err := deadlineError(ctx); err != nil {
		return Result{}, err
	}
	if opts.Check {
		fmt.Fprintf(r.Out, "Deployment contract valid for %s at %s.\n", manifest.Environment, short(commit))
		return Result{Environment: manifest.Environment}, nil
	}
	if err := r.proveRemoteRevision(ctx, opts.Remote, opts.Branch, opts.Tag, commit); err != nil {
		return Result{}, err
	}
	environment, err := commandEnvironment(manifest, commit, opts, r.budget.hardMS)
	if err != nil {
		return Result{}, err
	}

	started := time.Now()
	fmt.Fprintf(r.Out, "Deploying %s at %s.\n", manifest.Environment, short(commit))
	commandDeadline := r.budget.executes
	manifestDeadline := time.Now().Add(time.Duration(manifest.TimeoutSeconds) * time.Second)
	if manifestDeadline.Before(commandDeadline) {
		commandDeadline = manifestDeadline
	}
	commandCtx, commandCancel := context.WithDeadline(ctx, commandDeadline)
	defer commandCancel()
	cmd := newBoundedCommand(commandCtx, executable, manifest.Command[1:]...)
	cmd.Dir = snapshot
	cmd.Stdout = r.Out
	cmd.Stderr = r.Err
	cmd.Env = environment
	err = cmd.Run()
	elapsed := time.Since(started)
	if commandCtx.Err() == context.DeadlineExceeded {
		if deadlineError(ctx) != nil {
			return Result{}, fmt.Errorf("deployment command exceeded the total deploy-it deadline")
		}
		return Result{}, fmt.Errorf("deployment command exceeded its %s timeout", time.Duration(manifest.TimeoutSeconds)*time.Second)
	}
	if err != nil {
		return Result{}, fmt.Errorf("deployment command failed after %s: %w", elapsed.Round(time.Millisecond), err)
	}
	fmt.Fprintf(r.Out, "Deployed %s at %s in %s.\n", manifest.Environment, short(commit), elapsed.Round(time.Millisecond))
	return Result{Environment: manifest.Environment, Elapsed: elapsed}, nil
}

// ShippedHEADOptions resolves the only safe implicit deployment target: a clean
// checked-out branch whose HEAD is exactly present at its configured push
// destination. Tags are optional for explicit calls. Run proves the remote refs
// immediately before executing the contract.
func (r *Runner) ShippedHEADOptions() (Options, error) {
	ctx, cancel := r.budget.context()
	defer cancel()
	status, err := gitOutput(ctx, r.Root, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return Options{}, err
	}
	if strings.TrimSpace(status) != "" {
		return Options{}, errors.New("implicit deployment requires a clean worktree")
	}

	commit, err := r.resolveCommit(ctx, "", false)
	if err != nil {
		return Options{}, err
	}
	branch, err := gitOutput(ctx, r.Root, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || strings.TrimSpace(branch) == "" {
		return Options{}, errors.New("implicit deployment requires HEAD checked out on a branch")
	}
	branch = strings.TrimSpace(branch)
	destination, err := gitOutput(ctx, r.Root, "for-each-ref", "--format=%(push:remotename)%00%(push:remoteref)%00%(upstream:remotename)%00%(upstream:remoteref)", "refs/heads/"+branch)
	if err != nil {
		return Options{}, err
	}
	parts := strings.Split(strings.TrimSpace(destination), "\x00")
	if len(parts) == 4 && parts[1] == "" {
		// Git leaves push:remoteref empty without an explicit push refspec.
		mode, modeErr := gitOutput(ctx, r.Root, "config", "--get", "push.default")
		if modeErr != nil {
			mode = "simple"
		}
		switch strings.TrimSpace(mode) {
		case "current", "matching":
			parts[1] = "refs/heads/" + branch
		case "upstream", "tracking":
			if parts[0] == parts[2] {
				parts[1] = parts[3]
			}
		case "simple", "":
			if parts[0] != parts[2] || parts[3] == "refs/heads/"+branch {
				parts[1] = "refs/heads/" + branch
			}
		}
	}
	if len(parts) != 4 || !remoteRE.MatchString(parts[0]) || !strings.HasPrefix(parts[1], "refs/heads/") {
		return Options{}, errors.New("implicit deployment requires a configured branch push destination")
	}
	opts := Options{Commit: commit, Branch: strings.TrimPrefix(parts[1], "refs/heads/"), Remote: parts[0]}
	if err := r.proveRemoteRevision(ctx, opts.Remote, opts.Branch, "", commit); err != nil {
		return Options{}, err
	}
	return opts, nil
}

func loadContract(root string) ([]byte, Manifest, string, error) {
	manifestPath := filepath.Join(root, ManifestName)
	info, err := os.Lstat(manifestPath)
	if err != nil {
		return nil, Manifest{}, "", fmt.Errorf("manifest: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, Manifest{}, "", errors.New("manifest must be a regular file, not a symlink")
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, Manifest{}, "", err
	}
	manifest, err := decodeManifest(data)
	if err != nil {
		return nil, Manifest{}, "", err
	}
	executable, err := validateExecutable(root, manifest.Command[0])
	if err != nil {
		return nil, Manifest{}, "", err
	}
	return data, manifest, executable, nil
}

func decodeManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("invalid %s: %w", ManifestName, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Manifest{}, fmt.Errorf("invalid %s: multiple JSON values", ManifestName)
	}
	if manifest.Version != 1 {
		return Manifest{}, fmt.Errorf("invalid %s: version must be 1", ManifestName)
	}
	if !manifest.AfterShip {
		return Manifest{}, fmt.Errorf("invalid %s: after_ship must be true", ManifestName)
	}
	if !environmentRE.MatchString(manifest.Environment) {
		return Manifest{}, fmt.Errorf("invalid %s: environment is required and must be a simple name", ManifestName)
	}
	if len(manifest.Command) == 0 || strings.TrimSpace(manifest.Command[0]) == "" {
		return Manifest{}, fmt.Errorf("invalid %s: command must not be empty", ManifestName)
	}
	for _, arg := range manifest.Command {
		if strings.ContainsRune(arg, 0) {
			return Manifest{}, fmt.Errorf("invalid %s: command contains NUL", ManifestName)
		}
	}
	if manifest.TimeoutSeconds < 1 || manifest.TimeoutSeconds > int(maximumDeploymentDuration/time.Second) {
		return Manifest{}, fmt.Errorf("invalid %s: timeout_seconds must be between 1 and %d", ManifestName, int(maximumDeploymentDuration/time.Second))
	}
	seen := map[string]bool{}
	for _, name := range append(append([]string{}, manifest.Env...), manifest.OptionalEnv...) {
		if !envNameRE.MatchString(name) || strings.HasPrefix(name, "DEPLOY_IT_") {
			return Manifest{}, fmt.Errorf("invalid %s: invalid environment variable %q", ManifestName, name)
		}
		if seen[name] {
			return Manifest{}, fmt.Errorf("invalid %s: duplicate environment variable %q", ManifestName, name)
		}
		seen[name] = true
	}
	return manifest, nil
}

func validateExecutable(root, command string) (string, error) {
	if !strings.HasPrefix(command, "./") {
		return "", errors.New("deployment command must start with ./ and name a repo-local executable")
	}
	relative := filepath.Clean(strings.TrimPrefix(command, "./"))
	if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", errors.New("deployment command escapes the repository")
	}
	if relative == "ship.sh" || relative == "ship-it" {
		return "", errors.New("deployment command must not invoke ship-it or its ship.sh wrapper")
	}
	path := filepath.Join(root, relative)
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return "", fmt.Errorf("deployment executable: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("deployment executable path must not contain symlinks")
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("deployment executable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("deployment executable must be a regular file")
	}
	if info.Mode().Perm()&0o111 == 0 {
		return "", errors.New("deployment executable is not executable")
	}
	return path, nil
}

func (r *Runner) resolveCommit(ctx context.Context, explicit string, required bool) (string, error) {
	commit := strings.TrimSpace(explicit)
	if commit == "" && required {
		return "", errors.New("deployment requires an explicit full commit ID from ship-it")
	}
	if commit == "" {
		var err error
		commit, err = gitOutput(ctx, r.Root, "rev-parse", "HEAD")
		if err != nil {
			return "", err
		}
	}
	commit = strings.ToLower(strings.TrimSpace(commit))
	decoded, err := hex.DecodeString(commit)
	if err != nil || (len(decoded) != 20 && len(decoded) != 32) {
		return "", errors.New("commit must be a full hexadecimal Git object ID")
	}
	if _, err := gitOutput(ctx, r.Root, "cat-file", "-e", commit+"^{commit}"); err != nil {
		return "", errors.New("commit does not exist in this repository")
	}
	return commit, nil
}

func (r *Runner) pathAtCommit(ctx context.Context, commit, path string) (bool, error) {
	out, err := gitOutput(ctx, r.Root, "ls-tree", "--name-only", commit, "--", path)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == path, nil
}

func (r *Runner) proveRemoteRevision(ctx context.Context, remote, branch, tag, commit string) error {
	if err := deadlineError(ctx); err != nil {
		return err
	}
	if !remoteRE.MatchString(remote) {
		return errors.New("remote must be a simple configured Git remote name")
	}
	if strings.TrimSpace(branch) == "" || strings.HasPrefix(branch, "-") {
		return errors.New("deployment requires the shipped branch")
	}
	if _, err := gitOutput(ctx, r.Root, "check-ref-format", "--branch", branch); err != nil {
		return errors.New("invalid shipped branch")
	}
	branchRef := "refs/heads/" + branch
	branchOut, err := gitOutput(ctx, r.Root, "ls-remote", "--exit-code", remote, branchRef)
	if err != nil {
		if deadlineError(ctx) != nil {
			return errDeploymentDeadline
		}
		return errors.New("deployment commit is not the exact commit on the pushed remote branch")
	}
	if !remoteOutputHas(branchOut, branchRef, commit) {
		return errors.New("deployment commit is not the exact commit on the pushed remote branch")
	}
	if tag == "" {
		return nil
	}
	tagRef := "refs/tags/" + tag
	if _, err := gitOutput(ctx, r.Root, "check-ref-format", tagRef); err != nil {
		return errors.New("invalid shipped tag")
	}
	tagOut, err := gitOutput(ctx, r.Root, "ls-remote", "--exit-code", remote, tagRef)
	if err != nil {
		if deadlineError(ctx) != nil {
			return errDeploymentDeadline
		}
		return errors.New("deployment commit is not the exact commit at the pushed remote tag")
	}
	if !remoteOutputHas(tagOut, tagRef, commit) {
		return errors.New("deployment commit is not the exact commit at the pushed remote tag")
	}
	return nil
}

func remoteOutputHas(output, ref, commit string) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.ToLower(fields[0]) == commit && fields[1] == ref {
			return true
		}
	}
	return false
}

func commandEnvironment(manifest Manifest, commit string, opts Options, hardDeadlineMS string) ([]string, error) {
	allowed := []string{"PATH", "TMPDIR", "LANG", "LC_ALL", "TZ", "SSL_CERT_FILE", "SSL_CERT_DIR"}
	allowed = append(allowed, manifest.Env...)
	allowed = append(allowed, manifest.OptionalEnv...)
	environment := make([]string, 0, len(allowed)+5)
	seen := map[string]bool{}
	for _, name := range allowed {
		if seen[name] {
			continue
		}
		seen[name] = true
		value, ok := os.LookupEnv(name)
		if !ok {
			if contains(manifest.Env, name) {
				return nil, fmt.Errorf("required deployment environment variable %s is not set", name)
			}
			continue
		}
		environment = append(environment, name+"="+value)
	}
	environment = append(environment,
		"DEPLOY_IT_COMMIT="+commit,
		"DEPLOY_IT_BRANCH="+opts.Branch,
		"DEPLOY_IT_TAG="+opts.Tag,
		"DEPLOY_IT_ENVIRONMENT="+manifest.Environment,
		"SHIP_IT_DEADLINE_UNIX_MS="+hardDeadlineMS,
	)
	return environment, nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func gitOutput(ctx context.Context, cwd string, args ...string) (string, error) {
	if err := deadlineError(ctx); err != nil {
		return "", err
	}
	cmd := newBoundedCommand(ctx, "git", append([]string{"-C", cwd}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if err := cmd.Run(); err != nil {
		if deadlineError(ctx) != nil {
			return stdout.String(), errDeploymentDeadline
		}
		return stdout.String(), fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(stderr.String()), err)
	}
	return stdout.String(), nil
}

func short(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}
