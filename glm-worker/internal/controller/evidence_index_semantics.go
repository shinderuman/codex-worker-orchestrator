package controller

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func (record TaskIndexRevision) MarshalJSON() ([]byte, error) {
	canonical, err := canonicalTaskIndexRevision(record)
	if err != nil {
		return nil, err
	}
	type taskIndexRevisionJSON TaskIndexRevision
	return json.Marshal(taskIndexRevisionJSON(canonical))
}

func (record *TaskIndexRevision) UnmarshalJSON(data []byte) error {
	type taskIndexRevisionJSON TaskIndexRevision
	var decoded taskIndexRevisionJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	canonical, err := canonicalTaskIndexRevision(TaskIndexRevision(decoded))
	if err != nil {
		return err
	}
	*record = canonical
	return nil
}

func canonicalTaskIndexRevision(record TaskIndexRevision) (TaskIndexRevision, error) {
	if strings.TrimSpace(record.SemanticStatus) == "" || strings.TrimSpace(record.TransitionID) == "" {
		return TaskIndexRevision{}, fmt.Errorf("task evidence index semantic authority is incomplete")
	}
	if err := validateEvidenceRefs(record.FindingRecords); err != nil {
		return TaskIndexRevision{}, fmt.Errorf("task finding evidence: %w", err)
	}
	if err := validateEvidenceRefs(record.DependencyEdgeRecords); err != nil {
		return TaskIndexRevision{}, fmt.Errorf("task dependency evidence: %w", err)
	}
	if err := validateEvidenceRefs(record.PublicationLineageRecords); err != nil {
		return TaskIndexRevision{}, fmt.Errorf("task publication evidence: %w", err)
	}
	if record.TerminalRecord != nil {
		if err := validateEvidenceRef(*record.TerminalRecord); err != nil {
			return TaskIndexRevision{}, fmt.Errorf("task terminal evidence: %w", err)
		}
	}
	record.AttemptSeals = canonicalEvidenceRefs(record.AttemptSeals)
	record.Finalizations = canonicalEvidenceRefs(record.Finalizations)
	record.FindingRecords = canonicalEvidenceRefs(record.FindingRecords)
	record.DependencyEdgeRecords = canonicalEvidenceRefs(record.DependencyEdgeRecords)
	record.PublicationLineageRecords = canonicalEvidenceRefs(record.PublicationLineageRecords)
	return record, nil
}

func (record EpisodeIndexRevision) MarshalJSON() ([]byte, error) {
	canonical, err := canonicalEpisodeIndexRevision(record)
	if err != nil {
		return nil, err
	}
	type episodeIndexRevisionJSON EpisodeIndexRevision
	return json.Marshal(episodeIndexRevisionJSON(canonical))
}

func (record *EpisodeIndexRevision) UnmarshalJSON(data []byte) error {
	type episodeIndexRevisionJSON EpisodeIndexRevision
	var decoded episodeIndexRevisionJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	canonical, err := canonicalEpisodeIndexRevision(EpisodeIndexRevision(decoded))
	if err != nil {
		return err
	}
	*record = canonical
	return nil
}

func canonicalEpisodeIndexRevision(record EpisodeIndexRevision) (EpisodeIndexRevision, error) {
	if err := validateEpisodeIndexAuthority(record); err != nil {
		return EpisodeIndexRevision{}, err
	}
	record.AdmittedClosureTaskRefs = canonicalSemanticTaskRefs(record.AdmittedClosureTaskRefs)
	record.TaskIndexHeads = canonicalEvidenceHeads(record.TaskIndexHeads)
	record.AttemptSeals = canonicalEvidenceRefs(record.AttemptSeals)
	record.Finalizations = canonicalEvidenceRefs(record.Finalizations)
	record.FindingRecords = canonicalEvidenceRefs(record.FindingRecords)
	record.TransitionRecords = canonicalEvidenceRefs(record.TransitionRecords)
	record.IntegrationHistory = canonicalEvidenceRefs(record.IntegrationHistory)
	return record, nil
}

func validateEpisodeIndexAuthority(record EpisodeIndexRevision) error {
	if record.RootTaskRef.Empty() || strings.TrimSpace(record.DependencyGraphSnapshotID) == "" || strings.TrimSpace(record.State) == "" || strings.TrimSpace(record.TransitionID) == "" {
		return fmt.Errorf("episode evidence index semantic authority is incomplete")
	}
	if record.CurrentExecutionTaskRef != nil && record.CurrentExecutionTaskRef.Empty() {
		return fmt.Errorf("episode current execution task identity is incomplete")
	}
	if err := validateSemanticTaskRefs(record.AdmittedClosureTaskRefs); err != nil {
		return err
	}
	if err := validateTaskIndexHeads(record.TaskIndexHeads); err != nil {
		return err
	}
	return validateEpisodeIndexEvidenceRefs(record)
}

func validateEpisodeIndexEvidenceRefs(record EpisodeIndexRevision) error {
	if err := validateEvidenceRefs(record.FindingRecords); err != nil {
		return fmt.Errorf("episode finding evidence: %w", err)
	}
	if err := validateEvidenceRefs(record.TransitionRecords); err != nil {
		return fmt.Errorf("episode transition evidence: %w", err)
	}
	if err := validateEvidenceRefs(record.IntegrationHistory); err != nil {
		return fmt.Errorf("episode integration evidence: %w", err)
	}
	if record.CloseRecord != nil {
		if err := validateEvidenceRef(*record.CloseRecord); err != nil {
			return fmt.Errorf("episode close evidence: %w", err)
		}
	}
	return nil
}

func validateEvidenceRefs(refs []EvidenceObjectRef) error {
	for _, ref := range refs {
		if err := validateEvidenceRef(ref); err != nil {
			return err
		}
	}
	return nil
}

func validateSemanticTaskRefs(refs []SemanticTaskRef) error {
	seen := map[string]bool{}
	for _, ref := range refs {
		if ref.Empty() {
			return fmt.Errorf("semantic task evidence identity is incomplete")
		}
		key := semanticTaskEvidenceKey(ref)
		if seen[key] {
			return fmt.Errorf("semantic task evidence contains duplicate task")
		}
		seen[key] = true
	}
	return nil
}

func validateTaskIndexHeads(heads []EvidenceSubjectHead) error {
	seen := map[string]bool{}
	for _, head := range heads {
		if strings.TrimSpace(head.SubjectID) == "" {
			return fmt.Errorf("episode task-index head subject is incomplete")
		}
		if seen[head.SubjectID] {
			return fmt.Errorf("episode task-index heads contain duplicate subject")
		}
		seen[head.SubjectID] = true
		if err := validateTypedEvidenceRef(head.RevisionRef, "task-index-revision"); err != nil {
			return err
		}
	}
	return nil
}

func canonicalSemanticTaskRefs(refs []SemanticTaskRef) []SemanticTaskRef {
	result := append([]SemanticTaskRef(nil), refs...)
	sort.Slice(result, func(i, j int) bool {
		return semanticTaskEvidenceKey(result[i]) < semanticTaskEvidenceKey(result[j])
	})
	return result
}

func semanticTaskEvidenceKey(ref SemanticTaskRef) string {
	return ref.TaskPath + "\x00" + ref.ContractDigest
}
