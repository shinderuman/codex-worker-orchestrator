package controller

import (
	"path"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
)

type EvidenceAnalysisTaskResources struct {
	Status       string                         `json:"status"`
	Observations []EvidenceAnalysisTaskResource `json:"observations,omitempty"`
}

type EvidenceAnalysisTaskResource struct {
	Entry     string    `json:"entry"`
	TaskID    string    `json:"task_id,omitempty"`
	CallID    string    `json:"call_id,omitempty"`
	Role      string    `json:"role,omitempty"`
	Phase     string    `json:"phase,omitempty"`
	State     string    `json:"state"`
	Coverage  string    `json:"coverage,omitempty"`
	Samples   int       `json:"samples"`
	StartedAt time.Time `json:"started_at,omitempty"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
	Problem   string    `json:"problem,omitempty"`
}

func (b *evidenceExportBuilder) analysisTaskResources() EvidenceAnalysisTaskResources {
	result := EvidenceAnalysisTaskResources{Status: "absent"}
	for _, file := range b.files {
		task, taskID, bound := b.taskResourceSubject(file.Path)
		if !bound || path.Base(file.Path) != qualitygate.ResourceSamplesFile {
			continue
		}
		result.Observations = append(result.Observations, analysisTaskResource(file, taskID, task))
	}
	if len(result.Observations) == 0 {
		return result
	}
	result.Status = "collected"
	for _, observation := range result.Observations {
		if observation.Problem != "" || observation.Coverage != qualitygate.ResourceCoverageCollected {
			result.Status = "partial"
		}
	}
	return result
}

func (b *evidenceExportBuilder) taskResourceSubject(entry string) (SemanticTaskRef, string, bool) {
	for _, predecessor := range b.attemptSection.Predecessors {
		if strings.HasPrefix(entry, predecessor.EntriesPrefix+"/artifacts/task-resources/") {
			return predecessor.Task, predecessor.RuntimeTaskID, true
		}
	}
	for _, prefix := range []string{"live/task", "bound/task", "attempt"} {
		if strings.HasPrefix(entry, prefix+"/artifacts/task-resources/") {
			return b.targetTask, b.workflowRuntimeTaskID(), true
		}
	}
	return SemanticTaskRef{}, "", false
}

func analysisTaskResource(file EvidenceExportFile, taskID string, task SemanticTaskRef) EvidenceAnalysisTaskResource {
	entry := EvidenceAnalysisTaskResource{Entry: file.Path, State: "unreadable"}
	document, err := qualitygate.DecodeResourceSamples(file.Data)
	if err != nil {
		entry.Problem = err.Error()
		return entry
	}
	e := document.Execution
	if e == nil || e.CallID != path.Base(path.Dir(file.Path)) || e.TaskID != taskID ||
		e.TaskPath != task.TaskPath || e.TaskContractDigest != task.ContractDigest {
		entry.Problem = "resource execution does not bind to the exported task"
		return entry
	}
	entry.TaskID, entry.CallID, entry.Role, entry.Phase = e.TaskID, e.CallID, e.Role, e.Phase
	entry.State, entry.Coverage = document.Coverage.ExecutionState, document.Coverage.Status
	entry.Samples, entry.StartedAt, entry.EndedAt = len(document.Samples), document.Coverage.StartedAt, document.Coverage.EndedAt
	return entry
}
