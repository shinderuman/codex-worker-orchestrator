package app

import (
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type parentUsageBatchEvidence struct {
	association codexAssociation
	scan        bundleRolloutScan
	err         error
}

type parentUsageBatch struct {
	codexHome  string
	start      time.Time
	end        time.Time
	enumerate  func(string) ([]codexRollout, error)
	scanChain  func([]codexRollout, time.Time, time.Time) (bundleRolloutScan, error)
	byIdentity map[string]parentUsageBatchEvidence
}

func newParentUsageBatch(codexHome string, tasks []state.TaskStats, enumerate func(string) ([]codexRollout, error), scanChain func([]codexRollout, time.Time, time.Time) (bundleRolloutScan, error)) parentUsageBatch {
	batch := parentUsageBatch{codexHome: codexHome, scanChain: scanChain, byIdentity: make(map[string]parentUsageBatchEvidence)}
	for i, stats := range tasks {
		start, end, _ := analysisCollectionWindow(bundleTask{Stats: stats})
		if i == 0 || start.Before(batch.start) {
			batch.start = start
		}
		if end.After(batch.end) {
			batch.end = end
		}
	}
	var enumerated bool
	var rollouts []codexRollout
	var scanErr error
	batch.enumerate = func(home string) ([]codexRollout, error) {
		if !enumerated {
			rollouts, scanErr = enumerate(home)
			enumerated = true
		}
		return rollouts, scanErr
	}
	return batch
}

func (batch *parentUsageBatch) evidence(task bundleTask) parentUsageBatchEvidence {
	threadID, basis, failure := selectCodexParentIdentity(task)
	if failure != nil {
		return parentUsageBatchEvidence{association: *failure}
	}
	key := basis + "/" + threadID
	if cached, ok := batch.byIdentity[key]; ok {
		return cached
	}
	evidence := parentUsageBatchEvidence{association: resolveCodexAssociationWithScan(batch.codexHome, task, batch.enumerate)}
	if evidence.association.ParentStatus == codexStatusIncluded {
		evidence.scan, evidence.err = batch.scanChain(evidence.association.rolloutChain(), batch.start, batch.end)
	}
	batch.byIdentity[key] = evidence
	return evidence
}
