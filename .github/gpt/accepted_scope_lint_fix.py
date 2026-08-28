from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    target = Path(path)
    text = target.read_text()
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: match count={count} for {old[:120]!r}")
    target.write_text(text.replace(old, new, 1))


replace_once(
    "codex/instructions/glm-execution.md",
    "- Sol High自身が現在のnon-parent diffを確認し、fix本文で撤回・縮小を指示する部分以外は受理済みと明示判断した場合だけ、`--fix-stdin`へ`--accepted-scope current-diff`を追加してよい。`--origin`やreviewerのPASSからこの受理を推測しない。不確実なら指定しない。glm-workerはfix開始時のchange setをscopeとして保存し、fix後のchange setがそのsubsetである場合だけ同一HIGH riskの再Sol escalationを省略する。新しい変更・別replacement・新規file内容・比較不能なbinary/dirty baselineがあれば通常のrisk floorへ戻る。\n",
    "",
)
replace_once(
    "codex/instructions/glm-packets.md",
    "- 修正が必要ならCodex自身で編集せず、修正方針本文を`~/.codex/instructions/glm-execution.md`のstdin mode（`--fix-stdin <payload-bytes>`）で同じworker sessionへ差し戻す。修正後は独立reviewerまで自動再実行される。\n",
    "- 修正が必要ならCodex自身で編集せず、修正方針本文を`~/.codex/instructions/glm-execution.md`のstdin mode（`--fix-stdin <payload-bytes>`）で同じworker sessionへ差し戻す。修正後は独立reviewerまで自動再実行される。Sol自身が現diffの残存部分を受理し、fixが撤回・縮小だけなら`--accepted-scope current-diff`を付ける。不確実または新規変更を許すfixでは付けない。\n",
)

replace_once(
    "glm-worker/internal/app/app.go",
    'func applyStdinPayloadOption(command *Command, name, value, usage string, allowOrigin bool, seenSHA256 *bool) error {\n\tswitch name {',
    'func applyStdinPayloadOption(command *Command, name, value, usage string, allowOrigin bool, seenSHA256 *bool) error {\n\tif name == "--accepted-scope" {\n\t\treturn applyAcceptedScopeOption(command, value, usage, allowOrigin)\n\t}\n\tswitch name {',
)
replace_once(
    "glm-worker/internal/app/app.go",
    '\tcase "--accepted-scope":\n\t\tif !allowOrigin || command.AcceptedScope != "" || value != "current-diff" {\n\t\t\treturn usageError("%s", usage)\n\t\t}\n\t\tcommand.AcceptedScope = value\n\t\treturn nil\n',
    "",
)
replace_once(
    "glm-worker/internal/app/app.go",
    "\nfunc parsePayloadSHA256(value string) (string, error) {",
    '\nfunc applyAcceptedScopeOption(command *Command, value, usage string, allowOrigin bool) error {\n\tif !allowOrigin || command.AcceptedScope != "" || value != "current-diff" {\n\t\treturn usageError("%s", usage)\n\t}\n\tcommand.AcceptedScope = value\n\treturn nil\n}\n\nfunc parsePayloadSHA256(value string) (string, error) {',
)

replace_once(
    "glm-worker/internal/workflow/workflow.go",
    '\t\tpocStage := decl.pocStage()\n\t\tif err := w.state.Remove(acceptedFixScopeStateFile); err != nil {\n\t\t\treturn err\n\t\t}\n\t\tif err := w.state.Write("last-decision", decision); err != nil {\n\t\t\treturn err\n\t\t}',
    '\t\tpocStage := decl.pocStage()\n\t\tif err := w.replaceAcceptedScopeWithDecision(decision); err != nil {\n\t\t\treturn err\n\t\t}',
)
replace_once(
    "glm-worker/internal/workflow/workflow.go",
    "\nfunc (w *Workflow) ExecuteExplicitFix(instruction, origin string) error {",
    '\nfunc (w *Workflow) replaceAcceptedScopeWithDecision(decision string) error {\n\tif err := w.state.Remove(acceptedFixScopeStateFile); err != nil {\n\t\treturn err\n\t}\n\treturn w.state.Write("last-decision", decision)\n}\n\nfunc (w *Workflow) ExecuteExplicitFix(instruction, origin string) error {',
)
replace_once(
    "glm-worker/internal/workflow/workflow.go",
    '\treturn quietWhenParentFileGuardStopped(w.withTemp(func() error {\n\t\tif acceptedScope != "" && acceptedScope != acceptedFixScopeCurrentDiff {\n\t\t\treturn &WorkerError{Message: "unknown accepted fix scope: " + acceptedScope}\n\t\t}\n\t\tif w.state.Exists("pending-decision") {',
    '\treturn quietWhenParentFileGuardStopped(w.withTemp(func() error {\n\t\tif w.state.Exists("pending-decision") {',
)

scope_path = Path("glm-worker/internal/workflow/accepted_fix_scope.go")
scope = scope_path.read_text()
old_declarations = '''const (
\tacceptedFixScopeStateFile   = "accepted-fix-scope.json"
\tacceptedFixScopeCurrentDiff = "current-diff"
\tacceptedFixScopeVersion     = 1
)

type acceptedFixScope struct {
\tVersion      int            `json:"version"`
\tBaselineHead string         `json:"baseline_head"`
\tChanges      map[string]int `json:"changes"`
}
'''
new_declarations = '''type acceptedFixScope struct {
\tVersion      int            `json:"version"`
\tBaselineHead string         `json:"baseline_head"`
\tChanges      map[string]int `json:"changes"`
}

type acceptedPatchState struct {
\toldLine        int
\tinHunk         bool
\tpreviousChange byte
}

const (
\tacceptedFixScopeStateFile   = "accepted-fix-scope.json"
\tacceptedFixScopeCurrentDiff = "current-diff"
\tacceptedFixScopeVersion     = 1
)
'''
if scope.count(old_declarations) != 1:
    raise SystemExit("accepted_fix_scope declaration anchor mismatch")
scope = scope.replace(old_declarations, new_declarations, 1)
start = scope.index("func addPatchScopeChanges(")
end = scope.index("\nfunc changeSetSubset(", start)
replacement = r'''func addPatchScopeChanges(changes map[string]int, path string, patch []byte) error {
	if bytes.Contains(patch, []byte("GIT binary patch")) || bytes.Contains(patch, []byte("Binary files ")) || bytes.IndexByte(patch, 0) >= 0 {
		return fmt.Errorf("accepted scope cannot compare binary patch %s", path)
	}
	state := acceptedPatchState{}
	for _, line := range strings.Split(string(patch), "\n") {
		handled, err := state.addMetadataChange(changes, path, line)
		if err != nil {
			return err
		}
		if handled {
			continue
		}
		if err := state.addHunkChange(changes, path, line); err != nil {
			return err
		}
	}
	return nil
}

func (s *acceptedPatchState) addMetadataChange(changes map[string]int, path, line string) (bool, error) {
	switch {
	case line == "":
		return true, nil
	case strings.HasPrefix(line, "diff --git "), strings.HasPrefix(line, "index "), strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "):
		return true, nil
	case strings.HasPrefix(line, "old mode "), strings.HasPrefix(line, "new mode "), strings.HasPrefix(line, "new file mode "), strings.HasPrefix(line, "deleted file mode "):
		changes["meta\x00"+path+"\x00"+line]++
		return true, nil
	case strings.HasPrefix(line, "@@ "):
		match := zeroContextHunk.FindStringSubmatch(line)
		if match == nil {
			return true, fmt.Errorf("accepted scope cannot parse hunk %q", line)
		}
		value, err := strconv.Atoi(match[1])
		if err != nil {
			return true, err
		}
		s.oldLine = value
		s.inHunk = true
		s.previousChange = 0
		return true, nil
	case strings.HasPrefix(line, `\ No newline at end of file`):
		if !s.inHunk || s.previousChange == 0 {
			return true, fmt.Errorf("accepted scope cannot place no-newline marker in %s", path)
		}
		changes[fmt.Sprintf("newline\x00%s\x00%c\x00%d", path, s.previousChange, s.oldLine)]++
		return true, nil
	case !s.inHunk:
		return true, fmt.Errorf("accepted scope cannot parse patch metadata %q", line)
	default:
		return false, nil
	}
}

func (s *acceptedPatchState) addHunkChange(changes map[string]int, path, line string) error {
	switch line[0] {
	case '-':
		changes[fmt.Sprintf("line\x00%s\x00-\x00%d\x00%s", path, s.oldLine, line[1:])]++
		s.oldLine++
		s.previousChange = '-'
	case '+':
		changes[fmt.Sprintf("line\x00%s\x00+\x00%d\x00%s", path, s.oldLine, line[1:])]++
		s.previousChange = '+'
	case ' ':
		s.oldLine++
		s.previousChange = 0
	default:
		return fmt.Errorf("accepted scope cannot parse patch line %q", line)
	}
	return nil
}
'''
scope_path.write_text(scope[:start] + replacement + scope[end:])

Path("glm-worker/internal/workflow/accepted_fix_scope_risk_test.go").write_text(r'''package workflow

import (
	"io"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestAcceptedFixScopeSkipsRedundantRiskFloorCall(t *testing.T) {
	repo := t.TempDir()
	gitScope(t, repo, "init")
	gitScope(t, repo, "config", "user.email", "scope@example.invalid")
	gitScope(t, repo, "config", "user.name", "scope-test")
	writeScopeFile(t, repo, "code.go", "package sample\n")
	gitScope(t, repo, "add", ".")
	gitScope(t, repo, "commit", "-m", "baseline")

	cfg := config.AppConfig{RepoRoot: repo, StateBase: t.TempDir(), RepoHash: "scope-risk"}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	writeScopeFile(t, repo, "code.go", "package sample\n\nvar retained = 1\nvar removeMe = 2\n")

	runner := &scriptedRunner{}
	w := NewWorkflow(cfg, st, runner, io.Discard)
	w.prepareAcceptedFixScope(acceptedFixScopeCurrentDiff)
	writeScopeFile(t, repo, "code.go", "package sample\n\nvar retained = 1\n")

	result, stopped, err := w.enforceRiskFloor("request", packet.Result{}, 1, 0, "none", true, packet.Result{Status: packet.StatusPass})
	if err != nil || stopped || result.Status != packet.StatusPass {
		t.Fatalf("result = %#v, stopped=%v, err=%v", result, stopped, err)
	}
	if len(runner.phases) != 0 {
		t.Fatalf("redundant risk-floor model call phases = %v", runner.phases)
	}
}
''')
