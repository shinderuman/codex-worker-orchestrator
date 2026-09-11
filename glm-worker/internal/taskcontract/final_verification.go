package taskcontract

const FinalVerificationTaskPath = TasksDir + "/022-final-verification.md"

func (s PlanSchedule) TasksScheduledAfterFinalVerification() ([]string, error) {
	if err := s.parseError(); err != nil {
		return nil, err
	}
	if containsSchedulePath(s.Active, FinalVerificationTaskPath) {
		return append([]string(nil), s.Next...), nil
	}
	for index, path := range s.Next {
		if path == FinalVerificationTaskPath {
			return append([]string(nil), s.Next[index+1:]...), nil
		}
	}
	return nil, nil
}
