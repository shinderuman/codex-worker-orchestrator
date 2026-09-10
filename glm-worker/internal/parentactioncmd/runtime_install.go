package parentactioncmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskdiff"
)

type runtimeInstallRequirement struct {
	Required     bool
	Head         string
	SourceDigest string
	Paths        []string
}

type installedRuntimeProbe struct {
	RuntimeBuild struct {
		VCSRevision  *string `json:"vcs_revision"`
		VCSModified  *bool   `json:"vcs_modified"`
		Relationship string  `json:"relationship"`
	} `json:"runtime_build"`
}

type installSmokeProbe struct {
	Status string `json:"status"`
	Result string `json:"result"`
}

const (
	runtimeInstallFailureClassification = "runtime_install_classification_unavailable"
	runtimeInstallFailureDirty          = "runtime_install_tree_not_clean"
	runtimeInstallFailureEvidence       = "runtime_install_evidence_missing"
	runtimeInstallFailureStale          = "runtime_install_evidence_stale"
	runtimeInstallFailureInstalled      = "runtime_install_installed_mismatch"
	runtimeInstallFailureSmoke          = "runtime_install_smoke_failed"
	runtimeInstallFailureValidation     = "runtime_install_validation_record_failed"
)

func runtimeInstallRequirementForTask(repoRoot string, st *state.StateStore) (runtimeInstallRequirement, error) {
	paths, available, err := taskdiff.ChangedPaths(repoRoot, st)
	if err != nil {
		return runtimeInstallRequirement{}, fmt.Errorf("runtime install task diff: %w", err)
	}
	if !available {
		return runtimeInstallRequirement{}, fmt.Errorf("runtime install task baseline is unavailable")
	}
	runtimePaths := make([]string, 0, len(paths))
	for _, path := range paths {
		if runtimeInstallPath(path) {
			runtimePaths = append(runtimePaths, filepath.ToSlash(filepath.Clean(path)))
		}
	}
	sort.Strings(runtimePaths)
	if len(runtimePaths) == 0 {
		return runtimeInstallRequirement{}, nil
	}
	head, err := gitFinalizationOutput(repoRoot, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return runtimeInstallRequirement{}, fmt.Errorf("runtime install HEAD: %w", err)
	}
	digest, err := runtimeSourceDigest(repoRoot, runtimePaths)
	if err != nil {
		return runtimeInstallRequirement{}, err
	}
	return runtimeInstallRequirement{
		Required:     true,
		Head:         strings.TrimSpace(head),
		SourceDigest: digest,
		Paths:        runtimePaths,
	}, nil
}

func runtimeInstallPath(path string) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	switch path {
	case "install.sh", "quality-tools.yml", "claude/settings-managed.json":
		return true
	}
	if strings.HasPrefix(path, "codex/") {
		return true
	}
	if !strings.HasPrefix(path, "glm-worker/") || strings.HasSuffix(path, "_test.go") {
		return false
	}
	return path == "glm-worker/go.mod" || path == "glm-worker/go.sum" || strings.HasSuffix(path, ".go")
}

func runtimeSourceDigest(repoRoot string, paths []string) (string, error) {
	hash := sha256.New()
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(path)))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				_, _ = fmt.Fprintf(hash, "%s\x00missing\n", path)
				continue
			}
			return "", fmt.Errorf("runtime source %s: %w", path, err)
		}
		content := sha256.Sum256(data)
		_, _ = fmt.Fprintf(hash, "%s\x00%s\n", path, hex.EncodeToString(content[:]))
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func runtimeChangedBetween(repoRoot, fromHead, toHead string) (bool, error) {
	if fromHead == toHead {
		return false, nil
	}
	command := exec.Command("git", "-C", repoRoot, "diff", "--name-only", "-z", "--no-renames", fromHead, toHead, "--")
	output, err := command.Output()
	if err != nil {
		return false, fmt.Errorf("runtime install head comparison: %w", err)
	}
	for _, raw := range bytes.Split(output, []byte{0}) {
		if len(raw) != 0 && runtimeInstallPath(string(raw)) {
			return true, nil
		}
	}
	return false, nil
}

func verifyRuntimeInstalledFiles(cfg config.AppConfig, paths []string) error {
	for _, sourcePath := range paths {
		if err := verifyRuntimeInstalledFile(cfg, sourcePath); err != nil {
			return err
		}
	}
	return nil
}

func verifyRuntimeInstalledFile(cfg config.AppConfig, sourcePath string) error {
	installedPath, ok := installedManagedPath(cfg, sourcePath)
	if !ok {
		return nil
	}
	sourceFile := filepath.Join(cfg.RepoRoot, filepath.FromSlash(sourcePath))
	source, err := os.ReadFile(sourceFile)
	if err == nil {
		return compareRuntimeInstalledFile(sourcePath, installedPath, source)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read managed source %s: %w", sourcePath, err)
	}
	return verifyRuntimeInstalledDeletion(sourcePath, installedPath)
}

func compareRuntimeInstalledFile(sourcePath, installedPath string, source []byte) error {
	installed, err := os.ReadFile(installedPath)
	if err != nil {
		return fmt.Errorf("read installed managed file %s: %w", installedPath, err)
	}
	if !bytes.Equal(source, installed) {
		return fmt.Errorf("installed managed file does not match source: %s", sourcePath)
	}
	return nil
}

func verifyRuntimeInstalledDeletion(sourcePath, installedPath string) error {
	_, err := os.Lstat(installedPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat installed managed file %s: %w", installedPath, err)
	}
	return fmt.Errorf("installed managed file remains after source deletion: %s", sourcePath)
}

func installedManagedPath(cfg config.AppConfig, sourcePath string) (string, bool) {
	if cfg.CodexConfigDir == "" {
		return "", false
	}
	switch {
	case sourcePath == "codex/AGENTS.md":
		return filepath.Join(cfg.CodexConfigDir, "AGENTS.md"), true
	case strings.HasPrefix(sourcePath, "codex/instructions/"):
		return filepath.Join(cfg.CodexConfigDir, filepath.FromSlash(strings.TrimPrefix(sourcePath, "codex/"))), true
	case sourcePath == "codex/rules/glm-worker.rules":
		return filepath.Join(cfg.CodexConfigDir, "rules", "glm-worker.rules"), true
	case strings.HasPrefix(sourcePath, "codex/glm-worker/prompts/"):
		return filepath.Join(cfg.CodexConfigDir, filepath.FromSlash(strings.TrimPrefix(sourcePath, "codex/"))), true
	default:
		return "", false
	}
}

func verifyInstalledRuntime(cfg config.AppConfig, installedHead, currentHead string) (string, *finalizationFailure) {
	revision, relationship, failure := installedRuntimeStatus(cfg, installedHead)
	if failure != nil {
		return "", failure
	}
	if installedHead == currentHead {
		if relationship != "same" {
			return "", runtimeInstallFailure(runtimeInstallFailureInstalled, "installed glm-worker is not current")
		}
		return revision, nil
	}
	return verifyInstalledRuntimeAncestor(cfg.RepoRoot, revision, relationship, installedHead, currentHead)
}

func installedRuntimeStatus(cfg config.AppConfig, installedHead string) (string, string, *finalizationFailure) {
	worker, err := resolveGLMWorker()
	if err != nil {
		return "", "", runtimeInstallFailure(runtimeInstallFailureInstalled, err.Error())
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runResolvedWorker(worker, cfg.RepoRoot, []string{"--status"}, nil, &stdout, &stderr, nil); err != nil {
		return "", "", runtimeInstallFailure(runtimeInstallFailureInstalled, firstRuntimeInstallDiagnostic(stderr.String(), err.Error()))
	}
	var probe installedRuntimeProbe
	if err := json.Unmarshal(stdout.Bytes(), &probe); err != nil {
		return "", "", runtimeInstallFailure(runtimeInstallFailureInstalled, "installed glm-worker status is not valid JSON")
	}
	if probe.RuntimeBuild.VCSRevision == nil || probe.RuntimeBuild.VCSModified == nil || *probe.RuntimeBuild.VCSModified ||
		*probe.RuntimeBuild.VCSRevision != installedHead {
		return "", "", runtimeInstallFailure(runtimeInstallFailureInstalled, "installed glm-worker revision does not match install evidence")
	}
	return *probe.RuntimeBuild.VCSRevision, probe.RuntimeBuild.Relationship, nil
}

func verifyInstalledRuntimeAncestor(repoRoot, revision, relationship, installedHead, currentHead string) (string, *finalizationFailure) {
	if relationship != "ancestor" {
		return "", runtimeInstallFailure(runtimeInstallFailureInstalled, "installed glm-worker revision is not an ancestor of current HEAD")
	}
	changed, err := runtimeChangedBetween(repoRoot, installedHead, currentHead)
	if err != nil {
		return "", runtimeInstallFailure(runtimeInstallFailureInstalled, err.Error())
	}
	if changed {
		return "", runtimeInstallFailure(runtimeInstallFailureStale, "runtime source changed after install evidence")
	}
	return revision, nil
}

func runRuntimeInstallSmoke(cfg config.AppConfig) *finalizationFailure {
	worker, err := resolveGLMWorker()
	if err != nil {
		return runtimeInstallFailure(runtimeInstallFailureSmoke, err.Error())
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runResolvedWorker(worker, cfg.RepoRoot, []string{"--install-smoke", "--role", "parent"}, nil, &stdout, &stderr, nil); err != nil {
		return runtimeInstallFailure(runtimeInstallFailureSmoke, firstRuntimeInstallDiagnostic(stderr.String(), err.Error()))
	}
	var probe installSmokeProbe
	if err := json.Unmarshal(stdout.Bytes(), &probe); err != nil || probe.Status != "executed" || probe.Result != state.ValidationResultPass {
		return runtimeInstallFailure(runtimeInstallFailureSmoke, "install smoke did not return a pass result")
	}
	return nil
}

func recordRuntimeInstallValidation(st *state.StateStore, taskID string, requirement runtimeInstallRequirement) *finalizationFailure {
	record := state.TaskEventRecord{
		TaskID: taskID,
		Kind:   "validation",
		Validation: &state.TaskValidationEvent{
			Attribution: "task",
			Source:      "runtime-install",
			Form:        "install-smoke",
			Scope:       "current-runtime",
			Result:      state.ValidationResultPass,
			Evidence:    "head=" + requirement.Head + " source_sha256=" + requirement.SourceDigest,
		},
	}
	if err := st.AppendTaskEvent(record); err != nil {
		return runtimeInstallFailure(runtimeInstallFailureValidation, err.Error())
	}
	return nil
}

func persistRuntimeInstallCompletionAfterSmoke(cfg config.AppConfig, st *state.StateStore, requirement runtimeInstallRequirement) *finalizationFailure {
	current, err := runtimeInstallRequirementForTask(cfg.RepoRoot, st)
	if err != nil {
		return runtimeInstallFailure(runtimeInstallFailureClassification, err.Error())
	}
	if !current.Required || current.Head != requirement.Head || current.SourceDigest != requirement.SourceDigest {
		return runtimeInstallFailure(runtimeInstallFailureStale, "runtime source changed during install")
	}
	if err := verifyRuntimeInstalledFiles(cfg, current.Paths); err != nil {
		return runtimeInstallFailure(runtimeInstallFailureInstalled, err.Error())
	}
	if err := verifyRuntimeMergedConfigFiles(cfg, current.Paths); err != nil {
		return runtimeInstallFailure(runtimeInstallFailureInstalled, err.Error())
	}
	installedRevision, failure := verifyInstalledRuntime(cfg, current.Head, current.Head)
	if failure != nil {
		return failure
	}
	taskID, err := st.TaskID()
	if err != nil {
		return runtimeInstallFailure(runtimeInstallFailureEvidence, err.Error())
	}
	if failure := recordRuntimeInstallValidation(st, taskID, current); failure != nil {
		return failure
	}
	if err := st.SaveRuntimeInstallEvidence(state.RuntimeInstallEvidence{
		Version:           1,
		TaskID:            taskID,
		Head:              current.Head,
		SourceDigest:      current.SourceDigest,
		InstalledRevision: installedRevision,
		SmokeResult:       state.ValidationResultPass,
	}); err != nil {
		return runtimeInstallFailure(runtimeInstallFailureEvidence, err.Error())
	}
	return nil
}

func verifyRuntimeInstallCompletion(cfg config.AppConfig, st *state.StateStore) *finalizationFailure {
	decision, err := repositoryharness.Evaluate(cfg.RepoRoot)
	if err != nil {
		return runtimeInstallFailure(runtimeInstallFailureClassification, err.Error())
	}
	if !decision.Active {
		return nil
	}
	requirement, err := runtimeInstallRequirementForTask(cfg.RepoRoot, st)
	if err != nil {
		return runtimeInstallFailure(runtimeInstallFailureClassification, err.Error())
	}
	if !requirement.Required {
		return nil
	}
	evidence, failure := matchingRuntimeInstallEvidence(st, requirement)
	if failure != nil {
		return failure
	}
	if failure := verifyRuntimeInstallEvidenceFresh(cfg.RepoRoot, evidence, requirement.Head); failure != nil {
		return failure
	}
	if err := verifyRuntimeInstalledFiles(cfg, requirement.Paths); err != nil {
		return runtimeInstallFailure(runtimeInstallFailureInstalled, err.Error())
	}
	if err := verifyRuntimeMergedConfigFiles(cfg, requirement.Paths); err != nil {
		return runtimeInstallFailure(runtimeInstallFailureInstalled, err.Error())
	}
	_, failure = verifyInstalledRuntime(cfg, evidence.InstalledRevision, requirement.Head)
	return failure
}

func matchingRuntimeInstallEvidence(st *state.StateStore, requirement runtimeInstallRequirement) (state.RuntimeInstallEvidence, *finalizationFailure) {
	evidence, err := st.LoadRuntimeInstallEvidence()
	if err != nil {
		return state.RuntimeInstallEvidence{}, runtimeInstallFailure(runtimeInstallFailureEvidence, err.Error())
	}
	taskID, err := st.TaskID()
	if err != nil {
		return state.RuntimeInstallEvidence{}, runtimeInstallFailure(runtimeInstallFailureEvidence, err.Error())
	}
	if evidence.TaskID != taskID || evidence.SourceDigest != requirement.SourceDigest || evidence.InstalledRevision != evidence.Head ||
		evidence.SmokeResult != state.ValidationResultPass {
		return state.RuntimeInstallEvidence{}, runtimeInstallFailure(runtimeInstallFailureStale, "runtime install evidence does not match current task source")
	}
	return evidence, nil
}

func verifyRuntimeInstallEvidenceFresh(repoRoot string, evidence state.RuntimeInstallEvidence, currentHead string) *finalizationFailure {
	changed, err := runtimeChangedBetween(repoRoot, evidence.Head, currentHead)
	if err != nil {
		return runtimeInstallFailure(runtimeInstallFailureStale, err.Error())
	}
	if changed {
		return runtimeInstallFailure(runtimeInstallFailureStale, "runtime source changed after install evidence")
	}
	return nil
}

func runtimeInstallFailure(reason, detail string) *finalizationFailure {
	return &finalizationFailure{Stage: "install", Reason: reason, Detail: compactFinalizationDiagnostic(detail)}
}

func firstRuntimeInstallDiagnostic(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return "runtime install verification failed"
}

func writeInstallOutput(stdout io.Writer, output installOutput) error {
	return json.NewEncoder(stdout).Encode(output)
}
