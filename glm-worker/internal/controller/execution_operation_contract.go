package controller

import "fmt"

type executionOperationEffectPolicy uint8

const (
	executionOperationEffectsNone executionOperationEffectPolicy = iota
	executionOperationEffectsSingle
	executionOperationEffectsOneOrMore
)

type executionOperationContract struct {
	effects         executionOperationEffectPolicy
	validate        func(ExecutionOperation) error
	apply           func(ExecutionOperation) error
	verifyCommitted func(ExecutionOperation, RepositoryControllerHead) error
	projectResult   func(ExecutionOperation, RepositoryControllerHead, *ExecutionOperationResult) error
}

type executionOperationContractFactory func(*Store) executionOperationContract

var executionOperationContractTable = map[string]executionOperationContractFactory{
	executionModelCall: func(s *Store) executionOperationContract {
		return executionOperationContract{
			effects:         executionOperationEffectsNone,
			validate:        s.validateModelCallAdmission,
			apply:           s.applyModelCallAdmission,
			verifyCommitted: s.verifyCommittedModelCallAdmission,
			projectResult:   s.projectExecutionAdmissionResult,
		}
	},
	terminalRetire: func(s *Store) executionOperationContract {
		return executionOperationContract{
			effects:         executionOperationEffectsOneOrMore,
			validate:        s.validateTerminalMetadataOperation,
			apply:           s.applyTerminalMetadata,
			verifyCommitted: s.verifyCommittedTerminalMetadata,
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
	publicationAccept:     publicationExecutionOperationContract,
	publicationPromote:    publicationExecutionOperationContract,
	publicationPublish:    publicationExecutionOperationContract,
	publicationRebind:     publicationExecutionOperationContract,
	publicationAdopt:      publicationExecutionOperationContract,
	publicationRevalidate: publicationExecutionOperationContract,
	publicationReenter:    publicationExecutionOperationContract,
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
	switch policy {
	case executionOperationEffectsNone:
		if len(op.Transition.Effects) != 0 {
			return fmt.Errorf("execution operation effect authority is incomplete")
		}
	case executionOperationEffectsSingle:
		if len(op.Transition.Effects) != 1 {
			return fmt.Errorf("execution operation effect authority is incomplete")
		}
	case executionOperationEffectsOneOrMore:
		if len(op.Transition.Effects) == 0 {
			return fmt.Errorf("execution operation effect authority is incomplete")
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
