package taskcontract

import "testing"

func TestParseTaskDependenciesNoneAndProse(t *testing.T) {
	body := "# Task\n\n## Dependencies\n\nnone\n"
	paths, err := ParseTaskDependencies([]byte(body))
	if err != nil {
		t.Fatalf("ParseTaskDependencies: %v", err)
	}
	if len(paths) != 0 {
		t.Fatalf("paths = %#v", paths)
	}
	prose := "# Task\n\n## Dependencies\n\n- Final verification開始時点で他の実行可能taskが残っていないこと\n"
	paths, err = ParseTaskDependencies([]byte(prose))
	if err != nil {
		t.Fatalf("ParseTaskDependencies prose: %v", err)
	}
	if len(paths) != 0 {
		t.Fatalf("prose paths = %#v", paths)
	}
}

func TestParseTaskDependenciesPathBullets(t *testing.T) {
	body := "# Task\n\n## Dependencies\n\n- `IMPLEMENTATION_TASKS/first.md`\n- IMPLEMENTATION_TASKS/second.md\n- `IMPLEMENTATION_TASKS/first.md`\n"
	paths, err := ParseTaskDependencies([]byte(body))
	if err != nil {
		t.Fatalf("ParseTaskDependencies: %v", err)
	}
	want := []string{"IMPLEMENTATION_TASKS/first.md", "IMPLEMENTATION_TASKS/second.md"}
	if len(paths) != len(want) {
		t.Fatalf("paths = %#v", paths)
	}
	for index := range want {
		if paths[index] != want[index] {
			t.Fatalf("paths = %#v", paths)
		}
	}
}

func TestParseTaskDependenciesIgnoresFencedPath(t *testing.T) {
	body := "# Task\n\n## Dependencies\n\nnone\n\n```text\n- `IMPLEMENTATION_TASKS/fenced.md`\n```\n"
	paths, err := ParseTaskDependencies([]byte(body))
	if err != nil {
		t.Fatalf("ParseTaskDependencies: %v", err)
	}
	if len(paths) != 0 {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestParseTaskDependenciesRejectsMissingSection(t *testing.T) {
	if _, err := ParseTaskDependencies([]byte("# Task\n")); err == nil {
		t.Fatal("task without Dependencies section was accepted")
	}
}

func TestParseTaskDependenciesRejectsDuplicateSection(t *testing.T) {
	body := "# Task\n\n## Dependencies\n\nnone\n\n## Dependencies\n\nnone\n"
	if _, err := ParseTaskDependencies([]byte(body)); err == nil {
		t.Fatal("duplicate Dependencies section was accepted")
	}
}

func TestParseTaskDependenciesRejectsMalformedReference(t *testing.T) {
	for _, item := range []string{
		"- `IMPLEMENTATION_TASKS/../escape.md`",
		"- `IMPLEMENTATION_TASKS/missing-suffix.txt`",
		"- `IMPLEMENTATION_TASKS//double.md`",
		"- `IMPLEMENTATION_TASKS/first.md` と `IMPLEMENTATION_TASKS/second.md`",
		"- 依存先: `IMPLEMENTATION_TASKS/first.md`",
		"- IMPLEMENTATION_TASKS/first.md(`注記`付き)",
	} {
		body := "# Task\n\n## Dependencies\n\n" + item + "\n"
		if _, err := ParseTaskDependencies([]byte(body)); err == nil {
			t.Fatalf("malformed dependency %q was accepted", item)
		}
	}
}

func TestParseReviewFindings(t *testing.T) {
	none, err := ParseReviewFindings([]byte("# Task\n\n## Review findings\n\nnone\n"))
	if err != nil {
		t.Fatalf("ParseReviewFindings: %v", err)
	}
	if !none.Present || !none.None {
		t.Fatalf("findings = %#v", none)
	}
	open, err := ParseReviewFindings([]byte("# Task\n\n## Review findings\n\n- workerが契約外fileへ触れた\n"))
	if err != nil {
		t.Fatalf("ParseReviewFindings open: %v", err)
	}
	if !open.Present || open.None {
		t.Fatalf("findings = %#v", open)
	}
	if _, err := ParseReviewFindings([]byte("# Task\n")); err == nil {
		t.Fatal("task without Review findings section was accepted")
	}
	if _, err := ParseReviewFindings([]byte("# Task\n\n## Review findings\n\nnone\n\n## Review findings\n\nnone\n")); err == nil {
		t.Fatal("duplicate Review findings section was accepted")
	}
}
