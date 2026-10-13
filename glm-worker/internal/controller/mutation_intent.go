package controller

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type QualityMutationIntent struct {
	SchemaVersion int                `json:"schema_version"`
	Command       string             `json:"command"`
	Admission     Admission          `json:"admission"`
	Output        *WorkspaceSnapshot `json:"output,omitempty"`
	Provenance    string             `json:"provenance,omitempty"`
	CreatedAt     time.Time          `json:"created_at"`
}

const QualityMutationCommand = "quality:harnesslint-machine-fix"

func (s *Store) qualityMutationIntentPath() string {
	return filepath.Join(s.dir, "quality-mutation-intent.json")
}

func (s *Store) StageQualityMutation(admission Admission) error {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	intent, err := s.loadQualityMutationIntent()
	if err != nil {
		return err
	}
	if intent != nil {
		if intent.Command == QualityMutationCommand && sameQualityIntentAdmission(*intent, admission) {
			return nil
		}
		if settled, settleErr := s.qualityMutationSettled(*intent); settleErr != nil {
			return settleErr
		} else if settled {
			if err := s.removeQualityMutationIntent(); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("a different quality mutation intent is already staged for lease %s", intent.Admission.Lease.LeaseID)
		}
	}
	return s.writeQualityMutationIntent(&QualityMutationIntent{
		SchemaVersion: controllerSchemaVersion,
		Command:       QualityMutationCommand,
		Admission:     admission,
		CreatedAt:     time.Now().UTC(),
	})
}

func (s *Store) PreserveQualityMutationOutput(admission Admission, output WorkspaceSnapshot, provenance string) error {
	if provenance == "" {
		return fmt.Errorf("quality mutation provenance is required before settlement")
	}
	lock, err := s.acquireMutationLock()
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	intent, err := s.loadQualityMutationIntent()
	if err != nil {
		return err
	}
	if intent == nil || !sameQualityIntentAdmission(*intent, admission) {
		return fmt.Errorf("quality mutation output does not match the staged intent")
	}
	if intent.Output != nil && intent.Output.ID != output.ID {
		return fmt.Errorf("staged quality mutation output changed: staged=%s new=%s", intent.Output.ID, output.ID)
	}
	intent.Output = &output
	intent.Provenance = provenance
	return s.writeQualityMutationIntent(intent)
}

func (s *Store) ClearQualityMutationIntent(admission Admission) error {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	intent, err := s.loadQualityMutationIntent()
	if err != nil {
		return err
	}
	if intent == nil {
		return nil
	}
	if !sameQualityIntentAdmission(*intent, admission) {
		return fmt.Errorf("quality mutation intent belongs to a different admission")
	}
	return os.Remove(s.qualityMutationIntentPath())
}

func (s *Store) AdmitMutationSettlingQualityIntent(authority MutationAuthority, workspace WorkspaceIdentity, snapshot WorkspaceSnapshot) (Admission, error) {
	admission, err := s.AdmitMutation(authority, workspace, snapshot)
	if err == nil {
		return admission, nil
	}
	lock, lockErr := s.acquireMutationLock()
	if lockErr != nil {
		return Admission{}, errors.Join(err, lockErr)
	}
	defer func() { _ = lock.Close() }()
	settled, admitAgain, settleErr := s.settleQualityMutationLocked(authority, workspace, snapshot)
	if settleErr != nil {
		return Admission{}, errors.Join(err, settleErr)
	}
	if !settled {
		return Admission{}, err
	}
	return admitAgain, nil
}

func (s *Store) settleQualityMutationLocked(authority MutationAuthority, workspace WorkspaceIdentity, snapshot WorkspaceSnapshot) (bool, Admission, error) {
	intent, err := s.loadQualityMutationIntent()
	if err != nil {
		return false, Admission{}, err
	}
	if intent == nil {
		return false, Admission{}, nil
	}
	if err := s.validateStagedQualityMutation(authority, *intent, workspace, snapshot); err != nil {
		return false, Admission{}, err
	}
	if err := s.completeStagedQualityMutationLocked(*intent, *intent.Output); err != nil {
		return false, Admission{}, err
	}
	if err := s.removeQualityMutationIntent(); err != nil {
		return false, Admission{}, err
	}
	nextAuthority, authorityErr := s.currentHeadAuthority()
	if authorityErr != nil {
		return false, Admission{}, authorityErr
	}
	admission, err := s.AdmitMutation(nextAuthority, workspace, snapshot)
	if err != nil {
		return false, Admission{}, err
	}
	return true, admission, nil
}

func (s *Store) validateStagedQualityMutation(caller MutationAuthority, intent QualityMutationIntent, workspace WorkspaceIdentity, snapshot WorkspaceSnapshot) error {
	head, err := s.LoadHead()
	if err != nil {
		return err
	}
	if claimErr := validateMutationAuthorityClaim(head, caller); claimErr != nil {
		return fmt.Errorf("staged quality mutation settlement rejected the caller claim: %w", claimErr)
	}
	if err := validateQualityIntentIdentity(intent, workspace); err != nil {
		return err
	}
	if intent.Output == nil || intent.Provenance == "" {
		return fmt.Errorf("staged quality mutation intent for lease %s lacks preserved provenance", intent.Admission.Lease.LeaseID)
	}
	if head.LiveLeaseID != intent.Admission.Lease.LeaseID || head.ControllerGeneration != intent.Admission.Head.ControllerGeneration {
		return fmt.Errorf("staged quality mutation intent is stale for the live controller head")
	}
	if !sameWorkspaceSnapshot(*intent.Output, snapshot) {
		return fmt.Errorf(
			"workspace snapshot matches neither the staged quality mutation input %s nor its provenance-preserved output %s",
			intent.Admission.Snapshot.ID, intent.Output.ID,
		)
	}
	return nil
}

func (s *Store) completeStagedQualityMutationLocked(intent QualityMutationIntent, output WorkspaceSnapshot) error {
	current, err := s.AdmitMutation(intent.Admission.MutationAuthority(), intent.Admission.Workspace, intent.Admission.Snapshot)
	if err != nil {
		return fmt.Errorf("staged quality mutation admission replay failed: %w", err)
	}
	if current.Lease.LeaseID != intent.Admission.Lease.LeaseID {
		return fmt.Errorf("execution lease changed before staged quality mutation settlement")
	}
	existing, found, findErr := s.qualityMutationRecordForLease(intent.Admission.Lease.LeaseID)
	if findErr != nil {
		return findErr
	}
	if found {
		if existing.After.ID != output.ID {
			return fmt.Errorf("partially applied quality mutation record does not match the preserved output")
		}
		return s.completeExistingQualityMutationRecord(existing, current)
	}
	_, err = s.recordMutationLocked(intent.Admission, QualityMutationCommand, "success", output)
	return err
}

func (s *Store) currentHeadAuthority() (MutationAuthority, error) {
	head, err := s.LoadHead()
	if err != nil {
		return MutationAuthority{}, err
	}
	return MutationAuthorityFromHead(head)
}

func (s *Store) completeExistingQualityMutationRecord(record MutationRecord, current Admission) error {
	nextLease := current.Lease
	nextLease.LeaseID = record.TargetLeaseID
	nextLease.ControllerGeneration = record.TargetGeneration
	nextLease.ExpectedBaseOID = record.After.Head
	nextLease.ExpectedWorkspaceSnapshotID = record.After.ID
	nextLease.InFlightCallID = ""
	nextLease.CreatedAt = time.Now().UTC()
	if err := s.writeLease(nextLease); err != nil {
		return err
	}
	nextHead := current.Head
	nextHead.ControllerGeneration = record.TargetGeneration
	nextHead.LiveLeaseID = record.TargetLeaseID
	return s.writeHeadCAS(current.Head.ControllerGeneration, nextHead)
}

func (s *Store) qualityMutationRecordForLease(sourceLeaseID string) (MutationRecord, bool, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir, "mutations"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return MutationRecord{}, false, nil
		}
		return MutationRecord{}, false, err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != jsonRecordFileExt {
			continue
		}
		var record MutationRecord
		if err := readJSON(filepath.Join(s.dir, "mutations", entry.Name()), &record); err != nil {
			return MutationRecord{}, false, err
		}
		if record.SchemaVersion == controllerSchemaVersion && record.SourceLeaseID == sourceLeaseID && record.Command == QualityMutationCommand {
			return record, true, nil
		}
	}
	return MutationRecord{}, false, nil
}

func validateQualityIntentIdentity(intent QualityMutationIntent, workspace WorkspaceIdentity) error {
	admission := intent.Admission
	if admission.Workspace.ID != workspace.ID || admission.Workspace.RepositoryID != workspace.RepositoryID {
		return fmt.Errorf("staged quality mutation intent belongs to a different workspace")
	}
	if admission.Attempt.AttemptID == "" || admission.Lease.LeaseID == "" || admission.Head.ControllerGeneration == 0 || admission.Snapshot.ID == "" {
		return fmt.Errorf("staged quality mutation intent is missing its admission identity")
	}
	if intent.Output != nil && (intent.Output.Head != admission.Snapshot.Head || intent.Output.IndexDigest != admission.Snapshot.IndexDigest || intent.Output.RefDigest != admission.Snapshot.RefDigest) {
		return fmt.Errorf("staged quality mutation output changed protected surfaces outside the worktree")
	}
	return nil
}

func sameQualityIntentAdmission(intent QualityMutationIntent, admission Admission) bool {
	return intent.Admission.Lease.LeaseID == admission.Lease.LeaseID &&
		intent.Admission.Attempt.AttemptID == admission.Attempt.AttemptID &&
		intent.Admission.Head.ControllerGeneration == admission.Head.ControllerGeneration &&
		intent.Admission.Workspace.ID == admission.Workspace.ID &&
		intent.Admission.Snapshot.ID == admission.Snapshot.ID
}

func (s *Store) qualityMutationSettled(intent QualityMutationIntent) (bool, error) {
	head, err := s.LoadHead()
	if err != nil {
		return false, err
	}
	if head.LiveLeaseID != intent.Admission.Lease.LeaseID || head.ControllerGeneration != intent.Admission.Head.ControllerGeneration {
		return true, nil
	}
	_, found, findErr := s.qualityMutationRecordForLease(intent.Admission.Lease.LeaseID)
	if findErr != nil {
		return false, findErr
	}
	return found, nil
}

func sameWorkspaceSnapshot(left, right WorkspaceSnapshot) bool {
	return left.ID == right.ID && left.Head == right.Head &&
		left.IndexDigest == right.IndexDigest && left.WorktreeDigest == right.WorktreeDigest && left.RefDigest == right.RefDigest
}

func (s *Store) loadQualityMutationIntent() (*QualityMutationIntent, error) {
	var intent QualityMutationIntent
	if err := readJSON(s.qualityMutationIntentPath(), &intent); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if intent.SchemaVersion != controllerSchemaVersion || intent.Command != QualityMutationCommand {
		return nil, fmt.Errorf("quality mutation intent is invalid")
	}
	return &intent, nil
}

func (s *Store) writeQualityMutationIntent(intent *QualityMutationIntent) error {
	return writeJSONAtomic(s.qualityMutationIntentPath(), intent)
}

func (s *Store) removeQualityMutationIntent() error {
	if err := os.Remove(s.qualityMutationIntentPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
