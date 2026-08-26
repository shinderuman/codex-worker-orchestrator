package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestManagedRulesAllowOnlyQualityGateEntrypoint(t *testing.T) {
	codexBin, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("codex CLIがないためexecpolicy検証を省略します")
	}
	root := scenarioRepoRoot(t)
	rulesDir := filepath.Join(root, "codex", "rules")
	entries, err := filepath.Glob(filepath.Join(rulesDir, "*.rules"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatalf("管理rules fileがありません: %s", rulesDir)
	}
	sort.Strings(entries)

	cases := []struct {
		argv      []string
		wantAllow bool
	}{
		{argv: []string{"glm-worker", "--quality-gate", "go-test"}, wantAllow: true},
		{argv: []string{"glm-worker", "--quality-gate", "go-test-race"}, wantAllow: true},
		{argv: []string{"go", "test", "./..."}, wantAllow: false},
		{argv: []string{"go", "test", "./...", "-exec", "/bin/sh"}, wantAllow: false},
		{argv: []string{"go", "test", "-race", "./...", "-exec", "/bin/sh"}, wantAllow: false},
		{argv: []string{"go", "test", "-exec", "/bin/sh", "./..."}, wantAllow: false},
	}

	for _, tc := range cases {
		allowed := false
		for _, rulesFile := range entries {
			out, err := exec.Command(codexBin, append([]string{"execpolicy", "check", "--rules", rulesFile, "--"}, tc.argv...)...).Output()
			if err != nil {
				t.Fatalf("codex execpolicy check(%v, %s)が失敗しました: %v", tc.argv, rulesFile, err)
			}
			var decision struct {
				Decision string `json:"decision"`
			}
			if err := json.Unmarshal(out, &decision); err != nil {
				t.Fatalf("codex execpolicy check(%v, %s)の出力がJSONではありません: %v: %s", tc.argv, rulesFile, err, out)
			}
			if decision.Decision == "allow" {
				allowed = true
			}
		}
		if allowed != tc.wantAllow {
			t.Fatalf("管理rulesの判定が想定と異なります: argv=%v wantAllow=%v gotAllow=%v", tc.argv, tc.wantAllow, allowed)
		}
	}
}

func TestManagedRulesDirectoryHasNoDirectGoTestRule(t *testing.T) {
	root := scenarioRepoRoot(t)
	entries, err := filepath.Glob(filepath.Join(root, "codex", "rules", "*.rules"))
	if err != nil {
		t.Fatal(err)
	}
	for _, rulesFile := range entries {
		data, err := os.ReadFile(rulesFile)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `"go", "test"`) {
			t.Fatalf("管理rules fileが直接go testをallowしています: %s", rulesFile)
		}
	}
}
