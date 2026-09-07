package install

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillsOnlyInstallWritesBothSkills(t *testing.T) {
	home := testDir(t)
	t.Setenv("HOME", home)
	var out bytes.Buffer
	if err := Local(false, &out); err != nil {
		t.Fatal(err)
	}
	for _, agent := range []string{".codex", ".claude"} {
		data, err := os.ReadFile(filepath.Join(home, agent, "skills", "deploy-it", "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "name: deploy-it") {
			t.Fatalf("bad skill: %s", data)
		}
	}
}

// Keep fixtures when the host permits creating files but denies directory removal.
func testDir(t *testing.T) string {
	t.Helper()
	if os.Getenv("DEPLOY_IT_KEEP_TEST_DIRS") == "" {
		return t.TempDir()
	}
	dir, err := os.MkdirTemp("", "deploy-it-test-")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
