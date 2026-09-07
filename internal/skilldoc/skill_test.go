package skilldoc

import (
	"strings"
	"testing"
)

func TestEmbeddedSkillContract(t *testing.T) {
	for _, want := range []string{"name: deploy-it", "ship-it", ".deploy-it.json", "Never guess", "After diagnosis, rerun", "needs no release tag", "A failed attempt alone"} {
		if !strings.Contains(SkillMD, want) {
			t.Fatalf("skill missing %q", want)
		}
	}
	if !strings.Contains(OpenAIYAML, "$deploy-it") {
		t.Fatal("default prompt does not invoke skill")
	}
	if strings.Contains(SkillMD, "deploy-it trust") || !strings.Contains(SkillMD, "no machine-local trust gate") {
		t.Fatal("embedded skill still requires local trust")
	}
}
