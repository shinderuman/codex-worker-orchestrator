package controller

import "fmt"

type MutationAuthority struct {
	ControllerGeneration uint64
	AttemptID            string
	LeaseID              string
	SemanticTaskRef      SemanticTaskRef
}

func (a Admission) MutationAuthority() MutationAuthority {
	return MutationAuthority{
		ControllerGeneration: a.Head.ControllerGeneration,
		AttemptID:            a.Attempt.AttemptID,
		LeaseID:              a.Lease.LeaseID,
		SemanticTaskRef:      a.Lease.SemanticTaskRef,
	}
}

func MutationAuthorityFromHead(head RepositoryControllerHead) (MutationAuthority, error) {
	if head.ControllerGeneration == 0 || head.LiveAttemptID == "" || head.LiveLeaseID == "" || head.ExecutionTaskRef == nil {
		return MutationAuthority{}, fmt.Errorf("repository controller has no complete mutation authority")
	}
	return MutationAuthority{
		ControllerGeneration: head.ControllerGeneration,
		AttemptID:            head.LiveAttemptID,
		LeaseID:              head.LiveLeaseID,
		SemanticTaskRef:      *head.ExecutionTaskRef,
	}, nil
}

func validateMutationAuthorityClaim(head RepositoryControllerHead, authority MutationAuthority) error {
	if authority.ControllerGeneration != head.ControllerGeneration {
		return fmt.Errorf("mutation authority generation is stale: claim=%d controller=%d", authority.ControllerGeneration, head.ControllerGeneration)
	}
	if authority.AttemptID == "" || authority.AttemptID != head.LiveAttemptID {
		return fmt.Errorf("mutation authority attempt is stale")
	}
	if authority.LeaseID == "" || authority.LeaseID != head.LiveLeaseID {
		return fmt.Errorf("mutation authority lease is stale")
	}
	if head.ExecutionTaskRef == nil || !head.ExecutionTaskRef.Equal(authority.SemanticTaskRef) {
		return fmt.Errorf("mutation authority semantic task is stale")
	}
	return nil
}
