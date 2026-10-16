package controller

import (
	"fmt"
	"os"
	"reflect"
)

func (s *Store) planPrimaryPromotion(op *ExecutionOperation, c AcceptedCandidate) error {
	if !c.TaskRef.Equal(c.RootTaskRef) {
		return nil
	}
	before, err := s.capturePrimaryPromotionTrees(c.Policy)
	if err != nil {
		return err
	}
	if err := s.validatePromotionSourceOwnership(c, before); err != nil {
		return err
	}
	archive, _, err := s.CaptureGitObjectArchive(s.identity.PrimaryRoot, op.Transition.TransitionID+":promotion-primary", []string{op.Publication.LocalOld, before.IndexTree, before.WorktreeTree})
	if err != nil {
		return err
	}
	op.Publication.PrimaryArchive = &archive
	op.Publication.SourceTrees = &before
	op.Transition.Effects = s.primaryPromotionEffects(*op, c)
	return nil
}

func (s *Store) capturePrimaryPromotionTrees(policy PublicationPolicy) (ExecutionTrees, error) {
	branch, err := gitTrimmed(s.identity.PrimaryRoot, "symbolic-ref", "HEAD")
	if err != nil || branch != policy.LocalRef {
		return ExecutionTrees{}, fmt.Errorf("primary promotion requires the exact publication branch: %w", err)
	}
	entries, err := suspensionIndexEntries(s.identity.PrimaryRoot)
	if err != nil {
		return ExecutionTrees{}, err
	}
	index, err := cleanupEntryTree(s.identity.PrimaryRoot, entries)
	if err != nil {
		return ExecutionTrees{}, err
	}
	worktree, err := captureParentSourceTree(s.identity.PrimaryRoot)
	return ExecutionTrees{IndexTree: index, WorktreeTree: worktree}, err
}

func (s *Store) primaryPromotionEffects(op ExecutionOperation, c AcceptedCandidate) []EffectExpectation {
	before := op.Publication.SourceTrees
	return []EffectExpectation{
		{Surface: MutationSurfaceRef, Resource: c.Policy.LocalRef, ExpectedOld: op.Publication.LocalOld, ExpectedNew: c.CommitOID},
		{Surface: MutationSurfaceSource, Resource: s.identity.PrimaryRoot, ExpectedOld: before.WorktreeTree, ExpectedNew: c.TreeOID},
		{Surface: MutationSurfaceIndex, Resource: s.identity.PrimaryRoot, ExpectedOld: before.IndexTree, ExpectedNew: c.TreeOID},
	}
}

func (s *Store) validatePromotionSourceOwnership(c AcceptedCandidate, before ExecutionTrees) error {
	metadataBase, err := s.projectMetadataBase(c.ProjectSnapshotID)
	if err != nil {
		return err
	}
	for _, tree := range []string{before.IndexTree, before.WorktreeTree} {
		normalized, err := s.normalizePromotionTree(tree, metadataBase)
		if err != nil || normalized != tree {
			return fmt.Errorf("primary Metadata differs from accepted Project authority: %w", err)
		}
	}
	base, err := s.normalizePromotionTree(c.BaseOID, metadataBase)
	if err != nil {
		return err
	}
	allowed := map[string]bool{base: true, c.TreeOID: true}
	if allowed[before.IndexTree] && allowed[before.WorktreeTree] {
		return nil
	}
	if err := s.allowPromotionCandidateLineage(c, metadataBase, allowed); err != nil {
		return err
	}
	if allowed[before.IndexTree] && allowed[before.WorktreeTree] {
		return nil
	}
	return s.validatePromotionPredecessorTrees(c, before, metadataBase, allowed)
}

func (s *Store) allowPromotionCandidateLineage(c AcceptedCandidate, metadataBase string, allowed map[string]bool) error {
	for c.Previous != nil {
		previous, err := s.LoadAcceptedCandidate(*c.Previous)
		if err != nil {
			return err
		}
		if err := validatePromotionCandidatePredecessor(c, previous); err != nil {
			return err
		}
		for _, tree := range []string{previous.BaseOID, previous.TreeOID} {
			normalized, err := s.normalizePromotionTree(tree, metadataBase)
			if err != nil {
				return err
			}
			allowed[normalized] = true
		}
		c = previous
	}
	return nil
}

func validatePromotionCandidatePredecessor(c, previous AcceptedCandidate) error {
	if previous.ControllerGeneration >= c.ControllerGeneration || previous.AttemptID != c.AttemptID || !previous.TaskRef.Equal(c.TaskRef) || !previous.RootTaskRef.Equal(c.RootTaskRef) || previous.Policy != c.Policy || !evidenceRefsEqual(previous.SealRef, c.SealRef) || previous.State == candidateObserved || previous.ImplementationRef != nil {
		return fmt.Errorf("primary promotion candidate lineage differs")
	}
	return nil
}

func (s *Store) normalizePromotionTree(tree, metadataBase string) (string, error) {
	index, err := newSuspensionIndex(s.identity.PrimaryRoot, tree)
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(index) }()
	return normalizeCandidateTree(s.identity.PrimaryRoot, index, metadataBase)
}

func (s *Store) validatePromotionPredecessorTrees(c AcceptedCandidate, before ExecutionTrees, metadataBase string, allowed map[string]bool) error {
	head, err := s.LoadHead()
	if err != nil {
		return err
	}
	attempt, err := s.loadAttempt(c.AttemptID)
	if err != nil {
		return err
	}
	seen := map[string]bool{attempt.AttemptID: true}
	for attempt.PredecessorAttemptID != "" {
		predecessor, seal, err := s.promotionPredecessorSeal(head, attempt, c, seen)
		if err != nil {
			return err
		}
		for _, tree := range []string{seal.CurrentIndexTree, seal.CurrentWorktreeTree} {
			normalized, err := s.normalizePromotionTree(tree, metadataBase)
			if err != nil {
				return err
			}
			allowed[normalized] = true
		}
		if allowed[before.IndexTree] && allowed[before.WorktreeTree] {
			return nil
		}
		attempt = predecessor
	}
	return fmt.Errorf("primary index/worktree differs from accepted Task lineage")
}

func (s *Store) promotionPredecessorSeal(head RepositoryControllerHead, attempt AttemptRecord, c AcceptedCandidate, seen map[string]bool) (AttemptRecord, AttemptSeal, error) {
	predecessor, err := s.loadAttempt(attempt.PredecessorAttemptID)
	if err != nil {
		return predecessor, AttemptSeal{}, err
	}
	if seen[predecessor.AttemptID] || predecessor.SemanticTaskRef.TaskPath != c.TaskRef.TaskPath || predecessor.RootTaskRef.TaskPath != c.RootTaskRef.TaskPath || !sealedExecutionAttemptState(predecessor.AttemptState) {
		return predecessor, AttemptSeal{}, fmt.Errorf("primary promotion predecessor authority differs")
	}
	seen[predecessor.AttemptID] = true
	ref, seal, err := s.findAttemptSeal(head, predecessor)
	if err != nil {
		return predecessor, seal, err
	}
	if ref.LogicalIdentity != attempt.ResumedFromSealID {
		return predecessor, seal, fmt.Errorf("primary promotion predecessor seal differs")
	}
	return predecessor, seal, nil
}

func (s *Store) validatePrimaryPromotion(op ExecutionOperation, c AcceptedCandidate) error {
	if op.Publication.LocalOld != c.BaseOID && op.Publication.LocalOld != c.CommitOID {
		return fmt.Errorf("primary promotion local ref source differs")
	}
	if !c.TaskRef.Equal(c.RootTaskRef) {
		expected := []EffectExpectation{{Surface: MutationSurfaceRef, Resource: c.Policy.LocalRef, ExpectedOld: op.Publication.LocalOld, ExpectedNew: c.CommitOID}}
		if op.Publication.PrimaryArchive != nil || op.Publication.SourceTrees != nil || !reflect.DeepEqual(op.Transition.Effects, expected) {
			return fmt.Errorf("blocker promotion cannot replace private root source")
		}
		return nil
	}
	if op.Publication.PrimaryArchive == nil || op.Publication.SourceTrees == nil || !reflect.DeepEqual(op.Transition.Effects, s.primaryPromotionEffects(op, c)) {
		return fmt.Errorf("primary promotion lacks exact retained source/index effects")
	}
	if err := s.verifyPrimaryPromotionArchive(op); err != nil {
		return err
	}
	return s.validatePromotionSourceOwnership(c, *op.Publication.SourceTrees)
}

func (s *Store) verifyPrimaryPromotionArchive(op ExecutionOperation) error {
	roots, err := s.VerifyGitObjectArchive(*op.Publication.PrimaryArchive)
	if err != nil {
		return err
	}
	before := *op.Publication.SourceTrees
	expected := map[string]string{op.Publication.LocalOld: gitCommitObjectType, before.IndexTree: gitTreeObjectType, before.WorktreeTree: gitTreeObjectType}
	if len(roots) != len(expected) {
		return fmt.Errorf("primary promotion archive roots differ")
	}
	for _, root := range roots {
		if expected[root.OID] != root.Type {
			return fmt.Errorf("primary promotion archive does not retain exact input")
		}
	}
	return nil
}

func (s *Store) applyPrimaryPromotion(op ExecutionOperation, c AcceptedCandidate) error {
	if !c.TaskRef.Equal(c.RootTaskRef) {
		return nil
	}
	before := *op.Publication.SourceTrees
	current, err := s.capturePrimaryPromotionTrees(c.Policy)
	if err != nil {
		return err
	}
	if current.IndexTree != before.IndexTree && current.IndexTree != c.TreeOID {
		return fmt.Errorf("primary index changed after promotion preparation")
	}
	oldEntries, err := sourceImportTreeEntries(s.identity.PrimaryRoot, before.WorktreeTree)
	if err != nil {
		return err
	}
	newEntries, err := sourceImportTreeEntries(s.identity.PrimaryRoot, c.TreeOID)
	if err != nil {
		return err
	}
	if err := s.validatePrimaryPromotionEntries(current.WorktreeTree, oldEntries, newEntries); err != nil {
		return err
	}
	if err := applySourceTreeEntries(s.identity.PrimaryRoot, oldEntries, newEntries); err != nil {
		return err
	}
	if _, err := runGitBinary(s.identity.PrimaryRoot, nil, "read-tree", c.TreeOID); err != nil {
		return err
	}
	return s.verifyPrimaryPublicationTree(c)
}

func (s *Store) verifyPrimaryPublicationTree(c AcceptedCandidate) error {
	matches, err := s.primaryPublicationTreeMatches(c)
	if err != nil {
		return err
	}
	if !matches {
		return fmt.Errorf("primary source/index differs from exact accepted publication tree")
	}
	return nil
}

func (s *Store) primaryPublicationTreeMatches(c AcceptedCandidate) (bool, error) {
	if !c.TaskRef.Equal(c.RootTaskRef) {
		return true, nil
	}
	actual, err := s.capturePrimaryPromotionTrees(c.Policy)
	if err != nil {
		return false, err
	}
	return actual.IndexTree == c.TreeOID && actual.WorktreeTree == c.TreeOID, nil
}

func (s *Store) validatePrimaryPromotionEntries(current string, before, after []workspaceTreeEntry) error {
	actual, err := sourceImportTreeEntries(s.identity.PrimaryRoot, current)
	if err != nil {
		return err
	}
	oldByPath, newByPath, currentByPath := sourceImportEntryMap(before), sourceImportEntryMap(after), sourceImportEntryMap(actual)
	paths := append(append(append([]workspaceTreeEntry(nil), before...), after...), actual...)
	for _, entry := range paths {
		if err := s.validatePrimaryPromotionEntry(entry.path, oldByPath[entry.path], newByPath[entry.path], currentByPath[entry.path]); err != nil {
			return err
		}
	}
	return nil
}

func sourceImportEntryMap(entries []workspaceTreeEntry) map[string]workspaceTreeEntry {
	result := make(map[string]workspaceTreeEntry, len(entries))
	for _, entry := range entries {
		result[entry.path] = entry
	}
	return result
}

func (s *Store) validatePrimaryPromotionEntry(path string, before, after, current workspaceTreeEntry) error {
	if current != before && current != after {
		return fmt.Errorf("unattributed primary source mutation at %q", path)
	}
	if _, err := suspensionWorktreePath(s.identity.PrimaryRoot, path); err != nil {
		return err
	}
	if before != after && after.path != "" && after.mode != "100644" && after.mode != "100755" {
		return fmt.Errorf("primary promotion requires a regular output file: %q", path)
	}
	return nil
}
