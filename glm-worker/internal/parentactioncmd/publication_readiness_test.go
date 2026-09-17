package parentactioncmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestPublicationReadinessBlocksRequiredInstallWithoutCandidateEvidence(t *testing.T) {
	cfg, st, candidate := preparePublicationRuntimeCandidate(t)
	output := projectPublicationReadiness(cfg, st)
	if output.Status != publicationReadinessBlocked || output.CandidateOID != candidate.CommitOID {
		t.Fatalf("readiness = %#v", output)
	}
	gate := publicationGateNamed(t, output.Gates, "runtime-install")
	if !gate.Required || gate.Status != publicationGateMissing {
		t.Fatalf("runtime install gate = %#v", gate)
	}
}

func TestPublicationReadinessAllowsMetadataOnlyCandidateWithoutInstall(t *testing.T) {
	cfg, st := newInstallActionRepo(t)
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.RepoRoot, "README.md"), []byte("candidate metadata\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	publicationGit(t, cfg.RepoRoot, "add", "README.md")
	candidate, failure := preparePublicationCandidate(cfg, st, "metadata publication")
	if failure != nil {
		t.Fatalf("prepare failed: %#v", failure)
	}

	output := projectPublicationReadiness(cfg, st)
	if output.Status != publicationReadinessReady || output.CandidateOID != candidate.CommitOID || output.Failure != nil {
		t.Fatalf("readiness = %#v", output)
	}
	gate := publicationGateNamed(t, output.Gates, "runtime-install")
	if gate.Required || gate.Status != publicationGatePass {
		t.Fatalf("metadata runtime install gate = %#v", gate)
	}
}

func TestPublicationReadinessRejectsSourceMutationAfterPrepare(t *testing.T) {
	cfg, st, candidate := preparePublicationRuntimeCandidate(t)
	if err := os.WriteFile(filepath.Join(cfg.RepoRoot, installScriptName), []byte("#!/bin/sh\nexit 0\n# mutated after prepare\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	output := projectPublicationReadiness(cfg, st)
	if output.Status != publicationReadinessBlocked || output.CandidateOID != candidate.CommitOID || output.Failure == nil ||
		output.Failure.Reason != publicationFailureCandidateStale {
		t.Fatalf("mutated readiness = %#v", output)
	}
	gate := publicationGateNamed(t, output.Gates, "candidate-source")
	if gate.Status != publicationGateStale {
		t.Fatalf("source gate = %#v", gate)
	}
}

func TestVerifyPublicationQualityRunRejectsSnapshotTamper(t *testing.T) {
	cfg, st, candidate := preparePublicationRuntimeCandidate(t)
	runID := strings.Repeat("a", 32)
	completed := time.Now().UTC()
	dir := filepath.Join(st.Path("quality-gate-runs"), runID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "gate.log")
	if err := os.WriteFile(logPath, []byte("pass\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	record := publicationQualityRunRecord{
		ValidationRunID:               runID,
		Form:                          "go-test",
		Repository:                    cfg.RepoRoot,
		Head:                          candidate.Snapshot.Head,
		IndexDigest:                   candidate.Snapshot.IndexDigest,
		WorktreeDigest:                candidate.Snapshot.WorktreeDigest,
		WorktreeDigestExcludingParent: candidate.Snapshot.WorktreeDigestExcludingParent,
		TaskID:                        candidate.TaskID,
		CompletedAt:                   &completed,
		Status:                        publicationGatePass,
		ExitCode:                      0,
		ExitSource:                    state.ValidationExitSourceTarget,
		Log:                           logPath,
	}
	writePublicationQualityRun(t, filepath.Join(dir, "run.json"), record)
	if err := verifyPublicationQualityRun(st, cfg.RepoRoot, candidate, record.Form, runID); err != nil {
		t.Fatalf("matching run rejected: %v", err)
	}

	record.IndexDigest = strings.Repeat("f", 64)
	writePublicationQualityRun(t, filepath.Join(dir, "run.json"), record)
	if err := verifyPublicationQualityRun(st, cfg.RepoRoot, candidate, record.Form, runID); err == nil || !strings.Contains(err.Error(), "snapshot") {
		t.Fatalf("tampered run accepted: %v", err)
	}
}

func preparePublicationRuntimeCandidate(t *testing.T) (config.AppConfig, *state.StateStore, state.PublicationCandidate) {
	t.Helper()
	cfg, st := newInstallActionRepo(t)
	cfg.WorktreeBase = t.TempDir()
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.RepoRoot, installScriptName), []byte("#!/bin/sh\n# candidate runtime\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	publicationGit(t, cfg.RepoRoot, "add", installScriptName)
	candidate, failure := preparePublicationCandidate(cfg, st, "runtime publication")
	if failure != nil {
		t.Fatalf("prepare failed: %#v", failure)
	}
	return cfg, st, candidate
}

func publicationGateNamed(t *testing.T, gates []publicationGateProjection, name string) publicationGateProjection {
	t.Helper()
	for _, gate := range gates {
		if gate.Gate == name {
			return gate
		}
	}
	t.Fatalf("gate %q not found in %#v", name, gates)
	return publicationGateProjection{}
}

func writePublicationQualityRun(t *testing.T, path string, record publicationQualityRunRecord) {
	t.Helper()
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
