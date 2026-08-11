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
	"os/exec"
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
	Root string
	Out  io.Writer
	Err  io.Writer
}

func Open(cwd string, out, errOut io.Writer) (*Runner, error) {
	root, err := gitOutput(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, errors.New("not inside a Git repository")
	}
	return &Runner{Root: strings.TrimSpace(root), Out: out, Err: errOut}, nil
}

func (r *Runner) Run(opts Options) (Result, error) {
	commit, err := r.resolveCommit(opts.Commit, !opts.Check)
	if err != nil {
		return Result{}, err
	}
	present, err := r.pathAtCommit(commit, ManifestName)
	if err != nil {
		return Result{}, err
	}
	if !present {
		return Result{Skipped: true}, nil
	}

	snapshot, cleanup, err := materialize(r.Root, commit)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()
	manifestData, manifest, executable, err := loadContract(snapshot)
	if err != nil {
		return Result{}, err
	}
	identity, err := r.repositoryIdentity(opts.Remote)
	if err != nil {
		return Result{}, err
	}
	if err := verifyTrust(identity, manifestData, executable); err != nil {
		return Result{}, err
	}
	if opts.Check {
		fmt.Fprintf(r.Out, "Deployment contract trusted and valid for %s at %s.\n", manifest.Environment, short(commit))
		return Result{Environment: manifest.Environment}, nil
	}
	if err := r.proveRemoteRevision(opts.Remote, opts.Branch, opts.Tag, commit); err != nil {
		return Result{}, err
	}
	environment, err := commandEnvironment(manifest, commit, opts)
	if err != nil {
		return Result{}, err
	}

	started := time.Now()
	fmt.Fprintf(r.Out, "Deploying %s at %s.\n", manifest.Environment, short(commit))
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(manifest.TimeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, manifest.Command[1:]...)
	cmd.Dir = snapshot
	cmd.Stdout = r.Out
	cmd.Stderr = r.Err
	cmd.Env = environment
	err = cmd.Run()
	elapsed := time.Since(started)
	if ctx.Err() == context.DeadlineExceeded {
		return Result{}, fmt.Errorf("deployment command exceeded its %s timeout", time.Duration(manifest.TimeoutSeconds)*time.Second)
	}
	if err != nil {
		return Result{}, fmt.Errorf("deployment command failed after %s: %w", elapsed.Round(time.Millisecond), err)
	}
	fmt.Fprintf(r.Out, "Deployed %s at %s in %s.\n", manifest.Environment, short(commit), elapsed.Round(time.Millisecond))
	return Result{Environment: manifest.Environment, Elapsed: elapsed}, nil
}

func (r *Runner) Trust(remote string) error {
	manifestData, manifest, executable, err := loadContract(r.Root)
	if err != nil {
		return err
	}
	identity, err := r.repositoryIdentity(remote)
	if err != nil {
		return err
	}
	if err := saveTrust(identity, manifestData, executable); err != nil {
		return err
	}
	fmt.Fprintf(r.Out, "Trusted %s deployment contract for %s.\n", manifest.Environment, identity.Root)
	return nil
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
	if manifest.TimeoutSeconds < 1 || manifest.TimeoutSeconds > 3600 {
		return Manifest{}, fmt.Errorf("invalid %s: timeout_seconds must be between 1 and 3600", ManifestName)
	}
	seen := map[string]bool{}
	for _, name := range manifest.Env {
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

func (r *Runner) resolveCommit(explicit string, required bool) (string, error) {
	commit := strings.TrimSpace(explicit)
	if commit == "" && required {
		return "", errors.New("deployment requires an explicit full commit ID from ship-it")
	}
	if commit == "" {
		var err error
		commit, err = gitOutput(r.Root, "rev-parse", "HEAD")
		if err != nil {
			return "", err
		}
	}
	commit = strings.ToLower(strings.TrimSpace(commit))
	decoded, err := hex.DecodeString(commit)
	if err != nil || (len(decoded) != 20 && len(decoded) != 32) {
		return "", errors.New("commit must be a full hexadecimal Git object ID")
	}
	if _, err := gitOutput(r.Root, "cat-file", "-e", commit+"^{commit}"); err != nil {
		return "", errors.New("commit does not exist in this repository")
	}
	return commit, nil
}

func (r *Runner) pathAtCommit(commit, path string) (bool, error) {
	out, err := gitOutput(r.Root, "ls-tree", "--name-only", commit, "--", path)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == path, nil
}

func (r *Runner) repositoryIdentity(remote string) (repositoryIdentity, error) {
	if !remoteRE.MatchString(remote) {
		return repositoryIdentity{}, errors.New("remote must be a simple configured Git remote name")
	}
	url, err := gitOutput(r.Root, "remote", "get-url", remote)
	if err != nil {
		return repositoryIdentity{}, fmt.Errorf("resolve remote %q: %w", remote, err)
	}
	root, err := filepath.EvalSymlinks(r.Root)
	if err != nil {
		return repositoryIdentity{}, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return repositoryIdentity{}, err
	}
	return repositoryIdentity{Root: root, RemoteURL: strings.TrimSpace(url)}, nil
}

func (r *Runner) proveRemoteRevision(remote, branch, tag, commit string) error {
	if !remoteRE.MatchString(remote) {
		return errors.New("remote must be a simple configured Git remote name")
	}
	if strings.TrimSpace(branch) == "" || strings.HasPrefix(branch, "-") {
		return errors.New("deployment requires the shipped branch")
	}
	if _, err := gitOutput(r.Root, "check-ref-format", "--branch", branch); err != nil {
		return errors.New("invalid shipped branch")
	}
	branchRef := "refs/heads/" + branch
	branchOut, err := gitOutput(r.Root, "ls-remote", "--exit-code", remote, branchRef)
	if err != nil || !remoteOutputHas(branchOut, branchRef, commit) {
		return errors.New("deployment commit is not the exact commit on the pushed remote branch")
	}
	if tag == "" {
		return nil
	}
	tagRef := "refs/tags/" + tag
	if _, err := gitOutput(r.Root, "check-ref-format", tagRef); err != nil {
		return errors.New("invalid shipped tag")
	}
	tagOut, err := gitOutput(r.Root, "ls-remote", "--exit-code", remote, tagRef)
	if err != nil || !remoteOutputHas(tagOut, tagRef, commit) {
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

func commandEnvironment(manifest Manifest, commit string, opts Options) ([]string, error) {
	allowed := []string{"PATH", "TMPDIR", "LANG", "LC_ALL", "TZ", "SSL_CERT_FILE", "SSL_CERT_DIR"}
	environment := make([]string, 0, len(allowed)+len(manifest.Env)+4)
	seen := map[string]bool{}
	for _, name := range append(allowed, manifest.Env...) {
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

func gitOutput(cwd string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if err := cmd.Run(); err != nil {
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
