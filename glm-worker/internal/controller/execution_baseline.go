package controller

import (
	"encoding/json"
	"fmt"
)

func (s *Store) archiveExecutionBaseline(repo string, attempt *AttemptRecord) error {
	ref, _, err := s.CaptureGitObjectArchive(repo, "baseline:"+attempt.AttemptID, []string{attempt.ExecutionBaseOID, attempt.BaselineTrees.IndexTree, attempt.BaselineTrees.WorktreeTree})
	if err != nil {
		return err
	}
	attempt.BaselineArchive = ref
	return nil
}

func (s *Store) restoreExecutionBaseline(repo string, attempt AttemptRecord) error {
	roots, err := s.VerifyGitObjectArchive(attempt.BaselineArchive)
	if err != nil {
		return err
	}
	expected := map[string]string{attempt.ExecutionBaseOID: "commit", attempt.BaselineTrees.IndexTree: "tree", attempt.BaselineTrees.WorktreeTree: "tree"}
	if len(roots) != len(expected) {
		return fmt.Errorf("execution baseline archive roots differ from attempt")
	}
	for _, root := range roots {
		if expected[root.OID] != root.Type {
			return fmt.Errorf("execution baseline archive root authority is inconsistent")
		}
	}
	data, err := s.LoadEvidenceObject(attempt.BaselineArchive)
	if err != nil {
		return err
	}
	var archive gitObjectArchiveEnvelope
	if err := json.Unmarshal(data, &archive); err != nil {
		return err
	}
	_, err = runGitBinary(repo, archive.Pack, "unpack-objects", "-r")
	return err
}
