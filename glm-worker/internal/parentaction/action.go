package parentaction

type Action string

type PayloadAction struct {
	Action     Action
	WorkerMode string
}

const (
	ActionDecision           Action = "decision"
	ActionFix                Action = "fix"
	ActionStartMilestones    Action = "start-milestones"
	ActionReviseMilestones   Action = "revise-milestones"
	ActionObservationExecute Action = "observation-execute"
)

var payloadActions = map[Action]PayloadAction{
	ActionDecision: {
		Action:     ActionDecision,
		WorkerMode: "--decision-stdin",
	},
	ActionFix: {
		Action:     ActionFix,
		WorkerMode: "--fix-stdin",
	},
	ActionStartMilestones: {
		Action:     ActionStartMilestones,
		WorkerMode: "--execution-milestones-stdin",
	},
	ActionReviseMilestones: {
		Action:     ActionReviseMilestones,
		WorkerMode: "--execution-milestones-revise-stdin",
	},
}

func LookupPayloadAction(action string) (PayloadAction, bool) {
	descriptor, ok := payloadActions[Action(action)]
	return descriptor, ok
}

func IsControllerAction(action string) bool {
	switch action {
	case "controller-semantic", "controller-execution", "controller-publication", "controller-evidence":
		return true
	default:
		return false
	}
}
