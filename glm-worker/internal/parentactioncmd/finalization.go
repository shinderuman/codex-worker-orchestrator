package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type finalizationCheckOutput struct {
	Status     string                  `json:"status"`
	Form       string                  `json:"form"`
	Validation json.RawMessage         `json:"validation,omitempty"`
	Handoff    json.RawMessage         `json:"handoff,omitempty"`
	Git        *finalizationGitSummary `json:"git,omitempty"`
	Failure    *finalizationFailure    `json:"failure,omitempty"`
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

type finalizationHandoffProbe struct {
	Consistent  bool `json:"consistent"`
	Validations []struct {
		ValidationRunID string `json:"validation_run_id"`
		Status          string `json:"status"`
	} `json:"validations"`
}

const finalizationDiagnosticLimit = 2048

func runFinalizationCheck(repoRoot, form string, stdout io.Writer) error {
	if form != "go-test" && form != "go-test-race" {
		return fmt.Errorf("usage: glm-parent-action finalize-check <go-test|go-test-race>")
	}
	worker, err := resolveGLMWorker()
	if err != nil {
		return err
	}
	return runFinalizationCheckWithWorker(worker, repoRoot, form, stdout)
}

func runFinalizationCheckWithWorker(worker, repoRoot, form string, stdout io.Writer) error {
	validation, validationProbe, failure := collectFinalizationValidation(worker, repoRoot, form)
	if failure != nil {
		return writeFinalizationOutput(stdout, finalizationCheckOutput{Status: "blocked", Form: form, Failure: failure})
	}
	handoff, handoffProbe, failure := collectFinalizationHandoff(worker, repoRoot)
	if failure != nil {
		return writeFinalizationOutput(stdout, finalizationCheckOutput{Status: "blocked", Form: form, Validation: validation, Failure: failure})
	}
	if !handoffProbe.Consistent {
		return writeFinalizationOutput(stdout, finalizationCheckOutput{
			Status: "blocked", Form: form, Validation: validation, Handoff: handoff,
			Failure: &finalizationFailure{Stage: "handoff", Reason: "lifecycle_inconsistent"},
		})
	}
	if !handoffContainsValidation(handoffProbe, validationProbe.ValidationRunID) {
		return writeFinalizationOutput(stdout, finalizationCheckOutput{
			Status: "blocked", Form: form, Validation: validation, Handoff: handoff,
			Failure: &finalizationFailure{Stage: "snapshot", Reason: "validation_not_current_for_snapshot"},
		})
	}
	gitSummary, err := readFinalizationGitSummary(repoRoot)
	if err != nil {
		return writeFinalizationOutput(stdout, finalizationCheckOutput{
			Status: "blocked", Form: form, Validation: validation, Handoff: handoff,
			Failure: &finalizationFailure{Stage: "git", Reason: "git_summary_unavailable", Detail: compactFinalizationDiagnostic(err.Error())},
		})
	}
	return writeFinalizationOutput(stdout, finalizationCheckOutput{
		Status: "ready_for_parent_decision", Form: form, Validation: validation, Handoff: handoff, Git: &gitSummary,
	})
}

func collectFinalizationValidation(worker, repoRoot, form string) (json.RawMessage, finalizationValidationProbe, *finalizationFailure) {
	validation, failure := runFinalizationWorkerStep(worker, repoRoot, []string{"--quality-gate", form}, "validation")
	if failure != nil {
		return nil, finalizationValidationProbe{}, failure
	}
	var probe finalizationValidationProbe
	if err := json.Unmarshal(validation, &probe); err != nil || probe.Status != "pass" || probe.ValidationRunID == "" {
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

func runFinalizationWorkerStep(worker, repoRoot string, args []string, stage string) (json.RawMessage, *finalizationFailure) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runResolvedWorker(worker, repoRoot, args, nil, &stdout, &stderr, nil)
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
		if validation.ValidationRunID == validationRunID && validation.Status == "pass" {
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
