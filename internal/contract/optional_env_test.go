package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOptionalEnvironmentFromShippedContract(t *testing.T) {
	repo := newRepo(t, true)
	record := filepath.Join(testDir(t), "record")
	t.Setenv("DEPLOY_TEST_RECORD", record)
	t.Setenv("DEPLOY_TEST_OPTIONAL_VALUE", "present")
	t.Setenv("DEPLOY_TEST_OPTIONAL_EMPTY", "")
	t.Setenv("UNDECLARED_SECRET", "must-not-leak")
	t.Setenv("DEPLOY_TEST_OPTIONAL_ABSENT", "restored by cleanup")
	if err := os.Unsetenv("DEPLOY_TEST_OPTIONAL_ABSENT"); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(repo, ManifestName), `{"version":1,"after_ship":true,"environment":"production","command":["./deploy.sh"],"env":["DEPLOY_TEST_RECORD"],"optional_env":["DEPLOY_TEST_OPTIONAL_VALUE","DEPLOY_TEST_OPTIONAL_EMPTY","DEPLOY_TEST_OPTIONAL_ABSENT"],"timeout_seconds":30}`, 0o644)
	write(t, filepath.Join(repo, "deploy.sh"), `#!/bin/sh
set -eu
[ "$DEPLOY_TEST_OPTIONAL_VALUE" = present ]
[ "${DEPLOY_TEST_OPTIONAL_EMPTY+x}" = x ]
[ "$DEPLOY_TEST_OPTIONAL_EMPTY" = '' ]
[ "${DEPLOY_TEST_OPTIONAL_ABSENT+x}" = '' ]
[ "${UNDECLARED_SECRET+x}" = '' ]
printf accepted > "$DEPLOY_TEST_RECORD"
`, 0o755)
	commitAndPush(t, repo, "optional environment contract", "")
	runner, _ := openRunner(t, repo)
	opts, err := runner.ShippedHEADOptions()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(opts); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(record)
	if err != nil || string(data) != "accepted" {
		t.Fatalf("record=%q err=%v", data, err)
	}
	if err := os.Unsetenv("DEPLOY_TEST_RECORD"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(opts); err == nil || !strings.Contains(err.Error(), "required deployment environment variable DEPLOY_TEST_RECORD is not set") {
		t.Fatalf("missing required variable error=%v", err)
	}
}

func TestOptionalEnvironmentValidation(t *testing.T) {
	for _, names := range [][]string{
		{"BAD-NAME"}, {"DEPLOY_IT_COMMIT"}, {"DEPLOY_TEST_RECORD"}, {"OPTIONAL", "OPTIONAL"},
	} {
		t.Run(strings.Join(names, ","), func(t *testing.T) {
			var value Manifest
			if err := json.Unmarshal([]byte(manifest), &value); err != nil {
				t.Fatal(err)
			}
			value.OptionalEnv = names
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeManifest(data); err == nil {
				t.Fatalf("accepted optional environment %q", names)
			}
		})
	}
}
