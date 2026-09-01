package workflow

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reposearch"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExhaustiveRequirementUsesExplicitPrimaryTaskMarkerOnly(t *testing.T) {
	content := "# task\n\n## Contract\n\n- " + exhaustiveSearchRequiredMarker + "\n- exhaustive needle inspection\n\n## Must not\n\n- unrelated prose\n"
	if !hasExhaustiveRequirement(taskExhaustiveRequirementText(content)) {
		t.Fatal("Contractのexplicit exhaustive markerを検出できません")
	}
	negative := "# task\n\n## Contract\n\n- keep exhaustive search proof enabled\n- do not disable full-corpus proof\n\n## Must not\n\n- " + exhaustiveSearchRequiredMarker + "\n"
	if hasExhaustiveRequirement(taskExhaustiveRequirementText(negative)) {
		t.Fatal("自然言語proseまたはMust notだけのmarkerをactivation authorityにしてはいけません")
	}
}

func TestExhaustiveSearchContractStripsRequestMarkerAndPrefersActiveTaskAuthority(t *testing.T) {
	required, query, err := exhaustiveSearchContract(t.TempDir(), exhaustiveSearchRequiredMarker+"\nneedle target", "")
	if err != nil {
		t.Fatal(err)
	}
	if !required || query != "needle target" {
		t.Fatalf("planless contract = required:%v query:%q", required, query)
	}

	repoRoot := t.TempDir()
	taskPath := filepath.Join("IMPLEMENTATION_TASKS", "task.md")
	if err := os.MkdirAll(filepath.Join(repoRoot, "IMPLEMENTATION_TASKS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, taskPath), []byte("# task\n\n## Contract\n\n- keep exhaustive proof enabled\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	required, query, err = exhaustiveSearchContract(repoRoot, exhaustiveSearchRequiredMarker+"\nneedle target", taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if required || query != "needle target" {
		t.Fatalf("ACTIVE task authority should override request marker: required:%v query:%q", required, query)
	}
}

func TestExecuteNewTaskInjectsWorkerAndIndependentReviewerExhaustiveManifest(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	taskContent := taskWithExhaustiveRequirement(activeTaskGuardSeed)
	if err := os.WriteFile(filepath.Join(repoRoot, activeTaskGuardPath), []byte(taskContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "needle-target.txt"), []byte("needle implementation\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w, r, _, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}, "", 0, nil)

	request := "needle inspection bearer s3cr3t-credential"
	if err := w.ExecuteNewTask(request); err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 2 {
		t.Fatalf("worker/reviewer calls=%d want=2", len(r.prompts))
	}
	manifestPaths := make([]string, 0, 2)
	for i, prompt := range r.prompts {
		for _, want := range []string{
			"EXHAUSTIVE_SEARCH_PROOF:",
			"MODE: full-corpus-deterministic",
			"PREDICATE: any-normalized-query-token-in-path-or-text",
			"MATCH_MANIFEST:",
			"MATCH_LIST_INLINE: none",
			"BM25_TOP_N_AUTHORITY: none",
		} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("prompt %d missing %q:\n%s", i, want, prompt)
			}
		}
		if strings.Contains(prompt, "MATCH: needle-target.txt") {
			t.Fatalf("prompt %dへmatch一覧をinline展開しています:\n%s", i, prompt)
		}
		manifestPath := exhaustiveManifestPath(prompt)
		if manifestPath == "" {
			t.Fatalf("prompt %dからmanifest pathを取得できません", i)
		}
		manifest, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatalf("manifest %dを読めません: %v", i, err)
		}
		if !strings.Contains(string(manifest), "MATCH: needle-target.txt") {
			t.Fatalf("manifest %dがcomplete matchを保持していません:\n%s", i, manifest)
		}
		manifestPaths = append(manifestPaths, manifestPath)
	}
	if manifestPaths[0] == manifestPaths[1] {
		t.Fatalf("worker/reviewerは独立manifestを持つべきです: %q", manifestPaths[0])
	}
	if !strings.Contains(r.prompts[1], "ROLE: reviewer") || !strings.Contains(r.prompts[1], "WORKER_EXHAUSTIVE_PROOF_AUTHORITY: none") {
		t.Fatalf("reviewer独立proof markerがありません:\n%s", r.prompts[1])
	}

	events, rawLog := exhaustiveEventsFromLog(t, st)
	if len(events) != 2 {
		t.Fatalf("exhaustive event count=%d want=2: %s", len(events), rawLog)
	}
	phases := []string{workerExhaustiveSearchPhase, reviewerExhaustiveSearchPhase}
	for i, event := range events {
		if event.Phase != phases[i] || event.Subtype != exhaustiveSearchComplete {
			t.Fatalf("event %d = %s/%s want %s/%s", i, event.Phase, event.Subtype, phases[i], exhaustiveSearchComplete)
		}
		if len(event.SearchPaths) != 1 || event.SearchPaths[0] != "needle-target.txt" {
			t.Fatalf("event %dのmatch pathが証明を保持していません: %v", i, event.SearchPaths)
		}
	}
	if strings.Contains(rawLog, "\"search_query\"") || strings.Contains(rawLog, "s3cr3t") || strings.Contains(rawLog, request) {
		t.Fatalf("event logへrequest由来query dataまたはobsolete keyが漏れています: %s", rawLog)
	}
}

func TestExecuteNewTaskDoesNotActivateFromExhaustiveProse(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	taskContent := strings.Replace(activeTaskGuardSeed, "## Contract\n", "## Contract\n\n- keep exhaustive search proof enabled; do not disable full-corpus proof\n", 1)
	if err := os.WriteFile(filepath.Join(repoRoot, activeTaskGuardPath), []byte(taskContent), 0o644); err != nil {
		t.Fatal(err)
	}
	w, r, _, _ := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}, "", 0, nil)

	if err := w.ExecuteNewTask("exhaustive needle inspection"); err != nil {
		t.Fatal(err)
	}
	for i, prompt := range r.prompts {
		if strings.Contains(prompt, "EXHAUSTIVE_SEARCH_PROOF") {
			t.Fatalf("natural-language exhaustive prose activated prompt %d:\n%s", i, prompt)
		}
	}
}

func TestExhaustiveSearchFailureStopsBeforeModelUse(t *testing.T) {
	root := t.TempDir()
	w := &Workflow{config: configForExhaustiveFailure(root), state: newStateStoreT(t), now: testNow}
	_, err := w.exhaustiveSearchContext(exhaustiveSearchRequiredMarker+"\nneedle", "", state.WorkerRole, 1)
	if err == nil || !strings.Contains(err.Error(), "exhaustive search proof failed before worker dispatch") {
		t.Fatalf("err=%v", err)
	}
}

func TestExhaustiveSearchMarkerWithoutQueryFailsClosed(t *testing.T) {
	w := &Workflow{config: configForExhaustiveFailure(t.TempDir()), state: newStateStoreT(t), now: testNow}
	_, err := w.exhaustiveSearchContext(exhaustiveSearchRequiredMarker, "", state.WorkerRole, 1)
	if err == nil || !strings.Contains(err.Error(), "requires a non-marker query") {
		t.Fatalf("err=%v", err)
	}
}

func configForExhaustiveFailure(root string) config.AppConfig {
	return config.AppConfig{RepoRoot: root, RepoHash: "exhaustive-failure", StateBase: root}
}

func testNow() time.Time {
	return testFixedTime
}

func TestExhaustiveProofPersistsIntoAutoFixPrompt(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	if err := os.WriteFile(filepath.Join(repoRoot, activeTaskGuardPath), []byte(taskWithExhaustiveRequirement(activeTaskGuardSeed)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "needle-target.txt"), []byte("needle implementation\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w, r, _, _ := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("initial")},
		{structured: fixRequiredPacket()},
		{structured: implementedPacket("fixed")},
		{structured: needsSolReviewPacket()},
	}, "", 0, nil)
	if err := w.ExecuteNewTask("needle inspection"); err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 4 {
		t.Fatalf("calls=%d want=4 phases=%v", len(r.prompts), r.phases)
	}
	if !strings.Contains(r.prompts[2], "EXHAUSTIVE_SEARCH_PROOF:") || !strings.Contains(r.prompts[2], "ROLE: worker") {
		t.Fatalf("auto-fix prompt lost exhaustive proof:\n%s", r.prompts[2])
	}
	if strings.Contains(r.prompts[2], "MATCH: needle-target.txt") {
		t.Fatalf("auto-fix promptへmatch一覧をinline再注入しています:\n%s", r.prompts[2])
	}
}

func TestRenderExhaustiveProofStaysBoundedForLargeMatchSet(t *testing.T) {
	report := reposearch.ExhaustiveReport{
		Predicate:       "any-normalized-query-token-in-path-or-text",
		EnumeratedFiles: 512,
		ScannedFiles:    512,
		Matches:         make([]reposearch.ExhaustiveMatch, 0, 512),
	}
	for i := 0; i < 512; i++ {
		report.Matches = append(report.Matches, reposearch.ExhaustiveMatch{Path: fmt.Sprintf("pkg/path-%03d.go", i)})
	}
	got := renderExhaustiveSearchProof(state.WorkerRole, report, "/state/artifacts/task/exhaustive-search/worker-1.txt")
	if len(got) > 1600 {
		t.Fatalf("proof prompt grows with match list: bytes=%d", len(got))
	}
	if strings.Contains(got, "pkg/path-000.go") || strings.Contains(got, "MATCH: ") {
		t.Fatalf("proof prompt contains inline matches:\n%s", got)
	}
}

func taskWithExhaustiveRequirement(content string) string {
	const contractHeading = "## Contract\n"
	if index := strings.Index(content, contractHeading); index >= 0 {
		insert := index + len(contractHeading)
		return content[:insert] + "\n- " + exhaustiveSearchRequiredMarker + "\n" + content[insert:]
	}
	return content + "\n\n## Contract\n\n- " + exhaustiveSearchRequiredMarker + "\n"
}

func exhaustiveEventsFromLog(t *testing.T, st *state.StateStore) ([]state.TaskEventRecord, string) {
	t.Helper()
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(st.TaskEventLogPath(taskID))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	var events []state.TaskEventRecord
	var raw strings.Builder
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Bytes()
		raw.Write(line)
		raw.WriteByte('\n')
		event, err := state.ParseTaskEventLine(line)
		if err != nil {
			t.Fatalf("event parse失敗 %v: %s", err, line)
		}
		if event.Kind == "exhaustive-search" {
			events = append(events, event)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return events, raw.String()
}

func exhaustiveManifestPath(prompt string) string {
	const prefix = "MATCH_MANIFEST: "
	index := strings.Index(prompt, prefix)
	if index < 0 {
		return ""
	}
	value := prompt[index+len(prefix):]
	if end := strings.IndexByte(value, '\n'); end >= 0 {
		value = value[:end]
	}
	return strings.TrimSpace(value)
}
