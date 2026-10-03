package controller

import "fmt"

func (s *Store) preservePublicationConflict(head RepositoryControllerHead, snapshot *SuspensionSnapshot, cause error) error {
	observed := map[string]string{"integration_tip": head.IntegrationTip, "cause": cause.Error()}
	if snapshot != nil {
		archive, _, err := s.CaptureGitObjectArchive(s.identity.PrimaryRoot, "rebind-conflict:"+snapshot.SnapshotID, []string{snapshot.ExecutionBaseOID, snapshot.Baseline.IndexTree, snapshot.Baseline.WorktreeTree, snapshot.Current.IndexTree, snapshot.Current.WorktreeTree})
		if err != nil {
			return fmt.Errorf("rebind conflict archive could not be retained: %w", err)
		}
		observed["suspension_id"] = snapshot.SnapshotID
		observed["archive_digest"] = archive.Digest
		observed["archive_identity"] = archive.LogicalIdentity
	}
	_, err := s.failClosedLocked("publication source rebind conflict or ownership violation", head.PendingTransitionID, WorkspaceIdentity{}, WorkspaceSnapshot{}, WorkspaceSnapshot{}, observed)
	if err != nil {
		return err
	}
	return cause
}
