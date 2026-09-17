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
	PostCompletionTerminal  PostCompletionKind = "terminal"
	PostCompletionContinue  PostCompletionKind = "continue"
	PostCompletionBlocked   PostCompletionKind = "blocked"
	PostCompletionExhausted PostCompletionKind = "exhausted"
	PostCompletionGraph     PostCompletionKind = "graph"
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
		return classifyNonGoalPostCompletion(prepared)
	}
	if len(active) > 1 {
		return PostCompletionPlan{}, activeNotUniqueError(len(active))
	}
	prepared.Kind = PostCompletionGraph
	return prepared, nil
}

func classifyNonGoalPostCompletion(prepared PostCompletionPlan) (PostCompletionPlan, error) {
	switch {
	case len(prepared.Active) == 1:
		prepared.Kind = PostCompletionContinue
	case len(prepared.Active) > 1:
		return PostCompletionPlan{}, activeNotUniqueError(len(prepared.Active))
	case len(prepared.Next) != 0:
		return PostCompletionPlan{}, fmt.Errorf(
			"GOALのないPlanの完了同期にはACTIVE昇格済み・BLOCKEDのみ・空scheduleのいずれかが必要です(active=0 next=%d blocked=%d)",
			len(prepared.Next), len(prepared.Blocked),
		)
	case len(prepared.Blocked) != 0:
		prepared.Kind = PostCompletionBlocked
	default:
		prepared.Kind = PostCompletionExhausted
	}
	return prepared, nil
}

func (p PostCompletionPlan) Entries() []string {
	entries := append([]string{}, p.Active...)
	entries = append(entries, p.Next...)
	return append(entries, p.Blocked...)
}

func (p PostCompletionPlan) RequiresTaskGraph() bool {
	return p.Kind == PostCompletionGraph || p.Kind == PostCompletionBlocked
}

func NonGoalCompletionSyncApplied(prepared PostCompletionPlan, completedTask string) bool {
	if prepared.Goal.Present {
		return true
	}
	if completedTask == "" {
		return false
	}
	for _, entries := range [][]string{prepared.Active, prepared.Next, prepared.Blocked} {
		for _, path := range entries {
			if path == completedTask {
				return false
			}
		}
	}
	return true
}

func PrepareFinalHead(plan string) (FinalHeadPlan, error) {
	return prepareFinalHeadSchedule(taskcontract.ParsePlanSchedule(plan))
}

func PrepareParentCompletionHead(plan string) (FinalHeadPlan, error) {
	goal, err := taskcontract.ParsePlanGoal(plan)
	if err != nil {
		return FinalHeadPlan{}, err
	}
	if !goal.Present {
		return prepareNonGoalCompletionHead(plan)
	}
	if goal.Status == taskcontract.GoalStatusCompleted {
		return prepareCompletedFinalHead(plan)
	}
	if goal.Status == taskcontract.GoalStatusActive {
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

func prepareNonGoalCompletionHead(plan string) (FinalHeadPlan, error) {
	prepared, err := PreparePostCompletion(plan)
	if err != nil {
		return FinalHeadPlan{}, err
	}
	switch prepared.Kind {
	case PostCompletionContinue:
		return prepareFinalHeadSchedule(prepared.Schedule)
	case PostCompletionBlocked:
		for _, path := range prepared.Blocked {
			if err := taskcontract.ValidateActiveTaskPath(path); err != nil {
				return FinalHeadPlan{}, err
			}
		}
		return FinalHeadPlan{Schedule: prepared.Schedule, Tasks: append([]string(nil), prepared.Blocked...)}, nil
	case PostCompletionExhausted:
		return FinalHeadPlan{Schedule: prepared.Schedule, Tasks: []string{}}, nil
	}
	return FinalHeadPlan{}, fmt.Errorf("GOALのないPlanのcompletion head種別 %sを受理できません", prepared.Kind)
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

func activeNotUniqueError(active int) error {
	return fmt.Errorf("IMPLEMENTATION_PLAN.local.mdのACTIVE欄が一意ではありません(%d件)", active)
}
