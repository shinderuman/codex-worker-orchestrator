package qualitygate

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type RunToolchain struct {
	Backend             string `json:"backend,omitempty"`
	GoVersion           string `json:"go_version,omitempty"`
	RequiredGoVersion   string `json:"required_go_version,omitempty"`
	GOOS                string `json:"goos,omitempty"`
	GOARCH              string `json:"goarch,omitempty"`
	GoCache             string `json:"go_cache,omitempty"`
	Parallelism         int    `json:"parallelism,omitempty"`
	Command             string `json:"command,omitempty"`
	GoFlags             string `json:"go_flags"`
	EnvironmentRecorded bool   `json:"environment_recorded"`
	TestCache           string `json:"test_cache,omitempty"`
}

type RunRecord struct {
	Execution                     *Execution    `json:"execution,omitempty"`
	ValidationRunID               string        `json:"validation_run_id"`
	Form                          string        `json:"form"`
	Repository                    string        `json:"repository"`
	WorkingDir                    string        `json:"working_dir"`
	Head                          string        `json:"head"`
	IndexDigest                   string        `json:"index_digest"`
	WorktreeDigest                string        `json:"worktree_digest"`
	WorktreeDigestExcludingParent string        `json:"worktree_digest_excluding_parent,omitempty"`
	IgnoredInputsAbsent           *bool         `json:"ignored_inputs_absent,omitempty"`
	TaskID                        string        `json:"task_id,omitempty"`
	ProbeID                       string        `json:"probe_id,omitempty"`
	ArgvDigest                    string        `json:"argv_digest,omitempty"`
	TaskAuthorityDigest           string        `json:"task_authority_digest,omitempty"`
	InstallEvidenceDigest         string        `json:"install_evidence_digest,omitempty"`
	Toolchain                     *RunToolchain `json:"toolchain,omitempty"`
	StartedAt                     time.Time     `json:"started_at"`
	CompletedAt                   *time.Time    `json:"completed_at,omitempty"`
	Status                        string        `json:"status"`
	RunnerPID                     int           `json:"runner_pid,omitempty"`
	ExitCode                      int           `json:"exit_code,omitempty"`
	ExitSource                    string        `json:"exit_source,omitempty"`
	DurationMS                    int64         `json:"duration_ms,omitempty"`
	Log                           string        `json:"log,omitempty"`
	TestTimings                   string        `json:"test_timings,omitempty"`
	ResourceSamples               string        `json:"resource_samples,omitempty"`
	ResourceSamplesError          string        `json:"resource_samples_error,omitempty"`
}

const (
	RunDirectory      = "quality-gate-runs"
	RunFile           = "run.json"
	RunLog            = "gate.log"
	StatusRunning     = "running"
	StatusPass        = "pass"
	StatusFail        = "fail"
	StatusInterrupted = "interrupted"

	QualityToolsFile = "quality-tools.yml"
)

var requiredGoVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

func (t RunToolchain) Recorded() bool {
	return t.GoVersion != "" && t.GOOS != "" && t.GOARCH != "" && t.GoCache != "" && t.Parallelism > 0 && t.Command != "" && t.EnvironmentRecorded && t.TestCache == testCacheExecuted
}

func CaptureToolchain(workingDir string, goArgs, executionEnv []string) *RunToolchain {
	t := &RunToolchain{Backend: BackendNative, Parallelism: runtime.NumCPU(), Command: "go " + strings.Join(goArgs, " "), TestCache: "unknown"}
	if value := environmentValue(executionEnv, "GOMAXPROCS"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			return t
		}
		t.Parallelism = n
	}
	cmd := exec.Command("go", "env", "-json", "GOVERSION", "GOOS", "GOARCH", "GOCACHE", "GOFLAGS")
	cmd.Dir = workingDir
	cmd.Env = executionEnv
	output, err := cmd.Output()
	if err != nil {
		return t
	}
	var env map[string]string
	if json.Unmarshal(output, &env) != nil {
		return t
	}
	t.GoVersion, t.GOOS, t.GOARCH, t.GoCache = env["GOVERSION"], env["GOOS"], env["GOARCH"], env["GOCACHE"]
	flags, ok := env["GOFLAGS"]
	t.GoFlags, t.EnvironmentRecorded = flags, ok
	return t
}

func ResolveGoVersionContract(repository string) (string, bool, error) {
	data, err := os.ReadFile(filepath.Join(repository, QualityToolsFile))
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", true, fmt.Errorf("quality tool契約を読み取れません: %w", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		key, value, ok := strings.Cut(line, ": ")
		if !ok || key != "go" {
			continue
		}
		if !requiredGoVersionPattern.MatchString(value) {
			return "", true, fmt.Errorf("quality tool契約のgo値がversionとして解釈できません: %q", value)
		}
		return value, true, nil
	}
	return "", true, fmt.Errorf("quality tool契約がgo versionを宣言していません: %s", QualityToolsFile)
}

func RequiredGoVersion(repository string) (string, error) {
	version, declared, err := ResolveGoVersionContract(repository)
	if err != nil {
		return "", err
	}
	if !declared {
		return "", fmt.Errorf("quality tool契約のGo versionを取得できません: %sが存在しません", QualityToolsFile)
	}
	return version, nil
}

func (t *RunToolchain) MatchesRequiredGoVersion() bool {
	return t != nil && t.EnvironmentRecorded && t.RequiredGoVersion != "" && t.GoVersion == "go"+t.RequiredGoVersion
}

func TestCacheObservation(log []byte) string {
	mask := 0
	for _, line := range strings.Split(string(log), "\n") {
		mask |= testCacheLineMask(line)
	}
	switch mask {
	case 1:
		return "cached"
	case 2:
		return testCacheExecuted
	case 3:
		return "mixed"
	default:
		return "unknown"
	}
}

func testCacheLineMask(line string) int {
	var event goTestJSONEvent
	if json.Unmarshal([]byte(line), &event) != nil || event.Action != "output" || event.Package == "" || event.Test != "" {
		return 0
	}
	text := strings.TrimSpace(event.Output)
	if strings.HasPrefix(text, "ok ") || strings.HasPrefix(text, "ok\t") {
		if strings.Contains(text, "(cached)") {
			return 1
		}
		return 2
	}
	if strings.HasPrefix(text, "FAIL\t") || strings.HasPrefix(text, "FAIL ") {
		return 2
	}
	return 0
}

func NoIgnoredInputs(repository string) (bool, error) {
	data, err := exec.Command("git", "-C", repository, "ls-files", "--others", "--ignored", "--exclude-standard", "-z").Output()
	if err != nil {
		return false, fmt.Errorf("inspect ignored validation inputs: %w", err)
	}
	return len(data) == 0, nil
}

func NewRunID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("validation run idを生成できません: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func ValidRunID(runID string) bool {
	if len(runID) != 32 {
		return false
	}
	_, err := hex.DecodeString(runID)
	return err == nil && strings.ToLower(runID) == runID
}

func RunRelativePath(runID string) string {
	return filepath.Join(RunDirectory, runID, RunFile)
}

func Encode(record RunRecord) ([]byte, error) {
	return json.Marshal(record)
}

func Decode(data []byte) (RunRecord, error) {
	var record RunRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return RunRecord{}, err
	}
	return record, nil
}

func Read(st *state.StateStore, runID string) (RunRecord, error) {
	if !ValidRunID(runID) {
		return RunRecord{}, fmt.Errorf("invalid validation run id")
	}
	data, err := os.ReadFile(st.Path(RunRelativePath(runID)))
	if err != nil {
		return RunRecord{}, err
	}
	record, err := Decode(data)
	if err != nil {
		return RunRecord{}, err
	}
	if record.ValidationRunID != runID {
		return RunRecord{}, fmt.Errorf("quality gate run record identity mismatch")
	}
	return record, nil
}

func VerifyTerminalPass(record RunRecord) error {
	if err := VerifyExecution(record); err != nil {
		return err
	}
	if record.Status != StatusPass || record.CompletedAt == nil || record.ExitCode != 0 || record.ExitSource != state.ValidationExitSourceTarget {
		return fmt.Errorf("quality gate run did not complete with target-process PASS")
	}
	if record.Log == "" {
		return fmt.Errorf("quality gate run PASS has no log evidence")
	}
	if _, err := os.Stat(record.Log); err != nil {
		return fmt.Errorf("quality gate log evidence is unavailable: %w", err)
	}
	return nil
}

func environmentValue(env []string, key string) string {
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, key+"="); ok {
			return value
		}
	}
	return ""
}
