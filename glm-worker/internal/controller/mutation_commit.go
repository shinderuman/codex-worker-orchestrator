package controller

func (s *Store) RecordAdmittedMutation(
	admission Admission,
	command string,
	outcome string,
	after WorkspaceSnapshot,
) (Admission, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return Admission{}, err
	}
	defer func() { _ = lock.Close() }()
	return s.RecordMutation(admission, command, outcome, after)
}
