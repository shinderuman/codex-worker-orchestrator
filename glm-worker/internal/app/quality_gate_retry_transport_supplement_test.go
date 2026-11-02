package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestQualityGateFingerprintIgnoresOnlyAttemptTransport(t *testing.T) {
	record := qualitygate.RunRecord{Toolchain: &qualitygate.RunToolchain{RequiredGoVersion: "1.25.4"}}
	args := []string{"test", "-timeout=45m", "./..."}
	t.Setenv("GOTRACEBACK", "single")
	setQualityGateRetryAttemptTransport(t)
	initial, err := qualityGateInvocationDigest(record, args)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{state.WorkerProcessLaunchEnv, state.ParentActionAttemptIDEnv, state.ParentActionAttemptEnterEnv} {
		t.Setenv(name, os.Getenv(name)+"-changed")
		digest, err := qualityGateInvocationDigest(record, args)
		if err != nil || digest != initial {
			t.Fatalf("attempt transportがdigestへ混入: name=%s err=%v", name, err)
		}
		found := false
		for _, entry := range qualityGateEnv("1.25.4") {
			if entry == name+"="+os.Getenv(name) {
				found = true
			}
		}
		if !found {
			t.Fatalf("fingerprint処理が実行環境を変更: %s", name)
		}
	}
	t.Setenv("GOTRACEBACK", "system")
	diagnostic, err := qualityGateInvocationDigest(record, args)
	if err != nil || diagnostic == initial {
		t.Fatalf("有意な診断変更を無視: err=%v", err)
	}
}

func TestQualityGateDarwinSupplementFailureRetryAdmission(t *testing.T) {
	for _, tc := range []struct {
		name    string
		log     string
		missing bool
		repeat  bool
	}{
		{name: "stable target failure", log: "undefined: missingSymbol\n", repeat: true},
		{name: "network failure", log: "connection reset by peer\n"},
		{name: "timeout", log: "panic: test timed out\n"},
		{name: "resource failure", log: "resource temporarily unavailable\n"},
		{name: "missing log", missing: true},
		{name: "empty log"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, previous, candidate := qualityGateFailedRetryFixture(t)
			dockerDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dockerDir, "docker"), []byte("#!/bin/sh\ncase \"$1\" in\nimage) printf 'sha256:expected\\n';;\ninfo) printf '4 1000000\\n';;\n*) exit 1;;\nesac\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dockerDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			if err := os.WriteFile(filepath.Join(candidate.Repository, qualitygate.QualityToolsFile), []byte("lint-go: 1.25.4\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			candidate.Toolchain = &qualitygate.RunToolchain{RequiredGoVersion: "1.25.4"}
			candidate.Execution = &qualitygate.Execution{Backend: qualitygate.BackendDockerARM64}
			previous.Toolchain = candidate.Toolchain
			previous.Execution = &qualitygate.Execution{
				Backend:          qualitygate.BackendDockerARM64,
				Docker:           &qualitygate.DockerExecution{ImageID: "sha256:expected", CPUs: 4, MemoryBytes: 1000000},
				NativeSupplement: &qualitygate.NativeSupplement{Status: qualitygate.StatusFail, ExitCode: 1, ExitSource: state.ValidationExitSourceTarget},
			}
			if !tc.missing {
				previous.Execution.NativeSupplement.Log = writeUnstableQualityGateLog(t, tc.log)
			}
			if err := writeQualityGateRun(st, previous); err != nil {
				t.Fatal(err)
			}
			found, repeated, err := repeatedFailedQualityGateRun(st, candidate)
			if err != nil || repeated != tc.repeat || repeated && found.ValidationRunID != previous.ValidationRunID {
				t.Fatalf("Darwin補完の失敗原因を誤判定: repeated=%t want=%t err=%v", repeated, tc.repeat, err)
			}
			if _, err := os.Stat(st.Path(qualitygate.RunRelativePath(candidate.ValidationRunID))); !os.IsNotExist(err) {
				t.Fatalf("admissionだけで次のrunを作成: %v", err)
			}
		})
	}
}
