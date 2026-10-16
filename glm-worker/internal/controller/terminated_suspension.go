package controller

import "fmt"

func (s *Store) suspensionExecutionTerminated(head RepositoryControllerHead, snapshot SuspensionSnapshot) (bool, error) {
	ref := head.UserTerminationRef
	generation := head.ControllerGeneration
	for ref != nil {
		record, err := s.LoadUserTaskTermination(*ref)
		if err != nil {
			return false, err
		}
		source := record.SourceHead
		if record.SchemaVersion != controllerSchemaVersion || record.RepositoryIdentity != s.identity.LineageID || source.RepositoryIdentity != s.identity.LineageID || source.ControllerGeneration >= generation || source.ExecutionTaskRef == nil {
			return false, fmt.Errorf("user termination history authority is inconsistent")
		}
		if source.ExecutionTaskRef.TaskPath == snapshot.SemanticTaskRef.TaskPath && snapshot.ControllerGeneration <= source.ControllerGeneration {
			return true, nil
		}
		generation, ref = source.ControllerGeneration, source.UserTerminationRef
	}
	return false, nil
}
