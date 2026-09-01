package state

func (s *StateStore) InvalidateSession(role SessionRole) error {
	return s.Remove(
		string(role)+".id",
		string(role)+".ready",
	)
}

func (s *StateStore) InvalidateAllSessions() error {
	if err := s.InvalidateSession(WorkerRole); err != nil {
		return err
	}
	return s.InvalidateSession(ReviewerRole)
}
