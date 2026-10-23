package controller

import (
	"errors"
	"fmt"
)

type executionOperationEffectPolicy uint8

type executionOperationContract struct {
	effects         executionOperationEffectPolicy
	validate        func(ExecutionOperation) error
	apply           func(ExecutionOperation) error
	verifyCommitted func(ExecutionOperation, RepositoryControllerHead) error
	projectResult   func(ExecutionOperation, RepositoryControllerHead, *ExecutionOperationResult) error
}

type executionOperationContractFactory func(*Store) executionOperationContract

const (
	executionOperationEffectsNone executionOperationEffectPolicy = iota
	executionOperationEffectsSingle
	executionOperationEffectsOneOrMore
	executionOperationEffectAuthorityIncomplete = "execution operation effect authority is incomplete"
)

var executionOperationContractTable = map[string]executionOperationContractFactory{
	executionParentSourceImport: func(s *Store) executionOperationContract {
		return executionOperationContract{effects: executionOperationEffectsSingle, validate: s.validateParentSourceImport, apply: s.applyParentSourceImport, verifyCommitted: s.verifyCommittedParentSourceImport, projectResult: s.projectExecutionAdmissionResult}
	},
	executionTaskAuthoritySync: func(s *Store) executionOperationContract {
		return executionOperationContract{effects: executionOperationEffectsSingle, validate: s.validateTaskAuthoritySync, apply: s.applyParentSourceImport, verifyCommitted: s.verifyCommittedParentSourceImport, projectResult: s.projectExecutionAdmissionResult}
	},
	userTaskTransition: func(s *Store) executionOperationContract {
		return executionOperationContract{effects: executionOperationEffectsSingle, validate: s.validateUserTaskTransitionOperation, apply: s.applyUserTaskTransition, verifyCommitted: s.verifyCommittedUserTaskTransition}
	},
	userExecutionTransition: func(s *Store) executionOperationContract {
		return executionOperationContract{effects: executionOperationEffectsSingle, validate: s.validateUserTaskTransitionOperation, apply: s.applyUserTaskTransition, verifyCommitted: s.verifyCommittedUserExecutionTransition}
	},
	executionModelCall: func(s *Store) executionOperationContract {
		return executionOperationContract{
			effects:         executionOperationEffectsNone,
			validate:        s.validateModelCallAdmission,
			apply:           s.applyModelCallAdmission,
			verifyCommitted: s.verifyCommittedModelCallAdmission,
			projectResult:   s.projectExecutionAdmissionResult,
		}
	},
	executionSuspend: func(s *Store) executionOperationContract {
		return executionOperationContract{
			effects:         executionOperationEffectsSingle,
			validate:        s.validateSuspensionOperation,
			apply:           s.applyExecutionSuspension,
			verifyCommitted: s.verifyCommittedSuspension,
		}
	},
	executionMaterialize: func(s *Store) executionOperationContract {
		return executionOperationContract{
			effects:         executionOperationEffectsSingle,
			validate:        s.validateMaterializationOperation,
			apply:           s.applyExecutionMaterialization,
			verifyCommitted: s.verifyCommittedMaterialization,
			projectResult:   s.projectExecutionAdmissionResult,
		}
	},
	executionCleanup: func(s *Store) executionOperationContract {
		return executionOperationContract{
			effects:         executionOperationEffectsSingle,
			validate:        s.validateCleanupOperation,
			apply:           s.applyExecutionCleanup,
			verifyCommitted: s.verifyCommittedCleanup,
		}
	},
	executionGC: func(s *Store) executionOperationContract {
		return executionOperationContract{
			effects:         executionOperationEffectsSingle,
			validate:        validateSuspensionGCOperation,
			apply:           s.applySuspensionGC,
			verifyCommitted: s.verifyCommittedSuspensionGC,
		}
	},
	publicationAccept:  publicationExecutionOperationContract,
	publicationPromote: publicationExecutionOperationContract,
	publicationPublish: func(s *Store) executionOperationContract {
		return executionOperationContract{effects: executionOperationEffectsOneOrMore, validate: s.validateTerminalMetadataOperation, apply: s.applyCandidateCompletion, verifyCommitted: s.verifyCommittedCompletion, projectResult: projectTerminalExecutionResult}
	},
	publicationRebind:      publicationExecutionOperationContract,
	publicationAdopt:       publicationExecutionOperationContract,
	publicationAdoptSource: publicationExecutionOperationContract,
	publicationRevalidate:  publicationExecutionOperationContract,
	publicationReenter:     publicationExecutionOperationContract,
}

func (s *Store) executionOperationContract(kind string) (executionOperationContract, error) {
	factory, ok := executionOperationContractTable[kind]
	if !ok {
		return executionOperationContract{}, fmt.Errorf("unknown execution operation")
	}
	return factory(s), nil
}

func publicationExecutionOperationContract(s *Store) executionOperationContract {
	return executionOperationContract{
		effects:         executionOperationEffectsOneOrMore,
		validate:        s.validatePublicationOperation,
		apply:           s.applyPublicationOperation,
		verifyCommitted: s.verifyCommittedPublication,
		projectResult:   projectPublicationExecutionResult,
	}
}

func validateExecutionOperationContract(contract executionOperationContract) error {
	if contract.validate == nil || contract.apply == nil || contract.verifyCommitted == nil {
		return fmt.Errorf("execution operation contract is incomplete")
	}
	return nil
}

func validateExecutionOperationEffects(op ExecutionOperation, policy executionOperationEffectPolicy) error {
	seen := map[string]bool{}
	for _, effect := range op.Transition.Effects {
		if seen[effect.Key()] {
			return fmt.Errorf("execution operation repeats effect authority")
		}
		seen[effect.Key()] = true
	}
	switch policy {
	case executionOperationEffectsNone:
		if len(op.Transition.Effects) != 0 {
			return errors.New(executionOperationEffectAuthorityIncomplete)
		}
	case executionOperationEffectsSingle:
		if len(op.Transition.Effects) != 1 {
			return errors.New(executionOperationEffectAuthorityIncomplete)
		}
	case executionOperationEffectsOneOrMore:
		if len(op.Transition.Effects) == 0 {
			return errors.New(executionOperationEffectAuthorityIncomplete)
		}
	default:
		return fmt.Errorf("execution operation effect policy is invalid")
	}
	return nil
}

func (s *Store) projectExecutionAdmissionResult(op ExecutionOperation, head RepositoryControllerHead, result *ExecutionOperationResult) error {
	if op.Workspace == nil {
		return fmt.Errorf("execution operation workspace authority is missing")
	}
	workspace, err := ResolveWorkspaceIdentity(op.Workspace.Root, s.identity)
	if err != nil {
		return err
	}
	snapshot, err := CaptureWorkspaceSnapshot(workspace.Root)
	if err != nil {
		return err
	}
	authority, err := MutationAuthorityFromHead(head)
	if err != nil {
		return err
	}
	admission, err := s.AdmitMutation(authority, workspace, snapshot)
	if err != nil {
		return err
	}
	result.Admission = &admission
	return nil
}

func projectPublicationExecutionResult(op ExecutionOperation, _ RepositoryControllerHead, result *ExecutionOperationResult) error {
	if op.Publication != nil {
		result.CandidateRef = op.Publication.After
	}
	return nil
}

func projectTerminalExecutionResult(op ExecutionOperation, _ RepositoryControllerHead, result *ExecutionOperationResult) error {
	result.CandidateRef = &op.Terminal.Record.CandidateRef
	return nil
}
