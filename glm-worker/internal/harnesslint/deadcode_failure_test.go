package harnesslint

import (
	"strings"
	"testing"
)

func TestDeadcodeCommandFailureStaysLintViolation(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "go.mod", "module example.com/deadcodefailure\n")
	runner := &deadcodeFixtureRunner{outputs: map[string]commandResult{
		"linux": {exitCode: 2, output: "load failed"},
	}}

	violations, err := runDeadcodeModuleChecks(root, "", runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 {
		t.Fatalf("command failure must produce one lint violation: %+v", violations)
	}
	violation := violations[0]
	if violation.Rule != deadcodeToolName || violation.Path != "go.mod" || !strings.Contains(violation.Message, "deadcode linux/amd64 failed: load failed") {
		t.Fatalf("unexpected deadcode command failure violation: %+v", violation)
	}
}
