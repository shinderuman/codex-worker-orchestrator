package qualitygate

import (
	"encoding/json"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveMacBackendPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, config, host, arch, want string
		fail                           bool
	}{
		{"undeclared", "go: 1.25.4\n", "darwin", "arm64", BackendNative, false},
		{"darwin", "go-test-macos-backend: docker-linux-arm64\n", "darwin", "arm64", BackendDockerARM64, false},
		{"ci", "go-test-macos-backend: docker-linux-arm64\n", "linux", "amd64", BackendNative, false},
		{"invalid", "go-test-macos-backend: native\n", "darwin", "arm64", "", true},
		{"empty", "go-test-macos-backend:\n", "linux", "arm64", "", true},
		{"duplicate", "go-test-macos-backend: docker-linux-arm64\ngo-test-macos-backend: docker-linux-arm64\n", "darwin", "arm64", "", true},
		{"intel", "go-test-macos-backend: docker-linux-arm64\n", "darwin", "amd64", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, QualityToolsFile), []byte(tc.config), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := ResolveBackend(root, tc.host, tc.arch)
			if (err != nil) != tc.fail || got != tc.want {
				t.Fatalf("backend=%s error=%v", got, err)
			}
		})
	}
}

func darwinCoverageLog(t *testing.T) []byte {
	t.Helper()
	var result []byte
	for _, name := range []string{"TestConfinementProfileBoundsHostCredentialReads", "TestConfinementRuntimeDeniesCredentialReads", "TestRunIsolatedGoTestConfinementBlocksWritesOutsideAllowedRoots", "TestConfinementAdmissionDeclaresPlatformSupport"} {
		line, err := json.Marshal(map[string]string{"Action": "pass", "Test": name, "Package": "example/internal/observationexec"})
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, append(line, '\n')...)
	}
	return result
}

func completeDockerExecution(t *testing.T) RunRecord {
	t.Helper()
	unchanged := false
	zero := 0
	linux := &RunToolchain{Backend: BackendDockerARM64, RequiredGoVersion: "1.25.4", GoVersion: "go1.25.4", GOOS: "linux", GOARCH: "arm64", GoCache: "/cache", Parallelism: 8, EnvironmentRecorded: true, TestCache: "mixed", Command: "go " + strings.Join(GoTestArguments("go-test-race"), " ")}
	host := *linux
	host.GOOS = "darwin"
	host.Backend = BackendNative
	native := host
	native.Command = "go " + strings.Join(DarwinSupplementArguments("go-test-race"), " ")
	log := filepath.Join(t.TempDir(), "darwin.log")
	if err := os.WriteFile(log, darwinCoverageLog(t), 0600); err != nil {
		t.Fatal(err)
	}
	return RunRecord{Form: "go-test-race", Head: "head", IndexDigest: "index", WorktreeDigest: "work", Toolchain: linux, Execution: &Execution{Backend: BackendDockerARM64, HostToolchain: &host, Docker: &DockerExecution{TargetExitCode: &zero, Status: StatusPass, SourceChanged: &unchanged, SourceHead: "head", IndexDigest: "index", WorktreeDigest: "work", ArchiveSHA256: strings.Repeat("a", 64), ImageID: "sha256:image", CacheVolume: "cache", CPUs: 8, MemoryBytes: 1024}, NativeSupplement: &NativeSupplement{Toolchain: &native, Arguments: DarwinSupplementArguments("go-test-race"), Status: StatusPass, ExitSource: state.ValidationExitSourceTarget, Log: log}}}
}

func TestDockerPassRequiresBothPlatformsAndMatchingSource(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*RunRecord)
	}{
		{"valid", func(*RunRecord) {}},
		{"missing execution", func(r *RunRecord) { r.Execution = nil }},
		{"wrong native execution", func(r *RunRecord) { r.Execution.Backend = BackendNative }},
		{"wrong native Go contract", func(r *RunRecord) {
			r.Execution.NativeSupplement.Toolchain.RequiredGoVersion = "1.27.0"
			r.Execution.NativeSupplement.Toolchain.GoVersion = "go1.27.0"
		}},
		{"missing native", func(r *RunRecord) { r.Execution.NativeSupplement = nil }},
		{"failed native", func(r *RunRecord) { r.Execution.NativeSupplement.Status = StatusFail }},
		{"wrong race", func(r *RunRecord) { r.Execution.NativeSupplement.Arguments = DarwinSupplementArguments("go-test") }},
		{"wrong primary command", func(r *RunRecord) { r.Toolchain.Command = "go test ./internal/app" }},
		{"host recorded as Linux", func(r *RunRecord) { r.Execution.HostToolchain.GOOS = "linux" }},
		{"source drift", func(r *RunRecord) { r.Execution.Docker.WorktreeDigest = "other" }},
		{"missing attestation", func(r *RunRecord) { r.Toolchain.EnvironmentRecorded = false }},
		{"unknown cache", func(r *RunRecord) { r.Toolchain.TestCache = "unknown" }},
		{"missing target exit", func(r *RunRecord) { r.Execution.Docker.TargetExitCode = nil }},
		{"Docker failed", func(r *RunRecord) { r.Execution.Docker.Status = StatusFail }},
		{"Darwin skipped", func(r *RunRecord) {
			if err := os.WriteFile(r.Execution.NativeSupplement.Log, []byte("{\"Action\":\"skip\"}\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := completeDockerExecution(t)
			tc.change(&r)
			err := VerifyExecution(r)
			if (err == nil) != (tc.name == "valid") {
				t.Fatalf("verification error=%v", err)
			}
		})
	}
}
