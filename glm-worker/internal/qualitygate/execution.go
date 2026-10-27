package qualitygate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

type DockerExecution struct {
	TargetExitCode *int             `json:"target_exit_code"`
	ImageID        string           `json:"image_id"`
	GitVersion     string           `json:"git_version"`
	ArchiveSHA256  string           `json:"archive_sha256"`
	SourceHead     string           `json:"source_head"`
	IndexDigest    string           `json:"index_digest"`
	WorktreeDigest string           `json:"worktree_digest"`
	CacheVolume    string           `json:"cache_volume"`
	CPUs           int              `json:"cpus"`
	MemoryBytes    int64            `json:"memory_bytes"`
	PreparationMS  int64            `json:"preparation_ms"`
	TestMS         int64            `json:"test_ms"`
	TotalMS        int64            `json:"total_ms"`
	PhasesMS       map[string]int64 `json:"phases_ms"`
	SourceChanged  *bool            `json:"source_changed"`
	Status         string           `json:"status"`
}

type NativeSupplement struct {
	Toolchain  *RunToolchain `json:"toolchain,omitempty"`
	Arguments  []string      `json:"argv"`
	Status     string        `json:"status"`
	ExitCode   int           `json:"exit_code"`
	ExitSource string        `json:"exit_source"`
	DurationMS int64         `json:"duration_ms"`
	Log        string        `json:"log,omitempty"`
}

type Execution struct {
	Backend          string            `json:"backend"`
	HostToolchain    *RunToolchain     `json:"host_toolchain,omitempty"`
	Docker           *DockerExecution  `json:"docker,omitempty"`
	NativeSupplement *NativeSupplement `json:"native_supplement,omitempty"`
}

type TargetExitError struct{ Code int }

const BackendNative = "native"
const BackendDockerARM64 = "docker-linux-arm64"

const MacOSBackendKey = "go-test-macos-backend"
const NativeSupplementLog = "darwin-supplement.log"

const platformDarwin = "darwin"
const platformARM64 = "arm64"
const testCacheExecuted = "executed"

const formGoTest = "go-test"
const formGoTestRace = "go-test-race"

func ResolveBackend(repository, hostOS, hostArch string) (string, error) {
	data, err := os.ReadFile(filepath.Join(repository, QualityToolsFile))
	if err != nil {
		return "", err
	}
	configured := ""
	seen := false
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if key != MacOSBackendKey {
			continue
		}
		if seen || !ok {
			return "", fmt.Errorf("duplicate or malformed %s", MacOSBackendKey)
		}
		seen = true
		configured = strings.TrimSpace(value)
	}
	if !seen {
		return BackendNative, nil
	}
	if configured != BackendDockerARM64 {
		return "", fmt.Errorf("unsupported %s: %q", MacOSBackendKey, configured)
	}
	if hostOS != platformDarwin {
		return BackendNative, nil
	}
	if hostArch != platformARM64 {
		return "", fmt.Errorf("正規Docker gateにはDarwin ARM64 hostが必要です")
	}
	return BackendDockerARM64, nil
}

func DockerBackendActive(repository string) (bool, error) {
	if runtime.GOOS != platformDarwin || runtime.GOARCH != platformARM64 {
		return false, nil
	}
	if _, err := os.Stat(filepath.Join(repository, QualityToolsFile)); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	backend, err := ResolveBackend(repository, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return false, err
	}
	return backend == BackendDockerARM64, nil
}

func GoTestArguments(form string) []string {
	args := []string{"test"}
	switch form {
	case formGoTest:
	case formGoTestRace:
		args = append(args, "-race")
	default:
		return nil
	}
	return append(args, "-json", "-timeout=45m", "./...")
}

func DarwinSupplementArguments(form string) []string {
	args := GoTestArguments(form)
	if len(args) > 0 {
		args[len(args)-1] = "./internal/observationexec"
	}
	return args
}

func VerifyExecution(record RunRecord) error {
	if record.Execution == nil || record.Execution.Backend == BackendNative {
		if record.Toolchain != nil && record.Toolchain.Backend == BackendDockerARM64 {
			return fmt.Errorf("Docker実行の補完証拠が欠落しています")
		}
		return nil
	}
	if err := verifyDockerExecution(record); err != nil {
		return err
	}
	return verifyNativeSupplement(record)
}

func verifyDockerExecution(record RunRecord) error {
	e := record.Execution
	if e.Backend != BackendDockerARM64 {
		return fmt.Errorf("正規execution backendが不明です")
	}
	expected := GoTestArguments(record.Form)
	if len(expected) == 0 {
		return fmt.Errorf("正規Go test形式が不明です")
	}
	if !hostToolchainMatches(e.HostToolchain) {
		return fmt.Errorf("container host toolchain証拠が不足しています")
	}
	if !linuxToolchainMatches(record.Toolchain) {
		return fmt.Errorf("Linuxの実toolchain証拠が不足しています")
	}
	if e.HostToolchain.RequiredGoVersion != record.Toolchain.RequiredGoVersion {
		return fmt.Errorf("hostとcontainerのGo version契約が一致しません")
	}
	if record.Toolchain.Command != "go "+strings.Join(expected, " ") {
		return fmt.Errorf("正規Dockerコマンドが形式に一致しません")
	}
	return verifyDockerSource(record)
}

func verifyDockerSource(record RunRecord) error {
	d := record.Execution.Docker
	if d == nil || d.Status != StatusPass || d.TargetExitCode == nil || *d.TargetExitCode != 0 || !dockerSourceMatches(d, record) || len(d.ArchiveSHA256) != 64 || d.ImageID == "" || d.CacheVolume == "" || d.CPUs < 1 || d.MemoryBytes < 1 {
		return fmt.Errorf("container source実行証拠が不足または不一致です")
	}
	return nil
}

func verifyNativeSupplement(record RunRecord) error {
	n := record.Execution.NativeSupplement
	if !nativeSupplementComplete(n) {
		return fmt.Errorf("macOS補完検証が未成立です")
	}
	if n.Toolchain.RequiredGoVersion != record.Toolchain.RequiredGoVersion {
		return fmt.Errorf("macOS補完のGo version契約が一致しません")
	}
	if !slices.Equal(n.Arguments, DarwinSupplementArguments(record.Form)) || n.Toolchain.Command != "go "+strings.Join(n.Arguments, " ") || !executionToolchainRecorded(n.Toolchain) {
		return fmt.Errorf("macOS補完検証が正規形式に一致しません")
	}
	data, err := os.ReadFile(n.Log)
	if err != nil {
		return fmt.Errorf("macOS補完logを取得できません: %w", err)
	}
	return VerifyDarwinCoverage(data)
}

func nativeSupplementComplete(n *NativeSupplement) bool {
	return n != nil && n.Status == StatusPass && n.ExitCode == 0 && n.ExitSource == state.ValidationExitSourceTarget && n.Toolchain != nil && n.Toolchain.MatchesRequiredGoVersion() && n.Toolchain.GOOS == platformDarwin && n.Toolchain.GOARCH == platformARM64 && n.Log != ""
}

func executionToolchainRecorded(t *RunToolchain) bool {
	return t != nil && t.EnvironmentRecorded && t.GoCache != "" && t.Parallelism > 0 && t.Command != "" && t.GoFlags == "" && (t.TestCache == testCacheExecuted || t.TestCache == "mixed" || t.TestCache == "cached")
}

func VerifyDarwinCoverage(log []byte) error {
	required := map[string]bool{"TestConfinementProfileBoundsHostCredentialReads": false, "TestConfinementRuntimeDeniesCredentialReads": false, "TestRunIsolatedGoTestConfinementBlocksWritesOutsideAllowedRoots": false, "TestConfinementAdmissionDeclaresPlatformSupport": false}
	for _, line := range bytes.Split(log, []byte("\n")) {
		var event struct {
			Action  string
			Test    string
			Package string
		}
		if json.Unmarshal(line, &event) != nil || !strings.HasSuffix(event.Package, "/internal/observationexec") {
			continue
		}
		if _, ok := required[event.Test]; ok && event.Action == "pass" {
			required[event.Test] = true
		}
	}
	for name, passed := range required {
		if !passed {
			return fmt.Errorf("required Darwin test did not PASS: %s", name)
		}
	}
	return nil
}

func hostToolchainMatches(t *RunToolchain) bool {
	return t != nil && t.MatchesRequiredGoVersion() && t.GOOS == platformDarwin && t.GOARCH == platformARM64
}

func dockerSourceMatches(d *DockerExecution, record RunRecord) bool {
	return d.SourceChanged != nil && !*d.SourceChanged && d.SourceHead == record.Head && d.IndexDigest == record.IndexDigest && d.WorktreeDigest == record.WorktreeDigest
}

func linuxToolchainMatches(t *RunToolchain) bool {
	return t != nil && t.Backend == BackendDockerARM64 && t.MatchesRequiredGoVersion() && t.GOOS == "linux" && t.GOARCH == platformARM64 && executionToolchainRecorded(t)
}

func (e *TargetExitError) Error() string {
	return fmt.Sprintf("go test target exited with code %d", e.Code)
}
func (e *TargetExitError) ExitCode() int { return e.Code }
