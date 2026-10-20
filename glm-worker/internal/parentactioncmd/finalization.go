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

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type finalizationCheckOutput struct {
	Status         string                      `json:"status"`
	Form           string                      `json:"form"`
	Routing        *finalizationRoutingSummary `json:"routing,omitempty"`
	Validation     json.RawMessage             `json:"validation,omitempty"`
	RepositoryLint json.RawMessage             `json:"repository_lint,omitempty"`
	Git            *finalizationGitSummary     `json:"git,omitempty"`
	Failure        *finalizationFailure        `json:"failure,omitempty"`
}

type finalizationRoutingSummary struct {
	SelectedDir string `json:"selected_dir"`
	Basis       string `json:"basis"`
}

type finalizationFailure struct {
	Stage    string `json:"stage"`
	Reason   string `json:"reason"`
	ExitCode int    `json:"exit_code,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

type finalizationGitSummary struct {
	Head             string                 `json:"head"`
	Branch           string                 `json:"branch,omitempty"`
	Detached         bool                   `json:"detached"`
	Clean            bool                   `json:"clean"`
	StagedChanges    int                    `json:"staged_changes"`
	UnstagedChanges  int                    `json:"unstaged_changes"`
	UntrackedChanges int                    `json:"untracked_changes"`
	RemoteState      string                 `json:"remote_state"`
	Remote           *finalizationGitRemote `json:"remote"`
}

type finalizationGitRemote struct {
	Basis            string `json:"basis"`
	RemoteName       string `json:"remote_name,omitempty"`
	RemoteRef        string `json:"remote_ref,omitempty"`
	TrackingRef      string `json:"tracking_ref,omitempty"`
	TrackingOID      string `json:"tracking_oid,omitempty"`
	Ahead            int    `json:"ahead,omitempty"`
	Behind           int    `json:"behind,omitempty"`
	LastOperation    string `json:"last_operation,omitempty"`
	LastOperationOID string `json:"last_operation_oid,omitempty"`
}

type finalizationValidationProbe struct {
	Status          string `json:"status"`
	ValidationRunID string `json:"validation_run_id"`
}

const (
	finalizationDiagnosticLimit          = 2048
	finalizationValidationStatusPass     = "pass"
	finalizationStatusBlocked            = publicationPrepareStatusBlocked
	finalizationRoutingBasisCandidate    = "accepted_candidate"
	finalizationRoutingBasisCaller       = "caller_cwd"
	finalizationGoModFile                = "go.mod"
	finalizationRemoteBasisTrackingRef   = "local_tracking_ref"
	finalizationRemoteStateNoUpstream    = "no_upstream"
	finalizationRemoteStateTrackingMiss  = "tracking_ref_missing"
	finalizationRemoteStateUnverified    = "unverified"
	finalizationRemoteStateSynced        = "synced"
	finalizationRemoteStatePendingAhead  = "remote_sync_pending_ahead"
	finalizationRemoteStatePendingDiverg = "remote_sync_pending_diverged"
)

var acceptedCandidateValidationRequest = controller.AcceptedCandidateValidationRequest

func runFinalizationCheck(cfg config.AppConfig, validationDir, form string, stdout io.Writer) error {
	if form != "go-test" && form != "go-test-race" {
		return fmt.Errorf("usage: glm-parent-action finalize-check <go-test|go-test-race>")
	}
	cfg, validationDir, err := finalizationExecutionContext(cfg, validationDir)
	if err != nil {
		return err
	}
	candidateRoot, release, err := controller.PrepareFinalValidationWorkspace(cfg, form)
	if err != nil {
		return err
	}
	if release != nil {
		defer func() { _ = release() }()
	}
	if candidateRoot != "" && candidateRoot != cfg.RepoRoot {
		cfg.RepoRoot = candidateRoot
		validationDir = filepath.Join(candidateRoot, ".")
	}
	output, err := collectFinalizationCheck(cfg, validationDir, form)
	if err != nil {
		return err
	}
	if release != nil {
		if err := release(); err != nil {
			return fmt.Errorf("release final validation checkout: %w", err)
		}
	}
	return writeFinalizationOutput(stdout, output)
}

func collectFinalizationCheck(cfg config.AppConfig, validationDir, form string) (finalizationCheckOutput, error) {
	repoRoot := cfg.RepoRoot
	validatedDir, err := finalizationValidationDir(repoRoot, validationDir)
	if err != nil {
		return finalizationCheckOutput{}, err
	}
	worker, err := resolveGLMWorker()
	if err != nil {
		return finalizationCheckOutput{}, err
	}
	routing, failure := finalizationRoutingDecision(cfg, validatedDir, form)
	if failure != nil {
		return finalizationCheckOutput{Status: finalizationStatusBlocked, Form: form, Failure: failure}, nil
	}
	var aggregate bytes.Buffer
	if err := runFinalizationCheckWithWorker(worker, repoRoot, routing.SelectedDir, form, routing, &aggregate); err != nil {
		return finalizationCheckOutput{}, err
	}
	var output finalizationCheckOutput
	if err := json.Unmarshal(bytes.TrimSpace(aggregate.Bytes()), &output); err != nil {
		return output, fmt.Errorf("decode finalize-check aggregate: %w", err)
	}
	return output, nil
}

func finalizationExecutionContext(cfg config.AppConfig, validationDir string) (config.AppConfig, string, error) {
	bound, err := controller.ParentContinuationConfig(cfg)
	if err != nil || bound.RepoRoot == cfg.RepoRoot {
		return bound, validationDir, err
	}
	caller, err := finalizationValidationDir(cfg.RepoRoot, validationDir)
	if err != nil {
		return cfg, "", err
	}
	root, err := filepath.EvalSymlinks(cfg.RepoRoot)
	if err != nil {
		return cfg, "", err
	}
	relative, err := filepath.Rel(root, caller)
	if err != nil {
		return cfg, "", err
	}
	return bound, filepath.Join(bound.RepoRoot, relative), nil
}

func finalizationRoutingDecision(cfg config.AppConfig, callerDir, form string) (*finalizationRoutingSummary, *finalizationFailure) {
	request, lookup, err := acceptedCandidateValidationRequest(cfg, form)
	if err != nil {
		return nil, &finalizationFailure{Stage: "routing", Reason: "accepted_candidate_unavailable", Detail: compactFinalizationDiagnostic(err.Error())}
	}
	if lookup == controller.CandidateValidationFormAbsent {
		return nil, &finalizationFailure{
			Stage:  "routing",
			Reason: "form_not_declared_by_accepted_candidate",
			Detail: "accepted candidate declares no " + form + " final validation obligation",
		}
	}
	if lookup == controller.CandidateValidationDeclared {
		selected, failure := finalizationVerifiedCandidateDir(cfg.RepoRoot, request.WorkingDir)
		if failure != nil {
			return nil, failure
		}
		if selected == "" {
			return nil, &finalizationFailure{
				Stage:  "routing",
				Reason: "candidate_working_directory_not_a_module_root",
				Detail: compactFinalizationDiagnostic(request.WorkingDir),
			}
		}
		return &finalizationRoutingSummary{SelectedDir: selected, Basis: finalizationRoutingBasisCandidate}, nil
	}
	if finalizationDirIsModuleRoot(callerDir) {
		return &finalizationRoutingSummary{SelectedDir: callerDir, Basis: finalizationRoutingBasisCaller}, nil
	}
	return nil, &finalizationFailure{
		Stage:  "routing",
		Reason: "no_module_root_working_directory",
		Detail: compactFinalizationDiagnostic("caller working directory has no " + finalizationGoModFile + ": " + callerDir),
	}
}

func finalizationVerifiedCandidateDir(repoRoot, workingDir string) (string, *finalizationFailure) {
	joined := filepath.Join(repoRoot, workingDir)
	if finalizationDirLexicallyOutsideRepository(repoRoot, joined) {
		return "", &finalizationFailure{
			Stage:  "routing",
			Reason: "candidate_working_directory_outside_repository",
			Detail: compactFinalizationDiagnostic(workingDir),
		}
	}
	resolved, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return "", &finalizationFailure{
			Stage:  "routing",
			Reason: "candidate_working_directory_unavailable",
			Detail: compactFinalizationDiagnostic(err.Error()),
		}
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", &finalizationFailure{
			Stage:  "routing",
			Reason: "candidate_working_directory_unavailable",
			Detail: compactFinalizationDiagnostic(workingDir),
		}
	}
	if finalizationDirOutsideRepository(repoRoot, resolved) {
		return "", &finalizationFailure{
			Stage:  "routing",
			Reason: "candidate_working_directory_outside_repository",
			Detail: compactFinalizationDiagnostic(resolved),
		}
	}
	if !finalizationDirIsModuleRoot(resolved) {
		return "", nil
	}
	return resolved, nil
}

func finalizationDirLexicallyOutsideRepository(repoRoot, dir string) bool {
	repo, err := filepath.Abs(repoRoot)
	if err != nil {
		return true
	}
	candidate, err := filepath.Abs(dir)
	if err != nil {
		return true
	}
	rel, err := filepath.Rel(repo, candidate)
	return err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
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
	validation, failure := collectFinalizationValidation(worker, validationDir, form)
	if failure != nil {
		return writeFinalizationOutput(stdout, finalizationCheckOutput{Status: finalizationStatusBlocked, Form: form, Routing: routing, Failure: failure})
	}
	repositoryLint, failure := collectFinalizationRepositoryLint(repoRoot)
	if failure != nil {
		return writeFinalizationOutput(stdout, finalizationCheckOutput{
			Status: finalizationStatusBlocked, Form: form, Routing: routing, Validation: validation, RepositoryLint: repositoryLint, Failure: failure,
		})
	}
	gitSummary, err := readFinalizationGitSummary(repoRoot)
	if err != nil {
		return writeFinalizationOutput(stdout, finalizationCheckOutput{
			Status: finalizationStatusBlocked, Form: form, Routing: routing, Validation: validation, RepositoryLint: repositoryLint,
			Failure: &finalizationFailure{Stage: "git", Reason: "git_summary_unavailable", Detail: compactFinalizationDiagnostic(err.Error())},
		})
	}
	return writeFinalizationOutput(stdout, finalizationCheckOutput{
		Status: "ready_for_parent_decision", Form: form, Routing: routing, Validation: validation, RepositoryLint: repositoryLint, Git: &gitSummary,
	})
}

func collectFinalizationValidation(worker, validationDir, form string) (json.RawMessage, *finalizationFailure) {
	validation, failure := runFinalizationWorkerStep(worker, validationDir, []string{"--quality-gate", form}, "validation")
	if failure != nil {
		return nil, failure
	}
	var probe finalizationValidationProbe
	if err := json.Unmarshal(validation, &probe); err != nil || probe.Status != finalizationValidationStatusPass || probe.ValidationRunID == "" {
		return nil, &finalizationFailure{
			Stage: "validation", Reason: "invalid_validation_result", Detail: compactFinalizationDiagnostic(string(validation)),
		}
	}
	return validation, nil
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

func readFinalizationGitSummary(repoRoot string) (finalizationGitSummary, error) {
	head, err := state.ResolveGitHeadAuthority("git", repoRoot)
	if err != nil {
		return finalizationGitSummary{}, err
	}
	if head.Unborn || head.Head == "" {
		return finalizationGitSummary{}, fmt.Errorf("git HEAD does not resolve to a commit")
	}
	branch := ""
	if !head.Detached {
		if !strings.HasPrefix(head.SymbolicHead, "refs/heads/") {
			return finalizationGitSummary{}, fmt.Errorf("git HEAD symbolic target %q is not a local branch", head.SymbolicHead)
		}
		branch = strings.TrimPrefix(head.SymbolicHead, "refs/heads/")
		if branch == "" {
			return finalizationGitSummary{}, fmt.Errorf("git HEAD local branch is empty")
		}
	}
	status, err := gitFinalizationOutput(repoRoot, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return finalizationGitSummary{}, err
	}
	staged, unstaged, untracked := countFinalizationStatus(status)
	remote, remoteState := finalizationRemoteState(repoRoot, branch, head.Detached, head.Head)
	return finalizationGitSummary{
		Head:             head.Head,
		Branch:           branch,
		Detached:         head.Detached,
		Clean:            staged == 0 && unstaged == 0 && untracked == 0,
		StagedChanges:    staged,
		UnstagedChanges:  unstaged,
		UntrackedChanges: untracked,
		RemoteState:      remoteState,
		Remote:           remote,
	}, nil
}

func finalizationRemoteState(repoRoot, branch string, detached bool, headOID string) (*finalizationGitRemote, string) {
	remote := &finalizationGitRemote{Basis: finalizationRemoteBasisTrackingRef}
	if detached || branch == "" {
		return remote, finalizationRemoteStateNoUpstream
	}
	upstream, err := resolveGitUpstream(repoRoot, branch)
	if err != nil {
		if errors.Is(err, errGitRemoteURLMissing) {
			return remote, finalizationRemoteStateUnverified
		}
		return remote, finalizationRemoteStateNoUpstream
	}
	remote.RemoteName = upstream.RemoteName
	remote.RemoteRef = upstream.RemoteRef
	remote.TrackingRef = upstream.TrackingRef
	remote.TrackingOID = upstream.TrackingOID
	remote.LastOperation, remote.LastOperationOID = gitTrackingLastOperation(repoRoot, upstream.TrackingRef)
	if upstream.TrackingOID == "" {
		return remote, finalizationRemoteStateTrackingMiss
	}
	ahead, behind, err := gitAheadBehind(repoRoot, upstream.TrackingRef, headOID)
	if err != nil {
		return remote, finalizationRemoteStateUnverified
	}
	remote.Ahead = ahead
	remote.Behind = behind
	switch {
	case behind > 0:
		return remote, finalizationRemoteStatePendingDiverg
	case ahead > 0:
		return remote, finalizationRemoteStatePendingAhead
	default:
		return remote, finalizationRemoteStateSynced
	}
}

func gitTrackingLastOperation(repoRoot, trackingRef string) (string, string) {
	if trackingRef == "" {
		return "", ""
	}
	output, err := gitFinalizationOutput(repoRoot, "log", "-g", "-n", "1", "--format=%H%x09%gs", trackingRef)
	if err != nil {
		return "", ""
	}
	fields := strings.SplitN(strings.TrimSpace(output), "\t", 2)
	if len(fields) != 2 {
		return "", ""
	}
	return gitReflogOperation(fields[1]), fields[0]
}

func gitReflogOperation(subject string) string {
	switch {
	case strings.HasPrefix(subject, "update by push"):
		return "push"
	case strings.HasPrefix(subject, "pull"):
		return "pull"
	case strings.HasPrefix(subject, "fetch"):
		return "fetch"
	case strings.HasPrefix(subject, "clone"):
		return "clone"
	}
	return ""
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
