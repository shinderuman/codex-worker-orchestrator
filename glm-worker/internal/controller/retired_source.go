package controller

import (
	"errors"
	"fmt"
	"reflect"
)

type RetiredSourceAdvancementInput struct {
	ExpectedGeneration uint64            `json:"expected_generation"`
	Policy             PublicationPolicy `json:"policy"`
	SourceOID          string            `json:"source_oid"`
}

type externalSourceChange struct{ Path string }

func (s *Store) AdoptRetiredExternalSource(input RetiredSourceAdvancementInput) (ExecutionOperationResult, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	defer func() { _ = lock.Close() }()
	head, err := s.quiescentExecutionHead(input.ExpectedGeneration)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if _, err := s.retiredProjectAuthority(head); err != nil {
		return ExecutionOperationResult{}, err
	}
	if _, err := s.QuiescentRuntimeAdmission(head); err != nil {
		return ExecutionOperationResult{}, err
	}
	if head.PublicationPolicy == nil || input.Policy != *head.PublicationPolicy {
		return ExecutionOperationResult{}, fmt.Errorf("external source publication policy differs")
	}
	if input.SourceOID == "" || input.SourceOID == head.IntegrationTip {
		return ExecutionOperationResult{}, fmt.Errorf("external source requires an exact advanced commit")
	}
	if err := s.validateRetiredSourceBoundary(input.Policy, input.SourceOID); err != nil {
		return ExecutionOperationResult{}, err
	}
	op, err := s.planRetiredSourceAdvancement(head, input.Policy, input.SourceOID)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.executeRetiredAdvancementPlan(head, op)
}

func (s *Store) planRetiredSourceAdvancement(head RepositoryControllerHead, policy PublicationPolicy, source string) (ExecutionOperation, error) {
	authority, err := ResolveCommittedTaskAuthorityAt(s.identity.PrimaryRoot, source)
	if err != nil {
		return ExecutionOperation{}, err
	}
	op, err := s.retiredAdvancementOperation(head, policy, authority, source, source, publicationAdoptSource)
	if err != nil {
		return ExecutionOperation{}, err
	}
	if err := s.sealRetiredAdvancementPlan(head, &op, source); err != nil {
		return ExecutionOperation{}, err
	}
	return op, nil
}

func retiredBaseAdvancement(op ExecutionOperation) bool {
	return retiredMetadataAdvancement(op) || (op.Transition.Kind == publicationAdoptSource && op.Publication != nil && op.Publication.After == nil && op.Publication.Project != nil && retiredMetadataHead(op.Source.Head))
}

func (s *Store) validateRetiredAdvancement(op ExecutionOperation) error {
	if op.Transition.Kind == publicationAdoptSource {
		return s.validateRetiredSourceAdvancement(op)
	}
	return s.validateRetiredMetadataAdvancement(op)
}

func (s *Store) validateRetiredSourceAdvancement(op ExecutionOperation) error {
	p := op.Publication
	if err := validateRetiredSourceTransition(op); err != nil {
		return err
	}

	descendant, err := isPublicationDescendant(s.identity.PrimaryRoot, p.OldTip, p.NewTip)
	if err != nil || !descendant || p.OldTip == p.NewTip {
		return fmt.Errorf("external source is not a fast-forward: %w", err)
	}
	authority, err := ResolveCommittedTaskAuthorityAt(s.identity.PrimaryRoot, p.NewTip)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(authority.Snapshot, *p.Project) || !authority.Task.Equal(op.Transition.TargetRootTaskRef) {
		return fmt.Errorf("external source selected Task authority differs")
	}
	return nil
}

func validateRetiredSourceTransition(op ExecutionOperation) error {
	head := op.Source.Head
	p := op.Publication
	if !retiredBaseAdvancement(op) || p == nil || head.PublicationPolicy == nil || *head.PublicationPolicy != p.Policy || p.OldTip != head.IntegrationTip || p.NewTip != p.RemoteOID || p.LocalOld != "" || p.LocalNew != "" {
		return fmt.Errorf("external source advancement authority differs")
	}
	return validateRetiredSourceEffects(op)
}

func validateRetiredSourceEffects(op ExecutionOperation) error {
	head := op.Source.Head
	p := op.Publication
	if op.Transition.SourceGeneration != head.ControllerGeneration || op.Transition.ProjectSnapshotOld != head.ProjectSnapshotID || len(op.Transition.Effects) != 1 {
		return fmt.Errorf("external source transition authority differs")
	}
	effect := op.Transition.Effects[0]
	if effect.Surface != MutationSurfaceState || effect.Resource != "integration-tip" || effect.ExpectedOld != p.OldTip || effect.ExpectedNew != p.NewTip {
		return fmt.Errorf("external source advancement changes unauthorized surfaces")
	}
	return nil
}

func (s *Store) validateRetiredSourceBoundary(policy PublicationPolicy, source string) error {
	status, err := gitTrimmed(s.identity.PrimaryRoot, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return err
	}
	if status != "" {
		return fmt.Errorf("external source adoption requires a clean primary worktree")
	}
	branch, err := gitTrimmed(s.identity.PrimaryRoot, "symbolic-ref", "--quiet", "HEAD")
	if err != nil || branch != policy.LocalRef {
		return fmt.Errorf("external source adoption requires the configured primary branch: %w", err)
	}
	local, exists, err := readExecutionRef(s.identity.PrimaryRoot, policy.LocalRef)
	if err != nil || !exists || local != source {
		return fmt.Errorf("external source local commit differs: %w", err)
	}
	remote, err := observePublicationRemote(s.identity.PrimaryRoot, policy)
	if err != nil {
		return err
	}
	if remote != source {
		return fmt.Errorf("external source remote commit differs")
	}
	return nil
}

func (e *externalSourceChange) Error() string {
	return fmt.Sprintf("external advancement changes non-parent metadata: %s", e.Path)
}

func (s *Store) RetiredSourceAdvancementRequired(head RepositoryControllerHead, source string) (bool, error) {
	if !retiredMetadataHead(head) {
		return false, fmt.Errorf("external source synchronization requires retired authority")
	}
	err := validateParentMetadataHistory(s.identity.PrimaryRoot, head.IntegrationTip, source)
	var change *externalSourceChange
	if errors.As(err, &change) {
		return true, nil
	}
	return false, err
}

func (s *Store) validateRetiredAdvancementBoundary(op ExecutionOperation) error {
	if !retiredBaseAdvancement(op) {
		return nil
	}
	if op.Transition.Kind == publicationAdoptSource {
		if err := s.validateRetiredSourceBoundary(op.Publication.Policy, op.Publication.NewTip); err != nil {
			return err
		}
	}
	_, err := s.QuiescentRuntimeAdmission(op.Source.Head)
	return err
}
