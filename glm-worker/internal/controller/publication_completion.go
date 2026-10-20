package controller

import (
	"fmt"
)

func (s *Store) completionImplementation(c AcceptedCandidate) (AcceptedCandidate, error) {
	if c.ImplementationRef == nil {
		return c, nil
	}
	implementation, err := s.LoadAcceptedCandidate(*c.ImplementationRef)
	if err != nil {
		return implementation, err
	}
	if c.State != candidateObserved || !c.EvidenceValid || len(c.Evidence) == 0 {
		return implementation, fmt.Errorf("completion candidate is not observed with valid evidence")
	}
	if implementation.ImplementationRef != nil ||
		!sameCompletionSource(implementation, c) ||
		!evidenceRefListsEqual(implementation.Evidence, c.Evidence) {
		return implementation, fmt.Errorf("completion implementation authority differs")
	}
	return implementation, nil
}

func sameCompletionSource(left, right AcceptedCandidate) bool {
	return left.CommitOID == right.CommitOID && left.TreeOID == right.TreeOID &&
		left.AttemptID == right.AttemptID && left.ProjectSnapshotID == right.ProjectSnapshotID &&
		left.TaskRef.Equal(right.TaskRef)
}

func evidenceRefListsEqual(a, b []EvidenceObjectRef) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if !evidenceRefsEqual(a[index], b[index]) {
			return false
		}
	}
	return true
}
