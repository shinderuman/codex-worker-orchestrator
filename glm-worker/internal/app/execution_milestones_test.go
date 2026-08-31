package app

import "testing"

func TestExecutionMilestoneTaskCommandUsesNormalNewTaskAdmission(t *testing.T) {
	command, err := ParseCommand([]string{"--execution-milestones-stdin", "128", "--sha256", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	if command.Mode != ModeNewTask || !command.ExecutionMilestones || command.StdinBytes != 128 {
		t.Fatalf("command = %+v", command)
	}
}

func TestExecutionMilestoneRevisionCommandIsLockedMetadataAction(t *testing.T) {
	command, err := ParseCommand([]string{"--execution-milestones-revise-stdin", "64"})
	if err != nil {
		t.Fatal(err)
	}
	if command.Mode != ModeExecutionMilestonesRevise || command.ExecutionMilestones || command.StdinBytes != 64 {
		t.Fatalf("command = %+v", command)
	}
}
