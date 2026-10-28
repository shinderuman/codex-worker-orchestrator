package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type FinalValidationRequest struct {
	Form       string `json:"form"`
	WorkingDir string `json:"working_dir"`
}

type FinalValidationEvidence struct {
	Form            string `json:"form"`
	ValidationRunID string `json:"validation_run_id"`
}

type CandidateValidationLookup int

type finalValidationSource struct {
	CommitOID        string
	IndexDigest      string
	CleanWorktreeDig string
}

const (
	CandidateValidationNone CandidateValidationLookup = iota
	CandidateValidationFormAbsent
	CandidateValidationDeclared
)

func AcceptedCandidateValidationRequest(cfg config.AppConfig, form string) (FinalValidationRequest, CandidateValidationLookup, error) {
	_, candidate, err := acceptedValidationCandidate(cfg)
	if err != nil || candidate == nil {
		return FinalValidationRequest{}, CandidateValidationNone, err
	}
	for _, request := range candidate.RequiredValidations {
		if request.Form == form {
			return request, CandidateValidationDeclared, nil
		}
	}
	return FinalValidationRequest{}, CandidateValidationFormAbsent, nil
}

func (s *Store) resolveAdoptedValidations(admission Admission, input CandidateAcceptanceInput) ([]FinalValidationRequest, []SmokeProbeDeclaration, error) {
	declaration := input.RequiredValidations
	if declaration == nil {
		return nil, nil, fmt.Errorf("the required validation set is undetermined: declare the complete set to adopt the candidate")
	}
	declared, err := s.declaredFinalValidation(admission)
	if err != nil {
		return nil, nil, err
	}
	forms, err := normalizeRequiredValidations(declaration.Forms)
	if err != nil {
		return nil, nil, err
	}
	if declared != nil && !declaredFormCovered(forms, *declared) {
		return nil, nil, fmt.Errorf("declared validation set drops the Runtime obligation %s@%s", declared.Form, declared.WorkingDir)
	}
	probes, err := normalizeSmokeProbes(declaration.Probes)
	if err != nil {
		return nil, nil, err
	}
	if len(probes) != 0 && !input.Policy.RequireInstall {
		return nil, nil, fmt.Errorf("declared post-install smoke probes require verification deployment policy")
	}
	return forms, probes, nil
}

func declaredFormCovered(forms []FinalValidationRequest, declared FinalValidationRequest) bool {
	for _, request := range forms {
		if request.Form == declared.Form && filepath.Clean(request.WorkingDir) == filepath.Clean(declared.WorkingDir) {
			return true
		}
	}
	return false
}

func (s *Store) declaredFinalValidation(admission Admission) (*FinalValidationRequest, error) {
	return s.taskDeclaredFinalValidation(admission.Attempt.SemanticTaskRef)
}

func (s *Store) taskDeclaredFinalValidation(task SemanticTaskRef) (*FinalValidationRequest, error) {
	cfg := WorkflowTaskConfig(s.config, s.identity, task)
	cfg.RepoRoot = s.identity.PrimaryRoot
	runtime := state.AttachStateStore(cfg)
	raw, err := runtime.Read(state.FinalValidationObligationKey)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("final validation obligation record is missing: the bound Runtime did not complete a worker round under the current contract")
	}
	if err != nil {
		return nil, err
	}
	if raw == `{"None":true}` {
		return nil, nil
	}
	var stored struct {
		Form       string
		WorkingDir string
	}
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return nil, fmt.Errorf("declared final validation obligation is unreadable: %w", err)
	}
	normalized, err := normalizeRequiredValidations([]FinalValidationRequest{{Form: stored.Form, WorkingDir: stored.WorkingDir}})
	if err != nil {
		return nil, fmt.Errorf("declared final validation obligation is invalid: %w", err)
	}
	return &normalized[0], nil
}

func normalizeRequiredValidations(requests []FinalValidationRequest) ([]FinalValidationRequest, error) {
	if len(requests) == 0 {
		return nil, nil
	}
	byForm := make(map[string]FinalValidationRequest, len(requests))
	for _, request := range requests {
		if request.Form != "go-test" && request.Form != "go-test-race" {
			return nil, fmt.Errorf("required final validation form %q is not a known quality gate form", request.Form)
		}
		if !validFinalValidationWorkingDir(request.WorkingDir) {
			return nil, fmt.Errorf("required final validation %s has an invalid working directory %q", request.Form, request.WorkingDir)
		}
		request.WorkingDir = filepath.Clean(filepath.FromSlash(request.WorkingDir))
		previous, exists := byForm[request.Form]
		if exists && previous.WorkingDir != request.WorkingDir {
			return nil, fmt.Errorf("required final validation %s declares two working directories: %s and %s", request.Form, previous.WorkingDir, request.WorkingDir)
		}
		byForm[request.Form] = request
	}
	forms := make([]string, 0, len(byForm))
	for form := range byForm {
		forms = append(forms, form)
	}
	sort.Strings(forms)
	normalized := make([]FinalValidationRequest, 0, len(forms))
	for _, form := range forms {
		normalized = append(normalized, byForm[form])
	}
	return normalized, nil
}

func validFinalValidationWorkingDir(dir string) bool {
	if dir == "" {
		return false
	}
	cleaned := filepath.Clean(filepath.FromSlash(dir))
	if filepath.IsAbs(cleaned) {
		return false
	}
	for _, component := range strings.Split(cleaned, string(filepath.Separator)) {
		if component == ".." {
			return false
		}
	}
	return true
}

func (s *Store) collectFinalValidationEvidence(c AcceptedCandidate) ([]FinalValidationEvidence, error) {
	requests, err := normalizeRequiredValidations(c.RequiredValidations)
	if err != nil || len(requests) == 0 {
		return nil, err
	}
	source, err := s.finalValidationSource(c)
	if err != nil {
		return nil, err
	}
	evidence := make([]FinalValidationEvidence, 0, len(requests))
	for _, request := range requests {
		repoRoot, err := s.finalValidationWorkspace(c, request)
		if err != nil {
			return nil, err
		}
		module, err := s.finalValidationRunStore(c.TaskRef)
		if err != nil {
			return nil, err
		}
		record, ok, err := latestFinalValidationRun(module, repoRoot, request.Form)
		if err != nil {
			return nil, err
		}
		if !ok || !finalValidationMatchesSource(record, repoRoot, source, request) {
			return nil, fmt.Errorf("final validation %s has no terminal PASS run for the current candidate source", request.Form)
		}
		if err := qualitygate.VerifyTerminalPass(record); err != nil {
			return nil, fmt.Errorf("final validation %s run %s is not a terminal PASS: %w", request.Form, record.ValidationRunID, err)
		}
		evidence = append(evidence, FinalValidationEvidence{Form: request.Form, ValidationRunID: record.ValidationRunID})
	}
	return evidence, nil
}

func (s *Store) verifyFinalValidationEvidence(c AcceptedCandidate, recorded []FinalValidationEvidence) error {
	requests, err := normalizeRequiredValidations(c.RequiredValidations)
	if err != nil {
		return err
	}
	forms := finalValidationForms(requests)
	byForm, obligated, err := finalValidationRecordShape(forms, recorded)
	if err != nil || !obligated {
		return err
	}
	source, err := s.finalValidationSource(c)
	if err != nil {
		return err
	}
	for _, request := range requests {
		repoRoot, err := s.finalValidationWorkspace(c, request)
		if err != nil {
			return err
		}
		module, err := s.finalValidationRunStore(c.TaskRef)
		if err != nil {
			return err
		}
		if err := verifyFinalValidationRun(module, repoRoot, byForm[request.Form], request, source); err != nil {
			return err
		}
		latest, _, err := latestFinalValidationRun(module, repoRoot, request.Form)
		if err != nil {
			return err
		}
		if latest.ValidationRunID != byForm[request.Form] {
			return fmt.Errorf("final validation %s has a newer run; saved readiness is stale", request.Form)
		}
	}
	return nil
}

func finalValidationForms(requests []FinalValidationRequest) []string {
	forms := make([]string, 0, len(requests))
	for _, request := range requests {
		forms = append(forms, request.Form)
	}
	return forms
}

func finalValidationRecordShape(forms []string, recorded []FinalValidationEvidence) (map[string]string, bool, error) {
	if len(forms) == 0 {
		if len(recorded) != 0 {
			return nil, false, fmt.Errorf("completion readiness carries final validation evidence for an unobligated candidate")
		}
		return nil, false, nil
	}
	if len(recorded) != len(forms) {
		return nil, false, fmt.Errorf("completion readiness final validation evidence covers %d of %d required forms", len(recorded), len(forms))
	}
	byForm := make(map[string]string, len(recorded))
	for _, item := range recorded {
		if _, duplicate := byForm[item.Form]; duplicate || item.ValidationRunID == "" {
			return nil, false, fmt.Errorf("completion readiness final validation evidence is not one run per required form")
		}
		byForm[item.Form] = item.ValidationRunID
	}
	for _, form := range forms {
		if _, ok := byForm[form]; !ok {
			return nil, false, fmt.Errorf("completion readiness lacks final validation evidence for form %s", form)
		}
	}
	return byForm, true, nil
}

func verifyFinalValidationRun(module *state.StateStore, repoRoot, runID string, request FinalValidationRequest, source finalValidationSource) error {
	record, err := qualitygate.Read(module, runID)
	if err != nil {
		return fmt.Errorf("final validation %s run %s is unreadable: %w", request.Form, runID, err)
	}
	if record.Form != request.Form {
		return fmt.Errorf("final validation run %s records form %s, not %s", record.ValidationRunID, record.Form, request.Form)
	}
	if err := qualitygate.VerifyTerminalPass(record); err != nil {
		return fmt.Errorf("final validation %s run %s is not a terminal PASS: %w", request.Form, runID, err)
	}
	if !finalValidationMatchesSource(record, repoRoot, source, request) {
		return fmt.Errorf("final validation %s run %s does not match the current candidate source and declared module", request.Form, runID)
	}
	return nil
}

func (s *Store) finalValidationSource(c AcceptedCandidate) (finalValidationSource, error) {
	indexDigest, err := candidateTreeIndexDigest(s.identity.PrimaryRoot, c.TreeOID)
	if err != nil {
		return finalValidationSource{}, err
	}
	cleanWorktree, err := state.CleanWorktreeDigestExcludingParent()
	if err != nil {
		return finalValidationSource{}, err
	}
	return finalValidationSource{CommitOID: c.CommitOID, IndexDigest: indexDigest, CleanWorktreeDig: cleanWorktree}, nil
}

func candidateTreeIndexDigest(repo, tree string) (string, error) {
	index, err := newSuspensionIndex(repo, tree)
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(index) }()
	data, err := suspensionGit(repo, index, nil, "ls-files", "-s")
	if err != nil {
		return "", err
	}
	return digestBytes(data), nil
}

func (s *Store) finalValidationRunStore(task SemanticTaskRef) (*state.StateStore, error) {
	cfg := WorkflowTaskConfig(s.config, s.identity, task)
	module := state.AttachStateStore(cfg)
	if _, err := os.Stat(module.Path(qualitygate.RunDirectory)); err != nil {
		return nil, fmt.Errorf("no validation run store is bound to the final Task: %w", err)
	}
	return module, nil
}

func latestFinalValidationRun(module *state.StateStore, repoRoot, form string) (qualitygate.RunRecord, bool, error) {
	var latest qualitygate.RunRecord
	found := false
	entries, err := os.ReadDir(module.Path(qualitygate.RunDirectory))
	if err != nil {
		return latest, false, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !qualitygate.ValidRunID(entry.Name()) {
			continue
		}
		record, err := qualitygate.Read(module, entry.Name())
		if err != nil {
			return latest, false, fmt.Errorf("final validation run %s is unreadable: %w", entry.Name(), err)
		}
		if record.Form != form || filepath.Clean(record.Repository) != filepath.Clean(repoRoot) {
			continue
		}
		switch {
		case !found, latest.StartedAt.Before(record.StartedAt):
			latest, found = record, true
		case latest.StartedAt.Equal(record.StartedAt):
			return latest, false, fmt.Errorf("final validation run ordering is ambiguous")
		}
	}
	return latest, found, nil
}

func finalValidationMatchesSource(record qualitygate.RunRecord, repoRoot string, source finalValidationSource, request FinalValidationRequest) bool {
	return filepath.Clean(record.Repository) == filepath.Clean(repoRoot) &&
		record.IgnoredInputsAbsent != nil && *record.IgnoredInputsAbsent &&
		record.Head == source.CommitOID &&
		record.IndexDigest == source.IndexDigest &&
		record.WorktreeDigest == source.CleanWorktreeDig &&
		record.WorktreeDigestExcludingParent == source.CleanWorktreeDig &&
		finalValidationModuleMatches(repoRoot, request.WorkingDir, record.WorkingDir)
}

func finalValidationModuleMatches(repoRoot, declaredDir, recordWorkingDir string) bool {
	if declaredDir == "" || recordWorkingDir == "" {
		return false
	}
	expected := filepath.Join(repoRoot, filepath.FromSlash(declaredDir))
	return finalValidationSamePath(expected, recordWorkingDir)
}

func finalValidationSamePath(left, right string) bool {
	if filepath.Clean(left) == filepath.Clean(right) {
		return true
	}
	resolvedLeft, err := filepath.EvalSymlinks(left)
	if err != nil {
		return false
	}
	resolvedRight, err := filepath.EvalSymlinks(right)
	if err != nil {
		return false
	}
	return filepath.Clean(resolvedLeft) == filepath.Clean(resolvedRight)
}
