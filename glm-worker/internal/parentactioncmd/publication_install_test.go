package parentactioncmd

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

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

func writePublicationInstalledWorkerStub(t *testing.T, candidateOID string) {
	t.Helper()
	worker, err := exec.LookPath("glm-worker")
	if err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf(`#!/bin/sh
case "${1:-}" in
--status)
  printf '%%s\n' '{"runtime_build":{"vcs_revision":"%s","vcs_modified":false,"repository_head":"%s","relationship":"same"}}'
  ;;
--install-smoke)
  printf '%%s\n' '{"status":"executed","result":"pass","role":"parent","duration_ms":1}'
  ;;
*)
  exit 2
  ;;
esac
`, candidateOID, candidateOID)
	if err := os.WriteFile(worker, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}
