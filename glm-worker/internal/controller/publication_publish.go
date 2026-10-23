package controller

import (
	"errors"
	"fmt"
	"strings"
)

func (s *Store) PublishAcceptedCandidate(input PublicationInput) (ExecutionOperationResult, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	defer func() { _ = lock.Close() }()
	head, c, _, err := s.publicationCandidateAuthority(input)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	op, head, err := s.planTerminalMetadata(TerminalTaskInput{ExpectedGeneration: input.ExpectedGeneration, ProjectSnapshotID: head.ProjectSnapshotID, CandidateID: c.CandidateID, TaskRef: c.TaskRef})
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := s.prepareExecutionOperation(&op, head); err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.recoverExecutionOperationLocked(op)
}

func (s *Store) abortRacedPublication(op ExecutionOperation, remote string) error {
	head, err := s.LoadHead()
	if err != nil {
		return err
	}
	if err := s.ensurePublicationPrefix(head, remote); err != nil {
		return err
	}
	if remote == op.Publication.RemoteOID {
		return fmt.Errorf("publication did not apply; retained for retry")
	}
	descendant, err := isPublicationDescendant(s.identity.PrimaryRoot, op.Publication.RemoteOID, remote)
	if err != nil {
		return err
	}
	if !descendant {
		return fmt.Errorf("publication remote movement is outside race authority")
	}
	phase, err := s.loadTransitionState(op.Transition.TransitionID)
	if err != nil {
		return err
	}
	if head.ControllerGeneration != op.Transition.PreparedGeneration || head.PendingTransitionID != op.Transition.TransitionID {
		return fmt.Errorf("publication race no longer owns controller")
	}
	phase.Phase = TransitionPhaseAborted
	phase.Observed = map[string]string{op.Transition.Effects[0].Key(): remote}
	if err := s.writeTransitionState(phase); err != nil {
		return err
	}
	_, err = s.finishPublicationAbort(op, head, phase)
	return err
}

func (s *Store) pushAndObserveCandidate(op ExecutionOperation, c AcceptedCandidate, remote string) (string, error) {
	if remote != c.BaseOID {
		return "", fmt.Errorf("completion remote changed; journal retained")
	}
	if err := s.guardCompletionPush(op, PublicationPushGuardInput{RemoteName: c.Policy.Remote, LocalRef: c.Policy.LocalRef, LocalOID: c.CommitOID, RemoteRef: c.Policy.RemoteRef, RemoteOID: remote}); err != nil {
		return "", s.failCompletionPushGuard(op, err, remote)
	}
	output, pushErr := runGitBinary(s.identity.PrimaryRoot, nil, "push", "--porcelain", c.Policy.Remote, c.Policy.LocalRef+":"+c.Policy.RemoteRef)
	observed, err := observePublicationRemote(s.identity.PrimaryRoot, c.Policy)
	if err != nil {
		return "", fmt.Errorf("publication result is unresolved: %w", err)
	}
	contains, err := isPublicationDescendant(s.identity.PrimaryRoot, c.CommitOID, observed)
	if err != nil {
		return "", err
	}
	if !contains {
		if pushErr != nil && publicationPushRejected(string(output), c.Policy) {
			phase, err := s.loadTransitionState(op.Transition.TransitionID)
			if err != nil {
				return "", err
			}
			if err := s.saveCompletionFailure(op, &phase, "push-rejected", string(output), pushErr.Error(), observed); err != nil {
				return "", err
			}
			return "", s.abortCompletionPrepare(op, phase, observed)
		}
		return "", errors.Join(fmt.Errorf("completion publication outcome is unresolved; journal retained"), pushErr)
	}
	return observed, nil
}

func (s *Store) failCompletionPushGuard(op ExecutionOperation, cause error, remote string) error {
	phase, err := s.loadTransitionState(op.Transition.TransitionID)
	if err != nil {
		return errors.Join(cause, err)
	}
	return s.failUnwrittenCompletion(op, &phase, "push-guard", cause, remote)
}

func (s *Store) validateCandidatePublicationLocal(c AcceptedCandidate) error {
	local, exists, err := readExecutionRef(s.identity.PrimaryRoot, c.Policy.LocalRef)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("publication local ref is missing")
	}
	if local != c.CommitOID {
		return fmt.Errorf("publication local ref no longer matches promoted candidate")
	}
	return nil
}

func publicationPushRejected(output string, policy PublicationPolicy) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) == 3 && fields[0] == "!" && fields[1] == policy.LocalRef+":"+policy.RemoteRef && (strings.HasPrefix(fields[2], "[rejected] ") || strings.HasPrefix(fields[2], "[remote rejected] ")) {
			return true
		}
	}
	return false
}
