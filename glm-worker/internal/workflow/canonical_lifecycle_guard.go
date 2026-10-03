package workflow

import "fmt"

func (*Workflow) rejectLegacyLifecycleMutation(operation string) error {
	return &WorkerError{
		Phase:   operation,
		Message: fmt.Sprintf("%s is unavailable after canonical controller cutover; use the typed controller operation", operation),
	}
}
