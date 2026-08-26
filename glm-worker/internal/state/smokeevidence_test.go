package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func initSmokeRepo(t *testing.T) string {
	t.Helper()
	dir := initCommittedRepo(t)
	for _, path := range []string{
		"install.sh",
		"tests/install_smoke.sh",
		"codex/instructions/glm-execution.md",
		"claude/settings-managed.json",
		"glm-worker/go.mod",
		"tools/merge-json/go.mod",
	} {
		abs := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(path+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "--quiet", "-m", "smoke inputs")
	return dir
}

func TestCaptureSmokeTreeDigestAxes(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(t *testing.T, dir string)
		wantMatch bool
	}{
		{
			name:      "parent-managed metadata-only change keeps digest",
			mutate:    mutateSmokeParentPlan,
			wantMatch: true,
		},
		{
			name:      "parent-managed task file change keeps digest",
			mutate:    mutateSmokeParentTask,
			wantMatch: true,
		},
		{
			name:      "install.sh change breaks digest",
			mutate:    mutateSmokeInstaller,
			wantMatch: false,
		},
		{
			name:      "smoke script change breaks digest",
			mutate:    mutateSmokeScript,
			wantMatch: false,
		},
		{
			name:      "untracked new source file breaks digest",
			mutate:    mutateSmokeUntracked,
			wantMatch: false,
		},
		{
			name:      "tracked file deletion breaks digest",
			mutate:    mutateSmokeDeletion,
			wantMatch: false,
		},
		{
			name:      "exec bit change breaks digest",
			mutate:    mutateSmokeExecBit,
			wantMatch: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := initSmokeRepo(t)
			before, err := CaptureSmokeTreeDigest(dir)
			if err != nil {
				t.Fatal(err)
			}
			tt.mutate(t, dir)
			after, err := CaptureSmokeTreeDigest(dir)
			if err != nil {
				t.Fatal(err)
			}
			if matched := before == after; matched != tt.wantMatch {
				t.Errorf("tree digest match=%v want %v (before=%s after=%s)", matched, tt.wantMatch, before, after)
			}
		})
	}
}

func mutateSmokeParentPlan(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ParentPlanFile), []byte("# changed plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mutateSmokeParentTask(t *testing.T, dir string) {
	t.Helper()
	tasks := filepath.Join(dir, ParentTasksDir)
	if err := os.MkdirAll(tasks, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tasks, "new-task.md"), []byte("# Task: new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mutateSmokeInstaller(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "install.sh"), []byte("changed installer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mutateSmokeScript(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "tests", "install_smoke.sh"), []byte("changed smoke\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mutateSmokeUntracked(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "glm-worker", "untracked.go"), []byte("package extra\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mutateSmokeDeletion(t *testing.T, dir string) {
	t.Helper()
	if err := os.Remove(filepath.Join(dir, "claude", "settings-managed.json")); err != nil {
		t.Fatal(err)
	}
}

func mutateSmokeExecBit(t *testing.T, dir string) {
	t.Helper()
	target := filepath.Join(dir, "tests", "install_smoke.sh")
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestSmokeTreeDigestCommitInvariance(t *testing.T) {
	dir := initSmokeRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, "glm-worker", "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "glm-worker", "internal", "feature.go"), []byte("package internal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	uncommitted, err := CaptureSmokeTreeDigest(dir)
	if err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "--quiet", "-m", "candidate")
	committed, err := CaptureSmokeTreeDigest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if uncommitted != committed {
		t.Errorf("tree digest must be commit-invariant: uncommitted=%s committed=%s", uncommitted, committed)
	}
}

func TestSmokeTreeDigestIgnoresGitIgnoredFiles(t *testing.T) {
	dir := initSmokeRepo(t)
	before, err := CaptureSmokeTreeDigest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored-output.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".gitignore")
	gitRun(t, dir, "commit", "--quiet", "-m", "gitignore")
	baseline, err := CaptureSmokeTreeDigest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored-output.txt"), []byte("ignored\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := CaptureSmokeTreeDigest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if before == baseline {
		t.Fatal("gitignore addition must be visible in the digest")
	}
	if baseline != after {
		t.Errorf("git-ignored file must not change the digest: baseline=%s after=%s", baseline, after)
	}
}

func TestCaptureSmokeInputDigestAxes(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		content   string
		wantMatch bool
	}{
		{name: "install.sh", path: "install.sh", content: "installer change\n", wantMatch: false},
		{name: "smoke script", path: "tests/install_smoke.sh", content: "smoke change\n", wantMatch: false},
		{name: "codex instruction", path: "codex/instructions/glm-execution.md", content: "instruction change\n", wantMatch: false},
		{name: "unrelated doc", path: "README.md", content: "doc change\n", wantMatch: true},
		{name: "parent plan", path: "IMPLEMENTATION_PLAN.local.md", content: "# plan\n", wantMatch: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := initSmokeRepo(t)
			before, err := CaptureSmokeInputDigest(dir)
			if err != nil {
				t.Fatal(err)
			}
			abs := filepath.Join(dir, filepath.FromSlash(tt.path))
			if err := os.WriteFile(abs, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}
			after, err := CaptureSmokeInputDigest(dir)
			if err != nil {
				t.Fatal(err)
			}
			if matched := before == after; matched != tt.wantMatch {
				t.Errorf("input digest match=%v want %v (before=%s after=%s)", matched, tt.wantMatch, before, after)
			}
		})
	}
}

func TestCaptureSmokeEnvironmentClassifiesClaudeCLI(t *testing.T) {
	shimDir := t.TempDir()
	probeCases := []struct {
		name string
		body string
		want string
	}{
		{name: "supported", body: "#!/bin/sh\nprintf '%s\\n' 'usage: claude [--json-schema]'\n", want: SmokeClaudeProbeSupported},
		{name: "rejected", body: "#!/bin/sh\nprintf '%s\\n' 'usage: claude'\n", want: SmokeClaudeProbeRejected},
	}
	for _, probe := range probeCases {
		t.Run(probe.name, func(t *testing.T) {
			shim := filepath.Join(shimDir, "claude-"+probe.name)
			if err := os.WriteFile(shim, []byte(probe.body), 0o755); err != nil {
				t.Fatal(err)
			}
			if got := probeSmokeClaudeCLI(shim); got != probe.want {
				t.Errorf("probe=%q want %q", got, probe.want)
			}
		})
	}
	t.Run("missing", func(t *testing.T) {
		if got := probeSmokeClaudeCLI(filepath.Join(shimDir, "claude-absent")); got != SmokeClaudeProbeMissing {
			t.Errorf("probe=%q want %q", got, SmokeClaudeProbeMissing)
		}
	})
}

func smokeIdentityFixture() SmokeIdentity {
	return SmokeIdentity{
		TreeDigest:       "tree-1",
		SmokeInputDigest: "input-1",
		Environment: SmokeEnvironment{
			GoVersion: "go version go1.24.0 darwin/arm64",
			GOOS:      "darwin",
			GOARCH:    "arm64",
			Platform:  "Darwin arm64",
			ClaudeCLI: SmokeClaudeProbeSupported,
		},
	}
}

func TestDecideSmokeReuseMatrix(t *testing.T) {
	current := smokeIdentityFixture()
	passRecord := SmokeEvidenceRecord{Result: SmokeResultPass, Identity: current, CompletedAt: time.Now().UTC()}
	tests := []struct {
		name       string
		records    []SmokeEvidenceRecord
		reusable   bool
		wantReason string
	}{
		{
			name:       "no evidence",
			records:    nil,
			reusable:   false,
			wantReason: "no-evidence",
		},
		{
			name:       "matching pass is reusable",
			records:    []SmokeEvidenceRecord{passRecord},
			reusable:   true,
			wantReason: "identity-match",
		},
		{
			name: "latest matching failure blocks older pass",
			records: []SmokeEvidenceRecord{
				passRecord,
				{Result: SmokeResultFail, Identity: current},
			},
			reusable:   false,
			wantReason: "latest-matching-evidence-failed",
		},
		{
			name: "stale tree digest",
			records: []SmokeEvidenceRecord{
				{Result: SmokeResultPass, Identity: withTreeDigest(current, "tree-0")},
			},
			reusable:   false,
			wantReason: "stale:tree_digest",
		},
		{
			name: "stale environment",
			records: []SmokeEvidenceRecord{
				{Result: SmokeResultPass, Identity: withEnvironment(current, SmokeEnvironment{
					GoVersion: "go version go1.23.0 darwin/arm64",
					GOOS:      "darwin",
					GOARCH:    "arm64",
					Platform:  "Darwin arm64",
					ClaudeCLI: SmokeClaudeProbeSupported,
				})},
			},
			reusable:   false,
			wantReason: "stale:environment.go_version",
		},
		{
			name: "failure for different identity is stale not blocking",
			records: []SmokeEvidenceRecord{
				{Result: SmokeResultFail, Identity: withTreeDigest(current, "tree-0")},
			},
			reusable:   false,
			wantReason: "stale:tree_digest",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := DecideSmokeReuse(tt.records, current)
			if decision.Reusable != tt.reusable {
				t.Errorf("reusable=%v want %v", decision.Reusable, tt.reusable)
			}
			if decision.Reason != tt.wantReason {
				t.Errorf("reason=%q want %q", decision.Reason, tt.wantReason)
			}
			if decision.Reusable && decision.Record == nil {
				t.Error("reusable decision must carry the record")
			}
		})
	}
}

func withTreeDigest(identity SmokeIdentity, digest string) SmokeIdentity {
	identity.TreeDigest = digest
	return identity
}

func withEnvironment(identity SmokeIdentity, environment SmokeEnvironment) SmokeIdentity {
	identity.Environment = environment
	return identity
}

func TestSmokeEvidenceLedgerRoundTrip(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if records, err := st.ReadSmokeEvidence(); err != nil || records != nil {
		t.Fatalf("missing ledger must read as empty: records=%v err=%v", records, err)
	}
	record := SmokeEvidenceRecord{
		Result:      SmokeResultPass,
		ExitCode:    0,
		Role:        "worker",
		StartedAt:   time.Now().UTC(),
		CompletedAt: time.Now().UTC(),
		DurationMS:  302000,
		Identity:    smokeIdentityFixture(),
	}
	if err := st.AppendSmokeEvidence(record); err != nil {
		t.Fatal(err)
	}
	fail := record
	fail.Result = SmokeResultFail
	fail.ExitCode = 1
	fail.Role = "reviewer"
	if err := st.AppendSmokeEvidence(fail); err != nil {
		t.Fatal(err)
	}
	records, err := st.ReadSmokeEvidence()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("records=%d want 2", len(records))
	}
	if records[0].Result != SmokeResultPass || records[0].Role != "worker" {
		t.Errorf("first record=%+v", records[0])
	}
	if records[1].Result != SmokeResultFail || records[1].ExitCode != 1 {
		t.Errorf("second record=%+v", records[1])
	}
	if !records[0].Identity.Matches(smokeIdentityFixture()) {
		t.Error("identity must survive the ledger round trip")
	}
}

func TestSmokeEvidenceLedgerRejectsCorruption(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	path := st.Path(smokeEvidenceFile)
	if err := os.WriteFile(path, []byte("{broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReadSmokeEvidence(); err == nil {
		t.Error("corrupt ledger must fail closed")
	}
	if err := os.WriteFile(path, []byte(`{"version":99,"result":"pass"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReadSmokeEvidence(); err == nil {
		t.Error("unknown ledger version must fail closed")
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"result":"maybe"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReadSmokeEvidence(); err == nil {
		t.Error("unknown result must fail closed")
	}
}
