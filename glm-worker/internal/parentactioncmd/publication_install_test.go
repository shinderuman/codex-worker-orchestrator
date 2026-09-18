package parentactioncmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/cliinstall"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestPublicationCandidateInstallPersistsExactCandidateEvidence(t *testing.T) {
	cfg, st, candidate := preparePublicationRuntimeCandidate(t)
	writePublicationInstalledWorkerStub(t, candidate.CommitOID)

	output := installPublicationCandidate(cfg, st)
	if output.Status != publicationInstallStatusInstalled || !output.Required || output.CandidateOID != candidate.CommitOID || output.Failure != nil {
		t.Fatalf("install output = %#v", output)
	}
	evidence, err := st.LoadRuntimeInstallEvidence()
	if err != nil {
		t.Fatal(err)
	}
	if evidence.TaskID != candidate.TaskID || evidence.Head != candidate.CommitOID || evidence.InstalledRevision != candidate.CommitOID ||
		evidence.SmokeResult != state.ValidationResultPass {
		t.Fatalf("install evidence = %#v", evidence)
	}

	repeated := installPublicationCandidate(cfg, st)
	if repeated.Status != publicationInstallStatusInstalled || repeated.CandidateOID != candidate.CommitOID || repeated.Failure != nil {
		t.Fatalf("repeated install = %#v", repeated)
	}
}

func TestPublicationCandidateInstallRejectsMutationBeforeInstall(t *testing.T) {
	cfg, st, candidate := preparePublicationRuntimeCandidate(t)
	if err := os.WriteFile(cfg.RepoRoot+"/"+installScriptName, []byte("#!/bin/sh\nexit 0\n# mutated\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	output := installPublicationCandidate(cfg, st)
	if output.Status != publicationInstallStatusBlocked || output.CandidateOID != candidate.CommitOID || output.Failure == nil ||
		output.Failure.Reason != publicationFailureCandidateStale {
		t.Fatalf("mutated install = %#v", output)
	}
}

func TestPublicationFailedInstallBlocksPromotionPushAndComplete(t *testing.T) {
	cfg, st, candidate := preparePublicationRuntimeCandidate(t)
	baseHead := publicationGitOutput(t, cfg.RepoRoot, "rev-parse", "HEAD")
	writePublicationInstalledWorkerStubResult(t, candidate.CommitOID, state.ValidationResultFail)

	installed := installPublicationCandidate(cfg, st)
	if installed.Status != publicationInstallStatusFailed || installed.Failure == nil {
		t.Fatalf("failed install = %#v", installed)
	}
	promoted := promotePublicationCandidate(cfg, st)
	if promoted.Status != publicationPromotionStatusBlocked || promoted.Failure == nil {
		t.Fatalf("failed install promotion = %#v", promoted)
	}
	if got := publicationGitOutput(t, cfg.RepoRoot, "rev-parse", "HEAD"); got != baseHead {
		t.Fatalf("failed install advanced HEAD: %s != %s", got, baseHead)
	}
	remote := applyPublicationRemoteWriteGuardForTask(cfg, st, publicationRemoteWriteFixture(baseHead, false))
	if remote.Status != "blocked" || remote.RemoteWrite != nil || remote.Failure == nil {
		t.Fatalf("failed install remote write = %#v", remote)
	}
	if failure := verifyPublicationCompletionGate(cfg, st); failure == nil || failure.Reason != publicationFailureGateMissing {
		t.Fatalf("failed install completion gate = %#v", failure)
	}
}

func TestPublicationSuccessfulRuntimeCandidatePromotesAndAuthorizesSameOID(t *testing.T) {
	cfg, st, candidate := preparePublicationRuntimeCandidate(t)
	writePublicationInstalledWorkerStub(t, candidate.CommitOID)

	installed := installPublicationCandidate(cfg, st)
	if installed.Status != publicationInstallStatusInstalled || installed.Failure != nil {
		t.Fatalf("install = %#v", installed)
	}
	promoted := promotePublicationCandidate(cfg, st)
	if promoted.Status != publicationPromotionStatusPromoted || promoted.CandidateOID != candidate.CommitOID || promoted.Failure != nil {
		t.Fatalf("promotion = %#v", promoted)
	}
	remote := applyPublicationRemoteWriteGuardForTask(cfg, st, publicationRemoteWriteFixture(candidate.CommitOID, true))
	if remote.Status == "blocked" || remote.Failure != nil || remote.RemoteWrite == nil || remote.RemoteWrite.Authorization != pushBindingAuthorizationPublication {
		t.Fatalf("remote authorization = %#v", remote)
	}
	if failure := verifyPublicationCompletionGate(cfg, st); failure != nil {
		t.Fatalf("completion = %#v", failure)
	}
}

func writePublicationInstalledWorkerStub(t *testing.T, candidateOID string) {
	t.Helper()
	writePublicationInstalledWorkerStubResult(t, candidateOID, state.ValidationResultPass)
}

func writePublicationInstalledWorkerStubResult(t *testing.T, candidateOID, smokeResult string) {
	t.Helper()
	worker, err := exec.LookPath("glm-worker")
	if err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Dir(worker)
	buildDir := t.TempDir()
	for _, name := range []string{"glm-parent-action", "glm-codex-context", "commentlint", "harnesslint"} {
		if err := os.WriteFile(filepath.Join(buildDir, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script := fmt.Sprintf(`#!/bin/sh
case "${1:-}" in
--status)
  printf '%%s\n' '{"runtime_build":{"vcs_revision":"%s","vcs_modified":false,"repository_head":"%s","relationship":"same"}}'
  ;;
--install-smoke)
  printf '%%s\n' '{"status":"executed","result":"%s","role":"parent","duration_ms":1}'
  ;;
*)
  exit 2
  ;;
esac
`, candidateOID, candidateOID, smokeResult)
	if err := os.WriteFile(filepath.Join(buildDir, "glm-worker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := cliinstall.Install(buildDir, binDir); err != nil {
		t.Fatal(err)
	}
}
