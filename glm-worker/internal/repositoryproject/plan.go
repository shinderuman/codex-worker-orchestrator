package repositoryproject

import (
	"fmt"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

type ProjectStatePlan struct {
	Goal     taskcontract.PlanGoal
	Schedule taskcontract.PlanSchedule
	Active   []string
	Next     []string
	Blocked  []string
}

type PostCompletionKind string

type PostCompletionPlan struct {
	Kind     PostCompletionKind
	Goal     taskcontract.PlanGoal
	Schedule taskcontract.PlanSchedule
	Active   []string
	Next     []string
	Blocked  []string
}

type FinalHeadPlan struct {
	Schedule   taskcontract.PlanSchedule
	ActiveTask string
	Tasks      []string
}

const (
	PostCompletionTerminal PostCompletionKind = "terminal"
	PostCompletionUnbound  PostCompletionKind = "unbound"
	PostCompletionGraph    PostCompletionKind = "graph"
)

func PrepareProjectState(plan string) (ProjectStatePlan, error) {
	goal, err := taskcontract.ParsePlanGoal(plan)
	if err != nil {
		return ProjectStatePlan{}, err
	}
	schedule := taskcontract.ParsePlanSchedule(plan)
	next, blocked, err := schedule.NonActiveEntries()
	if err != nil {
		return ProjectStatePlan{}, err
	}
	if goal.Present && goal.Status == taskcontract.GoalStatusCompleted {
		active, err := schedule.ActiveEntries()
		if err != nil {
			return ProjectStatePlan{}, err
		}
		if len(active) > 0 || len(next) > 0 || len(blocked) > 0 {
			return ProjectStatePlan{}, completedGoalScheduleError(len(active), len(next), len(blocked))
		}
		return ProjectStatePlan{
			Goal: goal, Schedule: schedule, Active: []string{}, Next: []string{}, Blocked: []string{},
		}, nil
	}
	activeTask, err := schedule.ValidateComplete()
	if err != nil {
		return ProjectStatePlan{}, err
	}
	return ProjectStatePlan{
		Goal: goal, Schedule: schedule, Active: []string{activeTask}, Next: next, Blocked: blocked,
	}, nil
}

func PreparePostCompletion(plan string) (PostCompletionPlan, error) {
	goal, err := taskcontract.ParsePlanGoal(plan)
	if err != nil {
		return PostCompletionPlan{}, err
	}
	schedule := taskcontract.ParsePlanSchedule(plan)
	active, err := schedule.ActiveEntries()
	if err != nil {
		return PostCompletionPlan{}, err
	}
	next, blocked, err := schedule.NonActiveEntries()
	if err != nil {
		return PostCompletionPlan{}, err
	}
	prepared := PostCompletionPlan{
		Goal: goal, Schedule: schedule, Active: active, Next: next, Blocked: blocked,
	}
	if goal.Present && goal.Status == taskcontract.GoalStatusCompleted {
		if len(active) != 0 || len(next) != 0 || len(blocked) != 0 {
			return PostCompletionPlan{}, completedGoalScheduleError(len(active), len(next), len(blocked))
		}
		prepared.Kind = PostCompletionTerminal
		return prepared, nil
	}
	if !goal.Present {
		prepared.Kind = PostCompletionUnbound
		return prepared, nil
	}
	if len(active) > 1 {
		return PostCompletionPlan{}, fmt.Errorf("IMPLEMENTATION_PLAN.local.mdのACTIVE欄が一意ではありません(%d件)", len(active))
	}
	prepared.Kind = PostCompletionGraph
	return prepared, nil
}

func (p PostCompletionPlan) Entries() []string {
	entries := append([]string{}, p.Active...)
	entries = append(entries, p.Next...)
	return append(entries, p.Blocked...)
}

func PrepareFinalHead(plan string) (FinalHeadPlan, error) {
	return prepareFinalHeadSchedule(taskcontract.ParsePlanSchedule(plan))
}

func PrepareParentCompletionHead(plan string) (FinalHeadPlan, error) {
	goal, err := taskcontract.ParsePlanGoal(plan)
	if err != nil {
		return FinalHeadPlan{}, err
	}
	if goal.Present && goal.Status == taskcontract.GoalStatusCompleted {
		return prepareCompletedFinalHead(plan)
	}
	if goal.Present && goal.Status == taskcontract.GoalStatusActive {
		blockedOnly, prepared, err := prepareBlockedOnlyFinalHead(plan)
		if err != nil {
			return FinalHeadPlan{}, err
		}
		if blockedOnly {
			return prepared, nil
		}
	}
	return PrepareFinalHead(plan)
}

func prepareCompletedFinalHead(plan string) (FinalHeadPlan, error) {
	schedule := taskcontract.ParsePlanSchedule(plan)
	active, err := schedule.ActiveEntries()
	if err != nil {
		return FinalHeadPlan{}, err
	}
	next, blocked, err := schedule.NonActiveEntries()
	if err != nil {
		return FinalHeadPlan{}, err
	}
	if len(active) > 0 || len(next) > 0 || len(blocked) > 0 {
		return FinalHeadPlan{}, fmt.Errorf(
			"completed GOALのHEAD planはACTIVE/NEXT/BLOCKEDを空にする必要があります(active=%d next=%d blocked=%d)",
			len(active), len(next), len(blocked),
		)
	}
	return FinalHeadPlan{Schedule: schedule, Tasks: []string{}}, nil
}

func prepareBlockedOnlyFinalHead(plan string) (bool, FinalHeadPlan, error) {
	schedule := taskcontract.ParsePlanSchedule(plan)
	active, err := schedule.ActiveEntries()
	if err != nil {
		return false, FinalHeadPlan{}, err
	}
	next, blocked, err := schedule.NonActiveEntries()
	if err != nil {
		return false, FinalHeadPlan{}, err
	}
	if len(active) != 0 || len(next) != 0 || len(blocked) == 0 {
		return false, FinalHeadPlan{}, nil
	}
	for _, path := range blocked {
		if err := taskcontract.ValidateActiveTaskPath(path); err != nil {
			return true, FinalHeadPlan{}, err
		}
	}
	return true, FinalHeadPlan{Schedule: schedule, Tasks: append([]string(nil), blocked...)}, nil
}

func prepareFinalHeadSchedule(schedule taskcontract.PlanSchedule) (FinalHeadPlan, error) {
	activePath, err := schedule.ValidateComplete()
	if err != nil {
		return FinalHeadPlan{}, err
	}
	tasks := []string{activePath}
	tasks = append(tasks, schedule.Next...)
	tasks = append(tasks, schedule.Blocked...)
	return FinalHeadPlan{Schedule: schedule, ActiveTask: activePath, Tasks: tasks}, nil
}

func ValidateClosure(schedule taskcontract.PlanSchedule, entries []taskcontract.TaskCorpusEntry, prefix string) error {
	failures := schedule.ClosureFailures(entries)
	if len(failures) == 0 {
		return nil
	}
	reasons := make([]string, 0, len(failures))
	for _, failure := range failures {
		reasons = append(reasons, failure.Reason)
	}
	return fmt.Errorf("%s: %s", prefix, strings.Join(reasons, "; "))
}

func ValidateActiveTaskContent(content []byte) error {
	_, err := taskcontract.ParseExternalFeasibility(content)
	return err
}

func completedGoalScheduleError(active, next, blocked int) error {
	return fmt.Errorf(
		"completed GOALではACTIVE/NEXT/BLOCKEDを空にする必要があります(active=%d next=%d blocked=%d)",
		active, next, blocked,
	)
}
