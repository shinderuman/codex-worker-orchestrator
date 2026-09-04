package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type finalizationCheckOutput struct {
	Status     string                      `json:"status"`
	Form       string                      `json:"form"`
	Routing    *finalizationRoutingSummary `json:"routing,omitempty"`
	Validation json.RawMessage             `json:"validation,omitempty"`
	Handoff    json.RawMessage             `json:"handoff,omitempty"`
	Git        *finalizationGitSummary     `json:"git,omitempty"`
	Failure    *finalizationFailure        `json:"failure,omitempty"`
}

type finalizationRoutingSummary struct {
	SelectedDir     string `json:"selected_dir"`
	Basis           string `json:"basis"`
	ValidationRunID string `json:"validation_run_id,omitempty"`
	SnapshotMatch   string `json:"snapshot_match,omitempty"`
}

type finalizationFailure struct {
	Stage    string `json:"stage"`
	Reason   string `json:"reason"`
	ExitCode int    `json:"exit_code,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

type finalizationGitSummary struct {
	Head             string `json:"head"`
	Branch           string `json:"branch,omitempty"`
	Detached         bool   `json:"detached"`
	Clean            bool   `json:"clean"`
	StagedChanges    int    `json:"staged_changes"`
	UnstagedChanges  int    `json:"unstaged_changes"`
	UntrackedChanges int    `json:"untracked_changes"`
	RemoteState      string `json:"remote_state"`
}

type finalizationValidationProbe struct {
	Status          string `json:"status"`
	ValidationRunID string `json:"validation_run_id"`
}

type finalizationRoutingEvidenceProbe struct {
	ValidationRunID string `json:"validation_run_id"`
	Form            string `json:"form"`
	WorkingDir      string `json:"working_dir"`
	SnapshotMatch   string `json:"snapshot_match"`
}

type finalizationHandoffValidationProbe struct {
	ValidationRunID string `json:"validation_run_id"`
	Form            string `json:"form"`
	Status          string `json:"status"`
	WorkingDir      string `json:"working_dir"`
}

type finalizationHandoffProbe struct {
	Consistent      bool                                 `json:"consistent"`
	Validations     []finalizationHandoffValidationProbe `json:"validations"`
	RoutingEvidence []finalizationRoutingEvidenceProbe   `json:"routing_evidence"`
}

const (
	finalizationDiagnosticLimit        = 2048
	finalizationValidationStatusPass   = "pass"
	finalizationRoutingBasisValidation = "current_task_validation"
	finalizationRoutingBasisCaller     = "caller_cwd"
	finalizationGoModFile              = "go.mod"
)

func runFinalizationCheck(repoRoot, validationDir, form string, stdout io.Writer) error {
	if form != "go-test" && form != "go-test-race" {
		return fmt.Errorf("usage: glm-parent-action finalize-check <go-test|go-test-race>")
	}
	validatedDir, err := finalizationValidationDir(repoRoot, validationDir)
	if err != nil {
		return err
	}
	worker, err := resolveGLMWorker()
	if err != nil {
		return err
	}
	routing, failure := finalizationRoutingDecision(worker, repoRoot, validatedDir, form)
	if failure != nil {
		return writeFinalizationOutput(stdout, finalizationCheckOutput{Status: "blocked", Form: form, Failure: failure})
	}
	return runFinalizationCheckWithWorker(worker, repoRoot, routing.SelectedDir, form, routing, stdout)
}

func finalizationRoutingDecision(worker, repoRoot, callerDir, form string) (*finalizationRoutingSummary, *finalizationFailure) {
	evidence, evidenceFound := finalizationRoutingEvidenceCandidate(worker, repoRoot, form)
	if evidenceFound {
		selected, failure := finalizationVerifiedEvidenceDir(repoRoot, evidence)
		if failure != nil {
			return nil, failure
		}
		if selected != "" {
			return &finalizationRoutingSummary{
				SelectedDir:     selected,
				Basis:           finalizationRoutingBasisValidation,
				ValidationRunID: evidence.ValidationRunID,
				SnapshotMatch:   evidence.SnapshotMatch,
			}, nil
		}
	}
	if finalizationDirIsModuleRoot(callerDir) {
		return &finalizationRoutingSummary{SelectedDir: callerDir, Basis: finalizationRoutingBasisCaller}, nil
	}
	detail := "caller working directory has no " + finalizationGoModFile + ": " + callerDir
	if evidenceFound {
		detail = "routing evidence working directory has no " + finalizationGoModFile + ": " + evidence.WorkingDir + "; " + detail
	}
	return nil, &finalizationFailure{
		Stage:  "routing",
		Reason: "no_module_root_working_directory",
		Detail: compactFinalizationDiagnostic(detail),
	}
}

func finalizationRoutingEvidenceCandidate(worker, repoRoot, form string) (finalizationRoutingEvidenceProbe, bool) {
	_, handoff, failure := collectFinalizationHandoff(worker, repoRoot)
	if failure != nil || !handoff.Consistent {
		return finalizationRoutingEvidenceProbe{}, false
	}
	for _, evidence := range handoff.RoutingEvidence {
		if evidence.Form == form && evidence.WorkingDir != "" {
			return evidence, true
		}
	}
	return finalizationRoutingEvidenceProbe{}, false
}

func finalizationVerifiedEvidenceDir(repoRoot string, evidence finalizationRoutingEvidenceProbe) (string, *finalizationFailure) {
	resolved, err := filepath.EvalSymlinks(evidence.WorkingDir)
	if err != nil {
		return "", nil
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", nil
	}
	if finalizationDirOutsideRepository(repoRoot, resolved) {
		return "", &finalizationFailure{
			Stage:  "routing",
			Reason: "routing_evidence_outside_repository",
			Detail: compactFinalizationDiagnostic(resolved),
		}
	}
	if !finalizationDirIsModuleRoot(resolved) {
		return "", nil
	}
	return resolved, nil
}

func finalizationDirOutsideRepository(repoRoot, dir string) bool {
	repo, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return true
	}
	rel, err := filepath.Rel(repo, dir)
	return err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func finalizationDirIsModuleRoot(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, finalizationGoModFile))
	return err == nil && info.Mode().IsRegular()
}

func finalizationValidationDir(repoRoot, validationDir string) (string, error) {
	if _, err := filepath.EvalSymlinks(repoRoot); err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	candidate, err := filepath.EvalSymlinks(validationDir)
	if err != nil {
		return "", fmt.Errorf("resolve finalize-check working directory: %w", err)
	}
	info, err := os.Stat(candidate)
	if err != nil {
		return "", fmt.Errorf("stat finalize-check working directory: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("finalize-check working directory is not a directory")
	}
	if finalizationDirOutsideRepository(repoRoot, candidate) {
		return "", fmt.Errorf("finalize-check working directory must be inside repository")
	}
	return candidate, nil
}

func runFinalizationCheckWithWorker(worker, repoRoot, validationDir, form string, routing *finalizationRoutingSummary, stdout io.Writer) error {
	validation, validationProbe, failure := collectFinalizationValidation(worker, validationDir, form)
	if failure != nil {
		return writeFinalizationOutput(stdout, finalizationCheckOutput{Status: "blocked", Form: form, Routing: routing, Failure: failure})
	}
	handoff, handoffProbe, failure := collectFinalizationHandoff(worker, repoRoot)
	if failure != nil {
		return writeFinalizationOutput(stdout, finalizationCheckOutput{Status: "blocked", Form: form, Routing: routing, Validation: validation, Failure: failure})
	}
	if !handoffProbe.Consistent {
		return writeFinalizationOutput(stdout, finalizationCheckOutput{
			Status: "blocked", Form: form, Routing: routing, Validation: validation, Handoff: handoff,
			Failure: &finalizationFailure{Stage: "handoff", Reason: "lifecycle_inconsistent"},
		})
	}
	if !handoffContainsValidation(handoffProbe, validationProbe.ValidationRunID) {
		return writeFinalizationOutput(stdout, finalizationCheckOutput{
			Status: "blocked", Form: form, Routing: routing, Validation: validation, Handoff: handoff,
			Failure: &finalizationFailure{Stage: "snapshot", Reason: "validation_not_current_for_snapshot"},
		})
	}
	gitSummary, err := readFinalizationGitSummary(repoRoot)
	if err != nil {
		return writeFinalizationOutput(stdout, finalizationCheckOutput{
			Status: "blocked", Form: form, Routing: routing, Validation: validation, Handoff: handoff,
			Failure: &finalizationFailure{Stage: "git", Reason: "git_summary_unavailable", Detail: compactFinalizationDiagnostic(err.Error())},
		})
	}
	return writeFinalizationOutput(stdout, finalizationCheckOutput{
		Status: "ready_for_parent_decision", Form: form, Routing: routing, Validation: validation, Handoff: handoff, Git: &gitSummary,
	})
}

func collectFinalizationValidation(worker, validationDir, form string) (json.RawMessage, finalizationValidationProbe, *finalizationFailure) {
	validation, failure := runFinalizationWorkerStep(worker, validationDir, []string{"--quality-gate", form}, "validation")
	if failure != nil {
		return nil, finalizationValidationProbe{}, failure
	}
	var probe finalizationValidationProbe
	if err := json.Unmarshal(validation, &probe); err != nil || probe.Status != finalizationValidationStatusPass || probe.ValidationRunID == "" {
		return nil, finalizationValidationProbe{}, &finalizationFailure{
			Stage: "validation", Reason: "invalid_validation_result", Detail: compactFinalizationDiagnostic(string(validation)),
		}
	}
	return validation, probe, nil
}

func collectFinalizationHandoff(worker, repoRoot string) (json.RawMessage, finalizationHandoffProbe, *finalizationFailure) {
	handoff, failure := runFinalizationWorkerStep(worker, repoRoot, []string{"--handoff"}, "handoff")
	if failure != nil {
		return nil, finalizationHandoffProbe{}, failure
	}
	var probe finalizationHandoffProbe
	if err := json.Unmarshal(handoff, &probe); err != nil {
		return nil, finalizationHandoffProbe{}, &finalizationFailure{
			Stage: "handoff", Reason: "invalid_handoff_result", Detail: compactFinalizationDiagnostic(string(handoff)),
		}
	}
	return handoff, probe, nil
}

func runFinalizationWorkerStep(worker, workingDir string, args []string, stage string) (json.RawMessage, *finalizationFailure) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runResolvedWorker(worker, workingDir, args, nil, &stdout, &stderr, nil)
	if err != nil {
		failure := &finalizationFailure{Stage: stage, Reason: "worker_command_failed", Detail: compactFinalizationDiagnostic(stderr.String())}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			failure.ExitCode = childExitCode(exitErr)
		} else if failure.Detail == "" {
			failure.Detail = compactFinalizationDiagnostic(err.Error())
		}
		return nil, failure
	}
	payload := bytes.TrimSpace(stdout.Bytes())
	if len(payload) == 0 || !json.Valid(payload) {
		return nil, &finalizationFailure{Stage: stage, Reason: "invalid_machine_output", Detail: compactFinalizationDiagnostic(string(payload))}
	}
	return append(json.RawMessage(nil), payload...), nil
}

func handoffContainsValidation(handoff finalizationHandoffProbe, validationRunID string) bool {
	for _, validation := range handoff.Validations {
		if validation.ValidationRunID == validationRunID && validation.Status == finalizationValidationStatusPass {
			return true
		}
	}
	return false
}

func readFinalizationGitSummary(repoRoot string) (finalizationGitSummary, error) {
	head, err := gitFinalizationOutput(repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return finalizationGitSummary{}, err
	}
	branch, branchErr := gitFinalizationOutput(repoRoot, "symbolic-ref", "--short", "-q", "HEAD")
	detached := false
	if branchErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(branchErr, &exitErr) {
			return finalizationGitSummary{}, branchErr
		}
		detached = true
		branch = ""
	}
	status, err := gitFinalizationOutput(repoRoot, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return finalizationGitSummary{}, err
	}
	staged, unstaged, untracked := countFinalizationStatus(status)
	return finalizationGitSummary{
		Head:             strings.TrimSpace(head),
		Branch:           strings.TrimSpace(branch),
		Detached:         detached,
		Clean:            staged == 0 && unstaged == 0 && untracked == 0,
		StagedChanges:    staged,
		UnstagedChanges:  unstaged,
		UntrackedChanges: untracked,
		RemoteState:      "not_checked",
	}, nil
}

func gitFinalizationOutput(repoRoot string, args ...string) (string, error) {
	command := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(output), nil
}

func countFinalizationStatus(status string) (int, int, int) {
	var staged int
	var unstaged int
	var untracked int
	for _, line := range strings.Split(status, "\n") {
		if len(line) < 3 {
			continue
		}
		x := line[0]
		y := line[1]
		if x == '?' && y == '?' {
			untracked++
			continue
		}
		if x != ' ' {
			staged++
		}
		if y != ' ' {
			unstaged++
		}
	}
	return staged, unstaged, untracked
}

func compactFinalizationDiagnostic(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= finalizationDiagnosticLimit {
		return value
	}
	return value[len(value)-finalizationDiagnosticLimit:]
}

func writeFinalizationOutput(stdout io.Writer, output finalizationCheckOutput) error {
	return json.NewEncoder(stdout).Encode(output)
}
