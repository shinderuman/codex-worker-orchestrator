package workflow

// ExecuteExplicitFix keeps legacy regression call sites test-only while routing
// them through the production milestone-aware explicit-fix entrypoint.
func (w *Workflow) ExecuteExplicitFix(instruction, origin, cause string) error {
	return w.ExecuteExplicitFixWithExecutionMilestones(instruction, origin, cause, "")
}
