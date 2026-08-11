package skilldoc

import (
	"strings"
	"testing"
)

func TestEmbeddedSkillContract(t *testing.T) {
	for _, want := range []string{"name: deploy-it", "ship-it", ".deploy-it.json", "Never guess", "Do not rerun"} {
		if !strings.Contains(SkillMD, want) {
			t.Fatalf("skill missing %q", want)
		}
	}
	if !strings.Contains(OpenAIYAML, "$deploy-it") {
		t.Fatal("default prompt does not invoke skill")
	}
}
