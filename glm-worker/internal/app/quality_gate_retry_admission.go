package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func qualityGateInvocationDigest(record qualitygate.RunRecord, args []string) (string, error) {
	if record.Toolchain == nil || record.Toolchain.RequiredGoVersion == "" {
		return "", fmt.Errorf("quality gate toolchain identity is unavailable")
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		return "", err
	}
	executableSHA, err := qualityGateFileSHA256(executable)
	if err != nil {
		return "", err
	}
	goSHA, err := qualityGateFileSHA256(goTool)
	if err != nil {
		return "", err
	}
	environment := qualityGateEnv(record.Toolchain.RequiredGoVersion)
	sort.Strings(environment)
	body, err := json.Marshal(struct {
		ExecutableSHA string
		GoSHA         string
		Arguments     []string
		Environment   []string
		HostToolchain *qualitygate.RunToolchain
		Backend       string
	}{
		ExecutableSHA: executableSHA, GoSHA: goSHA, Arguments: args,
		Environment: environment, HostToolchain: qualityGateHostToolchain(record),
		Backend: qualityGateBackend(record),
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:]), nil
}

func qualityGateFileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func qualityGateHostToolchain(record qualitygate.RunRecord) *qualitygate.RunToolchain {
	if record.Execution != nil && record.Execution.HostToolchain != nil {
		return record.Execution.HostToolchain
	}
	return record.Toolchain
}

func qualityGateBackend(record qualitygate.RunRecord) string {
	if record.Execution != nil {
		return record.Execution.Backend
	}
	return qualitygate.BackendNative
}

func repeatedFailedQualityGateRun(st *state.StateStore, candidate qualitygate.RunRecord) (qualitygate.RunRecord, bool, error) {
	entries, err := os.ReadDir(st.Path(qualitygate.RunDirectory))
	if err != nil {
		return qualitygate.RunRecord{}, false, err
	}
	latest, found, err := latestMatchingQualityGateRun(st, candidate, entries)
	if err != nil || !found {
		return qualitygate.RunRecord{}, false, err
	}
	if !reusableFailedQualityGateRecord(latest, candidate) ||
		!qualityGateDockerImageUnchanged(candidate, latest) ||
		!qualityGateStableFailureLog(latest) {
		return qualitygate.RunRecord{}, false, nil
	}
	return latest, true, nil
}

func latestMatchingQualityGateRun(st *state.StateStore, candidate qualitygate.RunRecord, entries []os.DirEntry) (qualitygate.RunRecord, bool, error) {
	var latest qualitygate.RunRecord
	found := false
	for _, entry := range entries {
		if !entry.IsDir() || !qualitygate.ValidRunID(entry.Name()) || entry.Name() == candidate.ValidationRunID {
			continue
		}
		previous, err := readQualityGateRun(st, entry.Name())
		if err != nil {
			return qualitygate.RunRecord{}, false, err
		}
		if !sameQualityGateRecordSource(previous, candidate) {
			continue
		}
		if !found || latest.StartedAt.Before(previous.StartedAt) {
			latest, found = previous, true
		}
	}
	return latest, found, nil
}

func sameQualityGateRecordSource(previous, candidate qualitygate.RunRecord) bool {
	return previous.Form == candidate.Form &&
		previous.Repository == candidate.Repository &&
		sameQualityGateWorkingDir(previous.WorkingDir, candidate.WorkingDir) &&
		previous.Head == candidate.Head &&
		previous.IndexDigest == candidate.IndexDigest &&
		previous.WorktreeDigest == candidate.WorktreeDigest &&
		previous.WorktreeDigestExcludingParent == candidate.WorktreeDigestExcludingParent &&
		previous.TaskID == candidate.TaskID
}

func reusableFailedQualityGateRecord(previous, candidate qualitygate.RunRecord) bool {
	return previous.Status == qualitygate.StatusFail &&
		previous.CompletedAt != nil && previous.ExitCode > 0 &&
		previous.ExitSource == state.ValidationExitSourceTarget &&
		previous.InvocationDigest != "" &&
		previous.InvocationDigest == candidate.InvocationDigest &&
		qualityGateBackend(previous) == qualityGateBackend(candidate)
}

func qualityGateDockerImageUnchanged(candidate, previous qualitygate.RunRecord) bool {
	if qualityGateBackend(candidate) == qualitygate.BackendNative {
		return true
	}
	if previous.Execution == nil || previous.Execution.Docker == nil || previous.Execution.Docker.ImageID == "" {
		return false
	}
	version := candidate.Toolchain.RequiredGoVersion
	data, err := os.ReadFile(filepath.Join(candidate.Repository, qualitygate.QualityToolsFile))
	if err != nil {
		return false
	}
	lintVersion := ""
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "lint-go: ") {
			lintVersion = strings.TrimPrefix(line, "lint-go: ")
			break
		}
	}
	if lintVersion == "" {
		return false
	}
	tag := "codex-worker-orchestrator-go-test:go" + version + "-lint" + lintVersion + "-arm64"
	output, err := exec.Command("docker", "image", "inspect", tag, "--format", "{{.Id}}").Output()
	return err == nil && strings.TrimSpace(string(output)) == previous.Execution.Docker.ImageID
}

func qualityGateStableFailureLog(record qualitygate.RunRecord) bool {
	if record.Log == "" {
		return false
	}
	data, err := os.ReadFile(record.Log)
	if err != nil || len(data) == 0 {
		return false
	}
	text := strings.ToLower(string(data))
	for _, unstable := range []string{
		"test timed out", "signal: killed", "connection refused", "connection reset",
		"network is unreachable", "temporary failure", "resource temporarily unavailable",
		"no space left on device", "etxtbsy",
	} {
		if strings.Contains(text, unstable) {
			return false
		}
	}
	return true
}
