package controller

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func (s *Store) captureAttemptSemanticEvidence(op ExecutionOperation, seal *AttemptSeal) error {
	refs, err := s.captureOperationSemanticEvidence(op)
	if err != nil {
		return err
	}
	seal.EvidenceRefs = append(seal.EvidenceRefs, refs...)
	findings, err := s.captureAttemptFindings(op.Source.Attempt.AttemptID)
	if err != nil {
		return err
	}
	for _, ref := range findings {
		if ref.Kind == "finding-record" {
			seal.FindingRecordRefs = append(seal.FindingRecordRefs, ref)
		} else {
			seal.EvidenceRefs = append(seal.EvidenceRefs, ref)
		}
	}
	return nil
}

func (s *Store) captureOperationSemanticEvidence(op ExecutionOperation) ([]EvidenceObjectRef, error) {
	var refs []EvidenceObjectRef
	for _, id := range canonicalUniqueStrings([]string{op.Transition.ProjectSnapshotOld, op.Transition.ProjectSnapshotNew}) {
		project, err := s.operationProjectSnapshot(op, id)
		if err != nil {
			return nil, err
		}
		ref, err := s.putEvidenceJSON("project-snapshot", id, project)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	episodes, err := s.operationEvidenceEpisodes(op)
	if err != nil {
		return nil, err
	}
	for _, episode := range episodes {
		ref, err := s.putEvidenceJSON("episode-revision", episode.RevisionID, episode)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func (s *Store) captureAttemptFindings(attemptID string) ([]EvidenceObjectRef, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir, "findings"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var refs []EvidenceObjectRef
	for _, entry := range entries {
		identity, canonical, err := canonicalJSONRecordEntry(entry)
		if err != nil {
			return nil, err
		}
		if !canonical {
			continue
		}
		finding, err := s.LoadFinding(identity)
		if err != nil {
			return nil, err
		}
		if finding.SourceAttemptID != attemptID {
			continue
		}
		captured, err := s.captureFindingEvidence(finding)
		if err != nil {
			return nil, err
		}
		refs = append(refs, captured...)
	}
	return refs, nil
}

func (s *Store) captureFindingEvidence(finding FindingRecord) ([]EvidenceObjectRef, error) {
	ref, err := s.putEvidenceJSON("finding-record", finding.FindingID, finding)
	if err != nil {
		return nil, err
	}
	project, err := s.LoadProjectSnapshot(finding.ProjectSnapshotID)
	if err != nil {
		return nil, err
	}
	projectRef, err := s.putEvidenceJSON("project-snapshot", project.SnapshotID, project)
	if err != nil {
		return nil, err
	}
	refs := []EvidenceObjectRef{ref, projectRef}
	disposition, err := s.LoadFindingDisposition(finding.FindingID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		ref, err := s.putEvidenceJSON("finding-disposition", disposition.DispositionID, disposition)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	mutations, err := s.captureFindingMutationEvidence(finding)
	if err != nil {
		return nil, err
	}
	refs = append(refs, mutations...)
	return refs, nil
}

func (s *Store) operationProjectSnapshot(op ExecutionOperation, id string) (ProjectSnapshot, error) {
	if op.Terminal != nil {
		for _, project := range []ProjectSnapshot{op.Terminal.Record.SourceProject, op.Terminal.Record.ResultProject} {
			if project.SnapshotID == id {
				return project, nil
			}
		}
	}
	if op.Publication != nil && op.Publication.Project != nil && op.Publication.Project.SnapshotID == id {
		return *op.Publication.Project, nil
	}
	return s.LoadProjectSnapshot(id)
}

func (s *Store) finalizationSemanticEvidence(op ExecutionOperation) ([]EvidenceObjectRef, error) {
	refs, err := s.captureOperationSemanticEvidence(op)
	if err != nil {
		return nil, err
	}
	ref, err := s.putEvidenceJSON("transition-record", op.Transition.TransitionID, op.Transition)
	if err != nil {
		return nil, err
	}
	return append(refs, ref), nil
}

func (s *Store) operationEvidenceEpisodes(op ExecutionOperation) ([]BlockerEpisodeRevision, error) {
	var episodes []BlockerEpisodeRevision
	if op.Episode != nil {
		episodes = append(episodes, *op.Episode)
	}
	if op.Publication != nil && op.Publication.Episode != nil {
		episodes = append(episodes, *op.Publication.Episode)
	}
	if op.Terminal != nil && op.Terminal.Episode != nil {
		episodes = append(episodes, *op.Terminal.Episode)
	}
	if len(episodes) == 0 && op.Transition.SourceEpisodeID != "" {
		episode, err := s.LoadEpisodeRevision(op.Transition.SourceEpisodeID, op.Transition.SourceEpisodeRevision)
		if err != nil {
			return nil, err
		}
		episodes = append(episodes, episode)
	}
	return episodes, nil
}

func (s *Store) storeOperationFinalization(op ExecutionOperation, seal EvidenceObjectRef, refs []EvidenceObjectRef) (EvidenceObjectRef, error) {
	semanticRefs, err := s.finalizationSemanticEvidence(op)
	if err != nil {
		return EvidenceObjectRef{}, err
	}
	evidence := append(append([]EvidenceObjectRef(nil), refs...), semanticRefs...)
	ref, _, err := s.StoreAttemptFinalization(AttemptFinalizationRecord{SchemaVersion: evidenceSchemaVersion, AttemptSealRef: seal, ControllerGeneration: op.Transition.CommittedGeneration, TransitionID: op.Transition.TransitionID, ProjectSnapshotID: op.Transition.ProjectSnapshotNew, Kind: op.Transition.Kind, EvidenceRefs: evidence, CreatedAt: op.Transition.CreatedAt})
	return ref, err
}

func (s *Store) captureFindingMutationEvidence(finding FindingRecord) ([]EvidenceObjectRef, error) {
	if finding.ProofClass != FindingProofAttemptMutation {
		return nil, nil
	}
	var refs []EvidenceObjectRef
	for _, evidence := range finding.Evidence {
		var mutation MutationRecord
		if err := readJSON(s.mutationPath(evidence.ID), &mutation); err != nil {
			return nil, err
		}
		if evidence.Kind != FindingEvidenceMutation || mutation.SchemaVersion != controllerSchemaVersion || mutation.MutationID != evidence.ID || mutation.AttemptID != finding.SourceAttemptID || mutation.TargetGeneration != finding.SourceControllerGeneration {
			return nil, fmt.Errorf("finding mutation evidence identity is inconsistent")
		}
		ref, err := s.putEvidenceJSON("mutation-record", mutation.MutationID, mutation)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}
