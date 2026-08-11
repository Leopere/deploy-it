package install

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillsOnlyInstallWritesBothSkills(t *testing.T) {
	home := t.TempDir()
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
