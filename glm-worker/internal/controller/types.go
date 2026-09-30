package controller

import "time"

type ControllerStatus string

type AttemptState string

type TransitionPhase string

type EffectClassification string

type MutationSurface string

type SemanticTaskRef struct {
	TaskPath       string `json:"task_path"`
	ContractDigest string `json:"contract_digest"`
}

type RepositoryControllerHead struct {
	SchemaVersion         int              `json:"schema_version"`
	RepositoryIdentity    string           `json:"repository_identity"`
	ControllerGeneration  uint64           `json:"controller_generation"`
	Status                ControllerStatus `json:"status"`
	ProjectSnapshotID     string           `json:"project_snapshot_id,omitempty"`
	RootTaskRef           *SemanticTaskRef `json:"root_task_ref,omitempty"`
	ExecutionTaskRef      *SemanticTaskRef `json:"execution_task_ref,omitempty"`
	ActiveEpisodeID       string           `json:"active_episode_id,omitempty"`
	ActiveEpisodeRevision uint64           `json:"active_episode_revision,omitempty"`
	LiveAttemptID         string           `json:"live_attempt_id,omitempty"`
	LiveLeaseID           string           `json:"live_lease_id,omitempty"`
	PendingTransitionID   string           `json:"pending_transition_id,omitempty"`
	FailureID             string           `json:"failure_id,omitempty"`
}

type AttemptRecord struct {
	SchemaVersion             int             `json:"schema_version"`
	AttemptID                 string          `json:"attempt_id"`
	SemanticTaskRef           SemanticTaskRef `json:"semantic_task_ref"`
	RootTaskRef               SemanticTaskRef `json:"root_task_ref"`
	EpisodeID                 string          `json:"episode_id,omitempty"`
	EpisodeRevision           uint64          `json:"episode_revision,omitempty"`
	PredecessorAttemptID      string          `json:"predecessor_attempt_id,omitempty"`
	ResumedFromSealID         string          `json:"resumed_from_seal_id,omitempty"`
	ExecutionBaseOID          string          `json:"execution_base_oid"`
	BaselineSnapshotID        string          `json:"baseline_snapshot_id"`
	WorkspaceSnapshotID       string          `json:"workspace_snapshot_id"`
	StartControllerGeneration uint64          `json:"start_controller_generation"`
	PublicationAnchorAtStart  string          `json:"publication_anchor_at_start,omitempty"`
	AttemptState              AttemptState    `json:"attempt_state"`
	AttemptSealID             string          `json:"attempt_seal_id,omitempty"`
	CreatedAt                 time.Time       `json:"created_at"`
}

type ExecutionLease struct {
	SchemaVersion               int             `json:"schema_version"`
	LeaseID                     string          `json:"lease_id"`
	AttemptID                   string          `json:"attempt_id"`
	SemanticTaskRef             SemanticTaskRef `json:"semantic_task_ref"`
	Purpose                     string          `json:"purpose"`
	ControllerGeneration        uint64          `json:"controller_generation"`
	EpisodeID                   string          `json:"episode_id,omitempty"`
	EpisodeRevision             uint64          `json:"episode_revision,omitempty"`
	WorkspaceID                 string          `json:"workspace_id"`
	ExpectedBaseOID             string          `json:"expected_base_oid"`
	ExpectedWorkspaceSnapshotID string          `json:"expected_workspace_snapshot_id"`
	InFlightCallID              string          `json:"in_flight_call_id,omitempty"`
	CreatedAt                   time.Time       `json:"created_at"`
}

type EffectExpectation struct {
	Surface     MutationSurface `json:"surface"`
	Resource    string          `json:"resource"`
	ExpectedOld string          `json:"expected_old"`
	ExpectedNew string          `json:"expected_new"`
}

type TransitionRecord struct {
	SchemaVersion          int                 `json:"schema_version"`
	TransitionID           string              `json:"transition_id"`
	Kind                   string              `json:"kind"`
	SourceGeneration       uint64              `json:"source_generation"`
	PreparedGeneration     uint64              `json:"prepared_generation"`
	TargetGeneration       uint64              `json:"target_generation"`
	SourceEpisodeID        string              `json:"source_episode_id,omitempty"`
	SourceEpisodeRevision  uint64              `json:"source_episode_revision,omitempty"`
	SourceAttemptID        string              `json:"source_attempt_id,omitempty"`
	SourceLeaseID          string              `json:"source_lease_id,omitempty"`
	WorkspaceID            string              `json:"workspace_id,omitempty"`
	WorkspaceSnapshotOld   WorkspaceSnapshot   `json:"workspace_snapshot_old"`
	WorkspaceSnapshotNew   WorkspaceSnapshot   `json:"workspace_snapshot_new"`
	ProjectSnapshotOld     string              `json:"project_snapshot_old,omitempty"`
	ProjectSnapshotNew     string              `json:"project_snapshot_new,omitempty"`
	Effects                []EffectExpectation `json:"effects,omitempty"`
	CreatedAt              time.Time           `json:"created_at"`
}

type TransitionState struct {
	SchemaVersion   int                            `json:"schema_version"`
	TransitionID    string                         `json:"transition_id"`
	Phase           TransitionPhase                `json:"phase"`
	Observed        map[string]string              `json:"observed,omitempty"`
	Classifications map[string]EffectClassification `json:"classifications,omitempty"`
	UpdatedAt       time.Time                      `json:"updated_at"`
}

type MutationRecord struct {
	SchemaVersion    int               `json:"schema_version"`
	MutationID       string            `json:"mutation_id"`
	AttemptID        string            `json:"attempt_id"`
	SourceLeaseID    string            `json:"source_lease_id"`
	TargetLeaseID    string            `json:"target_lease_id"`
	SourceGeneration uint64            `json:"source_generation"`
	TargetGeneration uint64            `json:"target_generation"`
	Command          string            `json:"command"`
	Outcome          string            `json:"outcome"`
	Before           WorkspaceSnapshot `json:"before"`
	After            WorkspaceSnapshot `json:"after"`
	Surfaces         []MutationSurface `json:"surfaces"`
	CreatedAt        time.Time         `json:"created_at"`
}

type FailureRecord struct {
	SchemaVersion int               `json:"schema_version"`
	FailureID     string            `json:"failure_id"`
	Reason        string            `json:"reason"`
	TransitionID  string            `json:"transition_id,omitempty"`
	Generation    uint64            `json:"generation"`
	WorkspaceID   string            `json:"workspace_id,omitempty"`
	Expected      WorkspaceSnapshot `json:"expected,omitempty"`
	Actual        WorkspaceSnapshot `json:"actual,omitempty"`
	Observed      map[string]string `json:"observed,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
}

type Admission struct {
	Head      RepositoryControllerHead
	Attempt   AttemptRecord
	Lease     ExecutionLease
	Workspace WorkspaceIdentity
	Snapshot  WorkspaceSnapshot
}

const (
	ControllerStatusActive     ControllerStatus = "active"
	ControllerStatusFailClosed ControllerStatus = "failed-closed"

	AttemptStatePrepared            AttemptState = "prepared"
	AttemptStateLive                AttemptState = "live"
	AttemptStateQuiescing           AttemptState = "quiescing"
	AttemptStateSuspendedForBlocker AttemptState = "suspended-for-blocker"
	AttemptStateAccepted            AttemptState = "accepted"
	AttemptStateFailedClosed        AttemptState = "failed-closed"

	TransitionPhasePrepared   TransitionPhase = "prepared"
	TransitionPhaseApplied    TransitionPhase = "applied"
	TransitionPhaseCommitted  TransitionPhase = "committed"
	TransitionPhaseFinalizing TransitionPhase = "finalizing"
	TransitionPhaseFinalized  TransitionPhase = "finalized"
	TransitionPhaseAborted    TransitionPhase = "aborted"
	TransitionPhaseFailed     TransitionPhase = "failed-closed"

	EffectExpectedOld EffectClassification = "expected-old"
	EffectExpectedNew EffectClassification = "expected-new"
	EffectUnexpected  EffectClassification = "unexpected"

	MutationSurfaceSource  MutationSurface = "source"
	MutationSurfaceIndex   MutationSurface = "index"
	MutationSurfaceHead    MutationSurface = "head"
	MutationSurfaceRef     MutationSurface = "ref"
	MutationSurfaceHistory MutationSurface = "history"
	MutationSurfaceCleanup MutationSurface = "cleanup"
	MutationSurfaceState   MutationSurface = "controller-state"
)

func (ref SemanticTaskRef) Equal(other SemanticTaskRef) bool {
	return ref.TaskPath == other.TaskPath && ref.ContractDigest == other.ContractDigest
}

func (ref SemanticTaskRef) Empty() bool {
	return ref.TaskPath == "" && ref.ContractDigest == ""
}

func (effect EffectExpectation) Key() string {
	return string(effect.Surface) + "\x00" + effect.Resource
}
