package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanFinalHeadTaskPathValidatorMatchesRuntime(t *testing.T) {
	root := scenarioRepoRoot(t)
	installerBytes, err := os.ReadFile(filepath.Join(root, "install.sh"))
	if err != nil {
		t.Fatalf("read install.sh: %v", err)
	}
	installer := string(installerBytes)
	const marker = "validate_plan_task_path() {"
	start := strings.Index(installer, marker)
	if start < 0 {
		t.Fatalf("install.sh lacks validate_plan_task_path definition")
	}
	bodyEnd := strings.Index(installer[start:], "\n}\n")
	if bodyEnd < 0 {
		t.Fatalf("install.sh validate_plan_task_path body is not terminated")
	}
	script := installer[start:start+bodyEnd+2] + "\nvalidate_plan_task_path \"$1\"\n"

	candidates := []struct {
		path string
		note string
	}{
		{"IMPLEMENTATION_TASKS/002-plan-final-head-postcondition.md", "標準形"},
		{"IMPLEMENTATION_TASKS/sub/deep/task.md", "subdirectory許容"},
		{"IMPLEMENTATION_TASKS/.hidden/task.md", "dotfile segment許容"},
		{"IMPLEMENTATION_TASKS/.md", "prefix/suffixだけの最小形"},
		{"IMPLEMENTATION_TASKS/..md", "`.`/`..`でないdot segment"},
		{"tasks/task.md", "prefix違反"},
		{"implementation_tasks/task.md", "prefix大文字小文字"},
		{"IMPLEMENTATION_TASKS/task.txt", "suffix違反"},
		{"IMPLEMENTATION_TASKS", "directoryではなくfile契約"},
		{"IMPLEMENTATION_TASKS/", "空rest"},
		{"/IMPLEMENTATION_TASKS/task.md", "絶対path"},
		{"IMPLEMENTATION_TASKS//task.md", "空segment"},
		{"IMPLEMENTATION_TASKS/./task.md", "`.` segment"},
		{"IMPLEMENTATION_TASKS/../task.md", "`..` segment"},
		{"IMPLEMENTATION_TASKS/a/./b.md", "中間`.` segment"},
		{"IMPLEMENTATION_TASKS/a/../b.md", "中間`..` segment"},
		{"IMPLEMENTATION_TASKS\\task.md", "backslash混在"},
		{"IMPLEMENTATION_TASKS/task.md/", "末尾slash"},
		{"IMPLEMENTATION_TASKS/task.md/..", "末尾`..`"},
		{"IMPLEMENTATION_TASKS/..", "上方脱出"},
	}
	for _, c := range candidates {
		shellErr := exec.Command("sh", "-c", script, "validate_plan_task_path", c.path).Run()
		runtimeErr := validateActiveTaskPath(c.path)
		if (runtimeErr == nil) != (shellErr == nil) {
			t.Errorf("validate_plan_task_path(%q %s) = %v, runtime validateActiveTaskPath = %v", c.path, c.note, shellErr, runtimeErr)
		}
	}
}

func TestPlanFinalHeadBulletExtractionMatchesRuntime(t *testing.T) {
	root := scenarioRepoRoot(t)
	installerBytes, err := os.ReadFile(filepath.Join(root, "install.sh"))
	if err != nil {
		t.Fatalf("read install.sh: %v", err)
	}
	installer := string(installerBytes)
	const marker = "plan_bullet_paths() {"
	start := strings.Index(installer, marker)
	if start < 0 {
		t.Fatalf("install.sh lacks plan_bullet_paths definition")
	}
	bodyEnd := strings.Index(installer[start:], "\n}\n")
	if bodyEnd < 0 {
		t.Fatalf("install.sh plan_bullet_paths body is not terminated")
	}
	script := installer[start:start+bodyEnd+2] + "\nplan_bullet_paths\n"

	bullets := []struct {
		line string
		note string
	}{
		{"- `IMPLEMENTATION_TASKS/x.md`", "標準形式"},
		{"- IMPLEMENTATION_TASKS/x.md", "逆引用符なし直書き"},
		{"- `IMPLEMENTATION_TASKS/x.md", "閉じbacktick欠損"},
		{"- `IMPLEMENTATION_TASKS/x.md` suffix", "閉じbacktick後に余分なtext"},
		{"- prefix `IMPLEMENTATION_TASKS/x.md`", "開始backtick前に余分なtext"},
		{"- `a.md` `b.md`", "複数backtick組"},
		{"- ``", "空pathのbacktick組"},
		{"-   `IMPLEMENTATION_TASKS/x.md`", "marker直後の余分な空白"},
		{"- `IMPLEMENTATION_TASKS/x.md`   ", "行末空白"},
		{"- garbage", "非task path項目"},
		{"- ", "空項目"},
		{"plain text", "説明文の非bullet行"},
		{"* `IMPLEMENTATION_TASKS/b.md`", "未知markerのtask-like list行"},
		{"+ `IMPLEMENTATION_TASKS/b.md`", "`+`markerのlist行"},
		{"1. `IMPLEMENTATION_TASKS/b.md`", "番号付きmarkerのlist行"},
		{"-x", "`- `でないmarker行"},
		{"", "blank行"},
		{"   ", "空白のみの行"},
		{"  - `IMPLEMENTATION_TASKS/x.md`", "字下げbullet"},
	}
	for _, b := range bullets {
		cmd := exec.Command("sh", "-c", script)
		cmd.Stdin = strings.NewReader(b.line + "\n")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("plan_bullet_paths(%q %s): %v", b.line, b.note, err)
		}
		shellLine := strings.TrimSuffix(string(out), "\n")

		runtimeEntries, runtimeErr := activeSectionEntries("## ACTIVE\n\n" + b.line + "\n\n## NEXT\n")
		switch {
		case shellLine == "":
			if runtimeErr != nil || len(runtimeEntries) != 0 {
				t.Errorf("plan_bullet_paths(%q %s)はbulletを無視しましたがruntimeは扱います: entries=%v err=%v", b.line, b.note, runtimeEntries, runtimeErr)
			}
		case strings.HasPrefix(shellLine, "!"):
			if runtimeErr == nil {
				t.Errorf("plan_bullet_paths(%q %s)はmalformed扱いですがruntimeは受理します: %q", b.line, b.note, shellLine)
			}
		case strings.HasPrefix(shellLine, "+"):
			path := strings.TrimPrefix(shellLine, "+")
			if runtimeErr != nil {
				t.Errorf("plan_bullet_paths(%q %s)はpath候補 %qを返しましたがruntimeはmalformed扱いです: %v", b.line, b.note, path, runtimeErr)
				continue
			}
			if len(runtimeEntries) != 1 || runtimeEntries[0] != path {
				t.Errorf("plan_bullet_paths(%q %s) = %q, runtime = %v", b.line, b.note, path, runtimeEntries)
			}
		default:
			t.Fatalf("plan_bullet_paths(%q %s)が未知の出力を返しました: %q", b.line, b.note, shellLine)
		}
	}
}
