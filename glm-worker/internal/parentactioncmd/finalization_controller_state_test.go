package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
)

type finalizationControllerFixture struct {
	cfg       config.AppConfig
	storeDir  string
	candidate controller.AcceptedCandidate
	ref       controller.EvidenceObjectRef
}

func TestExecuteFinalizationCheckRealControllerRejectsUndeclaredForm(t *testing.T) {
	for _, tc := range []struct {
		name  string
		forms []controller.FinalValidationRequest
		form  string
	}{
		{name: "no obligations", form: "go-test"},
		{name: "race absent", forms: []controller.FinalValidationRequest{{Form: "go-test", WorkingDir: "module"}}, form: "go-test-race"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newFinalizationControllerFixture(t, tc.forms)
			marker := filepath.Join(t.TempDir(), "gate-called")
			writeFinalizationGateMarkerWorker(t, marker)
			checkoutMarker := observeFinalizationCheckoutCreation(t)
			before := finalizationControllerWorktrees(t, fixture.cfg.RepoRoot)
			result, err := executeFinalizationControllerCheck(t, fixture.cfg, tc.form)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != "blocked" || result.Failure == nil || result.Failure.Stage != "routing" || result.Failure.Reason != "form_not_declared_by_accepted_candidate" {
				t.Fatalf("undeclared candidate form result = %#v", result)
			}
			assertFinalizationControllerNotExecuted(t, fixture, marker, before)
			assertFinalizationCheckoutNotCreated(t, checkoutMarker)
		})
	}
}

func TestExecuteFinalizationCheckRealControllerRejectsInvalidAuthority(t *testing.T) {
	for _, kind := range []string{"corrupt candidate", "missing candidate", "foreign controller", "unknown form"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newFinalizationControllerFixture(t, nil)
			form := "go-test"
			switch kind {
			case "corrupt candidate":
				path := finalizationControllerCandidateObject(fixture)
				if err := os.WriteFile(path, []byte("corrupt candidate\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "missing candidate":
				if err := os.Remove(finalizationControllerCandidateObject(fixture)); err != nil {
					t.Fatal(err)
				}
			case "foreign controller":
				foreign := newFinalizationControllerFixture(t, nil)
				if err := os.RemoveAll(fixture.storeDir); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(foreign.storeDir, fixture.storeDir); err != nil {
					t.Fatal(err)
				}
			case "unknown form":
				form = "unknown-form"
			}
			marker := filepath.Join(t.TempDir(), "gate-called")
			writeFinalizationGateMarkerWorker(t, marker)
			checkoutMarker := observeFinalizationCheckoutCreation(t)
			before := finalizationControllerWorktrees(t, fixture.cfg.RepoRoot)
			result, err := executeFinalizationControllerCheck(t, fixture.cfg, form)
			if err == nil && (result.Status != "blocked" || result.Failure == nil || result.Failure.Reason != "accepted_candidate_unavailable") {
				t.Fatalf("invalid authority became an absent obligation: result=%#v error=%v", result, err)
			}
			assertFinalizationControllerNotExecuted(t, fixture, marker, before)
			assertFinalizationCheckoutNotCreated(t, checkoutMarker)
		})
	}
}

func TestExecuteFinalizationCheckRealControllerValidatesCandidateCheckout(t *testing.T) {
	fixture := newFinalizationControllerFixture(t, []controller.FinalValidationRequest{{Form: "go-test", WorkingDir: "module"}})
	marker := filepath.Join(t.TempDir(), "validated-checkout")
	t.Setenv("CANDIDATE_VALIDATION_MARKER", marker)
	t.Setenv("CANDIDATE_VALIDATION_COMMIT", fixture.candidate.CommitOID)
	t.Setenv("CANDIDATE_PRIMARY_ROOT", fixture.cfg.RepoRoot)
	activateFinalizationWorker(t, `
case "$1" in
  --quality-gate)
    test "$2" = "go-test"
    test "$PWD" != "$CANDIDATE_PRIMARY_ROOT/module"
    test "$(git rev-parse HEAD)" = "$CANDIDATE_VALIDATION_COMMIT"
    test "$(cat candidate.txt)" = "accepted candidate source"
    test "$(git show HEAD:module/candidate.txt)" = "accepted candidate source"
    printf '%s\n' "$PWD" >"$CANDIDATE_VALIDATION_MARKER"
    printf '%s\n' '{"status":"pass","validation_run_id":"candidate-source-checked"}'
    ;;
  *) exit 9 ;;
esac
`)
	before := finalizationControllerWorktrees(t, fixture.cfg.RepoRoot)
	result, err := executeFinalizationControllerCheck(t, fixture.cfg, "go-test")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "ready_for_parent_decision" || result.Failure != nil || result.Routing == nil || result.Routing.Basis != finalizationRoutingBasisCandidate {
		t.Fatalf("declared candidate validation result = %#v", result)
	}
	validated, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("candidate source was not checked: %v", err)
	}
	selected := strings.TrimSpace(string(validated))
	if selected != result.Routing.SelectedDir || filepath.Base(selected) != "module" || !strings.HasPrefix(selected, fixture.storeDir+string(filepath.Separator)) {
		t.Fatalf("gate did not use candidate checkout: marker=%q routing=%#v", selected, result.Routing)
	}
	if result.Git == nil || result.Git.Head != fixture.candidate.CommitOID || !result.Git.Detached || !result.Git.Clean {
		t.Fatalf("validated source does not match accepted candidate: %#v", result.Git)
	}
	if _, err := os.Stat(filepath.Dir(selected)); !os.IsNotExist(err) {
		t.Fatalf("candidate checkout was not cleaned up: %v", err)
	}
	if after := finalizationControllerWorktrees(t, fixture.cfg.RepoRoot); after != before {
		t.Fatalf("validation left a registered worktree: before=%s after=%s", before, after)
	}
}

func newFinalizationControllerFixture(t *testing.T, forms []controller.FinalValidationRequest) finalizationControllerFixture {
	t.Helper()
	repo := newFinalizationTestRepo(t)
	repo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		"IMPLEMENTATION_PLAN.local.md":      "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/root.md`\n\n## NEXT\n\n- `IMPLEMENTATION_TASKS/successor.md`\n",
		"IMPLEMENTATION_TASKS/root.md":      "# validation fixture\n\n## External feasibility\n\nstatus: not-applicable\n\n## Execution authority\n\nmode: parent-only\n\n## Contract\n\nvalidate candidate source\n\n## Dependencies\n\nnone\n",
		"IMPLEMENTATION_TASKS/successor.md": "# successor\n\n## External feasibility\n\nstatus: not-applicable\n\n## Contract\n\nsuccessor\n\n## Dependencies\n\nnone\n",
		"module/go.mod":                     "module example.invalid/controller-validation\n\ngo 1.22\n",
		"module/candidate.txt":              "original source\n",
	} {
		full := filepath.Join(repo, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runFinalizationGit(t, repo, "add", ".")
	runFinalizationGit(t, repo, "commit", "-qm", "candidate validation authority")
	remote := filepath.Join(t.TempDir(), "remote.git")
	runFinalizationGit(t, repo, "init", "--bare", remote)
	runFinalizationGit(t, repo, "remote", "add", "origin", remote)
	runFinalizationGit(t, repo, "push", "-q", "origin", "HEAD:refs/heads/main")
	stateRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.AppConfig{RepoRoot: repo, RepoHash: config.RepoHashFor(repo), StateBase: filepath.Join(stateRoot, "sessions")}
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "module", "candidate.txt"), []byte("accepted candidate source\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	materialized, err := store.AdmitParentOnlyExecution(controller.ParentOnlyMaterializeInput{ExpectedGeneration: head.ControllerGeneration})
	if err != nil || materialized.Admission == nil {
		t.Fatalf("materialize parent-only source: %v", err)
	}
	source := *materialized.Admission
	branch := finalizationControllerGitOutput(t, repo, "symbolic-ref", "HEAD")
	accepted, err := store.AcceptParentCompletion(source, controller.CandidateAcceptanceInput{
		Message:             "accepted source for validation",
		Policy:              controller.PublicationPolicy{Remote: "origin", RemoteRef: "refs/heads/main", LocalRef: branch},
		RequiredValidations: &controller.RequiredValidationDeclaration{Forms: forms},
	})
	if err != nil || accepted.CandidateRef == nil {
		t.Fatalf("accept candidate through controller API: %v", err)
	}
	candidate, err := store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	return finalizationControllerFixture{cfg: cfg, storeDir: filepath.Join(filepath.Dir(cfg.StateBase), "controllers", store.Identity().LineageID), candidate: candidate, ref: *accepted.CandidateRef}
}

func executeFinalizationControllerCheck(t *testing.T, cfg config.AppConfig, form string) (finalizationCheckOutput, error) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cfg.RepoRoot); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatal(err)
		}
	}()
	var stdout, stderr bytes.Buffer
	err = execute(cfg, []string{"finalize-check", form}, &stdout, &stderr)
	var result finalizationCheckOutput
	if err == nil {
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatalf("decode finalize-check: %v: %s", err, stdout.String())
		}
	}
	return result, err
}

func assertFinalizationControllerNotExecuted(t *testing.T, fixture finalizationControllerFixture, marker, before string) {
	t.Helper()
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("gate executed for rejected authority: %v", err)
	}
	entries, err := os.ReadDir(fixture.storeDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "validation-") {
			t.Fatalf("rejected request created a candidate checkout: %s", entry.Name())
		}
	}
	if after := finalizationControllerWorktrees(t, fixture.cfg.RepoRoot); after != before {
		t.Fatalf("rejected request changed worktrees: before=%s after=%s", before, after)
	}
}

func finalizationControllerCandidateObject(fixture finalizationControllerFixture) string {
	return filepath.Join(fixture.storeDir, "evidence", "objects", fixture.ref.Digest[:2], fixture.ref.Digest)
}

func observeFinalizationCheckoutCreation(t *testing.T) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "checkout-creation-attempted")
	t.Setenv("FINALIZATION_REAL_GIT", git)
	t.Setenv("FINALIZATION_CHECKOUT_MARKER", marker)
	dir := t.TempDir()
	if err := execfile.Write(filepath.Join(dir, "git"), []byte(`#!/bin/sh
set -eu
case " $* " in
  *" worktree add "*) touch "$FINALIZATION_CHECKOUT_MARKER" ;;
esac
exec "$FINALIZATION_REAL_GIT" "$@"
`), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return marker
}

func assertFinalizationCheckoutNotCreated(t *testing.T, marker string) {
	t.Helper()
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("rejected request attempted to create a validation checkout: %v", err)
	}
}

func finalizationControllerWorktrees(t *testing.T, repo string) string {
	t.Helper()
	return finalizationControllerGitOutput(t, repo, "worktree", "list", "--porcelain")
}

func finalizationControllerGitOutput(t *testing.T, repo string, args ...string) string {
	t.Helper()
	output, err := gitFinalizationOutput(repo, args...)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(output)
}
