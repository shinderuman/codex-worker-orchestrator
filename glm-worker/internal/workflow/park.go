package workflow

import "io"

func (w *Workflow) ExecutePark(_ io.Writer) error {
	return w.rejectLegacyLifecycleMutation("park")
}

func (w *Workflow) ExecuteUnpark(_ io.Writer) error {
	return w.rejectLegacyLifecycleMutation("unpark")
}
