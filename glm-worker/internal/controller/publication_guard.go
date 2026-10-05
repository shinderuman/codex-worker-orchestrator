package controller

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

const (
	publicationGuardConfigKey = "core.hooksPath"
	publicationGuardHookName  = "pre-push"
)

type PublicationGuardStatus struct {
	ConfigPath     string `json:"config_path"`
	ConfigValue    string `json:"config_value"`
	HookPath       string `json:"hook_path"`
	HookTarget     string `json:"hook_target"`
	WorkerPath     string `json:"worker_path"`
	WorkerExpected string `json:"worker_expected"`
}

type PublicationGuardInput struct {
	Repository string `json:"repository"`
	Remote     string `json:"remote"`
	Ref        string `json:"ref"`
	OldOID     string `json:"old_oid"`
	NewOID     string `json:"new_oid"`
}

func (s *Store) VerifyPublicationGuardSetup() (PublicationGuardStatus, error) {
	return verifyPublicationGuardSetup(s.identity.PrimaryRoot)
}

func verifyPublicationGuardSetup(repo string) (PublicationGuardStatus, error) {
	root, err := canonicalPath(repo)
	if err != nil {
		return PublicationGuardStatus{}, err
	}
	commonDir, err := gitCommonDir(root)
	if err != nil {
		return PublicationGuardStatus{}, err
	}
	configPath := filepath.Join(commonDir, "config")
	if info, err := os.Lstat(configPath); err != nil || !info.Mode().IsRegular() {
		return PublicationGuardStatus{}, fmt.Errorf("publication guard Git config is unavailable")
	}
	value, err := gitTrimmed(root, "config", "--local", "--get", publicationGuardConfigKey)
	if err != nil || value == "" {
		return PublicationGuardStatus{}, fmt.Errorf("publication guard hooksPath is unavailable")
	}
	if filepath.IsAbs(value) {
		return PublicationGuardStatus{}, fmt.Errorf("publication guard hooksPath must be repository-owned")
	}
	hookRoot, err := canonicalPath(filepath.Join(commonDir, value))
	if err != nil {
		return PublicationGuardStatus{}, err
	}
	hookPath := filepath.Join(hookRoot, publicationGuardHookName)
	hookInfo, err := os.Lstat(hookPath)
	if err != nil || !hookInfo.Mode().IsRegular() || hookInfo.Mode().Perm()&0o111 == 0 {
		return PublicationGuardStatus{}, fmt.Errorf("publication guard hook is unavailable")
	}
	target, err := os.ReadFile(hookPath)
	if err != nil {
		return PublicationGuardStatus{}, err
	}
	line := firstExecutableHookLine(string(target))
	if !strings.HasPrefix(line, "exec ") {
		return PublicationGuardStatus{}, fmt.Errorf("publication guard hook does not directly exec worker")
	}
	fields := strings.Fields(line)
	if len(fields) != 3 || fields[2] != "controller-publication-guard" {
		return PublicationGuardStatus{}, fmt.Errorf("publication guard hook has unexpected command")
	}
	workerPath := fields[1]
	if !filepath.IsAbs(workerPath) {
		return PublicationGuardStatus{}, fmt.Errorf("publication guard worker path is not absolute")
	}
	workerInfo, err := os.Lstat(workerPath)
	if err != nil || !workerInfo.Mode().IsRegular() || workerInfo.Mode().Perm()&0o111 == 0 {
		return PublicationGuardStatus{}, fmt.Errorf("publication guard worker is unavailable")
	}
	return PublicationGuardStatus{ConfigPath: configPath, ConfigValue: value, HookPath: hookPath, HookTarget: line, WorkerPath: workerPath, WorkerExpected: workerPath}, nil
}

func firstExecutableHookLine(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line
	}
	return ""
}

func (s *Store) ValidatePublicationGuard(input PublicationGuardInput) error {
	repository, err := canonicalPath(input.Repository)
	if err != nil {
		return err
	}
	if repository != s.identity.PrimaryRoot {
		return fmt.Errorf("publication guard repository is outside controller ownership")
	}
	op, present, err := s.pendingPublicationGuardOperation()
	if err != nil {
		return err
	}
	if !present {
		return nil
	}
	effect, found := publicationGuardEffect(op.Transition.Effects, MutationSurfaceRef, input.Ref)
	if !found {
		if publicationGuardOwnsGitMutation(op.Transition.Effects) {
			return fmt.Errorf("publication ref update rejected: pending controller transition owns another Git mutation")
		}
		return nil
	}
	if effect.ExpectedOld != input.OldOID || effect.ExpectedNew != input.NewOID {
		return fmt.Errorf("publication ref update differs from pending controller authority")
	}
	policy, ok := publicationGuardPolicy(op)
	if !ok || policy.Remote != input.Remote || policy.LocalRef != input.Ref {
		return fmt.Errorf("publication ref update lacks exact pending publication policy")
	}
	return nil
}

func (s *Store) pendingPublicationGuardOperation() (ExecutionOperation, bool, error) {
	head, err := s.LoadHead()
	if err != nil {
		return ExecutionOperation{}, false, err
	}
	if head.PendingTransitionID == "" {
		return ExecutionOperation{}, false, nil
	}
	var op ExecutionOperation
	if err := readJSON(s.executionOperationPath(head.PendingTransitionID), &op); err != nil {
		return ExecutionOperation{}, false, fmt.Errorf("publication guard cannot load pending controller operation: %w", err)
	}
	record, phase, err := s.LoadTransition(head.PendingTransitionID)
	if err != nil {
		return ExecutionOperation{}, false, fmt.Errorf("publication guard cannot load pending controller transition: %w", err)
	}
	if !reflect.DeepEqual(op.Transition, record) {
		return ExecutionOperation{}, false, fmt.Errorf("publication guard rejected mismatched controller operation journal")
	}
	if head.ControllerGeneration != record.PreparedGeneration || phase.Phase != TransitionPhasePrepared {
		return ExecutionOperation{}, false, fmt.Errorf("publication guard rejected stale controller transition")
	}
	if _, err := s.validateExecutionOperation(op); err != nil {
		return ExecutionOperation{}, false, fmt.Errorf("publication guard rejected invalid controller operation: %w", err)
	}
	return op, true, nil
}

func publicationGuardPolicy(op ExecutionOperation) (PublicationPolicy, bool) {
	if op.Publication != nil {
		return op.Publication.Policy, true
	}
	if op.Terminal != nil {
		return op.Terminal.Policy, true
	}
	return PublicationPolicy{}, false
}

func publicationGuardEffect(effects []EffectExpectation, surface MutationSurface, resource string) (EffectExpectation, bool) {
	for _, effect := range effects {
		if effect.Surface == surface && effect.Resource == resource {
			return effect, true
		}
	}
	return EffectExpectation{}, false
}

func publicationGuardOwnsGitMutation(effects []EffectExpectation) bool {
	for _, effect := range effects {
		if effect.Surface == MutationSurfaceRef || effect.Surface == MutationSurfaceHistory {
			return true
		}
	}
	return false
}

func (s *Store) ValidatePublicationGuardRequest(input PublicationGuardInput) error {
	if input.Repository == "" || input.Ref == "" || input.Remote == "" || input.OldOID == "" || input.NewOID == "" {
		return errors.New("publication guard input is incomplete")
	}
	return s.ValidatePublicationGuard(input)
}
