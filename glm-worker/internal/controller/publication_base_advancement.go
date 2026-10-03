package controller

import (
	"fmt"
	"os"
	"path/filepath"
)

type BaseAdvancementRecord struct {
	RepositoryIdentity   string            `json:"repository_identity"`
	OldTip               string            `json:"old_tip"`
	NewTip               string            `json:"new_tip"`
	TransitionID         string            `json:"transition_id"`
	ControllerGeneration uint64            `json:"controller_generation"`
	ProjectSnapshotID    string            `json:"project_snapshot_id"`
	GitArchive           EvidenceObjectRef `json:"git_archive"`
}

func (s *Store) preflightSuspendedRebind(tip string) error {
	head, err := s.LoadHead()
	if err != nil {
		return err
	}
	var episode BlockerEpisodeRevision
	if head.ActiveEpisodeID != "" {
		episode, err = s.LoadEpisodeRevision(head.ActiveEpisodeID, head.ActiveEpisodeRevision)
		if err != nil {
			return err
		}
	}
	snapshots, err := s.currentEpisodeSuspensions(head, episode)
	if err != nil {
		return err
	}
	for _, snapshot := range snapshots {
		if _, err := RebindSuspensionTrees(s.identity.PrimaryRoot, snapshot, tip); err != nil {
			return s.preservePublicationConflict(head, &snapshot, err)
		}
	}
	return nil
}

func (s *Store) currentEpisodeSuspensions(head RepositoryControllerHead, episode BlockerEpisodeRevision) ([]SuspensionSnapshot, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir, "suspensions"))
	if err != nil {
		return nil, err
	}
	latest := map[string]SuspensionSnapshot{}
	for _, entry := range entries {
		var snapshot SuspensionSnapshot
		if err := readJSON(filepath.Join(s.dir, "suspensions", entry.Name()), &snapshot); err != nil {
			return nil, err
		}
		if !s.neededEpisodeSuspension(snapshot, head, episode) {
			continue
		}
		old := latest[snapshot.SemanticTaskRef.TaskPath]
		if snapshot.ControllerGeneration > old.ControllerGeneration {
			latest[snapshot.SemanticTaskRef.TaskPath] = snapshot
		}
	}
	if len(latest) == 0 {
		return nil, fmt.Errorf("episode has no retained source suspension authority")
	}
	var result []SuspensionSnapshot
	for _, snapshot := range latest {
		verified, err := s.LoadSuspension(s.identity.PrimaryRoot, snapshot.SnapshotID)
		if err != nil {
			return nil, err
		}
		result = append(result, verified)
	}
	return result, nil
}

func (s *Store) validateBaseAdvancement(op ExecutionOperation) error {
	p := op.Publication
	if p.AdvancementRef == nil || p.Project == nil || p.OldTip == "" || p.NewTip == "" {
		return fmt.Errorf("base advancement authority is incomplete")
	}
	if p.Project.HeadOID != p.NewTip || p.Project.SnapshotID != op.Transition.ProjectSnapshotNew {
		return fmt.Errorf("base advancement project identity differs")
	}
	roots, err := s.VerifyGitObjectArchive(*p.AdvancementRef)
	if err != nil {
		return err
	}
	if len(roots) != 1 || roots[0].OID != p.NewTip || roots[0].Type != gitCommitObjectType {
		return fmt.Errorf("external advancement archive root differs")
	}
	return validatePublicationPolicy(s.identity.PrimaryRoot, p.Policy)
}

func (s *Store) applyBaseAdvancement(op ExecutionOperation) error {
	remote, err := observePublicationRemote(s.identity.PrimaryRoot, op.Publication.Policy)
	if err != nil {
		return err
	}
	if remote != op.Publication.RemoteOID {
		head, err := s.LoadHead()
		if err != nil {
			return err
		}
		if err := s.guardRacedLocalEffects(op, head, remote); err != nil {
			return err
		}
		return s.abortRacedPublication(op, remote)
	}
	if err := s.applyAdvancementSuspension(op); err != nil {
		return err
	}
	evidence, err := s.baseAdvancementEvidence(op)
	if err != nil {
		return err
	}
	if err := s.applyAdvancementLocalRef(op); err != nil {
		return err
	}
	actual, err := s.baseAdvancementEffects(op)
	if err != nil {
		return err
	}
	_, _, err = s.commitAuthorityTransitionWithEvidenceLocked(op.Transition, actual, evidence, func(next *RepositoryControllerHead) error {
		if err := s.writeProjectSnapshot(*op.Publication.Project); err != nil {
			return err
		}
		if err := s.commitAdvancementExecution(next, op); err != nil {
			return err
		}
		next.ProjectSnapshotID = op.Publication.Project.SnapshotID
		next.IntegrationTip = op.Publication.NewTip
		next.ObservedRemoteTip = remote
		next.PublicationPolicy = &op.Publication.Policy
		return nil
	})
	return err
}

func (s *Store) baseAdvancementEvidence(op ExecutionOperation) (EvidencePublicationInput, error) {
	head, err := s.LoadHead()
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	if op.Suspension != nil {
		return s.appendAdvancementLineage(op, op.Evidence)
	}
	if head.EvidenceHeadRef == nil {
		return EvidencePublicationInput{}, fmt.Errorf("external advancement evidence authority is missing")
	}
	previous, err := s.LoadEvidenceHead(*head.EvidenceHeadRef)
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	if head.ActiveEpisodeID == "" {
		return s.ordinarySuspendedAdvancementEvidence(op, head, previous)
	}
	old, ok := evidenceSubjectHead(previous.EpisodeHeads, head.ActiveEpisodeID)
	if !ok {
		return EvidencePublicationInput{}, fmt.Errorf("external advancement episode evidence is missing")
	}
	episode, err := s.LoadEpisodeIndexRevision(old.RevisionRef)
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	lineage, err := s.putEvidenceJSON("base-advancement", op.Transition.TransitionID, BaseAdvancementRecord{RepositoryIdentity: s.identity.LineageID, OldTip: op.Publication.OldTip, NewTip: op.Publication.NewTip, TransitionID: op.Transition.TransitionID, ControllerGeneration: op.Transition.CommittedGeneration, ProjectSnapshotID: op.Transition.ProjectSnapshotNew, GitArchive: *op.Publication.AdvancementRef})
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	episode.PreviousRevision = &old.RevisionRef
	episode.EpisodeRevision = op.Publication.Episode.Revision
	episode.DependencyGraphSnapshotID = op.Publication.Episode.RevisionID
	episode.IntegrationTip = op.Publication.NewTip
	episode.IntegrationHistory = append(episode.IntegrationHistory, lineage, *op.Publication.AdvancementRef)
	episode.ControllerGeneration = op.Transition.CommittedGeneration
	episode.TransitionID = op.Transition.TransitionID
	episode.CreatedAt = op.Transition.CreatedAt
	ref, _, err := s.StoreEpisodeIndexRevision(episode)
	return EvidencePublicationInput{EpisodeRevisionRefs: []EvidenceObjectRef{ref}}, err
}

func verifyCommittedBaseAdvancement(op ExecutionOperation, head RepositoryControllerHead) error {
	if head.LiveLeaseID != "" {
		return fmt.Errorf("committed base advancement retains live source lease")
	}
	if head.IntegrationTip != op.Publication.NewTip || head.ProjectSnapshotID != op.Transition.ProjectSnapshotNew {
		return fmt.Errorf("committed base advancement target differs")
	}
	return nil
}

func (s *Store) neededEpisodeSuspension(snapshot SuspensionSnapshot, head RepositoryControllerHead, episode BlockerEpisodeRevision) bool {
	if head.RootTaskRef == nil || !s.metadataTaskBindingProven(snapshot.RootTaskRef, *head.RootTaskRef) {
		return false
	}
	if s.metadataTaskBindingProven(snapshot.SemanticTaskRef, *head.RootTaskRef) {
		return true
	}
	for _, ref := range episode.AdmittedClosure {
		if s.metadataTaskBindingProven(snapshot.SemanticTaskRef, ref) {
			return !taskPathSatisfied(episode.SatisfiedTaskRefs, snapshot.SemanticTaskRef.TaskPath)
		}
	}
	return false
}
