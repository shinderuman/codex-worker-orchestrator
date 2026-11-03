package app

import (
	"context"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDockerGatePlanDoesNotRecordHostAsLinuxAttestation(t *testing.T) {
	root := t.TempDir()
	module := filepath.Join(root, "glm-worker")
	if err := os.Mkdir(module, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "quality-tools.yml"), []byte("go: 1.25.4\ngo-test-macos-backend: docker-linux-arm64\n"), 0600); err != nil {
		t.Fatal(err)
	}
	shim, _ := writeQualityGateGoShim(t, filepath.Join(t.TempDir(), "no-failure"))
	t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))
	identity := qualityGateStartIdentity{Repository: root, WorkingDir: module, GoArgs: qualitygate.GoTestArguments("go-test")}
	plan, toolchain, err := newQualityGateExecution(identity, "1.25.4")
	if runtime.GOOS == "darwin" && runtime.GOARCH != "arm64" {
		if err == nil {
			t.Fatal("unsupported host accepted")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "darwin" {
		if plan != nil {
			t.Fatal("Linux CI must keep native execution")
		}
		return
	}
	if plan == nil || plan.Backend != qualitygate.BackendDockerARM64 || plan.HostToolchain.GOOS != "darwin" {
		t.Fatalf("host plan: %#v", plan)
	}
	if toolchain.EnvironmentRecorded || toolchain.GoVersion != "" || toolchain.GOOS != "" {
		t.Fatalf("unexecuted container falsely attested: %#v", toolchain)
	}
	if toolchain.RequiredGoVersion != "1.25.4" {
		t.Fatal("required Go contract lost")
	}
}

func TestDarwinQualitySupplementAttributesToolchainMismatchAsFail(t *testing.T) {
	_, st := newQualityGateEnv(t)
	shimDir := t.TempDir()
	invocationLog := filepath.Join(t.TempDir(), "go-invocations.log")
	shim := "#!/bin/sh\n" +
		"if [ \"$1\" = env ]; then\n" +
		"  printf '%s\\n' '{\"GOVERSION\":\"go1.26.1\",\"GOOS\":\"darwin\",\"GOARCH\":\"arm64\",\"GOCACHE\":\"/fixture/cache\",\"GOFLAGS\":\"\"}'\n" +
		"  exit 0\n" +
		"fi\n" +
		"printf '%s\\n' \"$*\" >>'" + invocationLog + "'\n" +
		"exit 0\n"
	if err := execfile.Write(filepath.Join(shimDir, "go"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	record := &qualitygate.RunRecord{
		Form:       "go-test",
		WorkingDir: t.TempDir(),
		Toolchain:  &qualitygate.RunToolchain{RequiredGoVersion: "1.25.4"},
		Execution:  &qualitygate.Execution{Backend: qualitygate.BackendDockerARM64},
	}
	err := runDarwinQualitySupplement(context.Background(), st, record)
	if err == nil || !strings.Contains(err.Error(), "Darwin補完toolchainを利用できません") {
		t.Fatalf("Darwin補完toolchain不一致がerrorになりませんでした: %v", err)
	}
	supplement := record.Execution.NativeSupplement
	if supplement == nil || supplement.Status != qualitygate.StatusFail || supplement.ExitCode != 1 || supplement.ExitSource != state.ValidationExitSourceWrapper {
		t.Fatalf("toolchain不一致のsupplement終了帰属が未確定です: %#v", supplement)
	}
	if supplement.Log != "" || supplement.DurationMS != 0 {
		t.Fatalf("未開始コマンドの実行証拠が記録されました: %#v", supplement)
	}
	if _, statErr := os.Stat(invocationLog); !os.IsNotExist(statErr) {
		t.Fatalf("toolchain不一致でも補完コマンドが開始されました: %v", statErr)
	}
}
