package controller

import (
	"fmt"
	"os"
	"path/filepath"
)

func terminalMetadataEffects(p TerminalMetadataOperation, implementation string) []EffectExpectation {
	result := []EffectExpectation{{Surface: MutationSurfaceRef, Resource: p.Policy.LocalRef, ExpectedOld: implementation, ExpectedNew: p.Record.ResultProject.HeadOID}}
	for _, file := range p.Files {
		result = append(result, EffectExpectation{Surface: MutationSurfaceIndex, Resource: file.Path, ExpectedOld: file.OldOID, ExpectedNew: file.NewOID}, EffectExpectation{Surface: MutationSurfaceSource, Resource: file.Path, ExpectedOld: terminalFileIdentity(file.Mode, file.OldBytes, true), ExpectedNew: terminalFileIdentity(file.Mode, file.NewBytes, file.NewOID != "")})
	}
	return result
}

func terminalFileIdentity(mode string, data []byte, exists bool) string {
	if !exists {
		return ""
	}
	return digestStrings(mode, digestBytes(data))
}

func (s *Store) verifyTerminalMetadataSource(op ExecutionOperation) error {
	if err := s.verifyTerminalWorkspace(op); err != nil {
		return err
	}
	actual, err := s.observeTerminalFileEffects(op)
	if err != nil {
		return err
	}
	for _, effect := range op.Transition.Effects[1:] {
		if actual[effect.Key()] != effect.ExpectedOld {
			return fmt.Errorf("terminal metadata source is dirty or stale: %s", effect.Resource)
		}
	}
	return nil
}

func (s *Store) observeTerminalFileEffects(op ExecutionOperation) (map[string]string, error) {
	entries, err := suspensionIndexEntries(s.identity.PrimaryRoot)
	if err != nil {
		return nil, err
	}
	byPath := map[string]workspaceTreeEntry{}
	for _, entry := range entries {
		byPath[entry.path] = entry
	}
	actual := map[string]string{}
	for _, file := range op.Terminal.Files {
		entry, exists := byPath[file.Path]
		if exists && entry.mode != file.Mode {
			return nil, fmt.Errorf("terminal metadata index mode changed: %s", file.Path)
		}
		actual[(EffectExpectation{Surface: MutationSurfaceIndex, Resource: file.Path}).Key()] = entry.oid
		identity, err := s.terminalWorktreeFileIdentity(file.Path)
		if err != nil {
			return nil, err
		}
		actual[(EffectExpectation{Surface: MutationSurfaceSource, Resource: file.Path}).Key()] = identity
	}
	return actual, nil
}

func (s *Store) applyTerminalMetadataFiles(op ExecutionOperation) (map[string]string, error) {
	if err := s.validateTerminalPrimarySource(op); err != nil {
		return nil, err
	}
	actual, err := s.observeTerminalFileEffects(op)
	if err != nil {
		return nil, err
	}
	for _, effect := range terminalMetadataEffects(*op.Terminal, "")[1:] {
		if actual[effect.Key()] != effect.ExpectedOld && actual[effect.Key()] != effect.ExpectedNew {
			return nil, fmt.Errorf("terminal metadata unexpected partial state: %s", effect.Resource)
		}
	}
	p := op.Terminal
	if err := s.applyTerminalMetadataLocalRef(op); err != nil {
		return nil, err
	}
	for index, file := range p.Files {
		if err := s.applyTerminalMetadataFile(file, op.Transition.Effects[1+index*2:3+index*2]); err != nil {
			return nil, err
		}
	}
	actual, err = s.observeTerminalFileEffects(op)
	if err != nil {
		return nil, err
	}
	local, _, err := readExecutionRef(s.identity.PrimaryRoot, p.Policy.LocalRef)
	if err != nil {
		return nil, err
	}
	actual[op.Transition.Effects[0].Key()] = local
	return actual, nil
}

func (s *Store) applyTerminalMetadataFile(file TerminalMetadataFile, effects []EffectExpectation) error {
	actual, err := s.terminalMetadataFilePreconditions(file, effects)
	if err != nil {
		return err
	}
	if err := s.applyTerminalMetadataIndex(file, effects[0], actual); err != nil {
		return err
	}
	if actual[effects[1].Key()] == effects[1].ExpectedNew {
		return nil
	}
	return s.applyTerminalMetadataWorktreeFile(file)
}

func (s *Store) applyTerminalMetadataWorktreeFile(file TerminalMetadataFile) error {
	path, err := suspensionWorktreePath(s.identity.PrimaryRoot, file.Path)
	if err != nil {
		return err
	}
	parent := filepath.Dir(path)
	if file.NewOID == "" {
		if err := os.Remove(path); err != nil {
			return err
		}
		return syncDirectoryPath(parent)
	}
	mode := os.FileMode(0o644)
	if file.Mode == "100755" {
		mode = 0o755
	}
	return writeTerminalMetadataWorktreeFile(path, parent, mode, file.NewBytes)
}

func writeTerminalMetadataWorktreeFile(path, parent string, mode os.FileMode, data []byte) error {
	tmp, err := os.CreateTemp(parent, ".terminal-metadata-")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	return syncDirectoryPath(parent)
}

func (s *Store) verifyCommittedTerminalMetadata(op ExecutionOperation, head RepositoryControllerHead) error {
	if err := s.verifyTerminalWorkspace(op); err != nil {
		return err
	}

	p := op.Terminal
	if err := validateCommittedTerminalHead(*p, head); err != nil {
		return err
	}
	actual, err := s.observeTerminalFileEffects(op)
	if err != nil {
		return err
	}
	for _, effect := range terminalMetadataEffects(*p, "")[1:] {
		if actual[effect.Key()] != effect.ExpectedNew {
			return fmt.Errorf("committed terminal metadata changed: %s", effect.Resource)
		}
	}
	local, _, err := readExecutionRef(s.identity.PrimaryRoot, p.Policy.LocalRef)
	if err != nil || local != p.Record.ResultProject.HeadOID {
		return fmt.Errorf("committed terminal local tip changed")
	}
	if err := s.verifyTerminalTaskRecord(p.RecordRef); err != nil {
		return err
	}
	candidate, err := s.LoadAcceptedCandidate(p.Record.CandidateRef)
	if err != nil {
		return err
	}
	return s.verifyPrimaryPublicationTree(candidate)
}

func validateCommittedTerminalHead(p TerminalMetadataOperation, head RepositoryControllerHead) error {
	if head.ProjectSnapshotID != p.Record.ResultProject.SnapshotID || head.IntegrationTip != p.Record.ResultProject.HeadOID || head.MetadataLineageRef == nil || !evidenceRefsEqual(*head.MetadataLineageRef, p.RecordRef) {
		return fmt.Errorf("committed terminal metadata authority differs")
	}
	return nil
}

func (s *Store) validateTerminalPrimarySource(op ExecutionOperation) error {
	candidate, err := s.LoadAcceptedCandidate(op.Terminal.Record.CandidateRef)
	if err != nil || !candidate.TaskRef.Equal(candidate.RootTaskRef) {
		return err
	}
	implementation, err := s.completionImplementation(candidate)
	if err != nil {
		return err
	}
	before, err := sourceImportTreeEntries(s.identity.PrimaryRoot, implementation.TreeOID)
	if err != nil {
		return err
	}
	after, err := sourceImportTreeEntries(s.identity.PrimaryRoot, candidate.TreeOID)
	if err != nil {
		return err
	}
	actual, err := s.capturePrimaryPromotionTrees(candidate.Policy)
	if err != nil {
		return err
	}
	for _, tree := range []string{actual.IndexTree, actual.WorktreeTree} {
		if err := s.validatePrimaryPromotionEntries(tree, before, after); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) terminalWorktreeFileIdentity(relative string) (string, error) {
	path, err := suspensionWorktreePath(s.identity.PrimaryRoot, relative)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("terminal metadata worktree path is not regular: %s", relative)
	}
	data, mode, err := suspensionFileBytes(path, info)
	if err != nil {
		return "", err
	}
	return terminalFileIdentity(mode, data, true), nil
}

func (s *Store) applyTerminalMetadataLocalRef(op ExecutionOperation) error {
	if err := s.verifyTerminalWorkspace(op); err != nil {
		return err
	}
	effect := op.Transition.Effects[0]
	return updatePublicationRef(s.identity.PrimaryRoot, op.Terminal.Policy.LocalRef, effect.ExpectedOld, effect.ExpectedNew)
}

func (s *Store) commitTerminalMetadataTarget(head *RepositoryControllerHead, p TerminalMetadataOperation) error {
	if err := s.writeProjectSnapshot(p.Record.ResultProject); err != nil {
		return err
	}
	if p.Episode != nil {
		if err := s.writeEpisodeRevision(*p.Episode); err != nil {
			return err
		}
	}
	applyCompletionTargetAuthority(head, p)
	return nil
}

func (s *Store) applyTerminalMetadataIndex(file TerminalMetadataFile, effect EffectExpectation, actual map[string]string) error {
	if actual[effect.Key()] == effect.ExpectedOld {
		if file.NewOID == "" {
			if _, err := runGitBinary(s.identity.PrimaryRoot, nil, "update-index", "--force-remove", "--", file.Path); err != nil {
				return err
			}
		} else {
			if _, err := runGitBinary(s.identity.PrimaryRoot, nil, "update-index", "--add", "--cacheinfo", file.Mode+","+file.NewOID+","+file.Path); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) terminalMetadataFilePreconditions(file TerminalMetadataFile, effects []EffectExpectation) (map[string]string, error) {
	actual, err := s.observeTerminalFileEffects(ExecutionOperation{Terminal: &TerminalMetadataOperation{Files: []TerminalMetadataFile{file}}})
	if err != nil {
		return nil, err
	}
	for _, effect := range effects {
		if actual[effect.Key()] != effect.ExpectedOld && actual[effect.Key()] != effect.ExpectedNew {
			return nil, fmt.Errorf("terminal metadata changed before file effect: %s", effect.Resource)
		}
	}
	return actual, nil
}

func (s *Store) verifyTerminalWorkspace(op ExecutionOperation) error {
	if op.Workspace == nil || op.Workspace.Root != s.identity.PrimaryRoot || op.Workspace.ID != op.Transition.SourceWorkspaceID {
		return fmt.Errorf("terminal metadata workspace binding is invalid")
	}
	workspace, err := ResolveWorkspaceIdentity(s.identity.PrimaryRoot, s.identity)
	if err != nil {
		return err
	}
	if workspace != *op.Workspace {
		return fmt.Errorf("terminal metadata workspace was replaced")
	}
	ref, err := gitTrimmed(s.identity.PrimaryRoot, "symbolic-ref", "HEAD")
	if err != nil || ref != op.Terminal.Policy.LocalRef {
		return fmt.Errorf("terminal metadata primary workspace is not the publication branch")
	}
	return nil
}

func applyCompletionTargetAuthority(head *RepositoryControllerHead, p TerminalMetadataOperation) {
	head.ProjectSnapshotID = p.Record.ResultProject.SnapshotID
	head.RootTaskRef = p.RootTaskRef
	head.ExecutionTaskRef = nil
	head.IntegrationTip = p.Record.ResultProject.HeadOID
	head.MetadataLineageRef = &p.RecordRef
	head.AcceptedCandidateRef = nil
	head.LiveAttemptID = ""
	head.LiveLeaseID = ""
	if p.Episode != nil {
		head.ActiveEpisodeRevision = p.Episode.Revision
	}
}
