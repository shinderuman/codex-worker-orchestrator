package workflow

func (w *Workflow) ExecuteExplicitFix(instruction, origin, cause string) error {
	return w.ExecuteExplicitFixWithExecutionMilestones(instruction, origin, cause, "")
}
