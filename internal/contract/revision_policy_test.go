package contract

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func policyManifest(policy string) string {
	if policy == "" {
		return manifest
	}
	return strings.Replace(manifest, `"version":1`, `"version":1,"revision_policy":"`+policy+`"`, 1)
}

func revisionRepo(t *testing.T, policy string) (repo, commit, record string) {
	t.Helper()
	repo = newRepo(t, true)
	record = filepath.Join(testDir(t), "deployed")
	t.Setenv("DEPLOY_TEST_RECORD", record)
	write(t, filepath.Join(repo, ManifestName), policyManifest(policy), 0o644)
	write(t, filepath.Join(repo, "deploy.sh"), "#!/bin/sh\nprintf 'original:%s' \"$DEPLOY_IT_COMMIT\" > \"$DEPLOY_TEST_RECORD\"\n", 0o755)
	commitAndPush(t, repo, "deployment contract", "")
	return repo, output(t, repo, "rev-parse", "HEAD"), record
}

func secondClone(t *testing.T, repo string) string {
	t.Helper()
	clone := testDir(t)
	git(t, clone, "clone", "--branch", "main", output(t, repo, "remote", "get-url", "origin"), ".")
	git(t, clone, "config", "user.name", "Second publisher")
	git(t, clone, "config", "user.email", "second@example.com")
	return clone
}

func advanceRemote(t *testing.T, repo string) (clone, tip string) {
	t.Helper()
	clone = secondClone(t, repo)
	write(t, filepath.Join(clone, "seed"), "second publisher advanced main\n", 0o644)
	write(t, filepath.Join(clone, "deploy.sh"), "#!/bin/sh\nexit 29\n", 0o755)
	commitAndPush(t, clone, "second publisher", "")
	return clone, output(t, clone, "rev-parse", "HEAD")
}

func requireNoDeployment(t *testing.T, record string, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("unproven revision was accepted")
	}
	if _, statErr := os.Stat(record); !os.IsNotExist(statErr) {
		t.Fatalf("deployment command ran or record lookup failed: %v", statErr)
	}
}

func TestBranchAncestorExecutesOriginalSnapshotAfterIndependentPush(t *testing.T) {
	repo, commit, record := revisionRepo(t, "branch_ancestor")
	_, tip := advanceRemote(t, repo)
	if got := output(t, repo, "rev-parse", "refs/remotes/origin/main"); got != commit {
		t.Fatalf("test requires stale tracking ref: %s", got)
	}
	fetchHead := filepath.Join(repo, ".git", "FETCH_HEAD")
	write(t, fetchHead, "unrelated retained FETCH_HEAD\n", 0o644)
	runner, log := openRunner(t, repo)
	opts, err := runner.ShippedHEADOptions()
	if err != nil {
		t.Fatal(err)
	}
	if opts.Commit != commit {
		t.Fatalf("options changed immutable commit: %#v", opts)
	}
	if _, err := runner.Run(opts); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(record)
	if err != nil || string(data) != "original:"+commit {
		t.Fatalf("snapshot result=%q error=%v", data, err)
	}
	for _, ref := range []string{"HEAD", "refs/heads/main", "refs/remotes/origin/main"} {
		if got := output(t, repo, "rev-parse", ref); got != commit {
			t.Fatalf("proof changed %s: %s", ref, got)
		}
	}
	data, err = os.ReadFile(fetchHead)
	if err != nil || string(data) != "unrelated retained FETCH_HEAD\n" {
		t.Fatalf("proof changed FETCH_HEAD: %q, %v", data, err)
	}
	refs := strings.Split(output(t, repo, "for-each-ref", "--format=%(refname) %(objectname)", "refs/deploy-it/proof/"), "\n")
	if len(refs) != 2 || refs[0] == refs[1] {
		t.Fatalf("each proof needs a unique retained ref: %q", refs)
	}
	for _, ref := range refs {
		if !strings.HasSuffix(ref, " "+tip) {
			t.Fatalf("proof did not fetch current tip: %s", ref)
		}
	}
	if !strings.Contains(log.String(), tip) || !strings.Contains(log.String(), commit) {
		t.Fatalf("missing proof metadata: %s", log.String())
	}
}

func TestExactTipPoliciesRejectAdvancedRemoteAndWorktreeOptIn(t *testing.T) {
	for _, policy := range []string{"", "exact_tip"} {
		t.Run(policy, func(t *testing.T) {
			repo, commit, record := revisionRepo(t, policy)
			advanceRemote(t, repo)
			// An uncommitted policy change cannot authorize the frozen commit.
			write(t, filepath.Join(repo, ManifestName), policyManifest("branch_ancestor"), 0o644)
			runner, _ := openRunner(t, repo)
			if _, err := runner.HeadOptions(); err != nil {
				t.Fatalf("local metadata resolution should not wait on proof: %v", err)
			}
			_, err := runner.ShippedHEADOptions()
			requireNoDeployment(t, record, err)
			_, err = runner.Run(Options{Commit: commit, Branch: "main", Remote: "origin"})
			requireNoDeployment(t, record, err)
		})
	}
}

func TestBranchAncestorRejectsUnpublishedGraphsAndStaleLocalAuthority(t *testing.T) {
	for _, mode := range []string{"unpublished descendant", "divergent", "unrelated", "deleted branch", "fetch failure"} {
		t.Run(mode, func(t *testing.T) {
			repo, commit, record := revisionRepo(t, "branch_ancestor")
			switch mode {
			case "divergent":
				advanceRemote(t, repo)
				fallthrough
			case "unpublished descendant":
				write(t, filepath.Join(repo, "seed"), "local unpushed content", 0o644)
				git(t, repo, "add", "seed")
				git(t, repo, "commit", "-m", "local only")
				commit = output(t, repo, "rev-parse", "HEAD")
			case "unrelated":
				commit = output(t, repo, "commit-tree", output(t, repo, "rev-parse", commit+"^{tree}"), "-m", "unrelated root")
			case "deleted branch":
				git(t, output(t, repo, "remote", "get-url", "origin"), "update-ref", "-d", "refs/heads/main")
			case "fetch failure":
				git(t, repo, "remote", "set-url", "origin", filepath.Join(testDir(t), "missing-remote"))
			}
			// Neither a fabricated tracking ref nor a matching FETCH_HEAD may
			// authorize a commit absent from the freshly fetched remote branch.
			git(t, repo, "update-ref", "refs/remotes/origin/main", commit)
			write(t, filepath.Join(repo, ".git", "FETCH_HEAD"), commit+"\t\tbranch 'main'\n", 0o644)
			runner, _ := openRunner(t, repo)
			_, err := runner.Run(Options{Commit: commit, Branch: "main", Remote: "origin"})
			requireNoDeployment(t, record, err)
		})
	}
}

func TestBranchAncestorKeepsExactTagBinding(t *testing.T) {
	repo, commit, record := revisionRepo(t, "branch_ancestor")
	clone, _ := advanceRemote(t, repo)
	git(t, clone, "tag", "wrong-revision")
	git(t, clone, "push", "origin", "refs/tags/wrong-revision")
	runner, _ := openRunner(t, repo)
	_, err := runner.Run(Options{Commit: commit, Branch: "main", Remote: "origin", Tag: "wrong-revision"})
	requireNoDeployment(t, record, err)
	if !strings.Contains(err.Error(), "pushed remote tag") {
		t.Fatalf("wrong tag error: %v", err)
	}
}

func TestRevisionPolicyClosedEnumAndMissingManifestMetadata(t *testing.T) {
	for _, policy := range []string{"", "exact_tip", "branch_ancestor"} {
		if _, err := decodeManifest([]byte(policyManifest(policy))); err != nil {
			t.Fatalf("valid policy %q rejected: %v", policy, err)
		}
	}
	for _, policy := range []string{"ancestor", "branch_ancestor ", "EXACT_TIP"} {
		if _, err := decodeManifest([]byte(policyManifest(policy))); err == nil {
			t.Fatalf("unknown policy %q accepted", policy)
		}
	}
	repo, commit, record := revisionRepo(t, "unknown")
	runner, _ := openRunner(t, repo)
	_, err := runner.Run(Options{Commit: commit, Branch: "main", Remote: "origin"})
	requireNoDeployment(t, record, err)
	if _, err := runner.ShippedHEADOptions(); err == nil {
		t.Fatal("HEAD proof ignored invalid committed policy")
	}
	if _, err := runner.HeadOptions(); err != nil {
		t.Fatalf("non-proving metadata resolution changed: %v", err)
	}
	missing := newRepo(t, true)
	runner, _ = openRunner(t, missing)
	if _, err := runner.ShippedHEADOptions(); err != nil {
		t.Fatalf("missing manifest changed strict metadata resolution: %v", err)
	}
	advanceRemote(t, missing)
	if _, err := runner.ShippedHEADOptions(); err == nil {
		t.Fatal("missing committed manifest permitted stale HEAD")
	}
}

func TestHistoryOverlaysCannotForgeBranchAncestry(t *testing.T) {
	for _, overlay := range []string{"replace", "legacy graft", "environment graft"} {
		t.Run(overlay, func(t *testing.T) {
			repo, tip, record := revisionRepo(t, "branch_ancestor")
			tree := output(t, repo, "rev-parse", tip+"^{tree}")
			unrelated := output(t, repo, "commit-tree", tree, "-m", "unrelated root")
			if overlay == "replace" {
				forged := output(t, repo, "commit-tree", tree, "-p", unrelated, "-m", "forged parent")
				git(t, repo, "replace", tip, forged)
			} else {
				graft := filepath.Join(repo, ".git", "info", "grafts")
				if overlay == "environment graft" {
					graft = filepath.Join(testDir(t), "custom-grafts")
					t.Setenv("GIT_GRAFT_FILE", graft)
				}
				write(t, graft, tip+" "+unrelated+"\n", 0o644)
			}
			// Confirm this overlay would actually deceive an unprotected Git
			// merge-base call; this is not merely an unused attack fixture.
			git(t, repo, "merge-base", "--is-ancestor", unrelated, tip)
			runner, _ := openRunner(t, repo)
			_, err := runner.Run(Options{Commit: unrelated, Branch: "main", Remote: "origin"})
			requireNoDeployment(t, record, err)
		})
	}
}

func TestReplacementCannotChangeSnapshotPolicyOrExecutable(t *testing.T) {
	for _, policy := range []string{"exact_tip", "branch_ancestor"} {
		t.Run(policy, func(t *testing.T) {
			repo, commit, record := revisionRepo(t, policy)
			advanceRemote(t, repo)
			write(t, filepath.Join(repo, ManifestName), policyManifest("branch_ancestor"), 0o644)
			write(t, filepath.Join(repo, "deploy.sh"), "#!/bin/sh\nprintf forged > \"$DEPLOY_TEST_RECORD\"\n", 0o755)
			git(t, repo, "add", ManifestName, "deploy.sh")
			forged := output(t, repo, "commit-tree", output(t, repo, "write-tree"), "-m", "forged deployment snapshot")
			git(t, repo, "replace", commit, forged)
			runner, _ := openRunner(t, repo)
			_, err := runner.Run(Options{Commit: commit, Branch: "main", Remote: "origin"})
			if policy == "exact_tip" {
				requireNoDeployment(t, record, err)
				if _, err := runner.ShippedHEADOptions(); err == nil {
					t.Fatal("replacement manifest relaxed HEAD policy")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(record)
				if err != nil || string(data) != "original:"+commit {
					t.Fatalf("replacement executable entered snapshot: %q, %v", data, err)
				}
			}
		})
	}
}

func TestBranchFetchDeadlineFailsClosed(t *testing.T) {
	repo, commit, record := revisionRepo(t, "branch_ancestor")
	runner, _ := openRunner(t, repo)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := testDir(t)
	started := filepath.Join(bin, "fetch-started")
	write(t, filepath.Join(bin, "git"), "#!/bin/sh\nfor arg do\n  if [ \"$arg\" = fetch ]; then\n    printf started > '"+started+"'\n    sleep 10\n  fi\ndone\nexec '"+realGit+"' \"$@\"\n", 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	runner.budget, err = newDeploymentBudget(time.Now(), 600*time.Millisecond, 25*time.Millisecond, "")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = runner.Run(Options{Commit: commit, Branch: "main", Remote: "origin"})
	requireNoDeployment(t, record, err)
	if !errors.Is(err, errDeploymentDeadline) || time.Since(start) > 2*time.Second {
		t.Fatalf("fetch did not fail within common deadline: %v (%s)", err, time.Since(start))
	}
	if _, err := os.Stat(started); err != nil {
		t.Fatalf("test did not reach blocked fetch: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runner.proveRemoteRevision(ctx, "origin", "main", "", commit, "branch_ancestor"); !errors.Is(err, errDeploymentDeadline) {
		t.Fatalf("expired proof context accepted: %v", err)
	}
}
