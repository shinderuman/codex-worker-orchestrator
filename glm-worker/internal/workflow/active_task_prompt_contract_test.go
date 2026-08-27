package workflow

import "testing"

func TestActiveTaskWorkerContractPinsSemanticsWithoutProse(t *testing.T) {
	contract := newActiveTaskPromptContract("IMPLEMENTATION_TASKS/task.md", activeTaskAudienceWorker)
	if !contract.valid() {
		t.Fatal("worker contract must be valid")
	}
	if contract.requirements != activeTaskWorkerRequirements {
		t.Fatalf("requirements = %b want %b", contract.requirements, activeTaskWorkerRequirements)
	}
	if contract.verifyDerivedContract {
		t.Fatal("worker must not own derived-contract review")
	}
}

func TestActiveTaskReviewerContractAddsIndependentContractCheck(t *testing.T) {
	contract := newActiveTaskPromptContract("IMPLEMENTATION_TASKS/task.md", activeTaskAudienceReviewer)
	if !contract.valid() {
		t.Fatal("reviewer contract must be valid")
	}
	if contract.requirements != activeTaskReviewerRequirements {
		t.Fatalf("requirements = %b want %b", contract.requirements, activeTaskReviewerRequirements)
	}
	if !contract.verifyDerivedContract {
		t.Fatal("reviewer must verify derived contract against requirement source")
	}
}

func TestActiveTaskContractRejectsSemanticWeakening(t *testing.T) {
	contract := newActiveTaskPromptContract("IMPLEMENTATION_TASKS/task.md", activeTaskAudienceWorker)
	contract.requirements &^= activeTaskAcceptanceCriteria
	if contract.valid() {
		t.Fatal("dropping acceptance criteria must invalidate the machine contract")
	}

	contract = newActiveTaskPromptContract("IMPLEMENTATION_TASKS/task.md", activeTaskAudienceReviewer)
	contract.verifyDerivedContract = false
	if contract.valid() {
		t.Fatal("disabling reviewer derived-contract verification must invalidate the machine contract")
	}

	contract = newActiveTaskPromptContract("IMPLEMENTATION_TASKS/task.md", activeTaskAudienceWorker)
	contract.parentManaged = false
	if contract.valid() {
		t.Fatal("allowing worker ownership of the requirement source must invalidate the machine contract")
	}
}

func TestActiveTaskContractRendersStructuredContext(t *testing.T) {
	contract := newActiveTaskPromptContract("IMPLEMENTATION_TASKS/task.md", activeTaskAudienceWorker)
	want := "ACTIVE_TASK_CONTEXT:\n" +
		"PATH: IMPLEMENTATION_TASKS/task.md\n" +
		"AUDIENCE: worker\n" +
		"SOURCE_AUTHORITY: active-task-file\n" +
		"REQUIRED_SECTIONS: original-instruction,amendments,resolved-references,contract,must-not,acceptance-criteria\n" +
		"PARENT_MANAGED: true\n" +
		"DERIVED_CONTRACT_REVIEW: false\n" +
		"END_ACTIVE_TASK_CONTEXT\n"
	if got := renderActiveTaskPromptContract(contract); got != want {
		t.Fatalf("structured active-task context = %q want %q", got, want)
	}
}
