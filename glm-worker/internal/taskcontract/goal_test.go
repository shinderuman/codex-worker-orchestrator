package taskcontract

import "testing"

func TestParsePlanGoalAbsent(t *testing.T) {
	plan := "# plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/a.md`\n"
	goal, err := ParsePlanGoal(plan)
	if err != nil {
		t.Fatalf("ParsePlanGoal: %v", err)
	}
	if goal.Present {
		t.Fatalf("goal = %#v", goal)
	}
}

func TestParsePlanGoalActive(t *testing.T) {
	plan := "# plan\n\n## GOAL\n\nstatus: active\n\n利用者がGoalだけ提示して開発を完了できること。\n\n### Goal amendments\n\n- 2026-09-03 追加要求原文\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/a.md`\n"
	goal, err := ParsePlanGoal(plan)
	if err != nil {
		t.Fatalf("ParsePlanGoal: %v", err)
	}
	if !goal.Present || goal.Status != GoalStatusActive {
		t.Fatalf("goal = %#v", goal)
	}
}

func TestParsePlanGoalCompleted(t *testing.T) {
	plan := "## GOAL\n\nstatus: completed\n\ncompletion decision: acceptance met\n\n## ACTIVE\n"
	goal, err := ParsePlanGoal(plan)
	if err != nil {
		t.Fatalf("ParsePlanGoal: %v", err)
	}
	if !goal.Present || goal.Status != GoalStatusCompleted {
		t.Fatalf("goal = %#v", goal)
	}
}

func TestParsePlanGoalRejectsDuplicateSection(t *testing.T) {
	plan := "## GOAL\n\nstatus: active\n\n## GOAL\n\nstatus: active\n"
	if _, err := ParsePlanGoal(plan); err == nil {
		t.Fatal("duplicate GOAL section was accepted")
	}
}

func TestParsePlanGoalRejectsMissingStatus(t *testing.T) {
	plan := "## GOAL\n\n利用者がGoalだけ提示する。\n"
	if _, err := ParsePlanGoal(plan); err == nil {
		t.Fatal("GOAL section without status was accepted")
	}
}

func TestParsePlanGoalRejectsDuplicateStatus(t *testing.T) {
	plan := "## GOAL\n\nstatus: active\n\nstatus: completed\n"
	if _, err := ParsePlanGoal(plan); err == nil {
		t.Fatal("duplicate status was accepted")
	}
}

func TestParsePlanGoalRejectsUnknownStatus(t *testing.T) {
	plan := "## GOAL\n\nstatus: done\n"
	if _, err := ParsePlanGoal(plan); err == nil {
		t.Fatal("unknown status was accepted")
	}
}

func TestParsePlanGoalIgnoresFencedAndBulletStatusLines(t *testing.T) {
	plan := "## GOAL\n\n```\nstatus: completed\n```\n\n- status: completed\n\nstatus: active\n"
	goal, err := ParsePlanGoal(plan)
	if err != nil {
		t.Fatalf("ParsePlanGoal: %v", err)
	}
	if goal.Status != GoalStatusActive {
		t.Fatalf("goal = %#v", goal)
	}
}
