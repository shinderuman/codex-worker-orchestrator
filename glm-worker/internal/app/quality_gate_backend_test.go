package app

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"os"
	"path/filepath"
	"runtime"
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
